package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/spf13/cobra"
)

func newDiffCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		fullFlag bool
		statFlag bool
	)

	cmd := &cobra.Command{
		Use:               "diff <id>",
		Aliases:           []string{"d"},
		Short:             "Show diff for a workspace against its base branch",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			// Try normal repo-scoped lookup first.
			app, appErr := initApp()
			var sw *store.Workspace

			if appErr == nil {
				var err error
				sw, err = app.store.GetWorkspace(app.repoRoot, wsID)
				if err != nil {
					return fmt.Errorf("get workspace: %w", err)
				}
			}

			// If not in a repo or workspace not found in current repo, search globally.
			if sw == nil {
				var globalStore *store.SQLiteStore
				var err error
				sw, globalStore, _, err = resolveGlobal(wsID)
				if err != nil {
					if appErr != nil {
						return appErr
					}
					return err
				}
				defer globalStore.Close()
			}

			// Non-repo workspaces don't support diff.
			if sw.RepoRoot == "" {
				return fmt.Errorf("workspace %q is a non-repo workspace — diff is only available for git workspaces", wsID)
			}

			if *jsonFlag {
				files, _ := git.DiffFiles(sw.RepoRoot, sw.BaseBranch, sw.Branch)
				stat, _ := git.DiffStat(sw.RepoRoot, sw.BaseBranch, sw.Branch)
				added, removed := getDiffCounts(sw.RepoRoot, sw.BaseBranch, sw.Branch)
				statSummary := ""
				if stat != nil {
					statSummary = stat.Summary
				}
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id": wsID,
					"base_branch":  sw.BaseBranch,
					"branch":       sw.Branch,
					"files":        files,
					"stat":         statSummary,
					"added":        added,
					"removed":      removed,
				})
			}

			if statFlag {
				stat, err := git.DiffStat(sw.RepoRoot, sw.BaseBranch, sw.Branch)
				if err != nil {
					return fmt.Errorf("diff stat: %w", err)
				}
				fmt.Println(stat.Raw)
				return nil
			}

			// Check if there's committed branch diff first.
			branchDiff, _ := git.RunGit(sw.RepoRoot, "diff", "--shortstat", sw.BaseBranch+"..."+sw.Branch)

			if branchDiff != "" {
				// Has committed diff — show it.
				gitArgs := []string{"diff"}
				if !fullFlag {
					gitArgs = append(gitArgs, "--stat")
				}
				gitArgs = append(gitArgs, sw.BaseBranch+"..."+sw.Branch)

				gitCmd := exec.Command("git", gitArgs...)
				gitCmd.Dir = sw.RepoRoot
				gitCmd.Stdout = os.Stdout
				gitCmd.Stderr = os.Stderr
				return gitCmd.Run()
			}

			// No committed diff — fall back to worktree changes.
			if sw.WorktreePath != "" {
				wtArgs := []string{"diff"}
				if !fullFlag {
					wtArgs = append(wtArgs, "--stat")
				}
				wtCmd := exec.Command("git", wtArgs...)
				wtCmd.Dir = sw.WorktreePath
				wtCmd.Stdout = os.Stdout
				wtCmd.Stderr = os.Stderr
				_ = wtCmd.Run()

				// Also show staged changes.
				stagedDiff, _ := git.RunGit(sw.WorktreePath, "diff", "--cached", "--shortstat")
				if stagedDiff != "" {
					stArgs := []string{"diff", "--cached"}
					if !fullFlag {
						stArgs = append(stArgs, "--stat")
					}
					stCmd := exec.Command("git", stArgs...)
					stCmd.Dir = sw.WorktreePath
					stCmd.Stdout = os.Stdout
					stCmd.Stderr = os.Stderr
					_ = stCmd.Run()
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&fullFlag, "full", false, "show full diff (not just stat)")
	cmd.Flags().BoolVar(&statFlag, "stat", false, "show --stat summary")

	return cmd
}
