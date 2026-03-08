package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CopyGitIgnoredFiles copies files that exist in repoRoot but are excluded by
// .gitignore into the worktree. Because the worktree shares the same .gitignore,
// these copied files remain invisible to git operations (status, diff, merge).
func CopyGitIgnoredFiles(repoRoot, worktreePath string) error {
	// List ignored files that exist in the repo root.
	// --others: untracked files only
	// --ignored: show ignored files
	// --exclude-standard: use .gitignore, .git/info/exclude, etc.
	cmd := exec.Command("git", "ls-files", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing gitignored files: %s: %w", strings.TrimSpace(string(out)), err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil
	}

	for _, relPath := range strings.Split(output, "\n") {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}

		src := filepath.Join(repoRoot, relPath)
		dst := filepath.Join(worktreePath, relPath)

		// Skip if source is not a regular file (e.g. symlink, directory).
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		// Skip large files (>10MB) — likely build artifacts, not config.
		if info.Size() > 10*1024*1024 {
			continue
		}

		// Skip if destination already exists.
		if _, err := os.Stat(dst); err == nil {
			continue
		}

		if err := copyFile(src, dst, info.Mode()); err != nil {
			// Best-effort: log but don't fail the entire spawn.
			continue
		}
	}

	return nil
}

// copyFile copies a single file from src to dst, creating parent directories
// as needed and preserving the given file mode.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
