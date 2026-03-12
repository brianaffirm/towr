package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/agent"
	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newDispatchCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		waitFlag     bool
		headlessFlag bool
	)

	cmd := &cobra.Command{
		Use:               "dispatch <workspace-id> <prompt>",
		Aliases:           []string{"d"},
		Short:             "Dispatch a task to a workspace",
		Long:              "Send a prompt to a workspace's agent session. Uses interactive mode by default (sends to running Claude REPL). Use --headless for the old wrapper-based approach.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]
			prompt := args[1]

			app, err := initApp()
			if err != nil {
				return err
			}

			// 1. Validate workspace exists and is READY or IDLE.
			sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
			if err != nil {
				return fmt.Errorf("get workspace: %w", err)
			}
			if sw == nil {
				return fmt.Errorf("workspace %q not found", wsID)
			}
			status := workspace.WorkspaceStatus(sw.Status)
			if status != workspace.StatusReady && status != workspace.StatusIdle {
				return fmt.Errorf("workspace %q is %s (must be READY or IDLE)", wsID, sw.Status)
			}

			// 2. Check for active dispatch.
			latestDisp, err := app.store.LatestDispatch(app.repoRoot, wsID)
			if err != nil {
				return fmt.Errorf("check latest dispatch: %w", err)
			}
			if latestDisp != nil {
				// If the dispatch event is older than the workspace creation, ignore it.
				// This handles the case where a workspace was cleaned up and re-created
				// with the same name — old events are stale.
				stale := false
				if sw.CreatedAt != "" {
					wsCreatedAt, parseErr := time.Parse(time.RFC3339, sw.CreatedAt)
					if parseErr == nil && latestDisp.Timestamp.Before(wsCreatedAt) {
						stale = true
					}
				}
				if !stale {
					dispID, _ := latestDisp.Data["dispatch_id"].(string)
					if dispID != "" {
						latestEvt, err := app.store.LatestTaskEvent(app.repoRoot, wsID, dispID)
						if err != nil {
							return fmt.Errorf("check latest task event: %w", err)
						}
						if latestEvt != nil && latestEvt.Kind != store.EventTaskCompleted && latestEvt.Kind != store.EventTaskFailed {
							return fmt.Errorf("workspace %q has active dispatch %s (status: %s)", wsID, dispID, latestEvt.Kind)
						}
					}
				}
			}

			// 3. Check tmux session alive.
			alive, err := app.term.IsPaneAlive(wsID)
			if err != nil {
				return fmt.Errorf("check tmux session: %w", err)
			}
			if !alive {
				return fmt.Errorf("tmux session for workspace %q is not running", wsID)
			}

			// 4. Generate dispatch ID.
			events, err := app.store.QueryEvents(store.EventQuery{
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Kind:        store.EventTaskDispatched,
			})
			if err != nil {
				return fmt.Errorf("query dispatch events: %w", err)
			}
			dispatchID := fmt.Sprintf("d-%04d", len(events)+1)

			// 5. Set up comms directory and write prompt.
			commsDir, err := dispatch.EnsureCommsDir(wsID)
			if err != nil {
				return fmt.Errorf("ensure comms dir: %w", err)
			}
			if err := dispatch.WritePrompt(commsDir, prompt); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}

			// Branch: headless vs interactive mode.
			mode := "interactive"
			if headlessFlag {
				mode = "headless"
			}

			if headlessFlag {
				return runHeadlessDispatch(app, sw, wsID, dispatchID, prompt, commsDir, waitFlag, jsonFlag)
			}
			return runInteractiveDispatch(app, sw, wsID, dispatchID, prompt, commsDir, mode, waitFlag, jsonFlag)
		},
	}

	cmd.Flags().BoolVar(&waitFlag, "wait", false, "wait for the dispatch to complete")
	cmd.Flags().BoolVar(&headlessFlag, "headless", false, "use headless wrapper mode (claude -p) instead of interactive REPL")

	return cmd
}

