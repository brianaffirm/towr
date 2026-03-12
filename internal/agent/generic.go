package agent

import "fmt"

// Generic implements the Agent interface for a plain shell session.
// Useful as a fallback or for running non-AI scripts in towr workspaces.
type Generic struct{}

// Name returns "generic".
func (g *Generic) Name() string { return "generic" }

// LaunchCommand returns "bash" as the default shell.
func (g *Generic) LaunchCommand() string { return "bash" }

// LaunchEnv returns no extra environment variables.
func (g *Generic) LaunchEnv() map[string]string { return nil }

// IdlePattern returns a dollar-sign prompt pattern.
func (g *Generic) IdlePattern() string { return "\\$" }

// DialogIndicators returns nil since generic shells have no dialog prompts.
func (g *Generic) DialogIndicators() []string { return nil }

// StartupDialogs returns nil since generic shells need no startup dismissal.
func (g *Generic) StartupDialogs() []string { return nil }

// StartupKey returns "Enter" as default.
func (g *Generic) StartupKey() string { return "Enter" }

// CompletionMode returns "process_exit" since generic agents signal completion by exiting.
func (g *Generic) CompletionMode() string { return "process_exit" }

// DetectActivity returns an error since generic agents don't support activity detection.
func (g *Generic) DetectActivity(worktreePath string) (string, string, error) {
	return "", "", fmt.Errorf("generic agent does not support activity detection")
}

func init() {
	Register("generic", &Generic{})
}
