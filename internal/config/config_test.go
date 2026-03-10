package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Defaults.MergeStrategy != "rebase-ff" {
		t.Errorf("MergeStrategy = %q, want rebase-ff", cfg.Defaults.MergeStrategy)
	}
	if cfg.Defaults.BaseBranch != "" {
		t.Errorf("BaseBranch = %q, want empty (auto-detected at runtime)", cfg.Defaults.BaseBranch)
	}
	if cfg.Cleanup.StaleThreshold != "7d" {
		t.Errorf("StaleThreshold = %q, want 7d", cfg.Cleanup.StaleThreshold)
	}
}

func TestLoadFile_Missing(t *testing.T) {
	cfg, err := LoadFile("/nonexistent/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	// Should return defaults.
	if cfg.Defaults.MergeStrategy != "rebase-ff" {
		t.Errorf("expected default merge strategy, got %q", cfg.Defaults.MergeStrategy)
	}
}

func TestLoadFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[defaults]
merge_strategy = "squash"
base_branch = "develop"

[hooks]
pre_land = "make test"

[agents.claude]
command = "claude"
args = ["--task"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want squash", cfg.Defaults.MergeStrategy)
	}
	if cfg.Defaults.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want develop", cfg.Defaults.BaseBranch)
	}
	if cfg.Hooks.PreLand != "make test" {
		t.Errorf("PreLand = %q, want 'make test'", cfg.Hooks.PreLand)
	}
	if cfg.Agents == nil || cfg.Agents["claude"].Command != "claude" {
		t.Errorf("Agents[claude].Command not loaded correctly")
	}
}

func TestLoadFile_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("not valid [[ toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadGlobal_NoFile(t *testing.T) {
	t.Setenv("TOWR_HOME", t.TempDir())
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.MergeStrategy != "rebase-ff" {
		t.Errorf("expected defaults, got %q", cfg.Defaults.MergeStrategy)
	}
}

func TestLoadRepo_MergesConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TOWR_HOME", tmp)

	// Write global config.
	globalPath := filepath.Join(tmp, "global-config.toml")
	globalContent := `
[defaults]
merge_strategy = "rebase-ff"

[hooks]
post_create = "npm install"
`
	if err := os.WriteFile(globalPath, []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write repo config.
	repoRoot := "/my/repo"
	repoDir := filepath.Join(tmp, "repos", RepoHash(repoRoot))
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoContent := `
[hooks]
pre_land = "make test"

[defaults]
base_branch = "develop"
`
	if err := os.WriteFile(filepath.Join(repoDir, "config.toml"), []byte(repoContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Global value preserved.
	if cfg.Hooks.PostCreate != "npm install" {
		t.Errorf("PostCreate = %q, want 'npm install'", cfg.Hooks.PostCreate)
	}
	// Repo value overlaid.
	if cfg.Hooks.PreLand != "make test" {
		t.Errorf("PreLand = %q, want 'make test'", cfg.Hooks.PreLand)
	}
	// Repo override.
	if cfg.Defaults.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want develop", cfg.Defaults.BaseBranch)
	}
}

func TestDefaultConfig_ProtectedBranches(t *testing.T) {
	cfg := DefaultConfig()
	expected := []string{"main", "master", "develop", "release/*"}
	if len(cfg.Landing.ProtectedBranches) != len(expected) {
		t.Fatalf("ProtectedBranches = %v, want %v", cfg.Landing.ProtectedBranches, expected)
	}
	for i, b := range expected {
		if cfg.Landing.ProtectedBranches[i] != b {
			t.Errorf("ProtectedBranches[%d] = %q, want %q", i, cfg.Landing.ProtectedBranches[i], b)
		}
	}
}

func TestLoadRepo_MergesLandingConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TOWR_HOME", tmp)
	repoRoot := "/my/repo"
	repoDir := filepath.Join(tmp, "repos", RepoHash(repoRoot))
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoContent := `
[landing]
protected_branches = ["main", "staging"]
`
	if err := os.WriteFile(filepath.Join(repoDir, "config.toml"), []byte(repoContent), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Landing.ProtectedBranches) != 2 || cfg.Landing.ProtectedBranches[1] != "staging" {
		t.Errorf("ProtectedBranches = %v, want [main staging]", cfg.Landing.ProtectedBranches)
	}
}

