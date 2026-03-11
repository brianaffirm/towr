package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newOrchestrateCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrate <plan.yaml>",
		Short: "Execute a declarative task plan across workspaces",
		Long:  "Read a YAML task graph, spawn workspaces, dispatch tasks respecting dependency order, monitor with auto-approve, retry failures, and pass context from completed tasks to dependents.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planPath := args[0]

			// Load and validate the plan.
			plan, err := orchestrate.LoadPlan(planPath)
			if err != nil {
				return err
			}
			if err := plan.Validate(); err != nil {
				return fmt.Errorf("invalid plan: %w", err)
			}

			app, err := initApp()
			if err != nil {
				return err
			}

			// Build the runtime adapter.
			rt := &appRuntime{app: app}
			logger := &orchestrate.StdLogger{}

			exec := orchestrate.NewExecutor(plan, rt, logger)

			// Set up context with signal handling.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return exec.Run(ctx)
		},
	}

	return cmd
}

// appRuntime adapts appContext to the orchestrate.Runtime interface.
type appRuntime struct {
	app *appContext
}

func (r *appRuntime) SpawnWorkspace(id, task string) error {
	source := workspace.SpawnSource{Kind: workspace.SpawnFromTask, Value: task}

	// Determine base branch.
	baseBranch := r.app.cfg.Defaults.BaseBranch
	if baseBranch == "" {
		detected, err := workspace.DetectDefaultBranch(r.app.repoRoot)
		if err != nil {
			return fmt.Errorf("detecting default branch: %w", err)
		}
		baseBranch = detected
	}

	opts := workspace.CreateOpts{
		ID:         id,
		RepoRoot:   r.app.repoRoot,
		BaseBranch: baseBranch,
		Source:     source,
		CopyPaths:  r.app.cfg.Workspace.CopyPaths,
		LinkPaths:  r.app.cfg.Workspace.LinkPaths,
	}

	ws, err := r.app.manager.Create(opts)
	if err != nil {
		return err
	}

	// Create tmux pane.
	if !r.app.term.IsHeadless() {
		if err := r.app.term.CreatePane(ws.ID, ws.WorktreePath, ""); err != nil {
			// Non-fatal: log and continue.
			fmt.Fprintf(os.Stderr, "Warning: could not create tmux pane for %s: %v\n", id, err)
		}
	}

	return nil
}

func (r *appRuntime) DispatchPrompt(wsID, prompt string) (string, error) {
	// Validate workspace is ready.
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, wsID)
	if err != nil {
		return "", fmt.Errorf("get workspace: %w", err)
	}
	if sw == nil {
		return "", fmt.Errorf("workspace %q not found", wsID)
	}

	// Check tmux session alive.
	alive, err := r.app.term.IsPaneAlive(wsID)
	if err != nil {
		return "", fmt.Errorf("check tmux session: %w", err)
	}
	if !alive {
		return "", fmt.Errorf("tmux session for workspace %q is not running", wsID)
	}

	// Generate dispatch ID.
	events, err := r.app.store.QueryEvents(store.EventQuery{
		WorkspaceID: wsID,
		RepoRoot:    r.app.repoRoot,
		Kind:        store.EventTaskDispatched,
	})
	if err != nil {
		return "", fmt.Errorf("query dispatch events: %w", err)
	}
	dispatchID := fmt.Sprintf("d-%04d", len(events)+1)

	// Write prompt to comms dir.
	commsDir, err := dispatch.EnsureCommsDir(wsID)
	if err != nil {
		return "", fmt.Errorf("ensure comms dir: %w", err)
	}
	if err := dispatch.WritePrompt(commsDir, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}

	// Emit task.dispatched event.
	if err := r.app.store.EmitEvent(store.Event{
		ID:          uuid.New().String(),
		Kind:        store.EventTaskDispatched,
		WorkspaceID: wsID,
		RepoRoot:    r.app.repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"prompt":      truncate(prompt, 200),
			"mode":        "interactive",
			"source":      "orchestrate",
		},
	}); err != nil {
		return "", fmt.Errorf("emit dispatch event: %w", err)
	}

	// Update workspace status to RUNNING.
	sw.Status = string(workspace.StatusRunning)
	sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := r.app.store.SaveWorkspace(sw); err != nil {
		return "", fmt.Errorf("update workspace status: %w", err)
	}

	// Check if Claude is already running (idle prompt visible).
	captured, err := r.app.term.CapturePane(wsID, 50)
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}

	paneState := dispatch.DetectPaneState(captured)
	if paneState != dispatch.PaneIdle {
		// Claude not running — launch it.
		unlock, err := dispatch.AcquireLaunchLock()
		if err != nil {
			return "", fmt.Errorf("acquire launch lock: %w", err)
		}

		if err := r.app.term.PasteBuffer(wsID, "unset CLAUDECODE && claude"); err != nil {
			unlock()
			return "", fmt.Errorf("launch claude: %w", err)
		}

		// Wait for Claude to start.
		started := false
		for i := 0; i < 40; i++ {
			time.Sleep(1500 * time.Millisecond)
			captured, err = r.app.term.CapturePane(wsID, 50)
			if err != nil {
				continue
			}
			if strings.Contains(captured, "Enter to confirm") {
				_ = r.app.term.SendKeys(wsID, "Enter")
				time.Sleep(1 * time.Second)
				continue
			}
			if dispatch.DetectPaneState(captured) == dispatch.PaneIdle {
				started = true
				break
			}
		}
		unlock()
		if !started {
			return "", fmt.Errorf("timed out waiting for Claude to start in workspace %q", wsID)
		}
	}

	// Send the prompt.
	if err := r.app.term.PasteBuffer(wsID, prompt); err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}

	// Emit task.started event.
	_ = r.app.store.EmitEvent(store.Event{
		ID:          uuid.New().String(),
		Kind:        store.EventTaskStarted,
		WorkspaceID: wsID,
		RepoRoot:    r.app.repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"mode":        "interactive",
			"source":      "orchestrate",
		},
	})

	return dispatchID, nil
}

