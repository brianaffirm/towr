package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/terminal"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

// appContext holds the initialized dependencies shared across commands.
type appContext struct {
	repoRoot string
	cfg      *config.Config
	store    *store.SQLiteStore
	manager  *workspace.Manager
	term     terminal.Backend
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		repoFlag string
		jsonFlag bool
	)

	rootCmd := &cobra.Command{
		Use:   "towr",
		Short: "Governed merge pipeline for AI agent workspaces",
		Long:  "towr isolates, validates, and lands AI-generated code changes across any agent runtime.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// When no subcommand is given, launch the TUI dashboard.
	// RunE is set after command registration so cobra can still route subcommands.

	rootCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "repository root (default: detect from cwd)")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "output JSON instead of table")

	// Lazy initializer — only set up store/manager when a command actually runs.
	var app *appContext
	initApp := func() (*appContext, error) {
		if app != nil {
			return app, nil
		}

		repoRoot := repoFlag
		if repoRoot == "" {
			var err error
			repoRoot, err = git.GetRepoRoot("")
			if err != nil {
				return nil, fmt.Errorf("not inside a git repository (use --repo to specify): %w", err)
			}
		}

		if err := config.EnsureTowrDirs(); err != nil {
			return nil, err
		}

		// Ensure repo-specific state directory exists.
		repoState := config.RepoStatePath(repoRoot)
		if err := os.MkdirAll(repoState, 0o755); err != nil {
			return nil, fmt.Errorf("create repo state dir: %w", err)
		}

		cfg, err := config.LoadRepo(repoRoot)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}

		s := store.NewSQLiteStore()
		dbPath := filepath.Join(repoState, "state.db")
		if err := s.Init(dbPath); err != nil {
			return nil, fmt.Errorf("init store: %w", err)
		}

		// The workspace.Manager uses workspace.WorkspaceStore (in-memory style).
		// We need an adapter from store.SQLiteStore to workspace.WorkspaceStore.
		wsStore := &storeAdapter{s: s, repoRoot: repoRoot}
		mgr := workspace.NewManager(wsStore)

		// Terminal backend: use tmux if available, else headless.
		var term terminal.Backend
		term = terminal.NewTmuxBackend("towr")
		// Fall back to headless if tmux is not installed.
		if _, err := lookupTmux(); err != nil {
			term = terminal.NewHeadlessBackend()
		}

		app = &appContext{
			repoRoot: repoRoot,
			cfg:      cfg,
			store:    s,
			manager:  mgr,
			term:     term,
		}
		return app, nil
	}

	// Register all commands.
	rootCmd.AddCommand(
		newSpawnCmd(initApp, &jsonFlag),
		newAdoptCmd(initApp, &jsonFlag),
		newLsCmd(initApp, &jsonFlag),
		newLandCmd(initApp, &jsonFlag),
		newDiffCmd(initApp, &jsonFlag),
		newLogCmd(initApp, &jsonFlag),
		newOpenCmd(initApp, &jsonFlag),
		newCleanupCmd(initApp, &jsonFlag),
		newDoctorCmd(initApp, &jsonFlag),
		newQueueCmd(initApp, &jsonFlag),
		newApproveCmd(initApp, &jsonFlag),
		newDenyCmd(initApp, &jsonFlag),
		newRespondCmd(initApp, &jsonFlag),
		newPreviewCmd(initApp, &jsonFlag),
		newTUICmd(initApp),
		newShellHookCmd(),
		newNudgeCmd(),
		newOverlapCmd(initApp, &jsonFlag),
		newDispatchCmd(initApp, &jsonFlag),
		newSendCmd(initApp, &jsonFlag),
		newWaitCmd(initApp, &jsonFlag),
		newWatchCmd(initApp, &jsonFlag),
		newReportCmd(initApp, &jsonFlag),
		newPromoteCmd(initApp, &jsonFlag),
		newOrchestrateCmd(initApp, &jsonFlag),
		newAuditCmd(initApp, &jsonFlag),
	)

	// If no subcommand was provided, launch the TUI dashboard.
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runTUI(initApp)
	}

	err := rootCmd.Execute()

	// Clean up store on exit.
	if app != nil && app.store != nil {
		_ = app.store.Close()
	}

	return err
}

// lookupTmux checks if tmux is installed.
func lookupTmux() (string, error) {
	return exec.LookPath("tmux")
}

