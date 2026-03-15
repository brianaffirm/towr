package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/config"
	gitpkg "github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/terminal"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newCleanupCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		staleFlag     bool
		mergedFlag    bool
		dryRunFlag    bool
		forceFlag     bool
		reasonFlag    string
		dangerousFlag bool
	)

	cmd := &cobra.Command{
		Use:               "cleanup [id]",
		Short:             "Remove a workspace without merging, or garbage-collect stale ones",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: workspaceIDCompletion(initApp),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --dangerous nukes everything: all workspaces, worktrees, towr tmux
			// sessions, mux session, and state files. Dev/test use only.
			if dangerousFlag {
				return cleanupDangerous(dryRunFlag)
			}

			// --stale requires repo context (repo-scoped operation).
			if staleFlag {
				app, err := initApp()
				if err != nil {
					return err
				}
				return cleanupStale(app, dryRunFlag, *jsonFlag)
			}

			// --merged cleans up workspaces whose branches have been merged.
			if mergedFlag {
				app, err := initApp()
				if err != nil {
					return err
				}
				return cleanupMerged(app, dryRunFlag, *jsonFlag)
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a workspace ID or use --stale")
			}

			wsID := args[0]

			if dryRunFlag {
				if *jsonFlag {
					return cli.PrintJSON(map[string]string{
						"action": "cleanup",
						"id":     wsID,
						"status": "dry-run",
					})
				}
				fmt.Printf("Would clean up workspace %s\n", wsID)
				return nil
			}

			// Try normal repo-scoped lookup first.
			app, appErr := initApp()
			var term terminal.Backend
			var mgr *workspace.Manager
			var worktreePath string

			if appErr == nil {
				// Check workspace exists locally.
				sw, err := app.store.GetWorkspace(app.repoRoot, wsID)
				if err != nil {
					return fmt.Errorf("get workspace: %w", err)
				}
				if sw != nil {
					term = app.term
					mgr = app.manager
					worktreePath = sw.WorktreePath
				}
			}

			// If not in a repo or workspace not found locally, search globally.
			if mgr == nil {
				sw, globalStore, globalTerm, err := resolveGlobal(wsID)
				if err != nil {
					// Try non-repo workspace cleanup via global store.
					globalDBPath := filepath.Join(config.TowrHome(), "global-state.db")
					nrStore := store.NewSQLiteStore()
					if initErr := nrStore.Init(globalDBPath); initErr != nil {
						if appErr != nil {
							return appErr
						}
						return err
					}
					defer nrStore.Close()
					nrWs, _ := nrStore.GetWorkspace("", wsID)
					if nrWs == nil {
						if appErr != nil {
							return appErr
						}
						return err
					}
					// Non-repo cleanup: destroy tmux + delete store record.
					nrTerm := terminal.NewBackend()
					_ = nrTerm.DestroyPane(wsID)
					if delErr := nrStore.DeleteWorkspace("", wsID); delErr != nil {
						return fmt.Errorf("cleanup failed: %w", delErr)
					}
					if *jsonFlag {
						return cli.PrintJSON(map[string]string{
							"action": "cleanup",
							"id":     wsID,
							"status": "removed",
						})
					}
					fmt.Printf("Cleaned up workspace %s\n", wsID)
					return nil
				}

				// Non-repo workspace found via global resolution.
				if sw.RepoRoot == "" {
					defer globalStore.Close()
					_ = globalTerm.DestroyPane(wsID)
					if delErr := globalStore.DeleteWorkspace("", wsID); delErr != nil {
						return fmt.Errorf("cleanup failed: %w", delErr)
					}
					if *jsonFlag {
						return cli.PrintJSON(map[string]string{
							"action": "cleanup",
							"id":     wsID,
							"status": "removed",
						})
					}
					fmt.Printf("Cleaned up workspace %s\n", wsID)
					return nil
				}

				defer globalStore.Close()
				term = globalTerm
				wsStore := &storeAdapter{s: globalStore, repoRoot: sw.RepoRoot}
				mgr = workspace.NewManager(wsStore)
				worktreePath = sw.WorktreePath
			}

			// Check for uncommitted changes before cleanup.
			if worktreePath != "" && !forceFlag {
				mod, untrk, err := workspace.WorktreeStatus(worktreePath)
				if err == nil && (mod > 0 || untrk > 0) {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: workspace '%s' has %d modified file(s) and %d untracked file(s).\n", wsID, mod, untrk)
					fmt.Fprintf(cmd.ErrOrStderr(), "Proceed? [y/N] ")
					scanner := bufio.NewScanner(os.Stdin)
					if scanner.Scan() {
						response := strings.TrimSpace(scanner.Text())
						if len(response) == 0 || (response[0] != 'y' && response[0] != 'Y') {
							return fmt.Errorf("cleanup aborted")
						}
					} else {
						return fmt.Errorf("cleanup aborted")
					}
				}
			}

			// Emit audit event for forced cleanup.
			if forceFlag && app != nil {
				data := map[string]interface{}{
					"actor": os.Getenv("USER"),
				}
				if reasonFlag != "" {
					data["reason"] = reasonFlag
				}
				_ = app.store.EmitEvent(store.Event{
					Kind:        store.EventCleanupForced,
					WorkspaceID: wsID,
					RepoRoot:    app.repoRoot,
					Actor:       os.Getenv("USER"),
					Data:        data,
				})
			}

			// Destroy tmux pane if it exists.
			_ = term.DestroyPane(wsID)

			if err := mgr.Delete(wsID); err != nil {
				return fmt.Errorf("cleanup failed: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]string{
					"action": "cleanup",
					"id":     wsID,
					"status": "removed",
				})
			}

			fmt.Printf("Cleaned up workspace %s\n", wsID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&staleFlag, "stale", false, "garbage collect stale workspaces")
	cmd.Flags().BoolVar(&mergedFlag, "merged", false, "clean up workspaces whose branches have been merged")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "preview without executing")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "skip confirmation for dirty worktrees")
	cmd.Flags().StringVar(&reasonFlag, "reason", "", "audit reason for --force bypass")
	cmd.Flags().BoolVar(&dangerousFlag, "dangerous", false, "nuke all towr state: workspaces, worktrees, tmux sessions, mux-panes.json (dev/test use only)")

	return cmd
}