func (r *appRuntime) DetectState(wsID string) (string, string, error) {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, wsID)
	if err != nil || sw == nil {
		return "empty", "", fmt.Errorf("workspace not found")
	}

	var state dispatch.PaneState
	var summary string
	usedJSONL := false

	// Try JSONL-based detection first.
	if sw.WorktreePath != "" {
		jState, jSummary, jErr := dispatch.DetectClaudeActivity(sw.WorktreePath)
		if jErr == nil {
			state = jState
			summary = jSummary
			usedJSONL = true
		}
	}

	// Check capture-pane for blocked detection or as fallback.
	captured, captErr := r.app.term.CapturePane(wsID, 200)
	if captErr == nil {
		capState := dispatch.DetectPaneState(captured)
		if capState == dispatch.PaneBlocked {
			state = dispatch.PaneBlocked
		}
		if !usedJSONL {
			state = capState
		}
	} else if !usedJSONL {
		alive, aliveErr := r.app.term.IsPaneAlive(wsID)
		if aliveErr != nil || !alive {
			return "empty", "", nil
		}
		return "", "", fmt.Errorf("cannot detect state for %s", wsID)
	}

	return string(state), summary, nil
}

func (r *appRuntime) SendApprove(wsID string) error {
	return r.app.term.SendKeys(wsID, "Enter")
}

func (r *appRuntime) GetWorktreePath(wsID string) string {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, wsID)
	if err != nil || sw == nil {
		return ""
	}
	return sw.WorktreePath
}

func (r *appRuntime) MergeDeps(wsID string, depIDs []string) error {
	wtPath := r.GetWorktreePath(wsID)
	if wtPath == "" {
		return fmt.Errorf("no worktree path for %s", wsID)
	}

	for _, dep := range depIDs {
		// Each dependency has a branch named towr/<dep>
		branch := "towr/" + dep
		mergeCmd := exec.Command("git", "-C", wtPath, "merge", branch, "--no-edit", "-m",
			fmt.Sprintf("merge dependency %s into %s", dep, wsID))
		out, err := mergeCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("merge %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func (r *appRuntime) LandPR(wsID string) error {
	// Push the branch via towr land --pr (keeps workspace alive).
	// Pass --repo to ensure correct repo context regardless of cwd.
	cmd := exec.Command("towr", "land", wsID, "--pr", "--force", "--repo", r.app.repoRoot)
	cmd.Dir = r.app.repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("towr land --pr: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Create the actual PR via gh CLI.
	branch := "towr/" + wsID
	sw, _ := r.app.store.GetWorkspace(r.app.repoRoot, wsID)
	baseBranch := "main"
	if sw != nil && sw.BaseBranch != "" {
		baseBranch = sw.BaseBranch
	}
	ghCmd := exec.Command("gh", "pr", "create",
		"--base", baseBranch,
		"--head", branch,
		"--title", fmt.Sprintf("feat(%s): from towr orchestrate", wsID),
		"--body", fmt.Sprintf("Auto-generated by `towr orchestrate`.\n\nWorkspace: %s", wsID),
	)
	ghOut, ghErr := ghCmd.CombinedOutput()
	if ghErr != nil {
		// gh may not be installed or PR may already exist — non-fatal
		return fmt.Errorf("gh pr create: %s (push succeeded, PR creation failed)", strings.TrimSpace(string(ghOut)))
	}
	return nil
}

func (r *appRuntime) AutoCommit(wsID string) error {
	wtPath := r.GetWorktreePath(wsID)
	if wtPath == "" {
		return fmt.Errorf("no worktree path for %s", wsID)
	}
	// Check if there are uncommitted changes.
	cmd := exec.Command("git", "-C", wtPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil // nothing to commit
	}
	// Stage and commit everything.
	if err := exec.Command("git", "-C", wtPath, "add", "-A").Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	commitCmd := exec.Command("git", "-C", wtPath, "commit", "-m", fmt.Sprintf("feat(%s): auto-commit from towr orchestrate", wsID))
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", string(out), err)
	}
	return nil
}

func (r *appRuntime) EmitEvent(event store.Event) error {
	return r.app.store.EmitEvent(event)
}
