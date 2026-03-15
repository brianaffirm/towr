package control

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/store"
	"github.com/google/uuid"
)

// Logger handles output from the RunService.
type Logger interface {
	Log(format string, args ...interface{})
}

// taskState tracks the lifecycle of a single task within the RunService.
type taskState struct {
	status    string
	decision  RoutingDecision
	retries   int
	startedAt time.Time
	doneCh    chan struct{}
}

// RunService orchestrates plan execution, replacing the monolithic run handler.
type RunService struct {
	Store   ControlStore
	Runtime AgentRuntime
	Router  Router
	Policy  PolicyEngine
	Clock   Clock
	Logger  Logger
}

// DryRun computes routing decisions for all tasks without executing them.
func (s *RunService) DryRun(req RunRequest) []PreRunItem {
	items := make([]PreRunItem, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		d := s.Router.Route(task, req.Settings.DefaultModel, req.Settings.DefaultAgent)
		items = append(items, PreRunItem{
			TaskID:   task.ID,
			Decision: d,
		})
	}
	return items
}

// Start begins orchestrating the plan. It returns a RunHandle immediately and
// runs the orchestration loop in the background until all tasks complete, fail,
// or the context is cancelled.
func (s *RunService) Start(ctx context.Context, req RunRequest) (*RunHandle, error) {
	now := s.Clock()
	runID := uuid.New().String()[:8]

	run := &store.Run{
		ID:          runID,
		RepoRoot:    req.RepoRoot,
		PlanName:    req.PlanName,
		PlanContent: req.PlanContent,
		Status:      RunPending,
		OwnerPID:    os.Getpid(),
		FullAuto:    req.Options.FullAuto,
		Budget:      req.Options.Budget,
		CreatedAt:   now.Format(time.RFC3339),
		UpdatedAt:   now.Format(time.RFC3339),
	}

	if err := s.Store.CreateRun(run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	s.emitRunEvent(runID, req.RepoRoot, store.EventRunCreated, map[string]interface{}{
		"plan_name": req.PlanName,
		"tasks":     len(req.Tasks),
	})

	// Transition to running.
	run.Status = RunRunning
	run.StartedAt = now.Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	if err := s.Store.UpdateRun(run); err != nil {
		return nil, fmt.Errorf("update run to running: %w", err)
	}

	s.emitRunEvent(runID, req.RepoRoot, store.EventRunStarted, nil)

	// Initialize task states.
	states := make(map[string]*taskState, len(req.Tasks))
	taskStates := make(map[string]string, len(req.Tasks))
	for _, task := range req.Tasks {
		states[task.ID] = &taskState{
			status: RunPending,
			doneCh: make(chan struct{}),
		}
		taskStates[task.ID] = RunPending
	}

	ctx, cancel := context.WithCancel(ctx)

	handle := &RunHandle{
		ID:         runID,
		Status:     RunRunning,
		TaskStates: taskStates,
		Cancel:     cancel,
	}

	// Launch the orchestration loop in the background.
	go s.orchestrate(ctx, cancel, runID, req, states, handle)

	return handle, nil
}

// orchestrate is the main polling loop that dispatches and monitors tasks.
func (s *RunService) orchestrate(ctx context.Context, cancel context.CancelFunc, runID string, req RunRequest, states map[string]*taskState, handle *RunHandle) {
	defer cancel()

	maxRetries := req.Settings.MaxRetries

	pollInterval := req.Settings.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}

	var accCost float64

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Close all doneCh channels and mark cancelled.
			for _, st := range states {
				select {
				case <-st.doneCh:
				default:
					close(st.doneCh)
				}
			}

			now := s.Clock()
			run, err := s.Store.GetRun(runID)
			if err == nil {
				run.Status = RunCancelled
				run.FinishedAt = now.Format(time.RFC3339)
				run.UpdatedAt = now.Format(time.RFC3339)
				_ = s.Store.UpdateRun(run)
			}

			handle.Status = RunCancelled
			return

		case <-ticker.C:
			// 1. Dispatch ready tasks.
			for i := range req.Tasks {
				task := &req.Tasks[i]
				st := states[task.ID]
				if st.status != RunPending {
					continue
				}
				if !s.allDepsCompleted(task, states) {
					continue
				}
				// Budget check: mark as failed so the run can terminate.
				if req.Options.Budget > 0 && accCost >= req.Options.Budget {
					s.Logger.Log("budget exhausted (%.2f >= %.2f), skipping %s", accCost, req.Options.Budget, task.ID)
					st.status = RunFailed
					close(st.doneCh)
					handle.SetTaskState(task.ID, st.status)
					s.emitRunEvent(runID, req.RepoRoot, store.EventTaskFailed, map[string]interface{}{
						"task_id": task.ID,
						"reason":  "budget exhausted",
					})
					continue
				}
				s.spawnTask(runID, req, task, st)
				handle.SetTaskState(task.ID, st.status)
			}

			// 2. Check running tasks.
			for i := range req.Tasks {
				task := &req.Tasks[i]
				st := states[task.ID]
				if st.status != RunRunning {
					continue
				}
				s.checkTask(runID, req, task, st, maxRetries, &accCost)
				handle.SetTaskState(task.ID, st.status)
			}

			// 3. Check if all done.
			allDone := true
			anyFailed := false
			for _, st := range states {
				if st.status != RunCompleted && st.status != RunFailed {
					allDone = false
					break
				}
				if st.status == RunFailed {
					anyFailed = true
				}
			}

			if allDone {
				now := s.Clock()
				run, err := s.Store.GetRun(runID)
				if err != nil {
					return
				}
				if anyFailed {
					run.Status = RunFailed
					handle.Status = RunFailed
					s.emitRunEvent(runID, req.RepoRoot, store.EventRunFailed, nil)
				} else {
					run.Status = RunCompleted
					handle.Status = RunCompleted
					s.emitRunEvent(runID, req.RepoRoot, store.EventRunCompleted, nil)
				}
				run.FinishedAt = now.Format(time.RFC3339)
				run.UpdatedAt = now.Format(time.RFC3339)
				_ = s.Store.UpdateRun(run)
				return
			}
		}
	}
}

