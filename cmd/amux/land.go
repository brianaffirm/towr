package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianho/amux/internal/cli"
	"github.com/brianho/amux/internal/config"
	gitpkg "github.com/brianho/amux/internal/git"
	"github.com/brianho/amux/internal/landing"
	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/workspace"
	"github.com/spf13/cobra"
)

func newLandCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		dryRunFlag  bool
		squashFlag  bool
		pushFlag    bool
		prFlag      bool
		chainFlag   []string
		forceFlag   bool
		noHooksFlag bool
	)

	cmd := &cobra.Command{
		Use:               "land <id>",
		Aliases:           []string{"l"},
		Short:             "Validate, merge, and clean up a workspace",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := args[0]

			app, appErr := initApp()
			if appErr != nil {
				// Not in a repo — try global resolution.
				ws, globalStore, _, err := resolveGlobal(wsID)
				if err != nil {
					return appErr
				}
				globalStore.Close() // only needed ws.RepoRoot; close immediately
				if ws.RepoRoot == "" {
					return fmt.Errorf("workspace %q is a non-repo workspace — land is only available for git workspaces", wsID)
				}
				app, err = initAppForRepo(ws.RepoRoot)
				if err != nil {
					return fmt.Errorf("init for repo %s: %w", ws.RepoRoot, err)
				}
				defer app.store.Close()
			}

			// Warn if running from inside the worktree being landed.
			if !forceFlag {
				sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
				if err == nil && sw != nil && sw.WorktreePath != "" {
					cwd, cwdErr := os.Getwd()
					if cwdErr == nil && isInsideDir(cwd, sw.WorktreePath) {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"Warning: You're inside this worktree (%s).\n"+
								"Landing will delete it and your shell will be in a stale directory.\n"+
								"Consider running from the main repo or another worktree instead.\n"+
								"Continue? [y/N] ", sw.WorktreePath)
						var response string
						fmt.Fscan(os.Stdin, &response)
						response = strings.ToLower(strings.TrimSpace(response))
						if response != "y" && response != "yes" {
							return fmt.Errorf("aborted")
						}
					}
				}
			}

			// Warn if worktree has uncommitted changes.
			if !forceFlag {
				sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
				if err == nil && sw != nil && sw.WorktreePath != "" && sw.RepoRoot != "" {
					ds, dsErr := workspace.WorktreeDetailedStatus(sw.WorktreePath)
					if dsErr == nil && (ds.Staged > 0 || ds.Unstaged > 0 || ds.Untracked > 0) {
						var parts []string
						if ds.Unstaged > 0 {
							parts = append(parts, fmt.Sprintf("%d unstaged", ds.Unstaged))
						}
						if ds.Staged > 0 {
							parts = append(parts, fmt.Sprintf("%d staged", ds.Staged))
						}
						if ds.Untracked > 0 {
							parts = append(parts, fmt.Sprintf("%d untracked", ds.Untracked))
						}
						fmt.Fprintf(cmd.ErrOrStderr(),
							"Warning: worktree has %s files that won't be included in the merge.\n"+
								"Commit your changes first, or use --force to land anyway.\n"+
								"Continue? [y/N] ", strings.Join(parts, " and "))
						var response string
						fmt.Fscan(os.Stdin, &response)
						response = strings.ToLower(strings.TrimSpace(response))
						if response != "y" && response != "yes" {
							return fmt.Errorf("aborted")
						}
					}
				}
			}

			// Protected branch enforcement: block local merge to protected branches.
			if !pushFlag && !prFlag && !forceFlag {
				sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
				if err == nil && sw != nil && app.cfg.Landing.IsProtectedBranch(sw.BaseBranch) {
					return fmt.Errorf("%q is a protected branch — use --pr to push and create a PR, or --push to push only", sw.BaseBranch)
				}
			}

			// Build landing pipeline dependencies.
			lStore := &landingStoreAdapter{s: app.store, repoRoot: app.repoRoot}
			lOps := &landingOpsAdapter{mgr: app.manager}
			hookCfg := &landingHookConfig{cfg: app.cfg}
			pipeline := landing.NewLandingPipeline(lStore, lOps, hookCfg, 5*time.Minute)

			strategy := landing.StrategyRebaseFF
			if squashFlag {
				strategy = landing.StrategySquash
			}

			opts := landing.LandOpts{
				Strategy: strategy,
				DryRun:   dryRunFlag,
				Force:    forceFlag,
				Push:     pushFlag,
				PR:       prFlag,
				NoHooks:  noHooksFlag,
			}

			// Chain mode.
			if len(chainFlag) > 0 {
				ids := append([]string{wsID}, chainFlag...)
				return doChainLand(pipeline, app, ids, opts, *jsonFlag)
			}

			// Dry-run mode.
			if dryRunFlag {
				result, err := pipeline.DryRun(wsID, opts)
				if err != nil {
					return fmt.Errorf("dry-run failed: %w", err)
				}

				if *jsonFlag {
					return cli.PrintJSON(result)
				}

				fmt.Printf("Dry-run for workspace %s:\n", result.WorkspaceID)
				fmt.Printf("  Can land:   %v\n", result.CanLand)
				fmt.Printf("  Strategy:   %s\n", result.Strategy)
				if result.DiffStat != "" {
					fmt.Printf("  Diff:       %s\n", result.DiffStat)
				}
				if len(result.FilesChanged) > 0 {
					fmt.Printf("  Files:      %d changed\n", len(result.FilesChanged))
				}
				if len(result.ConflictFiles) > 0 {
					fmt.Printf("  Conflicts:  %v\n", result.ConflictFiles)
				}
				for _, issue := range result.Issues {
					fmt.Printf("  Issue:      %s\n", issue)
				}
				return nil
			}

			// Normal land.
			result, err := pipeline.Land(wsID, opts)
			if err != nil {
				return fmt.Errorf("land failed: %w", err)
			}

			// Push/PR mode — workspace stays alive.
			if result.PushedBranch != "" {
				if *jsonFlag {
					jsonData := map[string]interface{}{
						"workspace_id":  result.WorkspaceID,
						"pushed_branch": result.PushedBranch,
						"files_changed": result.FilesChanged,
						"duration":      result.Duration.String(),
					}
					if prFlag {
						// Try to get PR URL
						sw, _ := app.store.GetWorkspace(app.repoRoot, wsID)
						if sw != nil {
							remoteURL, err := gitpkg.GetRemoteURL(app.repoRoot, "origin")
							if err == nil {
								if prURL, ok := gitpkg.BuildPRURL(remoteURL, sw.BaseBranch, sw.Branch); ok {
									jsonData["pr_url"] = prURL
								}
							}
						}
					}
					return cli.PrintJSON(jsonData)
				}

				fmt.Printf("Pushed branch %s\n", result.PushedBranch)
				fmt.Printf("  Files: %d changed\n", len(result.FilesChanged))

				if prFlag {
					sw, _ := app.store.GetWorkspace(app.repoRoot, wsID)
					if sw != nil {
						remoteURL, err := gitpkg.GetRemoteURL(app.repoRoot, "origin")
						if err == nil {
							if prURL, ok := gitpkg.BuildPRURL(remoteURL, sw.BaseBranch, sw.Branch); ok {
								fmt.Printf("  PR URL: %s\n", prURL)
							} else {
								fmt.Printf("  Push complete — create PR manually (non-GitHub remote).\n")
							}
						}
					}
				}

				fmt.Printf("  Duration: %s\n", result.Duration.Round(time.Millisecond))
				fmt.Printf("\nWorkspace kept alive. Run 'amux cleanup %s' after PR merges.\n", wsID)
				return nil
			}

			// Local merge mode — clean up worktree, branch, and terminal session.
			_ = app.manager.Delete(wsID)
			_ = app.term.DestroyPane(wsID)

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"workspace_id":  result.WorkspaceID,
					"merge_commit":  result.MergeCommit,
					"files_changed": result.FilesChanged,
					"strategy":      result.Strategy,
					"duration":      result.Duration.String(),
				})
			}

			fmt.Printf("Landed workspace %s\n", result.WorkspaceID)
			fmt.Printf("  Merge commit: %s\n", result.MergeCommit[:min(12, len(result.MergeCommit))])
			fmt.Printf("  Files:        %d changed\n", len(result.FilesChanged))
			fmt.Printf("  Strategy:     %s\n", result.Strategy)
			fmt.Printf("  Duration:     %s\n", result.Duration.Round(time.Millisecond))
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "preview without executing")
	cmd.Flags().BoolVar(&squashFlag, "squash", false, "squash commits")
	cmd.Flags().BoolVar(&pushFlag, "push", false, "push branch instead of local merge")
	cmd.Flags().BoolVar(&prFlag, "pr", false, "push + open PR via gh")
	cmd.Flags().StringSliceVar(&chainFlag, "chain", nil, "land multiple workspaces sequentially")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "override status check")
	cmd.Flags().BoolVar(&noHooksFlag, "no-hooks", false, "skip hooks")

	return cmd
}

