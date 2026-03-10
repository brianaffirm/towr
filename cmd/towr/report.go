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

func newReportCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		statusFlag     string
		dispatchIDFlag string
		fileFlag       string
		summaryFlag    string
	)

	cmd := &cobra.Command{
		Use:               "report <workspace-id>",
		Short:             "Report task status from an agent",
		Long:              "Called by the dispatch wrapper to report task progress (started, success, failed, blocked).",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			app, err := initApp()
			if err != nil {
				return err
			}

			// 1. Map status to event kind and workspace status.
			var eventKind string
			var wsStatus workspace.WorkspaceStatus
			switch statusFlag {
			case "started":
				eventKind = store.EventTaskStarted
				wsStatus = workspace.StatusRunning
			case "success":
				eventKind = store.EventTaskCompleted
				wsStatus = workspace.StatusIdle
			case "failed":
				eventKind = store.EventTaskFailed
				wsStatus = workspace.StatusIdle
			case "blocked":
				eventKind = store.EventTaskBlocked
				wsStatus = workspace.StatusBlocked
			default:
				return fmt.Errorf("invalid --status %q (must be started|success|failed|blocked)", statusFlag)
			}

			// 2. Build event data.
			data := map[string]interface{}{
				"dispatch_id": dispatchIDFlag,
				"status":      statusFlag,
			}

			// 3. If --file provided, read it and extract summary.
			if fileFlag != "" {
				content, err := os.ReadFile(fileFlag)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not read file %s: %v\n", fileFlag, err)
				} else {
					data["file"] = fileFlag
					if summaryFlag == "" {
						lines := splitLines(string(content))
						if len(lines) > 0 {
							summaryFlag = truncate(lines[0], 200)
						}
					}
				}
			}
			if summaryFlag != "" {
				data["summary"] = summaryFlag
			}

			// 4. Emit event.
			if err := app.store.EmitEvent(store.Event{
				Kind:        eventKind,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Timestamp:   time.Now().UTC(),
				Data:        data,
			}); err != nil {
				return fmt.Errorf("emit event: %w", err)
			}

			// 5. Update workspace status.
			sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
			if err != nil {
				return fmt.Errorf("get workspace: %w", err)
			}
			if sw != nil {
				sw.Status = string(wsStatus)
				sw.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if err := app.store.SaveWorkspace(sw); err != nil {
					return fmt.Errorf("update workspace: %w", err)
				}
			}

			// 6. If success/failed, archive result.
			if statusFlag == "success" || statusFlag == "failed" {
				commsDir, err := dispatch.EnsureCommsDir(wsID)
				if err == nil {
					if _, archErr := dispatch.ArchiveResult(commsDir, dispatchIDFlag); archErr != nil {
						// Non-fatal: result.json may not exist.
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: archive result: %v\n", archErr)
					}
				}
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"dispatch_id":  dispatchIDFlag,
					"status":       statusFlag,
					"event_kind":   eventKind,
				})
			}
			fmt.Printf("Reported %s for %s dispatch %s\n", statusFlag, wsID, dispatchIDFlag)
			return nil
		},
	}

	cmd.Flags().StringVar(&statusFlag, "status", "", "task status (started|success|failed|blocked)")
	cmd.Flags().StringVar(&dispatchIDFlag, "dispatch-id", "", "dispatch ID")
	cmd.Flags().StringVar(&fileFlag, "file", "", "path to result/error file")
	cmd.Flags().StringVar(&summaryFlag, "summary", "", "human-readable summary")
	_ = cmd.MarkFlagRequired("status")
	_ = cmd.MarkFlagRequired("dispatch-id")

	return cmd
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
