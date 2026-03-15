package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newAdoptCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		idFlag   string
		baseFlag string
	)

	cmd := &cobra.Command{
		Use:   "adopt [path-or-branch]",
		Short: "Adopt an existing worktree or branch as a towr workspace",
		Long: `Register a pre-existing git worktree or branch as a towr workspace.
With no arguments, adopts the current directory's branch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			var worktreePath, branch string

			if len(args) == 0 {
				// Adopt current directory.
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("get cwd: %w", err)
				}
				worktreePath = cwd
				b, err := git.CurrentBranch(cwd)
				if err != nil {
					return fmt.Errorf("not on a branch: %w", err)
				}
				branch = b
			} else {
				arg := args[0]
				// Is it a filesystem path?
				absPath, _ := filepath.Abs(arg)
				if info, err := os.Stat(absPath); err == nil && info.IsDir() {
					worktreePath = absPath
					b, err := git.CurrentBranch(absPath)
					if err != nil {
						return fmt.Errorf("not a git directory or not on a branch: %w", err)
					}
					branch = b
				} else {
					// Treat as branch name — find its worktree.
					branch = arg
					wtPath, err := findWorktreeForBranch(app.repoRoot, branch)
					if err != nil {
						return fmt.Errorf("could not find worktree for branch %q: %w", branch, err)
					}
					worktreePath = wtPath
				}
			}

			// Derive workspace ID.
			wsID := idFlag
			if wsID == "" {
				// Strip towr/ prefix if present.
				name := branch
				name = strings.TrimPrefix(name, "towr/")
				wsID = orchestrate.Slugify(name)
			}

			// Check not already tracked.
			existing, _ := app.store.GetWorkspace(app.repoRoot, wsID)
			if existing != nil {
				return fmt.Errorf("workspace %q already tracked", wsID)
			}

			// Infer base branch.
			baseBranch := baseFlag
			if baseBranch == "" {
				baseBranch = app.cfg.Defaults.BaseBranch
			}
			if baseBranch == "" {
				detected, err := workspace.DetectDefaultBranch(app.repoRoot)
				if err != nil {
					return fmt.Errorf("detecting default branch: %w", err)
				}
				baseBranch = detected
			}

			// Get base ref for merge tracking.
			baseRef, _ := git.HeadRef(app.repoRoot)

			now := time.Now().UTC().Format(time.RFC3339)
			sw := &store.Workspace{
				ID:           wsID,
				RepoRoot:     app.repoRoot,
				BaseBranch:   baseBranch,
				BaseRef:      baseRef,
				Branch:       branch,
				WorktreePath: worktreePath,
				SourceKind:   "branch",
				SourceValue:  branch,
				Status:       "READY",
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := app.store.SaveWorkspace(sw); err != nil {
				return fmt.Errorf("save workspace: %w", err)
			}

			// Emit adopted event.
			_ = app.store.EmitEvent(store.Event{
				Kind:        store.EventWorkspaceAdopted,
				WorkspaceID: wsID,
				RepoRoot:    app.repoRoot,
				Actor:       os.Getenv("USER"),
				Data: map[string]interface{}{
					"branch":        branch,
					"worktree_path": worktreePath,
					"base_branch":   baseBranch,
				},
			})

			// Create tmux session if available.
			if !app.term.IsHeadless() {
				_ = app.term.CreatePane(wsID, worktreePath, "")
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"id":            wsID,
					"branch":        branch,
					"worktree_path": worktreePath,
					"base_branch":   baseBranch,
					"status":        "READY",
				})
			}

			fmt.Printf("Adopted workspace %s\n", wsID)
			fmt.Printf("  Branch:   %s\n", branch)
			fmt.Printf("  Worktree: %s\n", worktreePath)
			fmt.Printf("  Base:     %s\n", baseBranch)
			fmt.Printf("  Status:   %s\n", cli.ColorStatus("READY"))
			return nil
		},
	}

	cmd.Flags().StringVar(&idFlag, "id", "", "workspace ID (default: derived from branch name)")
	cmd.Flags().StringVar(&baseFlag, "base", "", "base branch (default: auto-detect)")

	return cmd
}

// findWorktreeForBranch finds the worktree path for a given branch name.
func findWorktreeForBranch(repoRoot, branch string) (string, error) {
	out, err := git.RunGit(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}

	var currentPath string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		}
		if strings.HasPrefix(line, "branch refs/heads/"+branch) && currentPath != "" {
			return currentPath, nil
		}
	}
	return "", fmt.Errorf("no worktree found for branch %q", branch)
}
