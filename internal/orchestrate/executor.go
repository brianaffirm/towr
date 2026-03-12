package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// TaskState tracks the lifecycle of a single task within the executor.
type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskSpawning  TaskState = "spawning"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
	TaskBlocked   TaskState = "blocked"
)

// Runtime abstracts the operations the executor needs from the host application.
// This avoids importing the cmd/towr appContext directly.
type Runtime interface {
	// SpawnWorkspace creates a new workspace with the given ID, task, and agent type.
	SpawnWorkspace(id, task, agentType string) error
	// DispatchPrompt sends a prompt to an existing workspace's agent session.
	DispatchPrompt(wsID, prompt string) (dispatchID string, err error)
	// DetectState checks the current state of a workspace's agent.
	// Returns (paneState, summary, error). paneState is one of: "idle", "working", "blocked", "empty".
	DetectState(wsID string) (state string, summary string, err error)
	// SendApprove sends Enter to approve a permission dialog.
	SendApprove(wsID string) error
	// GetWorktreePath returns the worktree path for a workspace.
	GetWorktreePath(wsID string) string
	// MergeDeps merges completed dependency branches into the workspace's worktree.
	// This ensures the workspace has the code from its dependencies before dispatch.
	MergeDeps(wsID string, depIDs []string) error
	// AutoCommit commits any uncommitted files in the workspace's worktree.
	AutoCommit(wsID string) error
	// LandPR pushes the workspace branch and creates a PR.
	LandPR(wsID string) error
	// EmitEvent records an event in the store.
	EmitEvent(event store.Event) error
}

// Logger handles output from the executor. Implementations can write to
// stdout (human-friendly) or emit JSON events.
type Logger interface {
	// Log prints a timestamped status message.
	Log(format string, args ...interface{})
}

// StdLogger writes to stdout with timestamps.
type StdLogger struct{}

func (l *StdLogger) Log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), msg)
}

// Executor manages the lifecycle of a plan's task graph.
type Executor struct {
	plan    *Plan
	runtime Runtime
	logger  Logger

	mu          sync.Mutex
	states      map[string]TaskState
	retries     map[string]int
	results     map[string]string // task ID -> completion summary
	dispatchIDs map[string]string // task ID -> dispatch ID
	sawWorking  map[string]bool   // task ID -> whether we've seen it working

	pollInterval time.Duration
	maxRetries   int
	autoApprove  bool
	done         bool
}

// NewExecutor creates an executor for the given plan and runtime.
func NewExecutor(plan *Plan, rt Runtime, logger Logger) *Executor {
	pollInterval := 10 * time.Second
	if plan.Settings.PollInterval != "" {
		if d, err := time.ParseDuration(plan.Settings.PollInterval); err == nil {
			pollInterval = d
		}
	}

	maxRetries := plan.Settings.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 0
	}

	states := make(map[string]TaskState, len(plan.Tasks))
	for _, t := range plan.Tasks {
		states[t.ID] = TaskPending
	}

	return &Executor{
		plan:         plan,
		runtime:      rt,
		logger:       logger,
		states:       states,
		retries:      make(map[string]int),
		results:      make(map[string]string),
		dispatchIDs:  make(map[string]string),
		sawWorking:   make(map[string]bool),
		pollInterval: pollInterval,
		maxRetries:   maxRetries,
		autoApprove:  plan.Settings.AutoApprove,
	}
}

// Run executes the plan's task graph until all tasks complete, fail, or the
// context is cancelled. Returns nil on success. On context cancellation it
// prints a summary and returns the context error.
func (e *Executor) Run(ctx context.Context) error {
	approveStr := "off"
	if e.autoApprove {
		approveStr = "on"
	}
	name := e.plan.Name
	if name == "" {
		name = "unnamed plan"
	}
	e.logger.Log("Orchestrating %q (%d tasks, auto-approve: %s)", name, len(e.plan.Tasks), approveStr)

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	// Run an initial tick immediately.
	if err := e.tick(ctx); err != nil {
		return err
	}
	if e.isDone() {
		return e.finalize()
	}

	for {
		select {
		case <-ctx.Done():
			e.logger.Log("Interrupted — printing summary")
			e.printSummary()
			return ctx.Err()
		case <-ticker.C:
			if err := e.tick(ctx); err != nil {
				return err
			}
			if e.isDone() {
				return e.finalize()
			}
		}
	}
}

