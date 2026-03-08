package git

import (
	"fmt"
	"strings"
)

// Push pushes the given branch to the remote.
func Push(dir, remote, branch string) error {
	if remote == "" {
		remote = "origin"
	}
	_, err := RunGit(dir, "push", "-u", remote, branch)
	if err != nil {
		return fmt.Errorf("push %s to %s: %w", branch, remote, err)
	}
	return nil
}

// GetRemoteURL returns the URL for the given remote (default: origin).
func GetRemoteURL(dir, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	return RunGit(dir, "remote", "get-url", remote)
}

// BuildPRURL constructs a GitHub PR creation URL from a remote URL.
// Returns the URL and true if the remote is GitHub, or ("", false) otherwise.
func BuildPRURL(remoteURL, baseBranch, headBranch string) (string, bool) {
	owner, repo, ok := parseGitHubRemote(remoteURL)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s?expand=1",
		owner, repo, baseBranch, headBranch), true
}

// parseGitHubRemote extracts owner/repo from a GitHub remote URL.
func parseGitHubRemote(url string) (owner, repo string, ok bool) {
	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
		return "", "", false
	}

	// HTTPS format: https://github.com/owner/repo.git
	if strings.Contains(url, "github.com/") {
		idx := strings.Index(url, "github.com/")
		path := url[idx+len("github.com/"):]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
		return "", "", false
	}

	return "", "", false
}
