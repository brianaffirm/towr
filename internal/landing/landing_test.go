package landing

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianho/amux/internal/git"
)

// --- Mock implementations ---

type mockStore struct {
	workspaces  map[string]*Workspace
	statuses    map[string]WorkspaceStatus
	mergeCommit map[string]string
}

func newMockStore() *mockStore {
	return &mockStore{
		workspaces:  make(map[string]*Workspace),
		statuses:    make(map[string]WorkspaceStatus),
		mergeCommit: make(map[string]string),
	}
}

func (s *mockStore) GetWorkspace(id string) (*Workspace, error) {
	ws, ok := s.workspaces[id]
	if !ok {
		return nil, fmt.Errorf("workspace %s not found", id)
	}
	// Return status from statuses map if it was updated
	if status, ok := s.statuses[id]; ok {
		ws.Status = status
	}
	return ws, nil
}

func (s *mockStore) UpdateStatus(id string, status WorkspaceStatus) error {
	s.statuses[id] = status
	return nil
}

func (s *mockStore) SetMergeCommit(id string, sha string) error {
	s.mergeCommit[id] = sha
	return nil
}

func (s *mockStore) AddWorkspace(ws *Workspace) {
	s.workspaces[ws.ID] = ws
}

type mockOps struct{}

func (o *mockOps) RemoveWorktree(id string) error { return nil }
func (o *mockOps) DeleteBranch(id string) error   { return nil }

type mockHookConfig struct {
	hooks map[HookType]string
}

func newMockHookConfig() *mockHookConfig {
	return &mockHookConfig{hooks: make(map[HookType]string)}
}

func (h *mockHookConfig) GetHook(hookType HookType) string {
	return h.hooks[hookType]
}

// --- Test helpers ---

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "README.md"), "# Test\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

// createWorktree creates a git worktree for testing and returns the worktree path.
func createWorktree(t *testing.T, repoDir, branch string) string {
	t.Helper()
	wtDir := t.TempDir()
	runGit(t, repoDir, "worktree", "add", "-b", branch, wtDir)
	return wtDir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.RunGit(dir, args...)
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseBranch(t *testing.T, dir string) string {
	t.Helper()
	b, err := git.CurrentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// --- Hook tests ---

func TestHookRunnerSubstitution(t *testing.T) {
	runner := NewHookRunner(10 * time.Second)
	vars := HookVars{
		WorkspaceID:  "auth",
		WorktreePath: "/tmp/wt",
		Branch:       "amux/auth",
		BaseBranch:   "main",
		RepoRoot:     "/tmp/repo",
	}

	result, err := runner.Run("echo ${WORKSPACE_ID} ${BRANCH}", vars)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "auth amux/auth" {
		t.Errorf("expected 'auth amux/auth', got %q", stdout)
	}
}

func TestHookRunnerExitCode(t *testing.T) {
	runner := NewHookRunner(10 * time.Second)
	vars := HookVars{}

	result, err := runner.Run("exit 1", vars)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
}

func TestHookRunnerTimeout(t *testing.T) {
	runner := NewHookRunner(0) // no default timeout

	result, err := runner.RunWithTimeout("sleep 10", HookVars{}, 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even on timeout")
	}
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 on timeout, got %d", result.ExitCode)
	}
}

func TestHookRunnerEmptyCommand(t *testing.T) {
	runner := NewHookRunner(10 * time.Second)
	result, err := runner.Run("", HookVars{})
	if err != nil {
		t.Fatalf("empty command should not error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("empty command exit code should be 0, got %d", result.ExitCode)
	}
}

func TestHookRunnerCaptureStderr(t *testing.T) {
	runner := NewHookRunner(10 * time.Second)
	result, err := runner.Run("echo error >&2", HookVars{})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(result.Stderr, "error") {
		t.Errorf("expected stderr to contain 'error', got %q", result.Stderr)
	}
}

// --- Pipeline tests ---

func TestLandRebaseFF(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	// Create worktree with a feature branch
	wtDir := createWorktree(t, repoDir, "amux/feature1")
	writeFile(t, filepath.Join(wtDir, "feature.txt"), "feature work")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "add feature")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "feature1",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/feature1",
		WorktreePath: wtDir,
		Status:       StatusReady,
		Task:         "Add feature 1",
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("feature1", LandOpts{Strategy: StrategyRebaseFF})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
	if result.Strategy != StrategyRebaseFF {
		t.Errorf("expected strategy rebase-ff, got %s", result.Strategy)
	}

	// Verify file exists on base branch
	if _, err := os.Stat(filepath.Join(repoDir, "feature.txt")); err != nil {
		t.Error("feature.txt should exist on base after landing")
	}

	// Verify status was updated to LANDED
	if store.statuses["feature1"] != StatusLanded {
		t.Errorf("expected status LANDED, got %s", store.statuses["feature1"])
	}
}

func TestLandSquash(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/squash-test")
	writeFile(t, filepath.Join(wtDir, "a.txt"), "a")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "commit a")
	writeFile(t, filepath.Join(wtDir, "b.txt"), "b")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "commit b")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "squash-test",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/squash-test",
		WorktreePath: wtDir,
		Status:       StatusReady,
		Task:         "Squash test task",
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("squash-test", LandOpts{Strategy: StrategySquash})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
}

