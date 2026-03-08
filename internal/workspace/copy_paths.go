package workspace

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyPaths copies the listed paths from repoRoot into worktreePath.
// Paths ending in "/" are treated as directories and copied recursively.
// Other paths are treated as single files.
// Missing sources are silently skipped (best-effort).
// Files >10MB and existing destinations are skipped.
func CopyPaths(repoRoot, worktreePath string, paths []string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		src := filepath.Join(repoRoot, p)
		info, err := os.Lstat(src)
		if err != nil {
			continue
		}

		if info.IsDir() || strings.HasSuffix(p, "/") {
			copyDir(src, repoRoot, worktreePath)
		} else if info.Mode().IsRegular() {
			copySingleFile(src, filepath.Join(worktreePath, p), info)
		}
	}
}

func copyDir(srcDir, repoRoot, worktreePath string) {
	filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if fi.Size() > 10*1024*1024 {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		dst := filepath.Join(worktreePath, rel)
		if _, err := os.Stat(dst); err == nil {
			return nil
		}
		copyFile(path, dst, fi.Mode())
		return nil
	})
}

func copySingleFile(src, dst string, info os.FileInfo) {
	if info.Size() > 10*1024*1024 {
		return
	}
	if _, err := os.Stat(dst); err == nil {
		return
	}
	copyFile(src, dst, info.Mode())
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
