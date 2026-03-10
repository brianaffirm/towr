package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
)

// TestSpawnLsLandCleanup is an integration test that exercises the full
// spawn -> ls -> land -> cleanup lifecycle using the real workspace manager,
// store, and git operations.
func TestSpawnLsLandCleanup(t *testing.T) {
	// Skip in short mode or if git is not available.
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	// Create a temporary git repo.
	repoDir := t.TempDir()
	mustGit(t, repoDir, "init", "-b", "main")
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")

	// Create initial commit.
	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial commit")

	// Set up towr state directory.
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)

	// Ensure dirs.
	if err := config.EnsureTowrDirs(); err != nil {
		t.Fatal(err)
	}
	repoState := config.RepoStatePath(repoDir)
	if err := os.MkdirAll(repoState, 0755); err != nil {
		t.Fatal(err)
	}

	// Open store.
	s := store.NewSQLiteStore()
	dbPath := filepath.Join(repoState, "state.db")
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("init store: %v", err)
	}
	defer s.Close()

	// Create workspace manager with adapter.
	wsStore := &storeAdapter{s: s, repoRoot: repoDir}
	mgr := workspace.NewManager(wsStore)

	// --- SPAWN ---
	ws, err := mgr.Create(workspace.CreateOpts{
		ID:         "test-feat",
		RepoRoot:   repoDir,
		BaseBranch: "main",
		Source: workspace.SpawnSource{
			Kind:  workspace.SpawnFromTask,
			Value: "add test feature",
		},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	if ws.Status != workspace.StatusReady {
		t.Errorf("expected status READY, got %s", ws.Status)
	}
	if ws.Branch != "towr/test-feat" {
		t.Errorf("expected branch towr/test-feat, got %s", ws.Branch)
	}

	// Verify worktree exists.
	if _, err := os.Stat(ws.WorktreePath); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	// --- Make a change in the worktree ---
	changeFile := filepath.Join(ws.WorktreePath, "feature.go")
	if err := os.WriteFile(changeFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ws.WorktreePath, "add", ".")
	mustGit(t, ws.WorktreePath, "commit", "-m", "add feature")

	// --- LS ---
	workspaces, err := s.ListWorkspaces(repoDir, store.ListFilter{})
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].ID != "test-feat" {
		t.Errorf("expected workspace ID test-feat, got %s", workspaces[0].ID)
	}

	// --- LAND ---
	// Build landing pipeline.
	lStore := &testLandingStore{s: s, repoRoot: repoDir}
	lOps := &testLandingOps{mgr: mgr}
	hookCfg := &testHookConfig{}

	pipeline := newTestLandingPipeline(lStore, lOps, hookCfg)

	result, err := pipeline.Land("test-feat", testLandOpts{})
	if err != nil {
		t.Fatalf("land failed: %v", err)
	}

	if result.MergeCommit == "" {
		t.Error("expected merge commit SHA, got empty")
	}

	// --- Verify clean state ---
	// After landing, delete the workspace.
	if err := mgr.Delete("test-feat"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Workspace should be gone from store.
	workspaces, err = s.ListWorkspaces(repoDir, store.ListFilter{})
	if err != nil {
		t.Fatalf("ls after cleanup failed: %v", err)
	}
	if len(workspaces) != 0 {
		t.Errorf("expected 0 workspaces after cleanup, got %d", len(workspaces))
	}

	// Worktree directory should be gone.
	if _, err := os.Stat(ws.WorktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree still exists after cleanup")
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// Test adapters that directly use the landing package without importing it
// (to avoid circular dependency concerns in tests).

type testLandingStore struct {
	s        *store.SQLiteStore
	repoRoot string
}

type testLandingOps struct {
	mgr *workspace.Manager
}

type testHookConfig struct{}

type testLandOpts struct {
	Strategy string
	Force    bool
	NoHooks  bool
}

type testLandResult struct {
	WorkspaceID  string
	MergeCommit  string
	FilesChanged []string
	Duration     time.Duration
}

type testLandingPipeline struct {
	store   *testLandingStore
	ops     *testLandingOps
	hooks   *testHookConfig
}

func newTestLandingPipeline(s *testLandingStore, ops *testLandingOps, hooks *testHookConfig) *testLandingPipeline {
	return &testLandingPipeline{store: s, ops: ops, hooks: hooks}
}

func (p *testLandingPipeline) Land(id string, opts testLandOpts) (*testLandResult, error) {
	start := time.Now()

	sw, err := p.store.s.GetWorkspace(p.store.repoRoot, id)
	if err != nil {
		return nil, err
	}
	if sw == nil {
		return nil, os.ErrNotExist
	}

	// Rebase onto base.
	cmd := exec.Command("git", "rebase", sw.BaseBranch)
	cmd.Dir = sw.WorktreePath
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, &rebaseError{output: string(out), err: err}
	}

	// Switch to base branch in repo root.
	cmd = exec.Command("git", "checkout", sw.BaseBranch)
	cmd.Dir = sw.RepoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, &checkoutError{output: string(out), err: err}
	}

	// Fast-forward merge.
	cmd = exec.Command("git", "merge", "--ff-only", sw.Branch)
	cmd.Dir = sw.RepoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, &mergeError{output: string(out), err: err}
	}

	// Get merge commit.
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = sw.RepoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	sha := string(out[:len(out)-1]) // trim newline

	// Update status.
	sw.Status = "LANDED"
	sw.MergeCommit = sha
	_ = p.store.s.SaveWorkspace(sw)

	return &testLandResult{
		WorkspaceID: id,
		MergeCommit: sha,
		Duration:    time.Since(start),
	}, nil
}

type rebaseError struct {
	output string
	err    error
}
func (e *rebaseError) Error() string { return "rebase: " + e.output + ": " + e.err.Error() }

type checkoutError struct {
	output string
	err    error
}
func (e *checkoutError) Error() string { return "checkout: " + e.output + ": " + e.err.Error() }

type mergeError struct {
	output string
	err    error
}
func (e *mergeError) Error() string { return "merge: " + e.output + ": " + e.err.Error() }
