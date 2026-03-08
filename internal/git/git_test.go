package git

import (
	"os"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary git repo with an initial commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")

	// Create an initial commit
	writeFile(t, filepath.Join(dir, "README.md"), "# Test Repo\n")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "initial commit")

	return dir
}

func mustRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := RunGit(dir, args...)
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

// --- helpers tests ---

func TestGetRepoRoot(t *testing.T) {
	dir := initTestRepo(t)
	root, err := GetRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks for macOS /private/var/... vs /var/...
	expected, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(root)
	if got != expected {
		t.Errorf("GetRepoRoot = %s, want %s", got, expected)
	}
}

func TestIsClean(t *testing.T) {
	dir := initTestRepo(t)

	clean, err := IsClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("expected clean repo")
	}

	// Make it dirty
	writeFile(t, filepath.Join(dir, "dirty.txt"), "dirty")
	clean, err = IsClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Error("expected dirty repo")
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initTestRepo(t)
	branch, err := CurrentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Could be "main" or "master" depending on git config
	if branch != "main" && branch != "master" {
		t.Errorf("CurrentBranch = %s, want main or master", branch)
	}
}

func TestBranchExists(t *testing.T) {
	dir := initTestRepo(t)

	// Current branch should exist
	branch, _ := CurrentBranch(dir)
	exists, err := BranchExists(dir, branch)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected current branch to exist")
	}

	// Non-existent branch
	exists, err = BranchExists(dir, "nonexistent-branch-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected nonexistent branch to not exist")
	}
}

// --- merge tests ---

func TestRebaseAndMergeFF(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	// Create a feature branch with a commit
	mustRunGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "feature.txt"), "feature work")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "add feature")

	// Add a commit to base branch (so rebase is necessary)
	mustRunGit(t, dir, "checkout", baseBranch)
	writeFile(t, filepath.Join(dir, "base.txt"), "base work")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "base update")

	// Go back to feature branch and rebase
	mustRunGit(t, dir, "checkout", "feature")
	if err := Rebase(dir, baseBranch); err != nil {
		t.Fatalf("Rebase failed: %v", err)
	}

	// Now ff-merge into base
	if err := CheckoutBranch(dir, baseBranch); err != nil {
		t.Fatalf("CheckoutBranch failed: %v", err)
	}
	if err := MergeFF(dir, "feature"); err != nil {
		t.Fatalf("MergeFF failed: %v", err)
	}

	// Verify feature.txt exists on base
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Error("feature.txt should exist after merge")
	}
}

func TestMergeSquash(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	// Create feature branch with multiple commits
	mustRunGit(t, dir, "checkout", "-b", "feature-squash")
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "commit a")
	writeFile(t, filepath.Join(dir, "b.txt"), "b")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "commit b")

	// Squash merge into base
	mustRunGit(t, dir, "checkout", baseBranch)
	if err := MergeSquash(dir, "feature-squash", "squashed feature"); err != nil {
		t.Fatalf("MergeSquash failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Error("a.txt should exist after squash merge")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Error("b.txt should exist after squash merge")
	}
}

func TestRebaseConflictAndAbort(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	// Create conflicting changes
	mustRunGit(t, dir, "checkout", "-b", "conflict-branch")
	writeFile(t, filepath.Join(dir, "conflict.txt"), "feature version")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "feature change")

	mustRunGit(t, dir, "checkout", baseBranch)
	writeFile(t, filepath.Join(dir, "conflict.txt"), "base version")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "base change")

	// Try to rebase — should fail
	mustRunGit(t, dir, "checkout", "conflict-branch")
	err := Rebase(dir, baseBranch)
	if err == nil {
		t.Fatal("expected rebase to fail with conflict")
	}

	// Abort should succeed and leave clean state
	if err := AbortRebase(dir); err != nil {
		t.Fatalf("AbortRebase failed: %v", err)
	}

	clean, _ := IsClean(dir)
	if !clean {
		t.Error("expected clean state after rebase abort")
	}
}

func TestCheckoutBranch(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	mustRunGit(t, dir, "checkout", "-b", "test-checkout")
	if err := CheckoutBranch(dir, baseBranch); err != nil {
		t.Fatalf("CheckoutBranch failed: %v", err)
	}
	current, _ := CurrentBranch(dir)
	if current != baseBranch {
		t.Errorf("expected branch %s, got %s", baseBranch, current)
	}
}

// --- diff tests ---

func TestDiffFiles(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	mustRunGit(t, dir, "checkout", "-b", "diff-test")
	writeFile(t, filepath.Join(dir, "new-file.txt"), "new content")
	writeFile(t, filepath.Join(dir, "README.md"), "# Updated\n")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "changes for diff")

	files, err := DiffFiles(dir, baseBranch, "diff-test")
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 changed files, got %d: %v", len(files), files)
	}
}

func TestDiffStat(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	mustRunGit(t, dir, "checkout", "-b", "stat-test")
	writeFile(t, filepath.Join(dir, "stats.txt"), "line1\nline2\nline3\n")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "add stats file")

	stat, err := DiffStat(dir, baseBranch, "stat-test")
	if err != nil {
		t.Fatalf("DiffStat failed: %v", err)
	}
	if stat.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if stat.Raw == "" {
		t.Error("expected non-empty raw output")
	}
}

func TestDiffFilesNoDiff(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	// Same ref — no diff
	files, err := DiffFiles(dir, baseBranch, baseBranch)
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no changed files, got %d", len(files))
	}
}

func TestMergeCommit(t *testing.T) {
	dir := initTestRepo(t)
	baseBranch, _ := CurrentBranch(dir)

	// Create feature branch
	mustRunGit(t, dir, "checkout", "-b", "merge-commit-test")
	writeFile(t, filepath.Join(dir, "merged.txt"), "merged content")
	mustRunGit(t, dir, "add", ".")
	mustRunGit(t, dir, "commit", "-m", "feature for merge commit")

	// Go back to base and merge with commit
	mustRunGit(t, dir, "checkout", baseBranch)
	if err := MergeCommit(dir, "merge-commit-test", "merge commit message"); err != nil {
		t.Fatalf("MergeCommit failed: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(filepath.Join(dir, "merged.txt")); err != nil {
		t.Error("merged.txt should exist after merge commit")
	}
}
