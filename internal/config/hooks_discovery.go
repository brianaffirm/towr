package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// HooksFile represents a .towr-hooks.toml file found in the repo tree.
type HooksFile struct {
	Hooks HooksConfig `toml:"hooks"`
}

// DiscoverHooks walks up from targetPath to repoRoot looking for .towr-hooks.toml
// files. Returns a merged HooksConfig where child overrides parent for the same
// hook name, and parent hooks apply when the child doesn't define them.
//
// Resolution order (most specific wins):
//  1. .towr-hooks.toml nearest to targetPath
//  2. .towr-hooks.toml at repo root
//  3. Existing config hooks (passed as fallback)
func DiscoverHooks(repoRoot, targetPath string, fallback HooksConfig) (HooksConfig, error) {
	if targetPath == "" || repoRoot == "" {
		return fallback, nil
	}

	// Collect hooks files from repo root down to target path.
	// Walk from target up to repo root, collect in reverse order.
	var hooksFiles []HooksConfig

	current := targetPath
	for {
		hooksPath := filepath.Join(current, ".towr-hooks.toml")
		hf, err := loadHooksFile(hooksPath)
		if err == nil {
			hooksFiles = append(hooksFiles, hf.Hooks)
		} else if !errors.Is(err, os.ErrNotExist) {
			// File exists but can't be read or parsed — surface the error.
			return fallback, fmt.Errorf("invalid hooks file %s: %w", hooksPath, err)
		}

		if current == repoRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break // reached filesystem root without finding repoRoot
		}
		current = parent
	}

	if len(hooksFiles) == 0 {
		return fallback, nil
	}

	// Merge: start from fallback, apply repo root (last in list), then more specific.
	// hooksFiles[0] is most specific (targetPath), last is least specific (repoRoot).
	result := fallback
	for i := len(hooksFiles) - 1; i >= 0; i-- {
		hf := hooksFiles[i]
		if hf.PostCreate != "" {
			result.PostCreate = hf.PostCreate
		}
		if hf.PreLand != "" {
			result.PreLand = hf.PreLand
		}
		if hf.PostLand != "" {
			result.PostLand = hf.PostLand
		}
	}

	return result, nil
}

// InferTargetPath determines the workspace target path from cwd relative to repoRoot.
// Returns cwd if it's inside repoRoot, otherwise returns repoRoot.
func InferTargetPath(repoRoot string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return repoRoot
	}
	rel, err := filepath.Rel(repoRoot, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return repoRoot
	}
	return cwd
}

func loadHooksFile(path string) (*HooksFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, err
	}
	var hf HooksFile
	if err := toml.Unmarshal(data, &hf); err != nil {
		return nil, err
	}
	return &hf, nil
}
