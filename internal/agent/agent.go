package agent

// Agent abstracts runtime-specific behavior for different AI coding tools.
type Agent interface {
	// Name returns a human-readable identifier for the agent.
	Name() string

	// LaunchCommand returns the shell command to start the agent's interactive REPL.
	LaunchCommand() string

	// LaunchEnv returns environment variables to set before launching.
	// An empty value means the variable should be unset.
	LaunchEnv() map[string]string

	// IdlePattern returns the string/pattern used to detect the agent's idle prompt.
	IdlePattern() string

	// DialogIndicators returns strings that indicate a permission/confirmation dialog.
	DialogIndicators() []string

	// StartupDialogs returns patterns to auto-dismiss during launch
	// (e.g., trust folder confirmations).
	StartupDialogs() []string

	// CompletionMode describes how the agent signals task completion.
	// Supported values: "jsonl", "idle_pattern", "process_exit".
	CompletionMode() string

	// DetectActivity checks the agent's own activity signals (e.g., JSONL logs)
	// for the given worktree path. Returns a state string, a summary, and any error.
	// State values match dispatch.PaneState: "working", "idle", "blocked", "empty".
	DetectActivity(worktreePath string) (state string, summary string, err error)
}
