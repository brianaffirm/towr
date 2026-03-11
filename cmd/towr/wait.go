package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newWaitCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		timeoutFlag time.Duration
		anyFlag     bool
		allFlag     bool
	)

	cmd := &cobra.Command{
		Use:               "wait [workspace-id]",
		Short:             "Wait for dispatch completion",
		Long:              "Poll for task completion on one or more workspaces. Use --any or --all for multi-workspace mode.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			if len(args) == 1 && !anyFlag && !allFlag {
				// Single workspace mode.
				wsID := args[0]
				latestDisp, err := app.store.LatestDispatch(app.repoRoot, wsID)
				if err != nil {
					return fmt.Errorf("get latest dispatch: %w", err)
				}
				if latestDisp == nil {
					return fmt.Errorf("no dispatch found for workspace %q", wsID)
				}
				dispID, _ := latestDisp.Data["dispatch_id"].(string)
				if dispID == "" {
					return fmt.Errorf("dispatch event missing dispatch_id")
				}

				// Check dispatch mode to decide polling strategy.
				mode, _ := latestDisp.Data["mode"].(string)
				if mode == "interactive" {
					return runInteractiveWait(app, wsID, dispID, timeoutFlag, jsonFlag)
				}
				return runWait(app, app.repoRoot, wsID, dispID, timeoutFlag, jsonFlag)
			}

			if !anyFlag && !allFlag {
				return fmt.Errorf("specify a workspace-id, or use --any or --all")
			}

			// Multi-workspace mode: find all workspaces with active dispatches.
			workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
			if err != nil {
				return fmt.Errorf("list workspaces: %w", err)
			}

			type activeDispatch struct {
				wsID       string
				dispatchID string
				mode       string // "interactive" or "headless"
			}
			var active []activeDispatch

			for _, ws := range workspaces {
				if workspace.WorkspaceStatus(ws.Status) != workspace.StatusRunning {
					continue
				}
				latestDisp, err := app.store.LatestDispatch(app.repoRoot, ws.ID)
				if err != nil || latestDisp == nil {
					continue
				}
				dispID, _ := latestDisp.Data["dispatch_id"].(string)
				if dispID == "" {
					continue
				}
				mode, _ := latestDisp.Data["mode"].(string)
				// Check if dispatch is still active.
				latestEvt, err := app.store.LatestTaskEvent(app.repoRoot, ws.ID, dispID)
				if err != nil {
					continue
				}
				if latestEvt != nil && (latestEvt.Kind == store.EventTaskCompleted || latestEvt.Kind == store.EventTaskFailed) {
					continue
				}
				active = append(active, activeDispatch{wsID: ws.ID, dispatchID: dispID, mode: mode})
			}

			if len(active) == 0 {
				if *jsonFlag {
					return cli.PrintJSON(map[string]interface{}{"status": "no_active_dispatches"})
				}
				fmt.Println("No active dispatches found.")
				return nil
			}

			// Poll loop for multi-workspace.
			deadline := time.Time{}
			if timeoutFlag > 0 {
				deadline = time.Now().Add(timeoutFlag)
			}

			completed := make(map[string]map[string]interface{})
			// Track whether we've seen interactive dispatches enter a working state.
			sawWorking := make(map[string]bool)
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()

			// Give dispatches a moment to start processing.
			time.Sleep(3 * time.Second)

			for {
				for _, ad := range active {
					if _, done := completed[ad.wsID]; done {
						continue
					}

					// For interactive dispatches, poll CapturePane.
					if ad.mode == "interactive" {
						captured, captErr := app.term.CapturePane(ad.wsID, 200)
						if captErr != nil {
							continue
						}
						state := dispatch.DetectPaneState(captured)
						if state == dispatch.PaneWorking || state == dispatch.PaneBlocked {
							sawWorking[ad.wsID] = true
						}
						if state == dispatch.PaneIdle && sawWorking[ad.wsID] {
							response := dispatch.ExtractLastResponse(captured)
							commsDir, _ := dispatch.EnsureCommsDir(ad.wsID)
							if commsDir != "" {
								_ = os.WriteFile(commsDir+"/result.txt", []byte(response), 0o644)
							}
							summary := truncate(response, 200)
							_ = app.store.EmitEvent(store.Event{
								ID:          fmt.Sprintf("wait-%s-%d", ad.wsID, time.Now().UnixNano()),
								Kind:        store.EventTaskCompleted,
								WorkspaceID: ad.wsID,
								RepoRoot:    app.repoRoot,
								Timestamp:   time.Now().UTC(),
								Data: map[string]interface{}{
									"dispatch_id": ad.dispatchID,
									"summary":     summary,
									"mode":        "interactive",
								},
							})
							sw, _ := app.store.GetWorkspace(app.repoRoot, ad.wsID)
							if sw != nil {
								sw.Status = string(workspace.StatusIdle)
								sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
								_ = app.store.SaveWorkspace(sw)
							}
							result := map[string]interface{}{
								"workspace_id": ad.wsID,
								"dispatch_id":  ad.dispatchID,
								"status":       store.EventTaskCompleted,
								"summary":      summary,
							}
							completed[ad.wsID] = result
							if !*jsonFlag {
								fmt.Fprintf(os.Stderr, "✓ %s %s: %s\n", ad.wsID, ad.dispatchID, summary)
							}
							if anyFlag {
								if *jsonFlag {
									return cli.PrintJSON(result)
								}
								return nil
							}
						}
						if state == dispatch.PaneBlocked && sawWorking[ad.wsID] {
							dialogCtx := dispatch.ExtractDialogContext(captured)
							result := map[string]interface{}{
								"workspace_id": ad.wsID,
								"dispatch_id":  ad.dispatchID,
								"status":       "blocked",
								"dialog":       dialogCtx,
								"hint":         "towr send " + ad.wsID + " --approve",
							}
							completed[ad.wsID] = result
							if !*jsonFlag {
								fmt.Fprintf(os.Stderr, "⚠ %s %s: %s\n  Run: towr send %s --approve\n", ad.wsID, ad.dispatchID, dialogCtx, ad.wsID)
							}
							if anyFlag {
								if *jsonFlag {
									return cli.PrintJSON(result)
								}
								return nil
							}
						}
						continue
					}

					// Headless mode: poll events.
					evt, err := app.store.LatestTaskEvent(app.repoRoot, ad.wsID, ad.dispatchID)
					if err != nil || evt == nil {
						checkHeartbeat(ad.wsID)
						continue
					}
					if evt.Kind == store.EventTaskCompleted || evt.Kind == store.EventTaskFailed || evt.Kind == store.EventTaskBlocked {
						summary, _ := evt.Data["summary"].(string)
						result := map[string]interface{}{
							"workspace_id": ad.wsID,
							"dispatch_id":  ad.dispatchID,
							"status":       evt.Kind,
							"summary":      summary,
						}
						completed[ad.wsID] = result

						if !*jsonFlag {
							icon := "✓"
							if evt.Kind == store.EventTaskFailed {
								icon = "✗"
							} else if evt.Kind == store.EventTaskBlocked {
								icon = "⊘"
							}
							fmt.Fprintf(os.Stderr, "%s %s %s: %s\n", icon, ad.wsID, ad.dispatchID, summary)
						}

						if anyFlag {
							if *jsonFlag {
								return cli.PrintJSON(result)
							}
							return nil
						}
					}
				}

				// Check if all completed.
				if allFlag && len(completed) == len(active) {
					if *jsonFlag {
						var results []map[string]interface{}
						for _, r := range completed {
							results = append(results, r)
						}
						return cli.PrintJSON(results)
					}
					return nil
				}

				// Check timeout.
				if !deadline.IsZero() && time.Now().After(deadline) {
					fmt.Fprintf(os.Stderr, "Timeout: %d of %d dispatches completed\n", len(completed), len(active))
					os.Exit(1)
				}

				<-ticker.C
			}
		},
	}

	cmd.Flags().DurationVar(&timeoutFlag, "timeout", 0, "maximum time to wait (e.g. 5m, 1h)")
	cmd.Flags().BoolVar(&anyFlag, "any", false, "wait for any active dispatch to complete")
	cmd.Flags().BoolVar(&allFlag, "all", false, "wait for all active dispatches to complete")

	return cmd
}

