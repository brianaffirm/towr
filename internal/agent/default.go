package agent

// Default returns the default agent (Claude Code).
func Default() Agent {
	return &ClaudeCode{}
}