// cleanupDangerous removes all towr state unconditionally.
// Intended for development and test plan cleanup only.
func cleanupDangerous(dryRun bool) error {
	towrHome := config.TowrHome()
	worktreeRoot := config.WorktreeRoot()

	steps := []struct {
		label string
		fn    func() error
	}{
		{
			label: "kill towr-mux tmux session",
			fn: func() error {
				out, err := exec.Command("tmux", "kill-session", "-t", "towr-mux").CombinedOutput()
				if err != nil && !strings.Contains(string(out), "no server") && !strings.Contains(string(out), "session not found") {
					return fmt.Errorf("%s", strings.TrimSpace(string(out)))
				}
				return nil
			},
		},
		{
			label: "kill towr/* tmux sessions",
			fn: func() error {
				out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
				if err != nil {
					return nil // no tmux server running
				}
				for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if strings.HasPrefix(name, "towr/") {
						_ = exec.Command("tmux", "kill-session", "-t", name).Run()
					}
				}
				return nil
			},
		},
		{
			label: fmt.Sprintf("remove all worktrees under %s", worktreeRoot),
			fn: func() error {
				entries, err := os.ReadDir(worktreeRoot)
				if err != nil {
					return nil // nothing there
				}
				for _, repo := range entries {
					repoWorktreeDir := filepath.Join(worktreeRoot, repo.Name())
					wts, err := os.ReadDir(repoWorktreeDir)
					if err != nil {
						continue
					}
					repoRoot := filepath.Join(os.Getenv("HOME"), "w", repo.Name())
					for _, wt := range wts {
						wtPath := filepath.Join(repoWorktreeDir, wt.Name())
						// Deregister from git if repo root exists, then force-remove.
						if _, err := os.Stat(repoRoot); err == nil {
							_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtPath).Run()
						}
						_ = os.RemoveAll(wtPath)
					}
					_ = os.Remove(repoWorktreeDir)
				}
				return nil
			},
		},
		{
			label: fmt.Sprintf("delete towr/* git branches across all repos in ~/w/"),
			fn: func() error {
				wDir := filepath.Join(os.Getenv("HOME"), "w")
				repos, err := os.ReadDir(wDir)
				if err != nil {
					return nil
				}
				for _, repo := range repos {
					repoRoot := filepath.Join(wDir, repo.Name())
					if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
						continue
					}
					out, err := exec.Command("git", "-C", repoRoot, "branch", "--list", "towr/*").Output()
					if err != nil || len(out) == 0 {
						continue
					}
					for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						branch := strings.TrimSpace(strings.TrimPrefix(line, "* "))
						if branch != "" {
							_ = exec.Command("git", "-C", repoRoot, "branch", "-D", branch).Run()
						}
					}
				}
				return nil
			},
		},
		{
			label: fmt.Sprintf("remove mux-panes.json from %s", towrHome),
			fn: func() error {
				return os.Remove(filepath.Join(towrHome, "mux-panes.json"))
			},
		},
		{
			label: fmt.Sprintf("wipe all workspace DB records under %s/repos/", towrHome),
			fn: func() error {
				reposDir := filepath.Join(towrHome, "repos")
				entries, err := os.ReadDir(reposDir)
				if err != nil {
					return nil
				}
				for _, entry := range entries {
					dbPath := filepath.Join(reposDir, entry.Name(), "state.db")
					s := store.NewSQLiteStore()
					if err := s.Init(dbPath); err != nil {
						continue
					}
					// Use AllRepos:true to list across all repo roots in this DB.
					workspaces, _ := s.ListWorkspaces("", store.ListFilter{AllRepos: true})
					for _, ws := range workspaces {
						_ = s.DeleteWorkspace(ws.RepoRoot, ws.ID)
					}
					s.Close()
				}
				return nil
			},
		},
		{
			label: "kill orphaned git status processes",
			fn: func() error {
				_ = exec.Command("pkill", "-f", "git status --porcelain").Run()
				_ = exec.Command("pkill", "-f", "git worktree add").Run()
				return nil
			},
		},
	}

	fmt.Println("⚠  towr cleanup --dangerous")
	fmt.Println("   This will destroy all towr sessions, worktrees, and state.")
	fmt.Println()

	for _, step := range steps {
		if dryRun {
			fmt.Printf("  [dry-run] %s\n", step.label)
			continue
		}
		fmt.Printf("  → %s ... ", step.label)
		if err := step.fn(); err != nil {
			fmt.Printf("warning: %v\n", err)
		} else {
			fmt.Println("done")
		}
	}

	if !dryRun {
		fmt.Println()
		fmt.Println("✓ towr state cleared.")
	}
	return nil
}