// allDepsCompleted checks whether all dependencies of a task have completed.
func (s *RunService) allDepsCompleted(task *TaskSpec, states map[string]*taskState) bool {
	for _, dep := range task.DependsOn {
		st, ok := states[dep]
		if !ok || st.status != RunCompleted {
			return false
		}
	}
	return true
}

// spawnTask routes, validates policy, spawns a workspace, and launches monitoring.
func (s *RunService) spawnTask(runID string, req RunRequest, task *TaskSpec, st *taskState) {
	decision := s.Router.Route(*task, req.Settings.DefaultModel, req.Settings.DefaultAgent)
	st.decision = decision

	// Policy check.
	if s.Policy != nil {
		if err := s.Policy.Evaluate(*task); err != nil {
			s.Logger.Log("policy rejected %s: %v", task.ID, err)
			st.status = RunFailed
			return
		}
	}

	s.emitRunEvent(runID, req.RepoRoot, store.EventTaskRouted, map[string]interface{}{
		"task_id": task.ID,
		"model":   decision.Model,
		"reason":  decision.Reason,
		"tier":    decision.Tier,
	})

	agentType := task.Agent
	if agentType == "" {
		agentType = req.Settings.DefaultAgent
	}

	if err := s.Runtime.SpawnWorkspace(task.ID, task.Prompt, agentType, req.RepoRoot, task.DependsOn); err != nil {
		s.Logger.Log("spawn failed for %s: %v", task.ID, err)
		st.status = RunFailed
		return
	}

	// Build enhanced prompt.
	prompt := task.Prompt
	prompt += "\n\nIMPORTANT: When you are done, git add and commit all your changes with a descriptive commit message. Do not leave uncommitted files."

	st.status = RunRunning
	st.startedAt = s.Clock()

	s.Runtime.LaunchAndMonitor(task.ID, prompt, decision, agentType, req.Options.FullAuto, st.doneCh)

	s.emitRunEvent(runID, req.RepoRoot, store.EventTaskDispatched, map[string]interface{}{
		"task_id": task.ID,
		"model":   decision.Model,
		"agent":   agentType,
	})

	s.Logger.Log("dispatched %s (model=%s, agent=%s)", task.ID, decision.Model, agentType)
}