func TestLoadFile_WorkspaceCopyPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[workspace]
copy_paths = ["CLAUDE.md", "AGENTS.md", ".coflow/"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspace.CopyPaths) != 3 {
		t.Fatalf("CopyPaths = %v, want 3 entries", cfg.Workspace.CopyPaths)
	}
	if cfg.Workspace.CopyPaths[2] != ".coflow/" {
		t.Errorf("CopyPaths[2] = %q, want .coflow/", cfg.Workspace.CopyPaths[2])
	}
}

func TestLoadRepo_MergesWorkspaceCopyPaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TOWR_HOME", tmp)

	// Global config with some paths.
	globalPath := filepath.Join(tmp, "global-config.toml")
	globalContent := `
[workspace]
copy_paths = ["CLAUDE.md", ".coflow/"]
`
	if err := os.WriteFile(globalPath, []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Repo config with additional paths (CLAUDE.md is a duplicate).
	repoRoot := "/my/repo"
	repoDir := filepath.Join(tmp, "repos", RepoHash(repoRoot))
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoContent := `
[workspace]
copy_paths = ["AGENTS.md", "CLAUDE.md"]
`
	if err := os.WriteFile(filepath.Join(repoDir, "config.toml"), []byte(repoContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Should merge and deduplicate: CLAUDE.md, .coflow/, AGENTS.md (3 unique)
	if len(cfg.Workspace.CopyPaths) != 3 {
		t.Fatalf("CopyPaths = %v, want 3 entries (merged+deduped)", cfg.Workspace.CopyPaths)
	}
	// Verify order: base paths first, then new repo paths
	expected := []string{"CLAUDE.md", ".coflow/", "AGENTS.md"}
	for i, want := range expected {
		if cfg.Workspace.CopyPaths[i] != want {
			t.Errorf("CopyPaths[%d] = %q, want %q", i, cfg.Workspace.CopyPaths[i], want)
		}
	}
}

func TestLoadFile_WorkspaceLinkPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[workspace]
link_paths = [".coflow/", ".secrets/"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspace.LinkPaths) != 2 {
		t.Fatalf("LinkPaths = %v, want 2 entries", cfg.Workspace.LinkPaths)
	}
	if cfg.Workspace.LinkPaths[0] != ".coflow/" {
		t.Errorf("LinkPaths[0] = %q, want .coflow/", cfg.Workspace.LinkPaths[0])
	}
}

func TestLoadRepo_MergesWorkspaceLinkPaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TOWR_HOME", tmp)

	globalPath := filepath.Join(tmp, "global-config.toml")
	globalContent := `
[workspace]
link_paths = [".coflow/"]
`
	if err := os.WriteFile(globalPath, []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	repoRoot := "/my/repo"
	repoDir := filepath.Join(tmp, "repos", RepoHash(repoRoot))
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoContent := `
[workspace]
link_paths = [".coflow/", ".secrets/"]
`
	if err := os.WriteFile(filepath.Join(repoDir, "config.toml"), []byte(repoContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Should merge and deduplicate: .coflow/, .secrets/ (2 unique)
	if len(cfg.Workspace.LinkPaths) != 2 {
		t.Fatalf("LinkPaths = %v, want 2 entries (merged+deduped)", cfg.Workspace.LinkPaths)
	}
	expected := []string{".coflow/", ".secrets/"}
	for i, want := range expected {
		if cfg.Workspace.LinkPaths[i] != want {
			t.Errorf("LinkPaths[%d] = %q, want %q", i, cfg.Workspace.LinkPaths[i], want)
		}
	}
}

func TestIsProtectedBranch(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"master", true},
		{"develop", true},
		{"release/1.0", true},
		{"release/2.3.4", true},
		{"towr/auth", false},
		{"feature/foo", false},
		{"main-backup", false},
	}
	for _, tt := range tests {
		if got := cfg.Landing.IsProtectedBranch(tt.branch); got != tt.want {
			t.Errorf("IsProtectedBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}
