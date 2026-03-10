package dispatch

import "strings"

// PaneState represents what Claude Code is doing in the tmux pane.
type PaneState string

const (
	PaneWorking PaneState = "working" // Claude is generating output
	PaneIdle    PaneState = "idle"    // Claude shows ❯ prompt, finished this turn
	PaneEmpty   PaneState = "empty"   // Pane has no Claude output (not launched yet)
)

// DetectPaneState analyzes tmux capture-pane output to determine Claude's state.
// It looks for the ❯ (U+276F) character on the last non-empty line.
func DetectPaneState(capturedOutput string) PaneState {
	lines := strings.Split(strings.TrimRight(capturedOutput, "\n"), "\n")

	// Walk backwards to find last non-empty line
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// ❯ is U+276F
		if strings.Contains(line, "❯") {
			return PaneIdle
		}
		return PaneWorking
	}
	return PaneEmpty
}

// ExtractLastResponse extracts text between the last two ❯ prompts,
// which is Claude's most recent response.
func ExtractLastResponse(capturedOutput string) string {
	lines := strings.Split(capturedOutput, "\n")

	// Find the last ❯ (current prompt) and second-to-last ❯ (previous prompt)
	var promptIndices []int
	for i, line := range lines {
		if strings.Contains(line, "❯") {
			promptIndices = append(promptIndices, i)
		}
	}

	if len(promptIndices) < 2 {
		// Can't extract cleanly, return everything before the last prompt
		if len(promptIndices) == 1 {
			return strings.TrimSpace(strings.Join(lines[:promptIndices[0]], "\n"))
		}
		return strings.TrimSpace(capturedOutput)
	}

	// Return text between second-to-last and last prompt
	start := promptIndices[len(promptIndices)-2] + 1
	end := promptIndices[len(promptIndices)-1]
	if start >= end {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}
