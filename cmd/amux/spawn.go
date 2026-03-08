package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianho/amux/internal/cli"
	"github.com/brianho/amux/internal/config"
	gitpkg "github.com/brianho/amux/internal/git"
	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/terminal"
	"github.com/brianho/amux/internal/workspace"
	"github.com/spf13/cobra"
)

func newSpawnCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		idFlag      string
		pathFlag    string
		baseFlag    string
		prFlag      string
		branchFlag  string
		agentFlag   string
		noHooksFlag   bool
		copyPathsFlag string
	)

	cmd := &cobra.Command{
		Use:     "spawn <task>",
		Aliases: []string{"s"},
		Short:   "Create a new workspace for a task",
		Long:    "Create a git worktree workspace, optionally launch an agent.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := args[0]

			// Derive workspace ID.
			wsID := idFlag
			if wsID == "" {
				wsID = slugify(task)
			}

			// Non-repo mode: if --path points to a non-repo dir, or not in a repo at all.
			if pathFlag != "" {
				absPath, err := filepath.Abs(pathFlag)
				if err != nil {
					return fmt.Errorf("resolve path: %w", err)
				}
				// Check if path is in a git repo.
				if _, err := gitpkg.GetRepoRoot(absPath); err != nil {
					return spawnNonRepo(cmd, task, wsID, absPath, jsonFlag)
				}
				// Path is in a repo — fall through to normal spawn.
			}

			app, err := initApp()
			if err != nil {
				// Not in a git repo and no --path — try non-repo with cwd.
				cwd, cwdErr := os.Getwd()
				if cwdErr != nil {
					return err // return original initApp error
				}
				return spawnNonRepo(cmd, task, wsID, cwd, jsonFlag)
			}

			// Determine spawn source.
			source := workspace.SpawnSource{Kind: workspace.SpawnFromTask, Value: task}
			if prFlag != "" {
				source = workspace.SpawnSource{Kind: workspace.SpawnFromPR, Value: prFlag}
			} else if branchFlag != "" {
				source = workspace.SpawnSource{Kind: workspace.SpawnFromBranch, Value: branchFlag}
			}

			// Determine base branch.
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

			// Agent identity (optional).
			var agent *workspace.AgentIdentity
			if agentFlag != "" {
				agent = &workspace.AgentIdentity{Runtime: agentFlag}
			}

			// Merge copy_paths: config + CLI flag, deduplicated.
			copyPaths := app.cfg.Workspace.CopyPaths
			if copyPathsFlag != "" {
				seen := make(map[string]bool)
				for _, p := range copyPaths {
					seen[p] = true
				}
				for _, p := range strings.Split(copyPathsFlag, ",") {
					p = strings.TrimSpace(p)
					if p != "" && !seen[p] {
						seen[p] = true
						copyPaths = append(copyPaths, p)
					}
				}
			}

			opts := workspace.CreateOpts{
				ID:         wsID,
				RepoRoot:   app.repoRoot,
				BaseBranch: baseBranch,
				Source:     source,
				Agent:      agent,
				CopyPaths:  copyPaths,
			}

			ws, err := app.manager.Create(opts)
			if err != nil {
				return fmt.Errorf("spawn failed: %w", err)
			}

			// Run post-create hooks if configured and not disabled.
			if !noHooksFlag && app.cfg.Hooks.PostCreate != "" {
				if hookErr := runShellHook(app.cfg.Hooks.PostCreate, ws); hookErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: post-create hook failed: %v\n", hookErr)
				}
			}

			// Create tmux pane for the workspace.
			if !app.term.IsHeadless() {
				if err := app.term.CreatePane(ws.ID, ws.WorktreePath, ""); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not create tmux pane: %v\n", err)
				}
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]interface{}{
					"id":            ws.ID,
					"branch":        ws.Branch,
					"worktree_path": ws.WorktreePath,
					"status":        ws.Status,
					"base_branch":   ws.BaseBranch,
					"headless":      app.term.IsHeadless(),
				})
			}

			fmt.Printf("Workspace %s created\n", ws.ID)
			fmt.Printf("  Branch:   %s\n", ws.Branch)
			fmt.Printf("  Worktree: %s\n", ws.WorktreePath)
			fmt.Printf("  Status:   %s\n", cli.ColorStatus(string(ws.Status)))
			if app.term.IsHeadless() {
				fmt.Printf("  Note:     tmux not available — use: cd %s\n", ws.WorktreePath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&idFlag, "id", "", "workspace ID (default: derived from task)")
	cmd.Flags().StringVar(&pathFlag, "path", "", "directory path for non-repo workspace")
	cmd.Flags().StringVar(&baseFlag, "base", "", "base branch to fork from")
	cmd.Flags().StringVar(&prFlag, "pr", "", "checkout existing PR")
	cmd.Flags().StringVar(&branchFlag, "branch", "", "checkout existing branch")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent runtime to launch")
	cmd.Flags().BoolVar(&noHooksFlag, "no-hooks", false, "skip post-create hooks")
	cmd.Flags().StringVar(&copyPathsFlag, "copy-paths", "", "comma-separated paths to copy into worktree (additive with config)")

	return cmd
}

