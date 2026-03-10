package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var migrateOnce sync.Once

// TowrHome returns the root towr directory, defaulting to ~/.towr.
// Respects the TOWR_HOME environment variable for overriding.
// On first call, auto-migrates from ~/.amux if needed.
func TowrHome() string {
	if v := os.Getenv("TOWR_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".towr")
	}
	migrateOnce.Do(func() { migrateFromAmux(home) })
	return filepath.Join(home, ".towr")
}

// migrateFromAmux renames ~/.amux to ~/.towr if the old path exists
// and the new path does not. Prints a one-time notice.
func migrateFromAmux(home string) {
	oldPath := filepath.Join(home, ".amux")
	newPath := filepath.Join(home, ".towr")
	if _, err := os.Stat(newPath); err == nil {
		return // already migrated
	}
	if _, err := os.Stat(oldPath); err != nil {
		return // nothing to migrate
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not migrate %s → %s: %v\n", oldPath, newPath, err)
		return
	}
	fmt.Fprintf(os.Stderr, "Migrated %s → %s\n", oldPath, newPath)
}

// RepoStatePath returns the per-repo state directory: ~/.towr/repos/<hash>/
func RepoStatePath(repoRoot string) string {
	return filepath.Join(TowrHome(), "repos", RepoHash(repoRoot))
}

// WorktreeRoot returns the directory where worktrees are stored.
// Defaults to ~/.towr/worktrees but can be overridden in config.
func WorktreeRoot() string {
	return filepath.Join(TowrHome(), "worktrees")
}

// RepoHash produces a short deterministic hash of a repo root path,
// used to namespace per-repo state without filesystem-unfriendly characters.
func RepoHash(repoRoot string) string {
	h := sha256.Sum256([]byte(repoRoot))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars — short but collision-resistant
}

// EnsureTowrDirs creates the core towr directory structure if it doesn't exist.
func EnsureTowrDirs() error {
	dirs := []string{
		TowrHome(),
		filepath.Join(TowrHome(), "repos"),
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
	return filepath.Join(TowrHome(), "global-config.toml")
}

// RepoConfigPath returns the path to a repo-specific config file.
func RepoConfigPath(repoRoot string) string {
	return filepath.Join(RepoStatePath(repoRoot), "config.toml")
}