func cleanupStale(app *appContext, dryRun bool, jsonOutput bool) error {
	workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	// Parse stale threshold from config.
	threshold := 7 * 24 * time.Hour // default 7 days
	if app.cfg.Cleanup.StaleThreshold != "" {
		if d, err := parseDuration(app.cfg.Cleanup.StaleThreshold); err == nil {
			threshold = d
		}
	}

	var stale []string
	for _, ws := range workspaces {
		if ws.Status == "LANDED" || ws.Status == "ARCHIVED" {
			continue
		}
		created, err := time.Parse(time.RFC3339, ws.CreatedAt)
		if err != nil {
			continue
		}
		if time.Since(created) > threshold {
			stale = append(stale, ws.ID)
		}
	}

	if jsonOutput {
		return cli.PrintJSON(map[string]interface{}{
			"stale":   stale,
			"dry_run": dryRun,
		})
	}

	if len(stale) == 0 {
		fmt.Println("No stale workspaces found.")
		return nil
	}

	for _, id := range stale {
		if dryRun {
			fmt.Printf("Would clean up: %s\n", id)
		} else {
			_ = app.term.DestroyPane(id)
			if err := app.manager.Delete(id); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clean up %s: %v\n", id, err)
				continue
			}
			fmt.Printf("Cleaned up: %s\n", id)
		}
	}

	return nil
}

func cleanupMerged(app *appContext, dryRun bool, jsonOutput bool) error {
	// Fetch to get latest remote state.
	_ = gitpkg.Fetch(app.repoRoot, "origin")

	workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	var merged []string
	for _, ws := range workspaces {
		if ws.Status == "LANDED" || ws.Status == "ARCHIVED" {
			continue
		}
		if ws.Branch == "" {
			continue
		}
		ok, err := isBranchMerged(app.repoRoot, ws.Branch, ws.BaseBranch)
		if err != nil || !ok {
			continue
		}
		merged = append(merged, ws.ID)
	}

	if jsonOutput {
		return cli.PrintJSON(map[string]interface{}{
			"merged":  merged,
			"dry_run": dryRun,
		})
	}

	if len(merged) == 0 {
		fmt.Println("No merged workspaces found.")
		return nil
	}

	for _, id := range merged {
		if dryRun {
			fmt.Printf("Would clean up (merged): %s\n", id)
		} else {
			_ = app.term.DestroyPane(id)
			if err := app.manager.Delete(id); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clean up %s: %v\n", id, err)
				continue
			}
			fmt.Printf("Cleaned up (merged): %s\n", id)
		}
	}

	return nil
}

// isBranchMerged checks if a branch has been merged into the base branch.
func isBranchMerged(repoRoot, branch, base string) (bool, error) {
	out, err := gitpkg.RunGit(repoRoot, "branch", "--merged", base)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
		if trimmed == branch {
			return true, nil
		}
	}
	return false, nil
}

// parseDuration parses duration strings like "7d", "24h", "30m".
func parseDuration(s string) (time.Duration, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}
	last := s[len(s)-1]
	num := s[:len(s)-1]
	var n int
	if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
		return time.ParseDuration(s) // fallback to Go standard
	}
	switch last {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 's':
		return time.Duration(n) * time.Second, nil
	default:
		return time.ParseDuration(s)
	}
}
