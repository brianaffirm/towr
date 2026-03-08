package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAmuxHome_Default(t *testing.T) {
	os.Unsetenv("AMUX_HOME")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := AmuxHome()
	want := filepath.Join(home, ".amux")
	if got != want {
		t.Errorf("AmuxHome() = %q, want %q", got, want)
	}
}

func TestAmuxHome_EnvOverride(t *testing.T) {
	t.Setenv("AMUX_HOME", "/tmp/test-amux")
	got := AmuxHome()
	if got != "/tmp/test-amux" {
		t.Errorf("AmuxHome() = %q, want /tmp/test-amux", got)
	}
}

func TestRepoHash_Deterministic(t *testing.T) {
	h1 := RepoHash("/home/user/myrepo")
	h2 := RepoHash("/home/user/myrepo")
	if h1 != h2 {
		t.Errorf("RepoHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("RepoHash length = %d, want 16", len(h1))
	}
}

func TestRepoHash_DifferentPaths(t *testing.T) {
	h1 := RepoHash("/home/user/repo-a")
	h2 := RepoHash("/home/user/repo-b")
	if h1 == h2 {
		t.Errorf("RepoHash collision: %q == %q for different paths", h1, h2)
	}
}

func TestRepoStatePath(t *testing.T) {
	t.Setenv("AMUX_HOME", "/tmp/test-amux")
	got := RepoStatePath("/my/repo")
	want := filepath.Join("/tmp/test-amux", "repos", RepoHash("/my/repo"))
	if got != want {
		t.Errorf("RepoStatePath() = %q, want %q", got, want)
	}
}

func TestWorktreeRoot(t *testing.T) {
	t.Setenv("AMUX_HOME", "/tmp/test-amux")
	got := WorktreeRoot()
	if got != "/tmp/test-amux/worktrees" {
		t.Errorf("WorktreeRoot() = %q, want /tmp/test-amux/worktrees", got)
	}
}

func TestEnsureAmuxDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AMUX_HOME", tmp)
	if err := EnsureAmuxDirs(); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"repos", "worktrees"} {
		p := filepath.Join(tmp, sub)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", p)
		}
	}
}

func TestGlobalConfigPath(t *testing.T) {
	t.Setenv("AMUX_HOME", "/tmp/test-amux")
	got := GlobalConfigPath()
	if got != "/tmp/test-amux/global-config.toml" {
		t.Errorf("GlobalConfigPath() = %q", got)
	}
}

func TestRepoConfigPath(t *testing.T) {
	t.Setenv("AMUX_HOME", "/tmp/test-amux")
	got := RepoConfigPath("/my/repo")
	hash := RepoHash("/my/repo")
	want := filepath.Join("/tmp/test-amux", "repos", hash, "config.toml")
	if got != want {
		t.Errorf("RepoConfigPath() = %q, want %q", got, want)
	}
}
