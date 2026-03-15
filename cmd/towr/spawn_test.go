package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestSpawnDeprecationWarning(t *testing.T) {
	// Create a spawn command that will fail (no git repo), but should still print deprecation.
	initApp := func() (*appContext, error) {
		return nil, &noRepoError{}
	}
	jsonFlag := false
	cmd := newSpawnCmd(initApp, &jsonFlag)

	// Capture stderr.
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})

	// Run with a task arg — will error because no repo, but deprecation should print first.
	cmd.SetArgs([]string{"test-task"})
	// Silence usage on error.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	_ = cmd.Execute()

	output := stderr.String()
	if !bytes.Contains([]byte(output), []byte("towr spawn is deprecated")) {
		t.Errorf("expected deprecation warning in stderr, got: %q", output)
	}
}

// noRepoError is a dummy error for tests that don't need a real git repo.
type noRepoError struct{}

func (e *noRepoError) Error() string { return "not in a git repository" }

// Ensure newSpawnCmd returns a cobra command (compilation test).
func TestSpawnCmdType(t *testing.T) {
	initApp := func() (*appContext, error) { return nil, nil }
	jsonFlag := false
	cmd := newSpawnCmd(initApp, &jsonFlag)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	var _ *cobra.Command = cmd // type assertion
}
