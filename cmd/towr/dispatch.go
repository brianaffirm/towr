package main

import (
	"fmt"
	"os"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newDispatchCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var waitFlag bool

	cmd := &cobra.Command{
		Use:               "dispatch <workspace-id> <prompt>",
		Aliases:           []string{"d"},
		Short:             "Dispatch a task to a workspace",
		Long:              "Send a prompt to a workspace's agent session via tmux paste buffer.",
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

			// 3. Check tmux session alive.
			alive, err := app.term.IsPaneAlive(wsID)
			if err != nil {
				return fmt.Errorf("check tmux session: %w", err)
			}
			if !alive {
				return fmt.Errorf("tmux session for workspace %q is not running", wsID)
			}

			// 4. Generate dispatch ID: count all task.dispatched events for this workspace.
			events, err := app.store.QueryEvents(store.EventQuery{
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Kind:        store.EventTaskDispatched,
			})
			if err != nil {
				return fmt.Errorf("query dispatch events: %w", err)
			}
			dispatchID := fmt.Sprintf("d-%04d", len(events)+1)

			// 5. Set up comms directory, write prompt and wrapper.
			commsDir, err := dispatch.EnsureCommsDir(wsID)
			if err != nil {
				return fmt.Errorf("ensure comms dir: %w", err)
			}
			if err := dispatch.WritePrompt(commsDir, prompt); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}
			wrapper := dispatch.BuildWrapper(wsID, dispatchID, commsDir)
			runShPath := commsDir + "/run.sh"
			if err := os.WriteFile(runShPath, []byte(wrapper), 0o755); err != nil {
				return fmt.Errorf("write run.sh: %w", err)
			}

			// 6. Emit task.dispatched event.
			if err := app.store.EmitEvent(store.Event{
				Kind:        store.EventTaskDispatched,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Timestamp:   time.Now().UTC(),
				Data: map[string]interface{}{
					"dispatch_id": dispatchID,
					"prompt":      truncate(prompt, 200),
				},
			}); err != nil {
				return fmt.Errorf("emit dispatch event: %w", err)
			}

			// 7. Update workspace status to RUNNING.
			sw.Status = string(workspace.StatusRunning)
			sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := app.store.SaveWorkspace(sw); err != nil {
				return fmt.Errorf("update workspace status: %w", err)
			}

			// 8. Deliver via PasteBuffer.
			runCmd := dispatch.BuildRunCommand(commsDir)
			if err := app.term.PasteBuffer(wsID, runCmd); err != nil {
				return fmt.Errorf("paste buffer: %w", err)
			}

			// 9. Output dispatch ID.
			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchID,
					"status":       "dispatched",
				})
			}
			fmt.Printf("Dispatched %s to %s\n", dispatchID, wsID)

			// 10. Optional --wait.
			if waitFlag {
				return runWait(app, app.repoRoot, wsID, dispatchID, 0, jsonFlag)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&waitFlag, "wait", false, "wait for the dispatch to complete")

	return cmd
}
