package landing

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HookType represents the type of hook to run.
type HookType string

const (
	HookPostCreate HookType = "post_create"
	HookPreLand    HookType = "pre_land"
	HookPostLand   HookType = "post_land"
	HookPrePause   HookType = "pre_pause"
)

// HookResult holds the result of running a hook.
type HookResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// HookVars holds variables available for substitution in hook commands.
type HookVars struct {
	WorkspaceID string
	WorktreePath string
	Branch       string
	BaseBranch   string
	RepoRoot     string
}

// HookRunner executes hook commands with variable substitution and timeout.
type HookRunner struct {
	// DefaultTimeout is the maximum duration for hook execution.
	// Zero means no timeout.
	DefaultTimeout time.Duration
}

// NewHookRunner creates a new HookRunner with the given default timeout.
func NewHookRunner(timeout time.Duration) *HookRunner {
	return &HookRunner{DefaultTimeout: timeout}
}

// Run executes a hook command string with variable substitution.
// The command is executed via /bin/sh -c.
// Returns the result including exit code, stdout, stderr, and duration.
func (r *HookRunner) Run(command string, vars HookVars) (*HookResult, error) {
	if command == "" {
		return &HookResult{ExitCode: 0}, nil
	}

	// Substitute variables
	expanded := substituteVars(command, vars)

	ctx := context.Background()
	var cancel context.CancelFunc
	if r.DefaultTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, r.DefaultTimeout)
		defer cancel()
	}

	return r.runCommand(ctx, expanded)
}

// RunWithTimeout executes a hook command with a specific timeout, overriding the default.
func (r *HookRunner) RunWithTimeout(command string, vars HookVars, timeout time.Duration) (*HookResult, error) {
	if command == "" {
		return &HookResult{ExitCode: 0}, nil
	}

	expanded := substituteVars(command, vars)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return r.runCommand(ctx, expanded)
}

func (r *HookRunner) runCommand(ctx context.Context, command string) (*HookResult, error) {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := &HookResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
			return result, fmt.Errorf("hook timed out: %w", ctx.Err())
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("hook execution failed: %w", err)
		}
	}

	return result, nil
}

// substituteVars replaces ${VAR_NAME} placeholders in the command string.
func substituteVars(command string, vars HookVars) string {
	replacements := map[string]string{
		"${WORKSPACE_ID}":  vars.WorkspaceID,
		"${WORKTREE_PATH}": vars.WorktreePath,
		"${BRANCH}":        vars.Branch,
		"${BASE_BRANCH}":   vars.BaseBranch,
		"${REPO_ROOT}":     vars.RepoRoot,
	}
	result := command
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}
