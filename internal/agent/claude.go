package agent

import (
	"github.com/brianaffirm/towr/internal/dispatch"
)

// ClaudeCode implements the Agent interface for Claude Code (Anthropic's CLI).
type ClaudeCode struct{}

// Name returns "claude-code".
func (c *ClaudeCode) Name() string {
	return "claude-code"
}

// LaunchCommand returns the shell command to launch Claude Code's interactive REPL.
// Unsets CLAUDECODE to allow launching from within a Claude session.
func (c *ClaudeCode) LaunchCommand() string {
	return "unset CLAUDECODE && claude"
}

// LaunchEnv returns env vars for launching Claude Code.
// Empty value for CLAUDECODE means it should be unset.
func (c *ClaudeCode) LaunchEnv() map[string]string {
	return map[string]string{"CLAUDECODE": ""}
}

// IdlePattern returns Claude Code's idle prompt character.
func (c *ClaudeCode) IdlePattern() string {
	return "❯"
}

// DialogIndicators returns all strings that indicate a Claude Code permission dialog.
// These match the patterns previously hardcoded in dispatch.isDialogIndicator.
func (c *ClaudeCode) DialogIndicators() []string {
	return []string{
		"Esc to cancel",
		"Do you want to",
		"Tab to amend",
		"Enter to confirm",
		"Command contains",
		"This command requires approval",
		"requires confirmation",
	}
}

// StartupDialogs returns patterns that appear during Claude Code's initial startup
// and should be auto-dismissed by pressing Enter.
func (c *ClaudeCode) StartupDialogs() []string {
	return []string{"Enter to confirm"}
}

// CompletionMode returns "jsonl" — Claude Code signals completion via JSONL event files.
func (c *ClaudeCode) CompletionMode() string {
	return "jsonl"
}

// DetectActivity checks Claude Code's JSONL event files for activity.
// Delegates to dispatch.DetectClaudeActivity and converts the result.
func (c *ClaudeCode) DetectActivity(worktreePath string) (string, string, error) {
	state, summary, err := dispatch.DetectClaudeActivity(worktreePath)
	return string(state), summary, err
}