// tick performs one cycle: dispatch ready tasks, check running tasks, detect completion.
func (e *Executor) tick(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 1. Find tasks whose deps are all completed and dispatch them.
	for i := range e.plan.Tasks {
		task := &e.plan.Tasks[i]
		if e.states[task.ID] != TaskPending {
			continue
		}
		if e.allDepsCompleted(task) {
			e.spawnAndDispatch(task)
		}
	}

	// 2. Check running tasks for completion/failure/blocked.
	for i := range e.plan.Tasks {
		task := &e.plan.Tasks[i]
		if e.states[task.ID] != TaskRunning {
			continue
		}
		e.checkTask(task)
	}

	// 3. Check if all tasks are in terminal states.
	if e.allDone() {
		e.done = true
	}

	return nil
}

func (e *Executor) isDone() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.done
}

func (e *Executor) allDepsCompleted(task *Task) bool {
	for _, dep := range task.DependsOn {
		if e.states[dep] != TaskCompleted {
			return false
		}
	}
	return true
}

func (e *Executor) allDone() bool {
	for _, state := range e.states {
		if state != TaskCompleted && state != TaskFailed {
			return false
		}
	}
	return true
}

// spawnAndDispatch creates a workspace and dispatches the task prompt.
func (e *Executor) spawnAndDispatch(task *Task) {
	e.states[task.ID] = TaskSpawning

	// Build dep context string.
	depInfo := e.buildDepContext(task)

	// Log what we're dispatching.
	if len(task.DependsOn) > 0 {
		depLabels := make([]string, len(task.DependsOn))
		for i, d := range task.DependsOn {
			depLabels[i] = d + " \u2713"
		}
		e.logger.Log("\u25b6 %s: spawning (deps: %s)", task.ID, strings.Join(depLabels, ", "))
	} else {
		e.logger.Log("\u25b6 %s: spawning (no dependencies)", task.ID)
	}

	// Spawn workspace with agent type from task or plan default.
	agentType := task.Agent
	if agentType == "" {
		agentType = e.plan.Settings.DefaultAgent
	}
	if err := e.runtime.SpawnWorkspace(task.ID, task.Prompt, agentType); err != nil {
		e.logger.Log("\u2717 %s: spawn failed — %v", task.ID, err)
		e.states[task.ID] = TaskFailed
		return
	}

	// Merge dependency branches into the new workspace so it has their code.
	if len(task.DependsOn) > 0 {
		if err := e.runtime.MergeDeps(task.ID, task.DependsOn); err != nil {
			e.logger.Log("\u26a0 %s: merge deps failed — %v (continuing anyway)", task.ID, err)
		} else {
			e.logger.Log("  %s: merged deps %v into workspace", task.ID, task.DependsOn)
		}
	}

	// Build enhanced prompt with dependency context and commit instruction.
	prompt := task.Prompt
	if depInfo != "" {
		prompt += "\n\nContext from completed tasks:\n" + depInfo
	}
	prompt += "\n\nIMPORTANT: When you are done, git add and commit all your changes with a descriptive commit message. Do not leave uncommitted files."

	// Dispatch prompt.
	dispID, err := e.runtime.DispatchPrompt(task.ID, prompt)
	if err != nil {
		e.logger.Log("\u2717 %s: dispatch failed — %v", task.ID, err)
		e.states[task.ID] = TaskFailed
		return
	}

	e.dispatchIDs[task.ID] = dispID
	e.states[task.ID] = TaskRunning
	e.logger.Log("\u25b6 %s: dispatched (%s)", task.ID, dispID)
}

// buildDepContext assembles context from completed dependency results.
func (e *Executor) buildDepContext(task *Task) string {
	if len(task.DependsOn) == 0 {
		return ""
	}
	var parts []string
	for _, dep := range task.DependsOn {
		summary := e.results[dep]
		if summary == "" {
			summary = "(no summary available)"
		}
		parts = append(parts, fmt.Sprintf("- %s: %s", dep, summary))
	}
	return strings.Join(parts, "\n")
}

