package main

import (
	"encoding/json"
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
		linkPathsFlag string
		envFlags    []string
	)

	cmd := &cobra.Command{
		Use:     "spawn [task]",
		Aliases: []string{"s"},
		Short:   "Create a new workspace for a task",
		Long:    "Create a git worktree workspace, optionally launch an agent.\nWith no arguments, auto-generates a workspace ID.",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var task string
			if len(args) > 0 {
				task = args[0]
			}

			// Derive workspace ID.
			wsID := idFlag
			if wsID == "" {
				if task != "" {
					wsID = slugify(task)
				} else {
					wsID = nextAutoID()
					task = wsID
				}
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

			// Merge link_paths: config + CLI flag, deduplicated.
			linkPaths := app.cfg.Workspace.LinkPaths
			if linkPathsFlag != "" {
				seen := make(map[string]bool)
				for _, p := range linkPaths {
					seen[p] = true
				}
				for _, p := range strings.Split(linkPathsFlag, ",") {
					p = strings.TrimSpace(p)
					if p != "" && !seen[p] {
						seen[p] = true
						linkPaths = append(linkPaths, p)
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
				LinkPaths:  linkPaths,
			}

			ws, err := app.manager.Create(opts)
			if err != nil {
				return fmt.Errorf("spawn failed: %w", err)
			}

			// Parse and persist env vars.
			envMap := parseEnvFlags(envFlags)
			if len(envMap) > 0 {
				envJSON, _ := json.Marshal(envMap)
				sw, _ := app.store.GetWorkspace(app.repoRoot, wsID)
				if sw != nil {
					sw.EnvVars = envJSON
					_ = app.store.SaveWorkspace(sw)
				}
			}

			// Discover hooks: .towr-hooks.toml in repo tree overrides config.
			targetPath := config.InferTargetPath(app.repoRoot)
			hooks, hooksErr := config.DiscoverHooks(app.repoRoot, targetPath, app.cfg.Hooks)
			if hooksErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", hooksErr)
			}

			// Run post-create hooks if configured and not disabled.
			if !noHooksFlag && hooks.PostCreate != "" {
				if hookErr := runShellHookWithEnv(hooks.PostCreate, ws, envMap); hookErr != nil {
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
	cmd.Flags().StringVar(&linkPathsFlag, "link-paths", "", "comma-separated paths to symlink into worktree (additive with config)")
	cmd.Flags().StringArrayVar(&envFlags, "env", nil, "environment variable KEY=VAL (repeatable, passed to hooks)")

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
	if err := config.EnsureTowrDirs(); err != nil {
		return err
	}
	globalDBPath := filepath.Join(config.TowrHome(), "global-state.db")
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
		term = terminal.NewTmuxBackend("towr")
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

// nextAutoID generates the next auto-increment workspace ID (ws-0001, ws-0002, ...).
// Scans ~/.towr/repos/*/state.db for existing ws-NNNN IDs to find the next counter.
func nextAutoID() string {
	reposDir := filepath.Join(config.TowrHome(), "repos")
	all, err := store.ListAllWorkspaces(reposDir)
	if err != nil {
		return "ws-0001"
	}
	maxN := 0
	for _, ws := range all {
		if strings.HasPrefix(ws.ID, "ws-") {
			numStr := strings.TrimPrefix(ws.ID, "ws-")
			var n int
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	return fmt.Sprintf("ws-%04d", maxN+1)
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

// parseEnvFlags parses --env KEY=VAL flags into a map.
func parseEnvFlags(flags []string) map[string]string {
	if len(flags) == 0 {
		return nil
	}
	m := make(map[string]string, len(flags))
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// runShellHookWithEnv runs a hook command with variable substitution and extra env vars.
func runShellHookWithEnv(hookCmd string, ws *workspace.Workspace, envVars map[string]string) error {
	expanded := hookCmd
	expanded = strings.ReplaceAll(expanded, "${WORKSPACE_ID}", ws.ID)
	expanded = strings.ReplaceAll(expanded, "${WORKTREE_PATH}", ws.WorktreePath)
	expanded = strings.ReplaceAll(expanded, "${BRANCH}", ws.Branch)
	expanded = strings.ReplaceAll(expanded, "${BASE_BRANCH}", ws.BaseBranch)
	expanded = strings.ReplaceAll(expanded, "${REPO_ROOT}", ws.RepoRoot)
	for k, v := range envVars {
		expanded = strings.ReplaceAll(expanded, "${"+k+"}", v)
	}

	cmd := exec.Command("/bin/sh", "-c", expanded)
	cmd.Dir = ws.WorktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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
