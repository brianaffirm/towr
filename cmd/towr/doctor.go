package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/brianaffirm/towr/internal/cli"
	gitpkg "github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

// Problem describes a diagnosed issue.
type Problem struct {
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	Detail      string `json:"detail"`
}

func newDoctorCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose workspace problems",
		Long:  "Check workspace state against filesystem reality and report problems.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
			if err != nil {
				return fmt.Errorf("list workspaces: %w", err)
			}

			var problems []Problem

			for _, ws := range workspaces {
				// Skip already-archived/landed workspaces.
				if ws.Status == "ARCHIVED" || ws.Status == "LANDED" {
					continue
				}

				// Check if worktree exists on disk.
				if ws.WorktreePath != "" {
					if _, err := os.Stat(ws.WorktreePath); os.IsNotExist(err) {
						problems = append(problems, Problem{
							WorkspaceID: ws.ID,
							Kind:        "missing_worktree",
							Detail:      fmt.Sprintf("worktree path %s does not exist", ws.WorktreePath),
						})
					}
				}

				// Check if branch exists.
				if ws.Branch != "" {
					exists, err := workspace.BranchExists(ws.RepoRoot, ws.Branch)
					if err == nil && !exists {
						problems = append(problems, Problem{
							WorkspaceID: ws.ID,
							Kind:        "missing_branch",
							Detail:      fmt.Sprintf("branch %s does not exist", ws.Branch),
						})
					}
				}

				// Check state consistency.
				if ws.Status == "RUNNING" || ws.Status == "CREATING" {
					// If workspace claims to be running but worktree is gone, it's orphaned.
					if ws.WorktreePath != "" {
						if _, err := os.Stat(ws.WorktreePath); os.IsNotExist(err) {
							problems = append(problems, Problem{
								WorkspaceID: ws.ID,
								Kind:        "orphaned",
								Detail:      fmt.Sprintf("status is %s but worktree is missing", ws.Status),
							})
						}
					}
				}
			}

			// Check system dependencies.
			_, tmuxErr := exec.LookPath("tmux")
			if tmuxErr != nil {
				problems = append(problems, Problem{
					Kind:   "missing_dependency",
					Detail: "tmux is not installed — terminal management disabled (towr open will print paths instead of attaching)",
				})
			} else {
				// tmux is installed — check if any towr sessions exist.
				panes, _ := app.term.ListPanes()
				if len(panes) == 0 {
					problems = append(problems, Problem{
						Kind:   "no_tmux_session",
						Detail: "no towr tmux sessions found — towr open will not be able to attach (sessions are created by towr spawn)",
					})
				}
			}

			// Check for orphaned towr branches not tracked in the store.
			branches, err := listTowrBranches(app.repoRoot)
			if err == nil {
				tracked := make(map[string]bool)
				for _, ws := range workspaces {
					tracked[ws.Branch] = true
				}
				for _, b := range branches {
					if !tracked[b] {
						problems = append(problems, Problem{
							Kind:   "orphaned_branch",
							Detail: fmt.Sprintf("branch %s exists but has no workspace", b),
						})
					}
				}
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"problems": problems,
					"ok":       len(problems) == 0,
				})
			}

			if len(problems) == 0 {
				fmt.Println("All workspaces healthy.")
				return nil
			}

			fmt.Printf("Found %d problem(s):\n\n", len(problems))
			for _, p := range problems {
				wsLabel := p.WorkspaceID
				if wsLabel == "" {
					wsLabel = "(no workspace)"
				}
				fmt.Printf("  [%s] %s: %s\n", p.Kind, wsLabel, p.Detail)
			}

			return nil
		},
	}

	return cmd
}

// listTowrBranches returns all branches with the towr/ prefix.
func listTowrBranches(repoRoot string) ([]string, error) {
	out, err := gitpkg.RunGit(repoRoot, "branch", "--list", "towr/*", "--format", "%(refname:short)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var branches []string
	for _, line := range splitLines(out) {
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
