package main

import (
	"fmt"
	"os"

	"github.com/brianho/amux/internal/cli"
	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/terminal"
	"github.com/spf13/cobra"
)

func newOpenCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "open <id>",
		Short:             "Open or print workspace path",
		Long:              "Attach to or switch to a workspace's tmux session, or print its worktree path. Works from any directory.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			// Try normal repo-scoped lookup first.
			app, appErr := initApp()
			var sw *store.Workspace
			var term terminal.Backend

			if appErr == nil {
				var err error
				sw, err = app.store.GetWorkspace(app.repoRoot, wsID)
				if err != nil {
					return fmt.Errorf("get workspace: %w", err)
				}
				term = app.term
			}

			// If not in a repo or workspace not found in current repo, search globally.
			if sw == nil {
				var globalStore *store.SQLiteStore
				var err error
				sw, globalStore, term, err = resolveGlobal(wsID)
				if err != nil {
					if appErr != nil {
						return appErr
					}
					return err
				}
				defer globalStore.Close()
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"id":            sw.ID,
					"worktree_path": sw.WorktreePath,
					"status":        sw.Status,
					"repo_root":     sw.RepoRoot,
					"headless":      term.IsHeadless(),
				})
			}

			if term.IsHeadless() {
				fmt.Fprintf(os.Stderr, "tmux not found — printing worktree path instead.\n")
				fmt.Fprintf(os.Stderr, "Install tmux for full terminal management, or use: cd %s\n", sw.WorktreePath)
				fmt.Println(sw.WorktreePath)
				return nil
			}

			// If the tmux session was killed, recreate it with the two-window layout.
			alive, aliveErr := term.IsPaneAlive(wsID)
			if aliveErr == nil && !alive {
				if createErr := term.CreatePane(wsID, sw.WorktreePath, ""); createErr != nil {
					fmt.Fprintf(os.Stderr, "Could not recreate tmux session: %v\n", createErr)
					fmt.Fprintf(os.Stderr, "Printing worktree path instead. Use: cd %s\n", sw.WorktreePath)
					fmt.Println(sw.WorktreePath)
					return nil
				}
			}

			// Try tmux attach, fall back to printing path.
			if err := term.Attach(wsID); err != nil {
				fmt.Fprintf(os.Stderr, "Could not attach to tmux session: %v\n", err)
				fmt.Fprintf(os.Stderr, "Printing worktree path instead. Use: cd %s\n", sw.WorktreePath)
				fmt.Println(sw.WorktreePath)
			}

			return nil
		},
	}

	return cmd
}