// spawnNonRepo creates a workspace backed by a plain directory (no git repo).
func spawnNonRepo(cmd *cobra.Command, task, wsID, dirPath string, jsonFlag *bool) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path %q: %w", absPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", absPath)
	}

	// Open a global store (not repo-scoped).
	if err := config.EnsureAmuxDirs(); err != nil {
		return err
	}
	globalDBPath := filepath.Join(config.AmuxHome(), "global-state.db")
	s := store.NewSQLiteStore()
	if err := s.Init(globalDBPath); err != nil {
		return fmt.Errorf("init global store: %w", err)
	}
	defer s.Close()

	// Check for ID collision.
	existing, _ := s.GetWorkspace("", wsID)
	if existing != nil {
		return fmt.Errorf("workspace %q already exists", wsID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sw := &store.Workspace{
		ID:           wsID,
		RepoRoot:     "",
		WorktreePath: absPath,
		SourceKind:   "task",
		SourceValue:  task,
		Status:       "READY",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.SaveWorkspace(sw); err != nil {
		return fmt.Errorf("save workspace: %w", err)
	}

	// Create tmux session if available.
	var term terminal.Backend
	if _, lookupErr := lookupTmux(); lookupErr != nil {
		term = terminal.NewHeadlessBackend()
	} else {
		term = terminal.NewTmuxBackend("amux")
	}

	if !term.IsHeadless() {
		if err := term.CreatePane(wsID, absPath, ""); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not create tmux pane: %v\n", err)
		}
	}

	if *jsonFlag {
		return cli.PrintJSON(map[string]interface{}{
			"id":     wsID,
			"path":   absPath,
			"status": "READY",
			"type":   "non-repo",
		})
	}

	fmt.Printf("Workspace %s created (non-repo)\n", wsID)
	fmt.Printf("  Path:   %s\n", absPath)
	fmt.Printf("  Status: %s\n", cli.ColorStatus("READY"))
	if term.IsHeadless() {
		fmt.Printf("  Note:   tmux not available — use: cd %s\n", absPath)
	}
	return nil
}

// slugify converts a task description into a short workspace ID.
func slugify(s string) string {
	var result []byte
	prevDash := false
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			result = append(result, c)
			prevDash = false
		case c >= 'A' && c <= 'Z':
			result = append(result, c+32) // lowercase
			prevDash = false
		case c == ' ' || c == '-' || c == '_' || c == '/':
			if !prevDash && len(result) > 0 {
				result = append(result, '-')
				prevDash = true
			}
		}
	}
	// Trim trailing dash.
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	// Limit length.
	if len(result) > 30 {
		result = result[:30]
	}
	if len(result) == 0 {
		return "workspace"
	}
	return string(result)
}

// runShellHook runs a hook command with variable substitution.
func runShellHook(hookCmd string, ws *workspace.Workspace) error {
	expanded := hookCmd
	expanded = strings.ReplaceAll(expanded, "${WORKSPACE_ID}", ws.ID)
	expanded = strings.ReplaceAll(expanded, "${WORKTREE_PATH}", ws.WorktreePath)
	expanded = strings.ReplaceAll(expanded, "${BRANCH}", ws.Branch)
	expanded = strings.ReplaceAll(expanded, "${BASE_BRANCH}", ws.BaseBranch)
	expanded = strings.ReplaceAll(expanded, "${REPO_ROOT}", ws.RepoRoot)

	cmd := exec.Command("/bin/sh", "-c", expanded)
	cmd.Dir = ws.WorktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