// checkTask inspects a running task and handles state transitions.
func (s *RunService) checkTask(runID string, req RunRequest, task *TaskSpec, st *taskState, maxRetries int, accCost *float64) {
	// Grace period: skip if task started less than 45s ago.
	if s.Clock().Sub(st.startedAt) < 45*time.Second {
		return
	}

	state, summary, err := s.Runtime.DetectState(task.ID)
	if err != nil {
		return
	}

	switch state {
	case "idle":
		// Task completed.
		_ = s.Runtime.AutoCommit(task.ID)
		// Mark workspace as completed with exit code 0.
		if sw, err := s.Store.GetWorkspace(req.RepoRoot, task.ID); err == nil && sw != nil {
			exitZero := 0
			sw.ExitCode = &exitZero
			_ = s.Store.SaveWorkspace(sw)
		}
		if req.Settings.CreatePR {
			if err := s.Runtime.CreatePR(task.ID); err != nil {
				s.Logger.Log("create PR failed for %s: %v", task.ID, err)
			}
		}
		st.status = RunCompleted
		close(st.doneCh)

		s.emitRunEvent(runID, req.RepoRoot, store.EventTaskCompleted, map[string]interface{}{
			"task_id": task.ID,
			"summary": truncate(summary, 200),
		})

		// Compute and emit cost.
		inputTokens, outputTokens, source, actualCost, opusCost := s.Runtime.ComputeCost(task.ID, st.decision.Model)
		*accCost += actualCost
		s.emitRunEvent(runID, req.RepoRoot, store.EventTaskCost, map[string]interface{}{
			"task_id":        task.ID,
			"input_tokens":   inputTokens,
			"output_tokens":  outputTokens,
			"token_source":   source,
			"estimated_cost": actualCost,
			"opus_baseline":  opusCost,
		})

		s.Logger.Log("completed %s — %s", task.ID, truncate(summary, 80))

	case "working":
		// No-op — still running.

	case "blocked":
		// No-op — monitor goroutine handles approvals.

	case "empty":
		// Agent exited — retry or fail.
		st.retries++
		if st.retries <= maxRetries && maxRetries > 0 {
			// Try escalation.
			next, ok := s.Router.Escalate(st.decision)
			if ok {
				s.Logger.Log("escalating %s: %s -> %s (retry %d/%d)", task.ID, st.decision.Model, next.Model, st.retries, maxRetries)
				st.decision = next
			} else {
				s.Logger.Log("retrying %s (%d/%d)", task.ID, st.retries, maxRetries)
			}

			// Re-dispatch with new doneCh.
			st.doneCh = make(chan struct{})
			st.startedAt = s.Clock()

			prompt := task.Prompt
			prompt += "\n\nIMPORTANT: When you are done, git add and commit all your changes with a descriptive commit message. Do not leave uncommitted files."

			agentType := task.Agent
			if agentType == "" {
				agentType = req.Settings.DefaultAgent
			}
			s.Runtime.LaunchAndMonitor(task.ID, prompt, st.decision, agentType, req.Options.FullAuto, st.doneCh)
		} else {
			s.Logger.Log("failed %s: no retries left", task.ID)
			st.status = RunFailed
			close(st.doneCh)

			s.emitRunEvent(runID, req.RepoRoot, store.EventTaskFailed, map[string]interface{}{
				"task_id": task.ID,
				"reason":  "agent exited, retries exhausted",
			})
		}
	}
}

// GetRun retrieves a run record by ID.
func (s *RunService) GetRun(ctx context.Context, runID string) (*store.Run, error) {
	return s.Store.GetRun(runID)
}

// Reconcile checks a single run for liveness — if the owning process is dead,
// marks the run as failed and emits a recovery event.
func (s *RunService) Reconcile(ctx context.Context, runID string) error {
	run, err := s.Store.GetRun(runID)
	if err != nil {
		return fmt.Errorf("get run %s: %w", runID, err)
	}

	if run.Status != RunRunning {
		return nil
	}

	if processAlive(run.OwnerPID) {
		return nil
	}

	now := s.Clock()
	run.Status = RunFailed
	run.FinishedAt = now.Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	if err := s.Store.UpdateRun(run); err != nil {
		return fmt.Errorf("update run %s: %w", runID, err)
	}

	s.emitRunEvent(runID, run.RepoRoot, store.EventRunRecovered, map[string]interface{}{
		"owner_pid": run.OwnerPID,
		"reason":    "owner process dead",
	})

	s.Logger.Log("reconciled run %s: owner pid %d dead, marked failed", runID, run.OwnerPID)
	return nil
}

// ReconcileAll reconciles all running runs for a given repo root.
func (s *RunService) ReconcileAll(ctx context.Context, repoRoot string) error {
	runs, err := s.Store.ListRuns(repoRoot)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	for _, run := range runs {
		if run.Status != RunRunning {
			continue
		}
		if err := s.Reconcile(ctx, run.ID); err != nil {
			s.Logger.Log("reconcile run %s failed: %v", run.ID, err)
		}
	}
	return nil
}

// emitRunEvent is a helper that emits a store event for run lifecycle changes.
func (s *RunService) emitRunEvent(runID, repoRoot, kind string, data map[string]interface{}) {
	_ = s.Store.EmitEvent(store.Event{
		ID:        uuid.New().String()[:8],
		Timestamp: s.Clock(),
		Kind:      kind,
		RunID:     runID,
		RepoRoot:  repoRoot,
		Data:      data,
	})
}

// processAlive checks whether a process with the given PID is alive.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
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