// checkTask polls the runtime for a running task's state and handles transitions.
func (e *Executor) checkTask(task *Task) {
	state, summary, err := e.runtime.DetectState(task.ID)
	if err != nil {
		// Transient detection error — keep polling.
		return
	}

	switch state {
	case "working":
		e.sawWorking[task.ID] = true

	case "blocked":
		e.sawWorking[task.ID] = true
		if e.autoApprove {
			if err := e.runtime.SendApprove(task.ID); err == nil {
				e.logger.Log("\u2713 %s: auto-approved permission", task.ID)
			}
		} else {
			e.logger.Log("\u26a0 %s: blocked on permission dialog", task.ID)
		}

	case "idle":
		if !e.sawWorking[task.ID] {
			// Haven't seen it working yet — don't treat idle as completed.
			return
		}
		// Task completed — auto-commit any uncommitted work.
		if err := e.runtime.AutoCommit(task.ID); err != nil {
			e.logger.Log("\u26a0 %s: auto-commit failed — %v", task.ID, err)
		}
		// Auto-land as PR if configured.
		if e.plan.Settings.LandPR {
			if err := e.runtime.LandPR(task.ID); err != nil {
				e.logger.Log("\u2717 %s: land --pr failed — %v", task.ID, err)
				e.states[task.ID] = TaskFailed
				return
			}
			e.logger.Log("\u2713 %s: PR created", task.ID)
		}
		e.states[task.ID] = TaskCompleted
		e.results[task.ID] = summary
		dispID := e.dispatchIDs[task.ID]
		if summary != "" {
			e.logger.Log("\u2713 %s %s: completed — %q", task.ID, dispID, truncate(summary, 80))
		} else {
			e.logger.Log("\u2713 %s %s: completed", task.ID, dispID)
		}

	case "empty":
		if e.sawWorking[task.ID] {
			// Claude was working and then exited.
			// Verify there's actual committed work — sawWorking alone isn't enough
			// because it's also set in the "blocked" state (permission dialog).
			commitErr := e.runtime.AutoCommit(task.ID)
			wtPath := e.runtime.GetWorktreePath(task.ID)

			// Check if the workspace has commits beyond the base.
			hasWork := commitErr == nil && wtPath != ""

			if !hasWork {
				// No committed work — treat as failure, not completion.
				e.logger.Log("\u26a0 %s: Claude exited after working but no committed work found", task.ID)
				break // fall through to retry logic below
			}

			// Has committed work — treat as completed.
			if e.plan.Settings.LandPR {
				if err := e.runtime.LandPR(task.ID); err != nil {
					e.logger.Log("\u2717 %s: land --pr failed — %v", task.ID, err)
					e.states[task.ID] = TaskFailed
					return
				}
				e.logger.Log("\u2713 %s: PR created", task.ID)
			}
			e.states[task.ID] = TaskCompleted
			e.results[task.ID] = summary
			dispID := e.dispatchIDs[task.ID]
			e.logger.Log("\u2713 %s %s: completed (Claude exited after working)", task.ID, dispID)
			return
		}
		// Agent exited without having worked — retry.
		if e.retries[task.ID] < e.maxRetries {
			e.retries[task.ID]++
			e.logger.Log("\u26a0 %s: agent exited without working, retrying (%d/%d)", task.ID, e.retries[task.ID], e.maxRetries)
			e.states[task.ID] = TaskPending
		} else {
			e.logger.Log("\u2717 %s: agent exited, no retries left", task.ID)
			e.states[task.ID] = TaskFailed
		}
	}
}

func (e *Executor) finalize() error {
	e.printSummary()

	// Check for failures.
	failed := 0
	for _, state := range e.states {
		if state == TaskFailed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d task(s) failed", failed)
	}
	return nil
}

func (e *Executor) printSummary() {
	succeeded := 0
	failed := 0
	pending := 0
	for _, state := range e.states {
		switch state {
		case TaskCompleted:
			succeeded++
		case TaskFailed:
			failed++
		default:
			pending++
		}
	}

	name := e.plan.Name
	if name == "" {
		name = "plan"
	}

	if failed == 0 && pending == 0 {
		e.logger.Log("Plan %q completed: %d/%d tasks succeeded.", name, succeeded, len(e.plan.Tasks))
	} else {
		e.logger.Log("Plan %q: %d succeeded, %d failed, %d pending.", name, succeeded, failed, pending)
	}
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
