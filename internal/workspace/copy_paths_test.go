package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyPaths_Files(t *testing.T) {
	repo := initTestRepoWithFile(t)

	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Agents\n"), 0644); err != nil {
		t.Fatal(err)
	}

	branch := "towr/copy-paths-test"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	CopyPaths(repo, wtPath, []string{"CLAUDE.md", "AGENTS.md"})

	content, err := os.ReadFile(filepath.Join(wtPath, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not copied: %v", err)
	}
	if string(content) != "# Context\n" {
		t.Errorf("CLAUDE.md content = %q", string(content))
	}

	content, err = os.ReadFile(filepath.Join(wtPath, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not copied: %v", err)
	}
	if string(content) != "# Agents\n" {
		t.Errorf("AGENTS.md content = %q", string(content))
	}
}

func TestCopyPaths_Directory(t *testing.T) {
	repo := initTestRepoWithFile(t)

	if err := os.MkdirAll(filepath.Join(repo, ".coflow", "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".coflow", "STATUS.md"), []byte("# Status\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".coflow", "docs", "design.md"), []byte("# Design\n"), 0644); err != nil {
		t.Fatal(err)
	}

	branch := "towr/copy-dir-test"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	CopyPaths(repo, wtPath, []string{".coflow/"})

	for _, tc := range []struct{ path, content string }{
		{".coflow/STATUS.md", "# Status\n"},
		{".coflow/docs/design.md", "# Design\n"},
	} {
		got, err := os.ReadFile(filepath.Join(wtPath, tc.path))
		if err != nil {
			t.Errorf("%s not copied: %v", tc.path, err)
			continue
		}
		if string(got) != tc.content {
			t.Errorf("%s = %q, want %q", tc.path, string(got), tc.content)
		}
	}
}

func TestCopyPaths_SkipsExisting(t *testing.T) {
	repo := initTestRepoWithFile(t)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	branch := "towr/skip-existing"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(wtPath, "CLAUDE.md"), []byte("custom\n"), 0644); err != nil {
		t.Fatal(err)
	}

	CopyPaths(repo, wtPath, []string{"CLAUDE.md"})

	content, _ := os.ReadFile(filepath.Join(wtPath, "CLAUDE.md"))
	if string(content) != "custom\n" {
		t.Errorf("existing file overwritten, got %q", string(content))
	}
}

func TestCopyPaths_MissingSrcSkipped(t *testing.T) {
	repo := initTestRepoWithFile(t)

	branch := "towr/missing-src"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	// Should not panic or error.
	CopyPaths(repo, wtPath, []string{"nonexistent.md", ".missing/"})
}

func TestCopyPaths_EmptyList(t *testing.T) {
	repo := initTestRepoWithFile(t)

	branch := "towr/empty-paths"
	if err := CreateBranch(repo, branch, "main"); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(repo, wtPath, branch); err != nil {
		t.Fatal(err)
	}

	CopyPaths(repo, wtPath, nil)
	CopyPaths(repo, wtPath, []string{})
}
