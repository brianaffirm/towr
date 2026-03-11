package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newWatchCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		intervalFlag   time.Duration
		autoApproveFlag bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Monitor all workspaces and react to state changes",
		Long:  "Continuously poll all active workspaces, detect state transitions (idle, blocked, completed), and react automatically. Replaces the manual towr wait + towr send --approve loop.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			return runWatch(app, intervalFlag, autoApproveFlag, jsonFlag)
		},
	}

	cmd.Flags().DurationVar(&intervalFlag, "interval", 10*time.Second, "poll interval (e.g. 5s, 30s)")
	cmd.Flags().BoolVar(&autoApproveFlag, "auto-approve", false, "automatically approve permission dialogs")

	return cmd
}

// watchState tracks per-workspace monitoring state.
type watchState struct {
	prevState  dispatch.PaneState
	sawWorking bool
	dispatchID string
	idleSince  time.Time // when workspace first entered idle (for stale-idle warning)
	warnedIdle bool      // whether we already warned about prolonged idle
	finalStatus string   // for exit summary: "completed", "working", "blocked", etc.
}

func runWatch(app *appContext, interval time.Duration, autoApprove bool, jsonFlag *bool) error {
	// Set up signal handling for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	states := make(map[string]*watchState)

	// Print initial header.
	now := time.Now()
	workspaces := countActiveWorkspaces(app)
	approveStr := "off"
	if autoApprove {
		approveStr = "on"
	}
	if *jsonFlag {
		emitJSON(map[string]interface{}{
			"time":         formatTime(now),
			"event":        "started",
			"workspaces":   workspaces,
			"interval":     interval.String(),
			"auto_approve": autoApprove,
		})
	} else {
		fmt.Printf("[%s] Watching %d workspaces (poll: %s, auto-approve: %s)\n",
			formatTime(now), workspaces, interval, approveStr)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			printSummary(app, states, jsonFlag)
			return nil
		case <-ticker.C:
			pollWorkspaces(app, states, autoApprove, jsonFlag)
		}
	}
}

func pollWorkspaces(app *appContext, states map[string]*watchState, autoApprove bool, jsonFlag *bool) {
	// Get all workspaces — we care about RUNNING and IDLE with active dispatches.
	allWS, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return
	}

	activeCount := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status != workspace.StatusRunning && status != workspace.StatusIdle {
			continue
		}

		// Find the active dispatch for this workspace.
		latestDisp, err := app.store.LatestDispatch(app.repoRoot, ws.ID)
		if err != nil || latestDisp == nil {
			continue
		}
		dispID, _ := latestDisp.Data["dispatch_id"].(string)
		if dispID == "" {
			continue
		}

		// Check if dispatch is still active (not completed/failed).
		latestEvt, err := app.store.LatestTaskEvent(app.repoRoot, ws.ID, dispID)
		if err != nil {
			continue
		}
		if latestEvt != nil && (latestEvt.Kind == store.EventTaskCompleted || latestEvt.Kind == store.EventTaskFailed) {
			// Already completed — skip unless we haven't recorded it.
			if st, ok := states[ws.ID]; ok && st.finalStatus == "" {
				st.finalStatus = "completed"
			}
			continue
		}

		activeCount++

		// Initialize state tracking if needed.
		if _, ok := states[ws.ID]; !ok {
			states[ws.ID] = &watchState{
				dispatchID: dispID,
			}
		}
		st := states[ws.ID]
		st.dispatchID = dispID

		// Detect current state.
		var currentState dispatch.PaneState
		var jsonlSummary string
		usedJSONL := false

		// Try JSONL-based detection first.
		if ws.WorktreePath != "" {
			jState, jSummary, jErr := dispatch.DetectClaudeActivity(ws.WorktreePath)
			if jErr == nil {
				currentState = jState
				jsonlSummary = jSummary
				usedJSONL = true
			}
		}

		// Always check capture-pane for blocked detection, or as fallback.
		captured, captErr := app.term.CapturePane(ws.ID, 200)
		if captErr == nil {
			capState := dispatch.DetectPaneState(captured)
			if capState == dispatch.PaneBlocked {
				currentState = dispatch.PaneBlocked
			}
			if !usedJSONL {
				currentState = capState
			}
		} else if !usedJSONL {
			// Neither JSONL nor capture-pane available.
			// Check if pane is alive.
			alive, aliveErr := app.term.IsPaneAlive(ws.ID)
			if aliveErr != nil || !alive {
				handleTransition(app, ws, st, dispatch.PaneEmpty, "", captured, autoApprove, jsonFlag)
			}
			continue
		}

		// Track working state.
		if currentState == dispatch.PaneWorking || currentState == dispatch.PaneBlocked {
			st.sawWorking = true
		}

		// Handle transitions.
		if currentState != st.prevState {
			handleTransition(app, ws, st, currentState, jsonlSummary, captured, autoApprove, jsonFlag)
			st.prevState = currentState
		}

		// Check for prolonged idle (>5min).
		if currentState == dispatch.PaneIdle && st.sawWorking {
			if st.idleSince.IsZero() {
				st.idleSince = time.Now()
			} else if time.Since(st.idleSince) > 5*time.Minute && !st.warnedIdle {
				st.warnedIdle = true
				now := time.Now()
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "idle_warning",
						"duration":  time.Since(st.idleSince).String(),
					})
				} else {
					fmt.Printf("[%s] \u23f3 %s: idle for >5min\n", formatTime(now), ws.ID)
				}
			}
		} else {
			st.idleSince = time.Time{}
			st.warnedIdle = false
		}
	}

	// Check if all workspaces are idle.
	if activeCount == 0 && len(states) > 0 {
		// Check if any workspace was ever active.
		anyActive := false
		for _, st := range states {
			if st.sawWorking {
				anyActive = true
				break
			}
		}
		if anyActive {
			now := time.Now()
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":  formatTime(now),
					"event": "all_idle",
				})
			} else {
				fmt.Printf("[%s] All workspaces idle. Watching for new dispatches...\n", formatTime(now))
			}
		}
	}
}

