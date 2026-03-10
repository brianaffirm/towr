package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brianho/amux/internal/git"
)

// NudgeResult describes whether the user should be nudged to adopt.
type NudgeResult struct {
	ShouldNudge bool
	Branch      string
	Message     string
}

// CheckNudge determines if the current directory has untracked work that
// should be adopted into amux. Designed to run on every shell prompt (<50ms).
func CheckNudge(cwd string) NudgeResult {
	// 1. Is this a git repo?
	repoRoot, err := git.GetRepoRoot(cwd)
	if err != nil {
		return NudgeResult{}
	}

	// 2. What branch? Skip default branches.
	branch, err := git.CurrentBranch(cwd)
	if err != nil || branch == "" {
		return NudgeResult{}
	}
	if isDefaultBranch(branch) {
		return NudgeResult{}
	}

	// 3. Rate limit: check cache file.
	if !shouldShowNudge(repoRoot, branch) {
		return NudgeResult{}
	}

	// 4. Are there uncommitted changes or commits on branch?
	hasChanges := false
	out, err := git.RunGit(cwd, "status", "--porcelain")
	if err == nil && strings.TrimSpace(out) != "" {
		hasChanges = true
	}

	if !hasChanges {
		// Check if branch has commits not on default branch.
		defaultBranch, err := DetectDefaultBranch(repoRoot)
		if err == nil {
			countOut, err := git.RunGit(repoRoot, "rev-list", "--count", defaultBranch+".."+branch)
			if err == nil {
				count, _ := strconv.Atoi(strings.TrimSpace(countOut))
				hasChanges = count > 0
			}
		}
	}

	if !hasChanges {
		return NudgeResult{}
	}

	// Record that we showed the nudge.
	recordNudge(repoRoot, branch)

	return NudgeResult{
		ShouldNudge: true,
		Branch:      branch,
		Message:     fmt.Sprintf("amux: untracked work on %s — 'amux adopt' to track", branch),
	}
}

func isDefaultBranch(branch string) bool {
	switch branch {
	case "main", "master", "develop", "development":
		return true
	}
	return false
}

// nudgeCachePath returns the path to the nudge rate-limit cache.
func nudgeCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".amux", "nudge-cache")
}

// shouldShowNudge checks if we've shown a nudge for this branch recently (5 min).
func shouldShowNudge(repoRoot, branch string) bool {
	data, err := os.ReadFile(nudgeCachePath())
	if err != nil {
		return true
	}

	key := repoRoot + ":" + branch
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] == key {
			ts, err := time.Parse(time.RFC3339, parts[1])
			if err == nil && time.Since(ts) < 5*time.Minute {
				return false
			}
		}
	}
	return true
}

// recordNudge writes the current time for this branch to the cache.
func recordNudge(repoRoot, branch string) {
	cachePath := nudgeCachePath()
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)

	key := repoRoot + ":" + branch
	now := time.Now().UTC().Format(time.RFC3339)

	// Read existing, filter out this key and stale entries.
	var lines []string
	if data, err := os.ReadFile(cachePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 && parts[0] != key {
				ts, err := time.Parse(time.RFC3339, parts[1])
				if err == nil && time.Since(ts) < 1*time.Hour {
					lines = append(lines, line)
				}
			}
		}
	}
	lines = append(lines, key+"|"+now)
	_ = os.WriteFile(cachePath, []byte(strings.Join(lines, "\n")), 0o644)
}