func doChainLand(pipeline *landing.LandingPipeline, app *appContext, ids []string, opts landing.LandOpts, jsonOutput bool) error {
	result, err := pipeline.ChainLand(ids, opts)
	if err != nil {
		return fmt.Errorf("chain land failed: %w", err)
	}

	// Clean up worktrees, branches, and terminal sessions for landed workspaces.
	for _, lr := range result.Landed {
		_ = app.manager.Delete(lr.WorkspaceID)
		_ = app.term.DestroyPane(lr.WorkspaceID)
	}

	if jsonOutput {
		return cli.PrintJSON(result)
	}

	for _, lr := range result.Landed {
		fmt.Printf("Landed %s (commit %s)\n", lr.WorkspaceID, lr.MergeCommit[:min(12, len(lr.MergeCommit))])
	}
	if result.Failed != nil {
		fmt.Printf("Failed on %s: %v\n", result.Failed.WorkspaceID, result.Failed.Error)
		if len(result.Failed.ConflictFiles) > 0 {
			fmt.Printf("  Conflict files: %v\n", result.Failed.ConflictFiles)
		}
	}
	fmt.Printf("Duration: %s\n", result.Duration.Round(time.Millisecond))
	return nil
}

// Landing pipeline adapters.

// landingStoreAdapter adapts store.SQLiteStore to landing.WorkspaceStore.
type landingStoreAdapter struct {
	s        *store.SQLiteStore
	repoRoot string
}

