package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the merged amux configuration (global + repo-level).
type Config struct {
	Defaults      DefaultsConfig                `toml:"defaults"`
	Cleanup       CleanupConfig                 `toml:"cleanup"`
	Hooks         HooksConfig                   `toml:"hooks"`
	Landing       LandingConfig                 `toml:"landing"`
	Agents        map[string]AgentConfig        `toml:"agents"`
	Notifications map[string]NotificationConfig `toml:"notifications"`
	Timeouts      TimeoutsConfig                `toml:"timeouts"`
}

// DefaultsConfig holds default workspace settings.
type DefaultsConfig struct {
	MergeStrategy string `toml:"merge_strategy"`
	WorktreeRoot  string `toml:"worktree_root"`
	BaseBranch    string `toml:"base_branch"`
}

// CleanupConfig holds cleanup/GC settings.
type CleanupConfig struct {
	StaleThreshold string `toml:"stale_threshold"`
}

// HooksConfig holds hook commands.
type HooksConfig struct {
	PostCreate string `toml:"post_create"`
	PreLand    string `toml:"pre_land"`
	PostLand   string `toml:"post_land"`
}

// LandingConfig holds settings for the land operation.
type LandingConfig struct {
	ProtectedBranches []string `toml:"protected_branches"`
}

// IsProtectedBranch returns true if branch matches any protected branch pattern.
func (lc *LandingConfig) IsProtectedBranch(branch string) bool {
	for _, pattern := range lc.ProtectedBranches {
		if pattern == branch {
			return true
		}
		if matched, _ := filepath.Match(pattern, branch); matched {
			return true
		}
	}
	return false
}

// AgentConfig holds per-agent-runtime settings.
type AgentConfig struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// NotificationConfig holds notification channel settings.
type NotificationConfig struct {
	Enabled    bool     `toml:"enabled"`
	OnComplete bool     `toml:"on_complete"`
	OnBlocked  bool     `toml:"on_blocked"`
	URL        string   `toml:"url"`
	Events     []string `toml:"events"`
}

// TimeoutsConfig holds timeout settings.
type TimeoutsConfig struct {
	Default       string `toml:"default"`
	TimeoutAction string `toml:"timeout_action"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Defaults: DefaultsConfig{
			MergeStrategy: "rebase-ff",
		},
		Cleanup: CleanupConfig{
			StaleThreshold: "7d",
		},
		Landing: LandingConfig{
			ProtectedBranches: []string{"main", "master", "develop", "release/*"},
		},
		Timeouts: TimeoutsConfig{
			Default:       "6h",
			TimeoutAction: "block",
		},
	}
}

// LoadGlobal loads the global config from ~/.amux/global-config.toml.
// Returns default config if the file doesn't exist.
func LoadGlobal() (*Config, error) {
	return LoadFile(GlobalConfigPath())
}

// LoadRepo loads the repo-specific config, merged on top of the global config.
func LoadRepo(repoRoot string) (*Config, error) {
	cfg, err := LoadGlobal()
	if err != nil {
		return nil, err
	}

	repoPath := RepoConfigPath(repoRoot)
	repoCfg, err := LoadFile(repoPath)
	if err != nil {
		return nil, err
	}

	return merge(cfg, repoCfg), nil
}

// LoadFile loads a config from a specific TOML file path.
// Returns default config if the file doesn't exist.
func LoadFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// merge overlays repo-level config onto a base config.
// Non-zero repo values override base values.
func merge(base, repo *Config) *Config {
	result := *base

	if repo.Defaults.MergeStrategy != "" && repo.Defaults.MergeStrategy != DefaultConfig().Defaults.MergeStrategy {
		result.Defaults.MergeStrategy = repo.Defaults.MergeStrategy
	}
	if repo.Defaults.WorktreeRoot != "" {
		result.Defaults.WorktreeRoot = repo.Defaults.WorktreeRoot
	}
	if repo.Defaults.BaseBranch != "" && repo.Defaults.BaseBranch != DefaultConfig().Defaults.BaseBranch {
		result.Defaults.BaseBranch = repo.Defaults.BaseBranch
	}
	if repo.Cleanup.StaleThreshold != "" && repo.Cleanup.StaleThreshold != DefaultConfig().Cleanup.StaleThreshold {
		result.Cleanup.StaleThreshold = repo.Cleanup.StaleThreshold
	}
	if repo.Hooks.PostCreate != "" {
		result.Hooks.PostCreate = repo.Hooks.PostCreate
	}
	if repo.Hooks.PreLand != "" {
		result.Hooks.PreLand = repo.Hooks.PreLand
	}
	if repo.Hooks.PostLand != "" {
		result.Hooks.PostLand = repo.Hooks.PostLand
	}
	if len(repo.Landing.ProtectedBranches) > 0 {
		result.Landing.ProtectedBranches = repo.Landing.ProtectedBranches
	}
	if repo.Agents != nil {
		if result.Agents == nil {
			result.Agents = make(map[string]AgentConfig)
		}
		for k, v := range repo.Agents {
			result.Agents[k] = v
		}
	}
	if repo.Notifications != nil {
		if result.Notifications == nil {
			result.Notifications = make(map[string]NotificationConfig)
		}
		for k, v := range repo.Notifications {
			result.Notifications[k] = v
		}
	}
	if repo.Timeouts.Default != "" && repo.Timeouts.Default != DefaultConfig().Timeouts.Default {
		result.Timeouts.Default = repo.Timeouts.Default
	}
	if repo.Timeouts.TimeoutAction != "" && repo.Timeouts.TimeoutAction != DefaultConfig().Timeouts.TimeoutAction {
		result.Timeouts.TimeoutAction = repo.Timeouts.TimeoutAction
	}

	return &result
}
