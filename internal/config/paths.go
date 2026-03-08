package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// AmuxHome returns the root amux directory, defaulting to ~/.amux.
// Respects the AMUX_HOME environment variable for overriding.
func AmuxHome() string {
	if v := os.Getenv("AMUX_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback — should not happen in practice.
		return filepath.Join(os.TempDir(), ".amux")
	}
	return filepath.Join(home, ".amux")
}

// RepoStatePath returns the per-repo state directory: ~/.amux/repos/<hash>/
func RepoStatePath(repoRoot string) string {
	return filepath.Join(AmuxHome(), "repos", RepoHash(repoRoot))
}

// WorktreeRoot returns the directory where worktrees are stored.
// Defaults to ~/.amux/worktrees but can be overridden in config.
func WorktreeRoot() string {
	return filepath.Join(AmuxHome(), "worktrees")
}

// RepoHash produces a short deterministic hash of a repo root path,
// used to namespace per-repo state without filesystem-unfriendly characters.
func RepoHash(repoRoot string) string {
	h := sha256.Sum256([]byte(repoRoot))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars — short but collision-resistant
}

// EnsureAmuxDirs creates the core amux directory structure if it doesn't exist.
func EnsureAmuxDirs() error {
	dirs := []string{
		AmuxHome(),
		filepath.Join(AmuxHome(), "repos"),
		WorktreeRoot(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}
	return nil
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	return filepath.Join(AmuxHome(), "global-config.toml")
}

// RepoConfigPath returns the path to a repo-specific config file.
func RepoConfigPath(repoRoot string) string {
	return filepath.Join(RepoStatePath(repoRoot), "config.toml")
}
