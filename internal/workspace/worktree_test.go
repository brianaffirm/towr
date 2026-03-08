package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepoWithFile creates a test repo with a committed tracked file.
func initTestRepoWithFile(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t)
	// Add a tracked file so we can test modifications.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "README"},
		{"git", "commit", "-m", "add readme"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s: %v", args, out, err)
		}
	}
	return dir
}

func TestWorktreeStatus_Clean(t *testing.T) {
	dir := initTestRepoWithFile(t)
	mod, untrk, err := WorktreeStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mod != 0 || untrk != 0 {
		t.Errorf("expected clean repo, got modified=%d untracked=%d", mod, untrk)
	}
}

func TestWorktreeStatus_Modified(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Modify an existing tracked file (unstaged).
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Stage a new file.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}

	mod, untrk, err := WorktreeStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mod != 2 {
		t.Errorf("expected 2 modified files, got %d", mod)
	}
	if untrk != 0 {
		t.Errorf("expected 0 untracked files, got %d", untrk)
	}
}

func TestWorktreeStatus_Untracked(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Create untracked files.
	for _, name := range []string{"new1.txt", "new2.txt", "new3.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("new\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mod, untrk, err := WorktreeStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mod != 0 {
		t.Errorf("expected 0 modified files, got %d", mod)
	}
	if untrk != 3 {
		t.Errorf("expected 3 untracked files, got %d", untrk)
	}
}

func TestWorktreeStatus_Mixed(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Modify tracked file.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create untracked files.
	if err := os.WriteFile(filepath.Join(dir, "untracked1.txt"), []byte("u\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked2.txt"), []byte("u\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mod, untrk, err := WorktreeStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mod != 1 {
		t.Errorf("expected 1 modified file, got %d", mod)
	}
	if untrk != 2 {
		t.Errorf("expected 2 untracked files, got %d", untrk)
	}
}

func TestDetailedStatus_Clean(t *testing.T) {
	dir := initTestRepoWithFile(t)
	ds, err := WorktreeDetailedStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Staged != 0 || ds.Unstaged != 0 || ds.Untracked != 0 {
		t.Errorf("expected all zeros, got staged=%d unstaged=%d untracked=%d", ds.Staged, ds.Unstaged, ds.Untracked)
	}
}

func TestDetailedStatus_StagedOnly(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Create and stage a new file.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "new.txt")

	ds, err := WorktreeDetailedStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Staged != 1 {
		t.Errorf("staged = %d, want 1", ds.Staged)
	}
	if ds.Unstaged != 0 {
		t.Errorf("unstaged = %d, want 0", ds.Unstaged)
	}
	if ds.Untracked != 0 {
		t.Errorf("untracked = %d, want 0", ds.Untracked)
	}
}

func TestDetailedStatus_UnstagedOnly(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Modify tracked file without staging.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ds, err := WorktreeDetailedStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Staged != 0 {
		t.Errorf("staged = %d, want 0", ds.Staged)
	}
	if ds.Unstaged != 1 {
		t.Errorf("unstaged = %d, want 1", ds.Unstaged)
	}
	if ds.Untracked != 0 {
		t.Errorf("untracked = %d, want 0", ds.Untracked)
	}
}

func TestDetailedStatus_Mixed(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Stage a new file.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("s\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "staged.txt")

	// Modify tracked file without staging.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create untracked files.
	for _, name := range []string{"u1.txt", "u2.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("u\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ds, err := WorktreeDetailedStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Staged != 1 {
		t.Errorf("staged = %d, want 1", ds.Staged)
	}
	if ds.Unstaged != 1 {
		t.Errorf("unstaged = %d, want 1", ds.Unstaged)
	}
	if ds.Untracked != 2 {
		t.Errorf("untracked = %d, want 2", ds.Untracked)
	}
}

func TestIsBranchMerged_NotMerged(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Record base ref before creating feature branch.
	baseRef := getGitOutput(t, dir, "rev-parse", "HEAD")

	// Create a branch with a new commit.
	runGitCmd(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "feature.txt")
	runGitCmd(t, dir, "commit", "-m", "feature commit")

	if IsBranchMerged(dir, "main", "feature", baseRef) {
		t.Error("expected branch not merged, got merged")
	}
}

func TestIsBranchMerged_Merged(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Record base ref before creating feature branch.
	baseRef := getGitOutput(t, dir, "rev-parse", "HEAD")

	// Create a branch with a commit, then merge it.
	runGitCmd(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "feature.txt")
	runGitCmd(t, dir, "commit", "-m", "feature commit")

	runGitCmd(t, dir, "checkout", "main")
	runGitCmd(t, dir, "merge", "--no-ff", "feature", "-m", "merge feature")

	if !IsBranchMerged(dir, "main", "feature", baseRef) {
		t.Error("expected branch merged, got not merged")
	}
}

func TestIsBranchMerged_NeverWorkedOn(t *testing.T) {
	dir := initTestRepoWithFile(t)

	// Record base ref and create a branch with no commits.
	baseRef := getGitOutput(t, dir, "rev-parse", "HEAD")
	runGitCmd(t, dir, "branch", "feature")

	// Even after main moves forward, feature should not be "merged".
	if err := os.WriteFile(filepath.Join(dir, "main-work.txt"), []byte("w\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "main-work.txt")
	runGitCmd(t, dir, "commit", "-m", "main moved forward")

	if IsBranchMerged(dir, "main", "feature", baseRef) {
		t.Error("expected branch NOT merged (never worked on), got merged")
	}
}

func TestIsBranchMerged_EmptyArgs(t *testing.T) {
	if IsBranchMerged("", "", "", "") {
		t.Error("expected false for empty args")
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
}

func getGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
