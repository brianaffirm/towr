package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRepo creates a temporary git repo with an initial commit on the given branch name.
func initBareRepo(t *testing.T, defaultBranch string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}

	run("init", "-b", defaultBranch)
	// Create an initial commit so branches are real.
	f := filepath.Join(dir, "README")
	if err := os.WriteFile(f, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	return dir
}

func TestDetectDefaultBranch_Main(t *testing.T) {
	repo := initBareRepo(t, "main")

	branch, err := DetectDefaultBranch(repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected 'main', got %q", branch)
	}
}

func TestDetectDefaultBranch_Master(t *testing.T) {
	repo := initBareRepo(t, "master")

	branch, err := DetectDefaultBranch(repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "master" {
		t.Errorf("expected 'master', got %q", branch)
	}
}

func TestDetectDefaultBranch_FallbackToHEAD(t *testing.T) {
	// Create a repo with a non-standard default branch name.
	repo := initBareRepo(t, "develop")

	branch, err := DetectDefaultBranch(repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "develop" {
		t.Errorf("expected 'develop' (current HEAD), got %q", branch)
	}
}

func TestDetectDefaultBranch_PrefersOriginHEAD(t *testing.T) {
	// Create a "remote" repo with main as default.
	remote := initBareRepo(t, "main")

	// Create a local repo that clones from the remote.
	local := t.TempDir()
	cmd := exec.Command("git", "clone", remote, local)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clone failed: %s: %v", out, err)
	}

	branch, err := DetectDefaultBranch(local)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected 'main' from origin/HEAD, got %q", branch)
	}
}
