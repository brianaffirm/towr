package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunGit executes a git command in the given directory and returns combined output.
// If dir is empty, it runs in the current directory.
func RunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w\nstderr: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GetRepoRoot returns the top-level directory of the main git repository
// containing dir. If dir is inside a worktree, this resolves back to the
// main repo root (not the worktree path). If dir is empty, uses the current
// directory.
func GetRepoRoot(dir string) (string, error) {
	// Use --git-common-dir which always points to the main repo's .git,
	// even when called from inside a worktree.
	gitCommon, err := RunGit(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	gitCommon = filepath.Clean(gitCommon)

	// If it's an absolute path ending in .git, the repo root is its parent.
	if filepath.IsAbs(gitCommon) && filepath.Base(gitCommon) == ".git" {
		return filepath.Dir(gitCommon), nil
	}

	// Relative path — resolve relative to the toplevel.
	toplevel, err := RunGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	abs := filepath.Clean(filepath.Join(toplevel, gitCommon))
	if filepath.Base(abs) == ".git" {
		return filepath.Dir(abs), nil
	}

	// Fallback to toplevel.
	return filepath.Clean(toplevel), nil
}

// IsClean returns true if the working tree in dir has no uncommitted changes.
func IsClean(dir string) (bool, error) {
	out, err := RunGit(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// CurrentBranch returns the name of the currently checked-out branch in dir.
func CurrentBranch(dir string) (string, error) {
	return RunGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// HeadRef returns the SHA of HEAD in dir.
func HeadRef(dir string) (string, error) {
	return RunGit(dir, "rev-parse", "HEAD")
}

// BranchExists returns true if the given branch name exists in the repository.
func BranchExists(dir, branch string) (bool, error) {
	_, err := RunGit(dir, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		// If error contains "not a valid", branch doesn't exist
		if strings.Contains(err.Error(), "not a valid") || strings.Contains(err.Error(), "exit status") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