// runHeadlessDispatch is the original wrapper-based dispatch flow.
func runHeadlessDispatch(app *appContext, sw *store.Workspace, wsID, dispatchID, prompt, commsDir string, waitFlag bool, jsonFlag *bool) error {
	// Write wrapper script.
	wrapper := dispatch.BuildWrapper(wsID, dispatchID, commsDir)
	runShPath := commsDir + "/run.sh"
	if err := os.WriteFile(runShPath, []byte(wrapper), 0o755); err != nil {
		return fmt.Errorf("write run.sh: %w", err)
	}

	// Emit task.dispatched event.
	if err := app.store.EmitEvent(store.Event{
		ID:          uuid.New().String(),
		Kind:        store.EventTaskDispatched,
		WorkspaceID: wsID,
		RepoRoot:    app.repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"prompt":      truncate(prompt, 200),
			"mode":        "headless",
		},
	}); err != nil {
		return fmt.Errorf("emit dispatch event: %w", err)
	}

	// Update workspace status to RUNNING.
	sw.Status = string(workspace.StatusRunning)
	sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := app.store.SaveWorkspace(sw); err != nil {
		return fmt.Errorf("update workspace status: %w", err)
	}

	// Deliver via PasteBuffer.
	runCmd := dispatch.BuildRunCommand(commsDir)
	if err := app.term.PasteBuffer(wsID, runCmd); err != nil {
		return fmt.Errorf("paste buffer: %w", err)
	}

	// Output dispatch ID.
	if *jsonFlag {
		return cli.PrintJSON(map[string]interface{}{
			"workspace_id": wsID,
			"dispatch_id":  dispatchID,
			"status":       "dispatched",
			"mode":         "headless",
		})
	}
	fmt.Printf("Dispatched %s to %s (headless)\n", dispatchID, wsID)

	if waitFlag {
		return runWait(app, app.repoRoot, wsID, dispatchID, 0, jsonFlag)
	}
	return nil
}

// runInteractiveDispatch sends the prompt directly to an interactive Claude REPL.
func runInteractiveDispatch(app *appContext, sw *store.Workspace, wsID, dispatchID, prompt, commsDir, mode string, waitFlag bool, jsonFlag *bool) error {
	// Emit task.dispatched event.
	if err := app.store.EmitEvent(store.Event{
		ID:          uuid.New().String(),
		Kind:        store.EventTaskDispatched,
		WorkspaceID: wsID,
		RepoRoot:    app.repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"prompt":      truncate(prompt, 200),
			"mode":        mode,
		},
	}); err != nil {
		return fmt.Errorf("emit dispatch event: %w", err)
	}

	// Update workspace status to RUNNING.
	sw.Status = string(workspace.StatusRunning)
	sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := app.store.SaveWorkspace(sw); err != nil {
		return fmt.Errorf("update workspace status: %w", err)
	}

	// Check if Claude is already running in the pane by looking for ❯.
	captured, err := app.term.CapturePane(wsID, 50)
	if err != nil {
		return fmt.Errorf("capture pane: %w", err)
	}

	// Select agent based on workspace metadata.
	ag := agent.Get(sw.AgentRuntime)

	paneState := dispatch.DetectPaneState(captured)
	if paneState != dispatch.PaneIdle {
		// Acquire launch lock to prevent concurrent agent startups.
		unlock, err := dispatch.AcquireLaunchLock()
		if err != nil {
			return fmt.Errorf("acquire launch lock: %w", err)
		}

		// Agent not running or not idle — launch it.
		if err := app.term.PasteBuffer(wsID, ag.LaunchCommand()); err != nil {
			unlock()
			return fmt.Errorf("launch %s: %w", ag.Name(), err)
		}

		// Wait for agent to start. Handle trust/permission dialogs.
		startupDialogs := ag.StartupDialogs()
		started := false
		for i := 0; i < 40; i++ { // up to ~60 seconds
			time.Sleep(1500 * time.Millisecond)
			captured, err = app.term.CapturePane(wsID, 50)
			if err != nil {
				continue
			}

			// Handle any dialog during startup by pressing Enter.
			dismissed := false
			for _, pattern := range startupDialogs {
				if strings.Contains(captured, pattern) {
					_ = app.term.SendKeys(wsID, "Enter")
					time.Sleep(1 * time.Second)
					dismissed = true
					break
				}
			}
			if dismissed {
				continue
			}

			if dispatch.DetectPaneState(captured) == dispatch.PaneIdle {
				started = true
				break
			}
		}
		unlock()
		if !started {
			return fmt.Errorf("timed out waiting for %s to start in workspace %q", ag.Name(), wsID)
		}
	}

	// Send the prompt via PasteBuffer (plain text, not a wrapper script).
	if err := app.term.PasteBuffer(wsID, prompt); err != nil {
		return fmt.Errorf("send prompt: %w", err)
	}

	// Emit task.started event.
	if err := app.store.EmitEvent(store.Event{
		ID:          uuid.New().String(),
		Kind:        store.EventTaskStarted,
		WorkspaceID: wsID,
		RepoRoot:    app.repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"mode":        mode,
		},
	}); err != nil {
		return fmt.Errorf("emit started event: %w", err)
	}

	// Output dispatch ID.
	if *jsonFlag {
		return cli.PrintJSON(map[string]interface{}{
			"workspace_id": wsID,
			"dispatch_id":  dispatchID,
			"status":       "dispatched",
			"mode":         mode,
		})
	}
	fmt.Printf("Dispatched %s to %s (interactive)\n", dispatchID, wsID)

	if waitFlag {
		return runInteractiveWait(app, wsID, dispatchID, 0, jsonFlag)
	}
	return nil
}

