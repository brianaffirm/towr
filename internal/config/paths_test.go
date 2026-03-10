package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTowrHome_Default(t *testing.T) {
	os.Unsetenv("TOWR_HOME")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := TowrHome()
	want := filepath.Join(home, ".towr")
	if got != want {
		t.Errorf("TowrHome() = %q, want %q", got, want)
	}
}

func TestTowrHome_EnvOverride(t *testing.T) {
	t.Setenv("TOWR_HOME", "/tmp/test-towr")
	got := TowrHome()
	if got != "/tmp/test-towr" {
		t.Errorf("TowrHome() = %q, want /tmp/test-towr", got)
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
	t.Setenv("TOWR_HOME", "/tmp/test-towr")
	got := RepoStatePath("/my/repo")
	want := filepath.Join("/tmp/test-towr", "repos", RepoHash("/my/repo"))
	if got != want {
		t.Errorf("RepoStatePath() = %q, want %q", got, want)
	}
}

func TestWorktreeRoot(t *testing.T) {
	t.Setenv("TOWR_HOME", "/tmp/test-towr")
	got := WorktreeRoot()
	if got != "/tmp/test-towr/worktrees" {
		t.Errorf("WorktreeRoot() = %q, want /tmp/test-towr/worktrees", got)
	}
}

func TestEnsureTowrDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TOWR_HOME", tmp)
	if err := EnsureTowrDirs(); err != nil {
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
	t.Setenv("TOWR_HOME", "/tmp/test-towr")
	got := GlobalConfigPath()
	if got != "/tmp/test-towr/global-config.toml" {
		t.Errorf("GlobalConfigPath() = %q", got)
	}
}

func TestRepoConfigPath(t *testing.T) {
	t.Setenv("TOWR_HOME", "/tmp/test-towr")
	got := RepoConfigPath("/my/repo")
	hash := RepoHash("/my/repo")
	want := filepath.Join("/tmp/test-towr", "repos", hash, "config.toml")
	if got != want {
		t.Errorf("RepoConfigPath() = %q, want %q", got, want)
	}
}
