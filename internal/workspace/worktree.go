package workspace

import (
	"fmt"
	"os/exec"
	"strings"
)

// CreateWorktree creates a new git worktree at worktreePath on the given branch.
// The branch must already exist.
func CreateWorktree(repoRoot, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", worktreePath, branch)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// RemoveWorktree removes a git worktree. It runs force removal to handle
// unclean worktrees.
func RemoveWorktree(repoRoot, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// WorktreeStatus checks a worktree directory for uncommitted changes by running
// git status --porcelain. It returns counts of modified (staged/unstaged) and
// untracked files.
func WorktreeStatus(worktreePath string) (modified int, untracked int, err error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("git status failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		if line[0] == '?' && line[1] == '?' {
			untracked++
		} else {
			modified++
		}
	}
	return modified, untracked, nil
}

// DetailedStatus holds granular worktree status counts.
type DetailedStatus struct {
	Staged    int // files with staged (index) changes
	Unstaged  int // files with unstaged (worktree) changes
	Untracked int // untracked files
}

// WorktreeDetailedStatus checks a worktree for staged, unstaged, and untracked
// files using git status --porcelain. The two-character status codes are:
// XY where X=index status, Y=worktree status.
func WorktreeDetailedStatus(worktreePath string) (DetailedStatus, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DetailedStatus{}, fmt.Errorf("git status failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var s DetailedStatus
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		x, y := line[0], line[1]
		if x == '?' && y == '?' {
			s.Untracked++
			continue
		}
		// X column: staged changes (anything other than ' ' or '?')
		if x != ' ' && x != '?' {
			s.Staged++
		}
		// Y column: unstaged changes (anything other than ' ' or '?')
		if y != ' ' && y != '?' {
			s.Unstaged++
		}
	}
	return s, nil
}

// IsBranchMerged checks whether the workspace branch has been merged into
// the base branch. baseRef is the commit SHA the branch was forked from
// (at spawn time). Returns true only if the branch has commits beyond
// baseRef that are now reachable from baseBranch. A branch that was never
// worked on (tip == baseRef) is NOT considered "merged".
func IsBranchMerged(repoRoot, baseBranch, branch, baseRef string) bool {
	if repoRoot == "" || baseBranch == "" || branch == "" {
		return false
	}

	// Get the branch tip.
	tipCmd := exec.Command("git", "rev-parse", branch)
	tipCmd.Dir = repoRoot
	tipOut, err := tipCmd.CombinedOutput()
	if err != nil {
		return false
	}
	branchTip := strings.TrimSpace(string(tipOut))

	// If the branch tip equals the spawn point, no work was ever done.
	if baseRef != "" && branchTip == baseRef {
		return false
	}

	// Check if branch is merged into base.
	mergedCmd := exec.Command("git", "branch", "--merged", baseBranch, "--list", branch)
	mergedCmd.Dir = repoRoot
	mergedOut, err := mergedCmd.CombinedOutput()
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(mergedOut)) == "" {
		return false
	}

	// Exclude branches at the same tip as base (identical, not "merged").
	baseCmd := exec.Command("git", "rev-parse", baseBranch)
	baseCmd.Dir = repoRoot
	baseOut, err := baseCmd.CombinedOutput()
	if err != nil {
		return false
	}
	if branchTip == strings.TrimSpace(string(baseOut)) {
		return false
	}

	return true
}

// WorktreeInfo holds parsed output from git worktree list.
type WorktreeInfo struct {
	Path   string
	Head   string
	Branch string
	Bare   bool
}

// ListWorktrees returns all worktrees for the repository at repoRoot.
func ListWorktrees(repoRoot string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var worktrees []WorktreeInfo
	var current *WorktreeInfo

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current != nil {
				worktrees = append(worktrees, *current)
				current = nil
			}
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			current = &WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		} else if current != nil {
			if strings.HasPrefix(line, "HEAD ") {
				current.Head = strings.TrimPrefix(line, "HEAD ")
			} else if strings.HasPrefix(line, "branch ") {
				current.Branch = strings.TrimPrefix(line, "branch ")
			} else if line == "bare" {
				current.Bare = true
			}
		}
	}
	if current != nil {
		worktrees = append(worktrees, *current)
	}

	return worktrees, nil
}
