package agent

import "fmt"

// CursorAgent implements the Agent interface for the Cursor CLI (cursor-agent).
type CursorAgent struct{}

// Name returns "cursor".
func (c *CursorAgent) Name() string { return "cursor" }

// LaunchCommand returns the shell command to start the Cursor agent.
func (c *CursorAgent) LaunchCommand() string {
	return "cursor-agent"
}

// LaunchEnv returns environment variables for launching Cursor.
func (c *CursorAgent) LaunchEnv() map[string]string {
	return nil
}

// IdlePattern returns the pattern used to detect Cursor's idle prompt.
func (c *CursorAgent) IdlePattern() string {
	return ">"
}

// DialogIndicators returns strings that indicate a permission/confirmation dialog.
func (c *CursorAgent) DialogIndicators() []string {
	return []string{
		"Esc to cancel",
		"Do you want to",
		"Tab to amend",
		"Enter to confirm",
		"Command contains",
		"This command requires approval",
	}
}

// StartupDialogs returns patterns to auto-dismiss during launch.
func (c *CursorAgent) StartupDialogs() []string {
	return []string{"Enter to confirm"}
}

// CompletionMode returns "idle_pattern" since Cursor doesn't have JSONL events.
func (c *CursorAgent) CompletionMode() string {
	return "idle_pattern"
}

// DetectActivity returns an error since Cursor doesn't support JSONL-based detection.
func (c *CursorAgent) DetectActivity(worktreePath string) (string, string, error) {
	return "", "", fmt.Errorf("cursor agent does not support JSONL detection")
}

func init() {
	Register("cursor", &CursorAgent{})
}