// runInteractiveWait polls for the agent to finish. Uses agent-specific detection
// (e.g., JSONL for Claude Code) with capture-pane fallback for permission dialog detection.
func runInteractiveWait(app *appContext, wsID, dispatchID string, timeout time.Duration, jsonFlag *bool) error {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// We must see the agent enter a working/blocked state before accepting idle as "done".
	// Otherwise we'll detect the prompt echo as idle immediately.
	sawWorking := false

	// Resolve the worktree path and agent for detection.
	worktreePath := ""
	var ag agent.Agent
	if sw, err := app.store.GetWorkspace(app.repoRoot, wsID); err == nil && sw != nil {
		worktreePath = sw.WorktreePath
		ag = agent.Get(sw.AgentRuntime)
	} else {
		ag = agent.Default()
	}

	// Give the agent a moment to start processing before first poll.
	time.Sleep(3 * time.Second)

	for {
		var state dispatch.PaneState
		var agentSummary string
		usedAgentDetect := false

		// Try agent-specific detection first (more reliable for idle/working).
		if worktreePath != "" {
			jState, jSummary, err := ag.DetectActivity(worktreePath)
			if err == nil && dispatch.PaneState(jState) != dispatch.PaneEmpty {
				// Agent detection gave a definitive answer.
				state = dispatch.PaneState(jState)
				agentSummary = jSummary
				usedAgentDetect = true
			}
			if err == nil && dispatch.PaneState(jState) == dispatch.PaneEmpty {
				agentSummary = jSummary // keep summary even if inconclusive
			}
		}

		// Always check capture-pane for permission dialog detection,
		// or as primary detection if JSONL is unavailable.
		// Always check capture-pane for permission dialog detection,
		// or as primary detection if agent detection is unavailable.
		captured, captureErr := app.term.CapturePane(wsID, 200)
		if captureErr != nil {
			// Check if pane is still alive.
			alive, aliveErr := app.term.IsPaneAlive(wsID)
			if aliveErr != nil || !alive {
				return fmt.Errorf("tmux session for %q died during dispatch", wsID)
			}
			if !usedAgentDetect {
				// No agent detection and no capture — transient error, keep polling.
				<-ticker.C
				continue
			}
		}

		// If capture-pane succeeded, check for blocked/idle using agent-specific patterns.
		if captureErr == nil {
			lastActivity := app.term.PaneLastActivity(wsID)
			capState := dispatch.DetectPaneStateWithActivity(captured, lastActivity, 15*time.Second)
			if capState == dispatch.PaneBlocked {
				state = dispatch.PaneBlocked
			}
			// If agent detection wasn't available, fall back to capture-pane state entirely.
			if !usedAgentDetect {
				state = capState
			}
		}

		if state == dispatch.PaneWorking || state == dispatch.PaneBlocked {
			sawWorking = true
		}
		if state == dispatch.PaneIdle && sawWorking {
			// Agent finished — extract summary from agent detection or capture-pane.
			summary := agentSummary
			if summary == "" && captureErr == nil {
				summary = truncate(dispatch.ExtractLastResponse(captured), 200)
			}

			// Write response to comms dir.
			commsDir, _ := dispatch.EnsureCommsDir(wsID)
			if commsDir != "" {
				response := summary
				if captureErr == nil {
					response = dispatch.ExtractLastResponse(captured)
				}
				_ = os.WriteFile(commsDir+"/result.txt", []byte(response), 0o644)
			}

			// Emit task.completed event.
			_ = app.store.EmitEvent(store.Event{
				ID:          uuid.New().String(),
				Kind:        store.EventTaskCompleted,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Timestamp:   time.Now().UTC(),
				Data: map[string]interface{}{
					"dispatch_id": dispatchID,
					"summary":     summary,
					"mode":        "interactive",
				},
			})

			// Update workspace status to IDLE.
			sw, _ := app.store.GetWorkspace(app.repoRoot, wsID)
			if sw != nil {
				sw.Status = string(workspace.StatusIdle)
				sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				_ = app.store.SaveWorkspace(sw)
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchID,
					"status":       store.EventTaskCompleted,
					"summary":      summary,
				})
			}
			fmt.Printf("completed %s %s: %s\n", wsID, dispatchID, summary)
			return nil
		}

		// Check if agent is blocked on a permission dialog.
		if state == dispatch.PaneBlocked && sawWorking {
			dialogCtx := "permission dialog active"
			if captureErr == nil {
				dialogCtx = dispatch.ExtractDialogContext(captured)
			}
			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchID,
					"status":       "blocked",
					"dialog":       dialogCtx,
					"hint":         "towr send " + wsID + " --approve",
				})
			}
			fmt.Fprintf(os.Stderr, "blocked %s %s: %s\n  Run: towr send %s --approve\n", wsID, dispatchID, dialogCtx, wsID)
			return nil
		}

		// Check timeout.
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "Timeout waiting for %s %s\n", wsID, dispatchID)
			os.Exit(1)
		}

		<-ticker.C
	}
}
