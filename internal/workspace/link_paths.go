package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// pathOverlap returns paths that appear in both lists (normalized, trailing slash stripped).
func pathOverlap(copyPaths, linkPaths []string) []string {
	if len(copyPaths) == 0 || len(linkPaths) == 0 {
		return nil
	}
	norm := func(p string) string {
		return strings.TrimRight(strings.TrimSpace(p), "/")
	}
	set := make(map[string]bool, len(copyPaths))
	for _, p := range copyPaths {
		if n := norm(p); n != "" {
			set[n] = true
		}
	}
	var overlap []string
	for _, p := range linkPaths {
		if n := norm(p); n != "" && set[n] {
			overlap = append(overlap, n)
		}
	}
	return overlap
}

// LinkPaths creates symlinks in worktreePath pointing back to repoRoot for each
// listed path. Missing sources and existing destinations are silently skipped.
func LinkPaths(repoRoot, worktreePath string, paths []string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Normalize: strip trailing slash for consistent path joining.
		clean := strings.TrimRight(p, "/")

		src := filepath.Join(repoRoot, clean)
		if _, err := os.Lstat(src); err != nil {
			continue // source doesn't exist, skip
		}

		dst := filepath.Join(worktreePath, clean)
		if _, err := os.Lstat(dst); err == nil {
			continue // destination already exists, skip
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}

		os.Symlink(src, dst)
	}
}
