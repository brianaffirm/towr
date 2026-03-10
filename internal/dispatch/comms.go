// Package dispatch implements task dispatch orchestration for towr workspaces.
package dispatch

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/brianaffirm/towr/internal/config"
)

// EnsureCommsDir creates and returns the comms directory for a workspace:
// ~/.towr/comms/<workspace-id>/
func EnsureCommsDir(workspaceID string) (string, error) {
	dir := filepath.Join(config.TowrHome(), "comms", workspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create comms dir: %w", err)
	}
	return dir, nil
}

// WritePrompt writes the prompt content to <commsDir>/prompt.md.
func WritePrompt(commsDir, content string) error {
	path := filepath.Join(commsDir, "prompt.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}
	return nil
}

// ArchiveResult moves <commsDir>/result.json to <commsDir>/archive/<dispatchID>/result.json
// and returns the archive path.
func ArchiveResult(commsDir, dispatchID string) (string, error) {
	src := filepath.Join(commsDir, "result.json")
	archiveDir := filepath.Join(commsDir, "archive", dispatchID)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}
	dst := filepath.Join(archiveDir, "result.json")
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("archive result: %w", err)
	}
	return dst, nil
}

// CleanCommsDir removes all files from the comms directory except the archive subdirectory.
func CleanCommsDir(commsDir string) error {
	entries, err := os.ReadDir(commsDir)
	if err != nil {
		return fmt.Errorf("read comms dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == "archive" {
			continue
		}
		path := filepath.Join(commsDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", e.Name(), err)
		}
	}
	return nil
}