func handleTransition(app *appContext, ws *store.Workspace, st *watchState, newState dispatch.PaneState, jsonlSummary, captured string, autoApprove bool, jsonFlag *bool) {
	now := time.Now()

	switch {
	case st.prevState == dispatch.PaneWorking && newState == dispatch.PaneIdle && st.sawWorking:
		// Task completed.
		summary := jsonlSummary
		if summary == "" && captured != "" {
			summary = truncate(dispatch.ExtractLastResponse(captured), 200)
		}

		// Write result to comms dir.
		commsDir, _ := dispatch.EnsureCommsDir(ws.ID)
		if commsDir != "" {
			response := summary
			if captured != "" {
				response = dispatch.ExtractLastResponse(captured)
			}
			_ = os.WriteFile(commsDir+"/result.txt", []byte(response), 0o644)
		}

		// Emit task.completed event.
		_ = app.store.EmitEvent(store.Event{
			ID:          uuid.New().String(),
			Kind:        store.EventTaskCompleted,
			WorkspaceID: ws.ID,
			RepoRoot:    app.repoRoot,
			Timestamp:   now.UTC(),
			Data: map[string]interface{}{
				"dispatch_id": st.dispatchID,
				"summary":     summary,
				"mode":        "interactive",
				"source":      "watch",
			},
		})

		// Update workspace status to IDLE.
		ws.Status = string(workspace.StatusIdle)
		ws.UpdatedAt = now.UTC().Format(time.RFC3339)
		_ = app.store.SaveWorkspace(ws)

		st.finalStatus = "completed"

		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":        formatTime(now),
				"workspace":   ws.ID,
				"dispatch_id": st.dispatchID,
				"event":       "completed",
				"summary":     summary,
			})
		} else {
			fmt.Printf("[%s] \u2713 %s %s: completed \u2014 %q\n", formatTime(now), ws.ID, st.dispatchID, truncate(summary, 80))
		}

	case newState == dispatch.PaneBlocked:
		// Permission dialog detected.
		dialogCtx := "permission dialog active"
		if captured != "" {
			dialogCtx = dispatch.ExtractDialogContext(captured)
		}

		if autoApprove {
			// Auto-approve: send Enter.
			if err := app.term.SendKeys(ws.ID, "Enter"); err == nil {
				st.finalStatus = "working" // still going after approve
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "blocked",
						"dialog":    dialogCtx,
					})
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "approved",
					})
				} else {
					fmt.Printf("[%s] \u26a0 %s: permission dialog \u2014 %q\n", formatTime(now), ws.ID, dialogCtx)
					fmt.Printf("[%s] \u2713 %s: auto-approved\n", formatTime(now), ws.ID)
				}
			}
		} else {
			st.finalStatus = "blocked"
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"workspace": ws.ID,
					"event":     "blocked",
					"dialog":    dialogCtx,
				})
			} else {
				fmt.Printf("[%s] \u26a0 %s: permission dialog \u2014 %q\n", formatTime(now), ws.ID, dialogCtx)
			}
		}

	case newState == dispatch.PaneEmpty:
		// Claude exited.
		st.finalStatus = "exited"
		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":      formatTime(now),
				"workspace": ws.ID,
				"event":     "exited",
			})
		} else {
			fmt.Printf("[%s] \u26a0 %s: Claude exited\n", formatTime(now), ws.ID)
		}

	case newState == dispatch.PaneWorking:
		// Entered working state.
		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":      formatTime(now),
				"workspace": ws.ID,
				"event":     "transition",
				"from":      string(st.prevState),
				"to":        "working",
			})
		} else {
			fmt.Printf("[%s] \u25b6 %s: working\n", formatTime(now), ws.ID)
		}
		st.finalStatus = "working"
	}
}

func printSummary(app *appContext, states map[string]*watchState, jsonFlag *bool) {
	if len(states) == 0 {
		return
	}

	if *jsonFlag {
		summaries := make([]map[string]interface{}, 0, len(states))
		for wsID, st := range states {
			status := st.finalStatus
			if status == "" {
				status = "unknown"
			}
			summaries = append(summaries, map[string]interface{}{
				"workspace":   wsID,
				"dispatch_id": st.dispatchID,
				"status":      status,
			})
		}
		emitJSON(map[string]interface{}{
			"event":      "stopped",
			"time":       formatTime(time.Now()),
			"workspaces": summaries,
		})
		return
	}

	fmt.Println("\nStopped watching. Summary:")
	for wsID, st := range states {
		icon := "\u25b6" // default: still working
		status := "still working"
		switch st.finalStatus {
		case "completed":
			icon = "\u2713"
			status = "completed"
		case "blocked":
			icon = "\u26a0"
			status = "blocked"
		case "exited":
			icon = "\u26a0"
			status = "exited"
		}
		dispID := st.dispatchID
		if dispID == "" {
			dispID = "---"
		}
		fmt.Printf("  %s: %s %s %s\n", wsID, dispID, icon, status)
	}
}

func countActiveWorkspaces(app *appContext) int {
	allWS, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return 0
	}
	count := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status == workspace.StatusRunning || status == workspace.StatusIdle {
			count++
		}
	}
	return count
}

func formatTime(t time.Time) string {
	return t.Format("15:04:05")
}

func emitJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Println(string(data))
}