func (a *landingStoreAdapter) GetWorkspace(id string) (*landing.Workspace, error) {
	sw, err := a.s.GetWorkspace(a.repoRoot, id)
	if err != nil {
		return nil, err
	}
	if sw == nil {
		return nil, fmt.Errorf("workspace %q not found", id)
	}
	return &landing.Workspace{
		ID:           sw.ID,
		RepoRoot:     sw.RepoRoot,
		BaseBranch:   sw.BaseBranch,
		Branch:       sw.Branch,
		WorktreePath: sw.WorktreePath,
		Status:       landing.WorkspaceStatus(sw.Status),
		Task:         sw.SourceValue,
	}, nil
}

func (a *landingStoreAdapter) UpdateStatus(id string, status landing.WorkspaceStatus) error {
	sw, err := a.s.GetWorkspace(a.repoRoot, id)
	if err != nil {
		return err
	}
	if sw == nil {
		return fmt.Errorf("workspace %q not found", id)
	}
	sw.Status = string(status)
	return a.s.SaveWorkspace(sw)
}

func (a *landingStoreAdapter) SetMergeCommit(id string, sha string) error {
	sw, err := a.s.GetWorkspace(a.repoRoot, id)
	if err != nil {
		return err
	}
	if sw == nil {
		return fmt.Errorf("workspace %q not found", id)
	}
	sw.MergeCommit = sha
	return a.s.SaveWorkspace(sw)
}

// landingOpsAdapter adapts workspace.Manager to landing.WorkspaceOps.
type landingOpsAdapter struct {
	mgr *workspace.Manager
}

func (a *landingOpsAdapter) RemoveWorktree(id string) error {
	ws, err := a.mgr.Get(id)
	if err != nil {
		return err
	}
	return workspace.RemoveWorktree(ws.RepoRoot, ws.WorktreePath)
}

func (a *landingOpsAdapter) DeleteBranch(id string) error {
	ws, err := a.mgr.Get(id)
	if err != nil {
		return err
	}
	return workspace.DeleteBranch(ws.RepoRoot, ws.Branch)
}

// landingHookConfig adapts config.Config to landing.HookConfig.
type landingHookConfig struct {
	cfg *config.Config
}

func (h *landingHookConfig) GetHook(hookType landing.HookType) string {
	switch hookType {
	case landing.HookPostCreate:
		return h.cfg.Hooks.PostCreate
	case landing.HookPreLand:
		return h.cfg.Hooks.PreLand
	case landing.HookPostLand:
		return h.cfg.Hooks.PostLand
	default:
		return ""
	}
}

// isInsideDir reports whether child is inside or equal to parent.
// Both paths are resolved to absolute paths before comparison.
func isInsideDir(child, parent string) bool {
	absChild, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	// Ensure trailing separator for prefix check so "/foo/bar" doesn't match "/foo/barbaz".
	if absChild == absParent {
		return true
	}
	return strings.HasPrefix(absChild, absParent+string(filepath.Separator))
}
