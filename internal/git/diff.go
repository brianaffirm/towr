package git

import (
	"fmt"
	"strings"
)

// DiffStat returns the diff stat (summary of changes) between two refs in dir.
type DiffStatResult struct {
	Summary string // Human-readable summary (e.g., "3 files changed, 10 insertions(+), 2 deletions(-)")
	Raw     string // Full --stat output
}

// DiffStat returns the diff stat between two refs.
func DiffStat(dir, base, head string) (*DiffStatResult, error) {
	out, err := RunGit(dir, "diff", "--stat", base+"..."+head)
	if err != nil {
		return nil, fmt.Errorf("diff --stat failed: %w", err)
	}
	lines := strings.Split(out, "\n")
	summary := ""
	if len(lines) > 0 {
		summary = lines[len(lines)-1]
	}
	return &DiffStatResult{
		Summary: strings.TrimSpace(summary),
		Raw:     out,
	}, nil
}

// DiffFiles returns the list of files changed between two refs.
func DiffFiles(dir, base, head string) ([]string, error) {
	out, err := RunGit(dir, "diff", "--name-only", base+"..."+head)
	if err != nil {
		return nil, fmt.Errorf("diff --name-only failed: %w", err)
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// HasConflictsWith uses git merge-tree to check if merging head into base
// would produce conflicts, without actually performing the merge.
// Returns the list of conflicting file paths (empty if no conflicts).
func HasConflictsWith(dir, base, head string) ([]string, error) {
	// Get merge base
	mergeBase, err := RunGit(dir, "merge-base", base, head)
	if err != nil {
		return nil, fmt.Errorf("merge-base failed: %w", err)
	}

	// git merge-tree returns the merge result to stdout.
	// With the 3-way form (merge-base, branch1, branch2), conflicts show up
	// in the output with conflict markers.
	out, mergeErr := RunGit(dir, "merge-tree", mergeBase, base, head)

	// Parse the output for conflict indicators
	var conflicts []string
	if out != "" {
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			// merge-tree outputs lines starting with "+" for files that have
			// content that needs merging. Actual conflicts show as sections
			// with "<<<<<<<" markers in the output, but we can detect conflict
			// files by looking for "changed in both" or conflict marker patterns.
			if strings.Contains(line, "changed in both") {
				// Extract filename - format: "changed in both"
				// The filename typically appears on lines like:
				//   +<file> ... changed in both
				parts := strings.Fields(line)
				if len(parts) > 0 {
					// Try to extract a plausible filename
					for _, p := range parts {
						if !strings.HasPrefix(p, "+") && !strings.HasPrefix(p, "-") &&
							!strings.HasPrefix(p, "<") && !strings.HasPrefix(p, ">") &&
							!strings.HasPrefix(p, "=") && p != "changed" && p != "in" &&
							p != "both" && p != "" {
							conflicts = append(conflicts, p)
							break
						}
					}
				}
			}
			if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, "<<<<<<< .") {
				// Presence of conflict markers means there are conflicts
				// but we already capture via "changed in both"
			}
		}
	}

	// If merge-tree returned an error AND we found no conflicts from parsing,
	// return the error
	if mergeErr != nil && len(conflicts) == 0 {
		return nil, mergeErr
	}

	return conflicts, nil
}
