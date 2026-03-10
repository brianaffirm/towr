package workspace

import (
	"fmt"
	"os/exec"
	"strings"
)

// BranchName returns the towr-namespaced branch name for a workspace ID.
func BranchName(workspaceID string) string {
	return "towr/" + workspaceID
}

// CreateBranch creates a new branch at the given base ref in the repository.
func CreateBranch(repoRoot, name, base string) error {
	cmd := exec.Command("git", "branch", name, base)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch create failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DeleteBranch deletes a branch. It uses -D (force) to handle unmerged branches.
func DeleteBranch(repoRoot, name string) error {
	cmd := exec.Command("git", "branch", "-D", name)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch delete failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GetCurrentBranch returns the current branch name for the repo at repoRoot.
func GetCurrentBranch(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchExists checks whether a branch with the given name exists in the repo.
func BranchExists(repoRoot, name string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+name)
	cmd.Dir = repoRoot
	err := cmd.Run()
	if err != nil {
		// Exit code 128 means ref doesn't exist — not an error for our purposes.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return false, nil
		}
		// Other non-zero exits also mean the branch doesn't exist.
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("git rev-parse failed: %w", err)
	}
	return true, nil
}

// DetectDefaultBranch determines the default branch for a repository.
// Detection order:
//  1. git symbolic-ref refs/remotes/origin/HEAD (works if remote exists)
//  2. Check if "main" branch exists locally
//  3. Check if "master" branch exists locally
//  4. Fall back to current HEAD branch
func DetectDefaultBranch(repoRoot string) (string, error) {
	// Try symbolic-ref for origin's default branch.
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = repoRoot
	if out, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		// ref looks like "refs/remotes/origin/main" — extract the branch name.
		if parts := strings.SplitN(ref, "refs/remotes/origin/", 2); len(parts) == 2 && parts[1] != "" {
			return parts[1], nil
		}
	}

	// Check common branch names.
	for _, name := range []string{"main", "master"} {
		exists, err := BranchExists(repoRoot, name)
		if err != nil {
			return "", fmt.Errorf("checking branch %s: %w", name, err)
		}
		if exists {
			return name, nil
		}
	}

	// Last resort: current HEAD branch.
	branch, err := GetCurrentBranch(repoRoot)
	if err != nil {
		return "", fmt.Errorf("detecting default branch: %w", err)
	}
	return branch, nil
}

// GetHeadRef returns the commit SHA that HEAD points to.
func GetHeadRef(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}
