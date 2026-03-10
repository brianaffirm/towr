package main

import (
	"fmt"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/spf13/cobra"
)

func newLogCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var followFlag bool

	cmd := &cobra.Command{
		Use:               "log <id>",
		Short:             "Show event log for a workspace",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			// Try normal repo-scoped lookup first.
			app, appErr := initApp()
			var eventStore *store.SQLiteStore
			var repoRoot string
			var foundLocally bool

			if appErr == nil {
				sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
				if err != nil {
					return fmt.Errorf("get workspace: %w", err)
				}
				if sw != nil {
					eventStore = app.store
					repoRoot = app.repoRoot
					foundLocally = true
				}
			}

			// If not in a repo or workspace not found locally, search globally.
			if !foundLocally {
				sw, globalStore, _, err := resolveGlobal(wsID)
				if err != nil {
					if appErr != nil {
						return appErr
					}
					return err
				}
				defer globalStore.Close()
				eventStore = globalStore
				repoRoot = sw.RepoRoot

				// Non-repo workspaces don't have event logs.
				if sw.RepoRoot == "" {
					return fmt.Errorf("workspace %q is a non-repo workspace — log is only available for git workspaces", wsID)
				}
			}

			printEvents := func() error {
				events, err := eventStore.QueryEvents(store.EventQuery{
					WorkspaceID: wsID,
					RepoRoot:    repoRoot,
				})
				if err != nil {
					return fmt.Errorf("query events: %w", err)
				}

				if *jsonFlag {
					return cli.PrintJSON(events)
				}

				if len(events) == 0 {
					fmt.Printf("No events for workspace %s\n", wsID)
					return nil
				}

				for _, e := range events {
					ts := e.Timestamp.Format("15:04:05")
					fmt.Printf("%s  %-30s  %s\n", ts, e.Kind, formatEventData(e.Data))
				}
				return nil
			}

			if err := printEvents(); err != nil {
				return err
			}

			if followFlag {
				// Simple polling follow mode.
				for {
					time.Sleep(2 * time.Second)
					if err := printEvents(); err != nil {
						return err
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "follow (tail) the log")

	return cmd
}

func formatEventData(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	var parts []string
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += " " + p
	}
	return result
}
