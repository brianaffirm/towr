package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary git repo with one commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("setup cmd %v failed: %s: %v", args, out, err)
		}
	}
	return dir
}

func TestCreateAndDeleteBranch(t *testing.T) {
	repo := initTestRepo(t)

	branch := "amux/test-branch"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	exists, err := BranchExists(repo, branch)
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Error("branch should exist after creation")
	}

	if err := DeleteBranch(repo, branch); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	exists, err = BranchExists(repo, branch)
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if exists {
		t.Error("branch should not exist after deletion")
	}
}

func TestBranchExists_NoSuchBranch(t *testing.T) {
	repo := initTestRepo(t)

	exists, err := BranchExists(repo, "no-such-branch")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if exists {
		t.Error("expected false for nonexistent branch")
	}
}

func TestGetCurrentBranch(t *testing.T) {
	repo := initTestRepo(t)

	branch, err := GetCurrentBranch(repo)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("GetCurrentBranch = %q, want main", branch)
	}
}

func TestGetHeadRef(t *testing.T) {
	repo := initTestRepo(t)

	ref, err := GetHeadRef(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref) != 40 {
		t.Errorf("GetHeadRef returned %q (len %d), want 40 char SHA", ref, len(ref))
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repo := initTestRepo(t)

	branch := "amux/wt-test"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(t.TempDir(), "worktree-dir")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Verify worktree directory exists.
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory should exist")
	}

	// List worktrees — should include the new one.
	wts, err := ListWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks for macOS /var -> /private/var
	resolvedWtPath, _ := filepath.EvalSymlinks(wtPath)
	found := false
	for _, wt := range wts {
		resolvedWt, _ := filepath.EvalSymlinks(wt.Path)
		if resolvedWt == resolvedWtPath || wt.Path == wtPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListWorktrees did not include %q", wtPath)
	}

	// Remove it.
	if err := RemoveWorktree(repo, wtPath); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	// Verify it's gone.
	wts, _ = ListWorktrees(repo)
	for _, wt := range wts {
		if wt.Path == wtPath {
			t.Error("worktree should be removed from list")
		}
	}
}

func TestListWorktrees_MainOnly(t *testing.T) {
	repo := initTestRepo(t)

	wts, err := ListWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 {
		t.Errorf("expected 1 worktree (main), got %d", len(wts))
	}
}
