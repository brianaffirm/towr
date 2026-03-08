package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopyGitIgnoredFiles(t *testing.T) {
	repo := initTestRepoWithFile(t)

	// Create a .gitignore that ignores CLAUDE.md and a directory.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("CLAUDE.md\nsecrets/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, repo, "add", ".gitignore")
	runGitCmd(t, repo, "commit", "-m", "add gitignore")

	// Create gitignored files in the repo root.
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Agent context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "secrets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "secrets", "key.txt"), []byte("secret\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a worktree.
	branch := "amux/copy-test"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	// Verify gitignored files don't exist yet in worktree.
	if _, err := os.Stat(filepath.Join(wtPath, "CLAUDE.md")); err == nil {
		t.Fatal("CLAUDE.md should not exist in worktree before copy")
	}

	// Copy gitignored files.
	if err := CopyGitIgnoredFiles(repo, wtPath); err != nil {
		t.Fatalf("CopyGitIgnoredFiles: %v", err)
	}

	// Verify files were copied.
	content, err := os.ReadFile(filepath.Join(wtPath, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not copied: %v", err)
	}
	if string(content) != "# Agent context\n" {
		t.Errorf("CLAUDE.md content = %q, want %q", string(content), "# Agent context\n")
	}

	content, err = os.ReadFile(filepath.Join(wtPath, "secrets", "key.txt"))
	if err != nil {
		t.Fatalf("secrets/key.txt not copied: %v", err)
	}
	if string(content) != "secret\n" {
		t.Errorf("secrets/key.txt content = %q, want %q", string(content), "secret\n")
	}

	// Verify the copied files are invisible to git status in the worktree.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status failed: %s: %v", out, err)
	}
	if len(out) > 0 {
		t.Errorf("expected clean git status in worktree, got:\n%s", string(out))
	}
}

func TestCopyGitIgnoredFiles_SkipsExisting(t *testing.T) {
	repo := initTestRepoWithFile(t)

	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("CLAUDE.md\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, repo, "add", ".gitignore")
	runGitCmd(t, repo, "commit", "-m", "add gitignore")
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	branch := "amux/skip-test"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	// Pre-create the file in the worktree with different content.
	if err := os.WriteFile(filepath.Join(wtPath, "CLAUDE.md"), []byte("custom\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyGitIgnoredFiles(repo, wtPath); err != nil {
		t.Fatal(err)
	}

	// Should preserve existing content, not overwrite.
	content, err := os.ReadFile(filepath.Join(wtPath, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "custom\n" {
		t.Errorf("expected existing file preserved, got %q", string(content))
	}
}

func TestCopyGitIgnoredFiles_NoIgnoredFiles(t *testing.T) {
	repo := initTestRepoWithFile(t)

	wtPath := filepath.Join(t.TempDir(), "wt")
	branch := "amux/no-ignored"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	// Should succeed with nothing to copy.
	if err := CopyGitIgnoredFiles(repo, wtPath); err != nil {
		t.Fatalf("CopyGitIgnoredFiles with no ignored files: %v", err)
	}
}
