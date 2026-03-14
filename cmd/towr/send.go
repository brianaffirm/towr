package main

import (
	"fmt"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newSendCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		waitFlag    bool
		approveFlag bool
	)

	cmd := &cobra.Command{
		Use:               "send <workspace-id> <message>",
		Short:             "Send a follow-up message or approve a permission dialog",
		Long:              "Send a follow-up message to a workspace running an interactive Claude session. Use --approve to send Enter to approve a permission dialog without requiring idle state.",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			app, err := initApp()
			if err != nil {
				return err
			}

			// --approve mode: just send Enter to approve a permission dialog.
			if approveFlag {
				if err := app.term.Approve(wsID, "Enter"); err != nil {
					return fmt.Errorf("send enter: %w", err)
				}
				fmt.Printf("Sent approval to %s\n", wsID)
				return nil
			}

			if len(args) < 2 {
				return fmt.Errorf("message argument required (or use --approve)")
			}
			message := args[1]

			// 1. Validate workspace exists and is IDLE (interactive session waiting).
			sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
			if err != nil {
				return fmt.Errorf("get workspace: %w", err)
			}
			if sw == nil {
				return fmt.Errorf("workspace %q not found", wsID)
			}
			status := workspace.WorkspaceStatus(sw.Status)
			if status != workspace.StatusIdle && status != workspace.StatusReady {
				return fmt.Errorf("workspace %q is %s (must be IDLE or READY to send follow-up)", wsID, sw.Status)
			}

			// 2. Check agent session alive.
			alive, err := app.term.IsPaneAlive(wsID)
			if err != nil {
				return fmt.Errorf("check agent session: %w", err)
			}
			if !alive {
				return fmt.Errorf("agent session for workspace %q is not running", wsID)
			}

			// 3. Verify Claude is idle in the pane (❯ visible).
			captured, err := app.term.CaptureOutput(wsID, 50)
			if err != nil {
				return fmt.Errorf("capture pane: %w", err)
			}
			paneState := dispatch.DetectPaneState(captured)
			if paneState != dispatch.PaneIdle {
				return fmt.Errorf("Claude is not idle in workspace %q (state: %s)", wsID, paneState)
			}

			// 4. Generate a new dispatch ID for tracking.
			events, err := app.store.QueryEvents(store.EventQuery{
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Kind:        store.EventTaskDispatched,
			})
			if err != nil {
				return fmt.Errorf("query dispatch events: %w", err)
			}
			dispatchID := fmt.Sprintf("d-%04d", len(events)+1)

			// 5. Write the message to comms dir.
			commsDir, err := dispatch.EnsureCommsDir(wsID)
			if err != nil {
				return fmt.Errorf("ensure comms dir: %w", err)
			}
			if err := dispatch.WritePrompt(commsDir, message); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}

			// 6. Emit task.dispatched event.
			if err := app.store.EmitEvent(store.Event{
				ID:          uuid.New().String(),
				Kind:        store.EventTaskDispatched,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Timestamp:   time.Now().UTC(),
				Data: map[string]interface{}{
					"dispatch_id": dispatchID,
					"prompt":      truncate(message, 200),
					"mode":        "interactive",
					"followup":    true,
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

			// 8. Send the message via SendInput.
			if err := app.term.SendInput(wsID, message); err != nil {
				return fmt.Errorf("send message: %w", err)
			}

			// 9. Emit task.started event.
			if err := app.store.EmitEvent(store.Event{
				ID:          uuid.New().String(),
				Kind:        store.EventTaskStarted,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Timestamp:   time.Now().UTC(),
				Data: map[string]interface{}{
					"dispatch_id": dispatchID,
					"mode":        "interactive",
					"followup":    true,
				},
			}); err != nil {
				return fmt.Errorf("emit started event: %w", err)
			}

			// 10. Output.
			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchID,
					"status":       "sent",
					"mode":         "interactive",
				})
			}
			fmt.Printf("Sent %s to %s (follow-up)\n", dispatchID, wsID)

			// 11. Optional --wait.
			if waitFlag {
				return runInteractiveWait(app, wsID, dispatchID, 0, jsonFlag)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&waitFlag, "wait", false, "wait for the response to complete")
	cmd.Flags().BoolVar(&approveFlag, "approve", false, "send Enter to approve a permission dialog")

	return cmd
}