// runWait polls for a single dispatch to complete. Called by both `wait` and `dispatch --wait`.
func runWait(app *appContext, repoRoot, wsID, dispatchID string, timeout time.Duration, jsonFlag *bool) error {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		evt, err := app.store.LatestTaskEvent(repoRoot, wsID, dispatchID)
		if err != nil {
			return fmt.Errorf("poll task event: %w", err)
		}
		if evt != nil && (evt.Kind == store.EventTaskCompleted || evt.Kind == store.EventTaskFailed || evt.Kind == store.EventTaskBlocked) {
			summary, _ := evt.Data["summary"].(string)
			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchID,
					"status":       evt.Kind,
					"summary":      summary,
				})
			}
			icon := "✓"
			if evt.Kind == store.EventTaskFailed {
				icon = "✗"
			} else if evt.Kind == store.EventTaskBlocked {
				icon = "⊘"
			}
			fmt.Printf("%s %s %s: %s\n", icon, wsID, dispatchID, summary)
			return nil
		}

		// Check heartbeat staleness.
		checkHeartbeat(wsID)

		// Check timeout.
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "Timeout waiting for %s %s\n", wsID, dispatchID)
			os.Exit(1)
		}

		<-ticker.C
	}
}

// checkHeartbeat warns to stderr if the heartbeat file is stale (>120s).
func checkHeartbeat(wsID string) {
	heartbeatPath := filepath.Join(config.TowrHome(), "comms", wsID, "heartbeat")
	info, err := os.Stat(heartbeatPath)
	if err != nil {
		return // No heartbeat file yet — not necessarily an error.
	}
	if time.Since(info.ModTime()) > 120*time.Second {
		fmt.Fprintf(os.Stderr, "Warning: heartbeat for %s is stale (last: %s ago)\n", wsID, time.Since(info.ModTime()).Truncate(time.Second))
	}
}
