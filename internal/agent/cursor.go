package agent

import "fmt"

// CursorAgent implements the Agent interface for the Cursor CLI (cursor-agent).
type CursorAgent struct {
	ModelFlag string // e.g. "cursor-auto", "cursor-sonnet"
	FullAuto  bool   // skip all permission prompts
}

// Name returns "cursor" or "cursor:<model>" if ModelFlag is set.
func (c *CursorAgent) Name() string {
	if c.ModelFlag != "" {
		return "cursor:" + c.ModelFlag
	}
	return "cursor"
}

// cursorCLIFlag maps towr model names to Cursor's CLI --model flag values.
var cursorCLIFlags = map[string]string{
	"cursor-auto":   "auto",
	"cursor-sonnet": "sonnet-4.6",
}

func cursorCLIFlag(model string) string {
	if f, ok := cursorCLIFlags[model]; ok {
		return f
	}
	return model // pass through unknown names
}

// LaunchCommand returns the shell command to start the Cursor agent.
func (c *CursorAgent) LaunchCommand() string {
	cmd := "cursor-agent"
	if c.ModelFlag != "" {
		cmd += " --model " + cursorCLIFlag(c.ModelFlag)
	}
	if c.FullAuto {
		cmd += " --yolo"
	}
	return cmd
}

// LaunchEnv returns environment variables for launching Cursor.
func (c *CursorAgent) LaunchEnv() map[string]string {
	return nil
}

// IdlePattern returns the pattern used to detect Cursor's idle prompt.
// Cursor shows "→ Plan, search, build anything" on first launch,
// and "→ Add a follow-up" after completing a task.
// Both contain the → character inside a bordered input box.
func (c *CursorAgent) IdlePattern() string {
	return "→"
}

// DialogIndicators returns strings that indicate a permission/confirmation dialog.
// Cursor prompts for shell commands not in the allowlist.
// File edits are auto-approved — only shell commands trigger dialogs.
func (c *CursorAgent) DialogIndicators() []string {
	return []string{
		"Run this command?",
		"Not in allowlist",
		"Waiting for approval",
		"Run (once)",
		"Trust this workspace",
		"Use arrow keys to navigate",
	}
}

// StartupDialogs returns patterns to auto-dismiss during launch.
// Cursor shows a trust dialog on first run — press 'a' to accept.
func (c *CursorAgent) StartupDialogs() []string {
	return []string{"Trust this workspace"}
}

// StartupKey returns the key to press for the startup trust dialog.
// Cursor uses 'a' for "Trust this workspace", not Enter.
func (c *CursorAgent) StartupKey() string {
	return "a"
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