func TestLandFFOnly(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	// ff-only works when feature is ahead of base with no divergence
	wtDir := createWorktree(t, repoDir, "amux/ff-test")
	writeFile(t, filepath.Join(wtDir, "ff.txt"), "ff content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "ff commit")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "ff-test",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/ff-test",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("ff-test", LandOpts{Strategy: StrategyFFOnly})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
}

func TestLandMergeCommit(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/merge-test")
	writeFile(t, filepath.Join(wtDir, "merge.txt"), "merge content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "merge commit feature")

	// Add a commit on base so merge commit is created (not ff)
	runGit(t, repoDir, "checkout", base)
	writeFile(t, filepath.Join(repoDir, "base-change.txt"), "base")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "base change")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "merge-test",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/merge-test",
		WorktreePath: wtDir,
		Status:       StatusReady,
		Task:         "Merge commit test",
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("merge-test", LandOpts{Strategy: StrategyMerge})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
}

func TestLandBlocksOnBadStatus(t *testing.T) {
	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:     "blocked",
		Status: StatusLanding,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	_, err := pipeline.Land("blocked", LandOpts{})
	if err == nil {
		t.Error("expected error for non-landable status")
	}
	if !strings.Contains(err.Error(), "LANDING") {
		t.Errorf("expected error mentioning LANDING status, got: %v", err)
	}
}

func TestLandForceBypassesStatus(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/forced")
	writeFile(t, filepath.Join(wtDir, "forced.txt"), "forced")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "forced commit")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "forced",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/forced",
		WorktreePath: wtDir,
		Status:       StatusBlocked, // Normally not landable
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("forced", LandOpts{Force: true})
	if err != nil {
		t.Fatalf("Land with --force failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
}

func TestLandPreHookFailure(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/hookfail")
	writeFile(t, filepath.Join(wtDir, "hookfail.txt"), "content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "hook fail commit")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "hookfail",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/hookfail",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	hooks.hooks[HookPreLand] = "exit 1"

	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	_, err := pipeline.Land("hookfail", LandOpts{})
	if err == nil {
		t.Error("expected error from pre-land hook failure")
	}
	if !strings.Contains(err.Error(), "pre-land hook failed") {
		t.Errorf("expected pre-land hook error, got: %v", err)
	}
	if store.statuses["hookfail"] != StatusBlocked {
		t.Errorf("expected status BLOCKED after hook failure, got %s", store.statuses["hookfail"])
	}
}

func TestLandNoHooksSkipsHooks(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/nohooks")
	writeFile(t, filepath.Join(wtDir, "nohooks.txt"), "content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "no hooks commit")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "nohooks",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/nohooks",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	hooks.hooks[HookPreLand] = "exit 1" // Would fail if run

	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("nohooks", LandOpts{NoHooks: true})
	if err != nil {
		t.Fatalf("Land with NoHooks failed: %v", err)
	}
	if result.MergeCommit == "" {
		t.Error("expected non-empty merge commit")
	}
}

// --- DryRun tests ---

func TestDryRunClean(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/dry-clean")
	writeFile(t, filepath.Join(wtDir, "dry.txt"), "dry run content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "dry run commit")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "dry-clean",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/dry-clean",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.DryRun("dry-clean", LandOpts{})
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if !result.CanLand {
		t.Errorf("expected CanLand=true, issues: %v", result.Issues)
	}
	if len(result.FilesChanged) == 0 {
		t.Error("expected at least one changed file")
	}

	// Verify DryRun didn't mutate — base branch should still be at original commit
	currentBranch, _ := git.CurrentBranch(repoDir)
	if currentBranch != base {
		t.Errorf("DryRun changed the current branch to %s", currentBranch)
	}
}

func TestDryRunBadStatus(t *testing.T) {
	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:     "bad-status",
		Status: StatusLanding,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.DryRun("bad-status", LandOpts{})
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if result.CanLand {
		t.Error("expected CanLand=false for non-landable status")
	}
}

// --- Chain landing tests ---

func TestChainLandSuccess(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	// Create 3 worktrees with non-conflicting changes
	wt1 := createWorktree(t, repoDir, "amux/chain1")
	writeFile(t, filepath.Join(wt1, "chain1.txt"), "chain1")
	runGit(t, wt1, "add", ".")
	runGit(t, wt1, "commit", "-m", "chain1")

	wt2 := createWorktree(t, repoDir, "amux/chain2")
	writeFile(t, filepath.Join(wt2, "chain2.txt"), "chain2")
	runGit(t, wt2, "add", ".")
	runGit(t, wt2, "commit", "-m", "chain2")

	wt3 := createWorktree(t, repoDir, "amux/chain3")
	writeFile(t, filepath.Join(wt3, "chain3.txt"), "chain3")
	runGit(t, wt3, "add", ".")
	runGit(t, wt3, "commit", "-m", "chain3")

	store := newMockStore()
	for i, wt := range []string{wt1, wt2, wt3} {
		id := fmt.Sprintf("chain%d", i+1)
		store.AddWorkspace(&Workspace{
			ID:           id,
			RepoRoot:     repoDir,
			BaseBranch:   base,
			Branch:       "amux/" + id,
			WorktreePath: wt,
			Status:       StatusReady,
		})
	}

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.ChainLand([]string{"chain1", "chain2", "chain3"}, LandOpts{})
	if err != nil {
		t.Fatalf("ChainLand failed: %v", err)
	}
	if result.Failed != nil {
		t.Fatalf("expected no failures, got: %v", result.Failed.Error)
	}
	if len(result.Landed) != 3 {
		t.Errorf("expected 3 landed, got %d", len(result.Landed))
	}

	// Verify all files exist on base
	for _, f := range []string{"chain1.txt", "chain2.txt", "chain3.txt"} {
		if _, err := os.Stat(filepath.Join(repoDir, f)); err != nil {
			t.Errorf("%s should exist on base after chain landing", f)
		}
	}
}

func TestChainLandConflictStops(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	// Create 2 worktrees that conflict with each other
	wt1 := createWorktree(t, repoDir, "amux/cchain1")
	writeFile(t, filepath.Join(wt1, "shared.txt"), "version A")
	runGit(t, wt1, "add", ".")
	runGit(t, wt1, "commit", "-m", "cchain1")

	wt2 := createWorktree(t, repoDir, "amux/cchain2")
	writeFile(t, filepath.Join(wt2, "shared.txt"), "version B")
	runGit(t, wt2, "add", ".")
	runGit(t, wt2, "commit", "-m", "cchain2")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "cchain1",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/cchain1",
		WorktreePath: wt1,
		Status:       StatusReady,
	})
	store.AddWorkspace(&Workspace{
		ID:           "cchain2",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/cchain2",
		WorktreePath: wt2,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.ChainLand([]string{"cchain1", "cchain2"}, LandOpts{})
	if err != nil {
		t.Fatalf("ChainLand returned error: %v", err)
	}

	// First should have landed
	if len(result.Landed) != 1 {
		t.Errorf("expected 1 landed, got %d", len(result.Landed))
	}

	// Second should have failed due to conflict
	if result.Failed == nil {
		t.Fatal("expected failure on second workspace")
	}
	if result.Failed.WorkspaceID != "cchain2" {
		t.Errorf("expected failure on cchain2, got %s", result.Failed.WorkspaceID)
	}
}

func TestChainLandEmpty(t *testing.T) {
	store := newMockStore()
	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	_, err := pipeline.ChainLand([]string{}, LandOpts{})
	if err == nil {
		t.Error("expected error for empty chain")
	}
}

// TestLandDefaultStrategy verifies that empty strategy defaults to rebase-ff.
func TestLandDefaultStrategy(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/default-strat")
	writeFile(t, filepath.Join(wtDir, "default.txt"), "default")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "default strategy")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "default-strat",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/default-strat",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("default-strat", LandOpts{})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if result.Strategy != StrategyRebaseFF {
		t.Errorf("expected default strategy rebase-ff, got %s", result.Strategy)
	}
}

func TestLandNotFound(t *testing.T) {
	store := newMockStore()
	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	_, err := pipeline.Land("nonexistent", LandOpts{})
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestLandPostHookRuns(t *testing.T) {
	repoDir := initTestRepo(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/posthook")
	writeFile(t, filepath.Join(wtDir, "posthook.txt"), "content")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "posthook commit")

	// Create a temp file that the post-land hook will write to
	markerFile := filepath.Join(t.TempDir(), "post-hook-ran")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "posthook",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/posthook",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	hooks.hooks[HookPostLand] = fmt.Sprintf("touch %s", markerFile)

	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("posthook", LandOpts{})
	if err != nil {
		t.Fatalf("Land failed: %v", err)
	}
	if _, ok := result.HookResults[HookPostLand]; !ok {
		t.Error("expected post-land hook result")
	}
	if _, err := os.Stat(markerFile); err != nil {
		t.Error("post-land hook did not run (marker file not created)")
	}
}

// --- Push mode tests ---

// initTestRepoWithRemote creates a repo with a bare remote for push testing.
// Returns (workingRepoDir, bareRemoteDir).
func initTestRepoWithRemote(t *testing.T) (string, string) {
	t.Helper()
	// Create a bare remote
	bareDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = bareDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v\n%s", err, out)
	}

	// Clone it to get a working repo
	workDir := t.TempDir()
	cmd = exec.Command("git", "clone", bareDir, workDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	runGit(t, workDir, "config", "user.email", "test@test.com")
	runGit(t, workDir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(workDir, "README.md"), "# Test\n")
	runGit(t, workDir, "add", ".")
	runGit(t, workDir, "commit", "-m", "initial")
	runGit(t, workDir, "push", "-u", "origin", "HEAD")

	return workDir, bareDir
}

func TestLandPushMode(t *testing.T) {
	repoDir, _ := initTestRepoWithRemote(t)
	base := baseBranch(t, repoDir)

	// Create worktree with a feature branch
	wtDir := createWorktree(t, repoDir, "amux/push-feature")
	writeFile(t, filepath.Join(wtDir, "push-feature.txt"), "push feature work")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "add push feature")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "push-feature",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/push-feature",
		WorktreePath: wtDir,
		Status:       StatusReady,
		Task:         "Push feature test",
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("push-feature", LandOpts{Push: true})
	if err != nil {
		t.Fatalf("Land with Push failed: %v", err)
	}

	// Push mode should NOT produce a merge commit
	if result.MergeCommit != "" {
		t.Errorf("expected empty MergeCommit in push mode, got %q", result.MergeCommit)
	}

	// Push mode should set PushedBranch
	if result.PushedBranch != "amux/push-feature" {
		t.Errorf("expected PushedBranch 'amux/push-feature', got %q", result.PushedBranch)
	}

	// Status should end at READY, not LANDED
	if store.statuses["push-feature"] != StatusReady {
		t.Errorf("expected status READY after push, got %s", store.statuses["push-feature"])
	}

	// Verify the branch was pushed to remote
	remoteRefs := runGit(t, repoDir, "ls-remote", "--heads", "origin", "amux/push-feature")
	if !strings.Contains(remoteRefs, "amux/push-feature") {
		t.Error("expected branch to be pushed to remote")
	}
}

func TestLandPRMode(t *testing.T) {
	repoDir, _ := initTestRepoWithRemote(t)
	base := baseBranch(t, repoDir)

	wtDir := createWorktree(t, repoDir, "amux/pr-feature")
	writeFile(t, filepath.Join(wtDir, "pr-feature.txt"), "pr feature work")
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "add pr feature")

	store := newMockStore()
	store.AddWorkspace(&Workspace{
		ID:           "pr-feature",
		RepoRoot:     repoDir,
		BaseBranch:   base,
		Branch:       "amux/pr-feature",
		WorktreePath: wtDir,
		Status:       StatusReady,
	})

	hooks := newMockHookConfig()
	pipeline := NewLandingPipeline(store, &mockOps{}, hooks, 30*time.Second)

	result, err := pipeline.Land("pr-feature", LandOpts{PR: true})
	if err != nil {
		t.Fatalf("Land with PR failed: %v", err)
	}

	// PR mode behaves like push mode
	if result.MergeCommit != "" {
		t.Errorf("expected empty MergeCommit in PR mode, got %q", result.MergeCommit)
	}
	if result.PushedBranch != "amux/pr-feature" {
		t.Errorf("expected PushedBranch 'amux/pr-feature', got %q", result.PushedBranch)
	}
	if store.statuses["pr-feature"] != StatusReady {
		t.Errorf("expected status READY after PR push, got %s", store.statuses["pr-feature"])
	}
}

func init() {
	// Ensure git is available for tests
	if _, err := exec.LookPath("git"); err != nil {
		panic("git not found in PATH; required for tests")
	}
}