// initAppForRepo initializes a full appContext for a specific repo root.
// Unlike the closure-based initApp, this is a standalone function used when
// the caller already knows the repo root (e.g., via global resolution).
func initAppForRepo(repoRoot string) (*appContext, error) {
	if err := config.EnsureTowrDirs(); err != nil {
		return nil, err
	}

	repoState := config.RepoStatePath(repoRoot)
	if err := os.MkdirAll(repoState, 0o755); err != nil {
		return nil, fmt.Errorf("create repo state dir: %w", err)
	}

	cfg, err := config.LoadRepo(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	s := store.NewSQLiteStore()
	dbPath := filepath.Join(repoState, "state.db")
	if err := s.Init(dbPath); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	wsStore := &storeAdapter{s: s, repoRoot: repoRoot}
	mgr := workspace.NewManager(wsStore)

	var term terminal.Backend
	term = terminal.NewTmuxBackend("towr")
	if _, err := lookupTmux(); err != nil {
		term = terminal.NewHeadlessBackend()
	}

	return &appContext{
		repoRoot: repoRoot,
		cfg:      cfg,
		store:    s,
		manager:  mgr,
		term:     term,
	}, nil
}

// resolveGlobal finds a workspace by ref (bare ID or repo:id) across all repos.
// Returns the workspace, a store for that repo (caller must close), and a terminal backend.
func resolveGlobal(ref string) (*store.Workspace, *store.SQLiteStore, terminal.Backend, error) {
	reposDir := filepath.Join(config.TowrHome(), "repos")
	sw, err := store.FindWorkspaceByID(reposDir, ref)
	if err != nil {
		return nil, nil, nil, err
	}

	// Open the appropriate store: repo-scoped or global for non-repo workspaces.
	var dbPath string
	if sw.RepoRoot == "" {
		dbPath = filepath.Join(config.TowrHome(), "global-state.db")
	} else {
		repoState := config.RepoStatePath(sw.RepoRoot)
		dbPath = filepath.Join(repoState, "state.db")
	}
	s := store.NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		return nil, nil, nil, fmt.Errorf("open store for %s: %w", sw.RepoRoot, err)
	}

	// Create a terminal backend (tmux if available, headless if not).
	var term terminal.Backend
	if _, lookupErr := lookupTmux(); lookupErr != nil {
		term = terminal.NewHeadlessBackend()
	} else {
		term = terminal.NewTmuxBackend("towr")
	}

	return sw, s, term, nil
}

// storeAdapter adapts store.SQLiteStore to workspace.WorkspaceStore.
type storeAdapter struct {
	s        *store.SQLiteStore
	repoRoot string
}

func (a *storeAdapter) Save(ws *workspace.Workspace) error {
	sw := workspaceToStore(ws)
	return a.s.SaveWorkspace(sw)
}

func (a *storeAdapter) Get(id string) (*workspace.Workspace, error) {
	sw, err := a.s.GetWorkspace(a.repoRoot, id)
	if err != nil {
		return nil, err
	}
	if sw == nil {
		return nil, fmt.Errorf("workspace %q not found", id)
	}
	return storeToWorkspace(sw), nil
}

func (a *storeAdapter) List(filter workspace.ListFilter) ([]*workspace.Workspace, error) {
	sf := store.ListFilter{
		Status:   string(filter.Status),
		AllRepos: filter.RepoRoot == "",
	}
	sws, err := a.s.ListWorkspaces(a.repoRoot, sf)
	if err != nil {
		return nil, err
	}
	var result []*workspace.Workspace
	for _, sw := range sws {
		result = append(result, storeToWorkspace(sw))
	}
	return result, nil
}

func (a *storeAdapter) Delete(id string) error {
	return a.s.DeleteWorkspace(a.repoRoot, id)
}

func workspaceToStore(ws *workspace.Workspace) *store.Workspace {
	sw := &store.Workspace{
		ID:           ws.ID,
		RepoRoot:     ws.RepoRoot,
		BaseBranch:   ws.BaseBranch,
		BaseRef:      ws.BaseRef,
		Branch:       ws.Branch,
		WorktreePath: ws.WorktreePath,
		SourceKind:   string(ws.Source.Kind),
		SourceValue:  ws.Source.Value,
		Status:       string(ws.Status),
		Error:        ws.Error,
		MergeCommit:  ws.MergeCommit,
		CreatedAt:    ws.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    ws.UpdatedAt.Format(time.RFC3339),
	}
	if ws.Agent != nil {
		sw.AgentRuntime = ws.Agent.Runtime
		sw.AgentID = ws.Agent.AgentID
		sw.AgentModelVersion = ws.Agent.ModelVersion
	}
	if ws.ExitCode != nil {
		sw.ExitCode = ws.ExitCode
	}
	return sw
}

func storeToWorkspace(sw *store.Workspace) *workspace.Workspace {
	ws := &workspace.Workspace{
		ID:           sw.ID,
		RepoRoot:     sw.RepoRoot,
		BaseBranch:   sw.BaseBranch,
		BaseRef:      sw.BaseRef,
		Branch:       sw.Branch,
		WorktreePath: sw.WorktreePath,
		Source: workspace.SpawnSource{
			Kind:  workspace.SpawnSourceKind(sw.SourceKind),
			Value: sw.SourceValue,
		},
		Status:      workspace.WorkspaceStatus(sw.Status),
		Error:       sw.Error,
		MergeCommit: sw.MergeCommit,
		ExitCode:    sw.ExitCode,
	}
	if sw.AgentRuntime != "" {
		ws.Agent = &workspace.AgentIdentity{
			Runtime:      sw.AgentRuntime,
			AgentID:      sw.AgentID,
			ModelVersion: sw.AgentModelVersion,
		}
	}
	ws.CreatedAt, _ = time.Parse(time.RFC3339, sw.CreatedAt)
	ws.UpdatedAt, _ = time.Parse(time.RFC3339, sw.UpdatedAt)
	return ws
}

// workspaceIDCompletion returns a cobra completion function for workspace IDs.
func workspaceIDCompletion(initApp func() (*appContext, error)) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		app, err := initApp()
		if err != nil {
			// Global completion: scan all repos.
			reposDir := filepath.Join(config.TowrHome(), "repos")
			all, err := store.ListAllWorkspaces(reposDir)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var ids []string
			for _, ws := range all {
				repoName := filepath.Base(ws.RepoRoot)
				ids = append(ids, repoName+":"+ws.ID)
			}
			return ids, cobra.ShellCompDirectiveNoFileComp
		}
		sws, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var ids []string
		for _, sw := range sws {
			ids = append(ids, sw.ID)
		}
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
}
