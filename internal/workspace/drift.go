package workspace

import (
	"strconv"
	"strings"

	"github.com/brianho/amux/internal/git"
)

// DriftCount returns how many commits the base branch is ahead of the branch point.
// A higher number means more risk of merge conflicts.
func DriftCount(repoRoot, baseBranch, branch string) int {
	if repoRoot == "" || baseBranch == "" || branch == "" {
		return 0
	}
	out, err := git.RunGit(repoRoot, "rev-list", "--count", branch+".."+baseBranch)
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(out))
	return count
}
