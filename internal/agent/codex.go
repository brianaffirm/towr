package agent

import "fmt"

// CodexAgent implements the Agent interface for the OpenAI Codex CLI.
type CodexAgent struct {
	ModelFlag string // e.g. "codex-mini", "gpt-5.3-codex", "gpt-5.4"
	FullAuto  bool   // skip all permission prompts
}

// Name returns "codex" or "codex:<model>" if ModelFlag is set.
func (c *CodexAgent) Name() string {
	if c.ModelFlag != "" {
		return "codex:" + c.ModelFlag
	}
	return "codex"
}

// LaunchCommand returns the shell command to start Codex.
// --no-alt-screen is required for tmux capture-pane to work
// (without it, Codex uses the alternate screen buffer which tmux can't capture).
func (c *CodexAgent) LaunchCommand() string {
	cmd := "codex --no-alt-screen"
	// Only pass -m for non-default models; the default "codex-mini"
	// may not be available on all accounts (e.g. ChatGPT auth).
	if c.ModelFlag != "" && c.ModelFlag != "codex-mini" {
		cmd += " -m " + c.ModelFlag
	}
	if c.FullAuto {
		cmd += " --full-auto"
	}
	return cmd
}

// LaunchEnv returns no extra environment variables.
func (c *CodexAgent) LaunchEnv() map[string]string {
	return nil
}

// IdlePattern returns the pattern for Codex's idle prompt.
// Codex shows "›" followed by placeholder text when waiting for input.
func (c *CodexAgent) IdlePattern() string {
	return "›"
}

// DialogIndicators returns strings that indicate a permission/confirmation dialog.
// Codex prompts for shell commands that hit sandbox restrictions.
func (c *CodexAgent) DialogIndicators() []string {
	return []string{
		"Would you like to run",
		"Press enter to confirm or esc to cancel",
		"Yes, proceed",
		"Do you trust the contents",
	}
}

// StartupDialogs returns patterns to auto-dismiss during launch.
func (c *CodexAgent) StartupDialogs() []string {
	return []string{"Do you trust the contents"}
}

// StartupKey returns "Enter" for Codex trust dialogs.
func (c *CodexAgent) StartupKey() string {
	return "Enter"
}

// CompletionMode returns "idle_pattern" since Codex uses an interactive REPL.
func (c *CodexAgent) CompletionMode() string {
	return "idle_pattern"
}

// DetectActivity returns an error since Codex doesn't support JSONL-based detection.
func (c *CodexAgent) DetectActivity(worktreePath string) (string, string, error) {
	return "", "", fmt.Errorf("codex agent does not support JSONL detection")
}

func init() {
	Register("codex", &CodexAgent{})
}
