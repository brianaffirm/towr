package git

import "fmt"

// Rebase rebases the current branch in dir onto the given base branch.
func Rebase(dir, base string) error {
	_, err := RunGit(dir, "rebase", base)
	if err != nil {
		return fmt.Errorf("rebase onto %s failed: %w", base, err)
	}
	return nil
}

// AbortRebase aborts an in-progress rebase in dir.
func AbortRebase(dir string) error {
	_, err := RunGit(dir, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("rebase --abort failed: %w", err)
	}
	return nil
}

// MergeFF performs a fast-forward-only merge of the given branch into the
// current branch in dir.
func MergeFF(dir, branch string) error {
	_, err := RunGit(dir, "merge", "--ff-only", branch)
	if err != nil {
		return fmt.Errorf("fast-forward merge of %s failed: %w", branch, err)
	}
	return nil
}

// MergeSquash performs a squash merge of the given branch into the current
// branch in dir, then commits with the provided message.
func MergeSquash(dir, branch, message string) error {
	_, err := RunGit(dir, "merge", "--squash", branch)
	if err != nil {
		return fmt.Errorf("squash merge of %s failed: %w", branch, err)
	}
	_, err = RunGit(dir, "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("commit after squash merge failed: %w", err)
	}
	return nil
}

// MergeCommit performs a regular merge (with merge commit) of the given branch
// into the current branch in dir with the provided message.
func MergeCommit(dir, branch, message string) error {
	_, err := RunGit(dir, "merge", "--no-ff", branch, "-m", message)
	if err != nil {
		return fmt.Errorf("merge of %s failed: %w", branch, err)
	}
	return nil
}

// Fetch fetches the specified remote (or "origin" if empty) in dir.
func Fetch(dir, remote string) error {
	if remote == "" {
		remote = "origin"
	}
	_, err := RunGit(dir, "fetch", remote)
	if err != nil {
		return fmt.Errorf("fetch %s failed: %w", remote, err)
	}
	return nil
}

// CheckoutBranch checks out the given branch in dir.
func CheckoutBranch(dir, branch string) error {
	_, err := RunGit(dir, "checkout", branch)
	if err != nil {
		return fmt.Errorf("checkout %s failed: %w", branch, err)
	}
	return nil
}
