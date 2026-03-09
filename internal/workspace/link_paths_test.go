package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkPaths_Directory(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	// Create a directory with files in repo root.
	coflowDir := filepath.Join(repo, ".coflow")
	os.MkdirAll(filepath.Join(coflowDir, "docs"), 0o755)
	os.WriteFile(filepath.Join(coflowDir, "STATUS.md"), []byte("status"), 0o644)
	os.WriteFile(filepath.Join(coflowDir, "docs", "design.md"), []byte("design"), 0o644)

	LinkPaths(repo, wtPath, []string{".coflow/"})

	// Should be a symlink.
	dst := filepath.Join(wtPath, ".coflow")
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink, got regular file/dir")
	}

	// Should resolve to the original.
	target, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != coflowDir {
		t.Errorf("symlink target = %q, want %q", target, coflowDir)
	}

	// Files should be accessible through symlink.
	data, err := os.ReadFile(filepath.Join(dst, "STATUS.md"))
	if err != nil {
		t.Fatalf("read through symlink: %v", err)
	}
	if string(data) != "status" {
		t.Errorf("content = %q, want %q", data, "status")
	}
}

func TestLinkPaths_File(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	os.WriteFile(filepath.Join(repo, "shared.toml"), []byte("config"), 0o644)

	LinkPaths(repo, wtPath, []string{"shared.toml"})

	dst := filepath.Join(wtPath, "shared.toml")
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink, got regular file")
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read through symlink: %v", err)
	}
	if string(data) != "config" {
		t.Errorf("content = %q, want %q", data, "config")
	}
}

func TestLinkPaths_SkipsExisting(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("original"), 0o644)
	// Pre-create a regular file at destination.
	os.WriteFile(filepath.Join(wtPath, "file.txt"), []byte("existing"), 0o644)

	LinkPaths(repo, wtPath, []string{"file.txt"})

	// Should NOT have been replaced.
	data, _ := os.ReadFile(filepath.Join(wtPath, "file.txt"))
	if string(data) != "existing" {
		t.Errorf("existing file was overwritten, content = %q", data)
	}
}

func TestLinkPaths_MissingSrcSkipped(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	// Should not panic or error.
	LinkPaths(repo, wtPath, []string{"nonexistent", ".missing/"})
}

func TestLinkPaths_EmptyList(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	// Should not panic or error.
	LinkPaths(repo, wtPath, nil)
	LinkPaths(repo, wtPath, []string{})
}

func TestLinkPaths_CreatesParentDirs(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	// Create nested source.
	nested := filepath.Join(repo, "deep", "nested")
	os.MkdirAll(nested, 0o755)
	os.WriteFile(filepath.Join(nested, "file.txt"), []byte("deep"), 0o644)

	LinkPaths(repo, wtPath, []string{"deep/nested"})

	dst := filepath.Join(wtPath, "deep", "nested")
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink")
	}
}

func TestPathOverlap_DetectsOverlap(t *testing.T) {
	overlap := pathOverlap(
		[]string{"CLAUDE.md", ".coflow/"},
		[]string{".coflow", "other/"},
	)
	if len(overlap) != 1 || overlap[0] != ".coflow" {
		t.Errorf("overlap = %v, want [.coflow]", overlap)
	}
}

func TestPathOverlap_NoOverlap(t *testing.T) {
	overlap := pathOverlap(
		[]string{"CLAUDE.md", "AGENTS.md"},
		[]string{".coflow/"},
	)
	if len(overlap) != 0 {
		t.Errorf("overlap = %v, want empty", overlap)
	}
}

func TestPathOverlap_EmptyLists(t *testing.T) {
	if overlap := pathOverlap(nil, []string{".coflow/"}); overlap != nil {
		t.Errorf("overlap = %v, want nil", overlap)
	}
	if overlap := pathOverlap([]string{"CLAUDE.md"}, nil); overlap != nil {
		t.Errorf("overlap = %v, want nil", overlap)
	}
}

func TestLinkPaths_WritesThroughSymlink(t *testing.T) {
	repo := t.TempDir()
	wtPath := t.TempDir()

	coflowDir := filepath.Join(repo, ".coflow")
	os.MkdirAll(coflowDir, 0o755)
	os.WriteFile(filepath.Join(coflowDir, "STATUS.md"), []byte("v1"), 0o644)

	LinkPaths(repo, wtPath, []string{".coflow"})

	// Write through the symlink.
	os.WriteFile(filepath.Join(wtPath, ".coflow", "STATUS.md"), []byte("v2"), 0o644)

	// Original should be updated.
	data, _ := os.ReadFile(filepath.Join(coflowDir, "STATUS.md"))
	if string(data) != "v2" {
		t.Errorf("write-through failed: original content = %q, want %q", data, "v2")
	}
}
