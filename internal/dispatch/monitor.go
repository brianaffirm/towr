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
// It scans the last ~15 non-empty lines for Claude's input prompt ❯ (U+276F).
// We check multiple lines because Claude's UI has a status bar and horizontal rules
// below the prompt (e.g., "? for shortcuts", "────────").
//
// Permission dialogs also contain ❯ (for menu items) so we first check for
// dialog indicators and return PaneWorking if a dialog is active.
func DetectPaneState(capturedOutput string) PaneState {
	lines := strings.Split(strings.TrimRight(capturedOutput, "\n"), "\n")

	// First pass: check if a permission dialog is active in the last 15 lines.
	// Dialog indicators: "Esc to cancel", "Do you want to", "Tab to amend".
	checked := 0
	hasContent := false
	for i := len(lines) - 1; i >= 0 && checked < 15; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		checked++
		hasContent = true
		if isDialogIndicator(line) {
			return PaneWorking
		}
	}

	if !hasContent {
		return PaneEmpty
	}

	// Second pass: look for idle prompt.
	checked = 0
	for i := len(lines) - 1; i >= 0 && checked < 15; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		checked++
		if isIdlePrompt(line) {
			return PaneIdle
		}
	}
	return PaneWorking
}

// isDialogIndicator checks if a line indicates an active permission/confirmation dialog.
func isDialogIndicator(line string) bool {
	return strings.Contains(line, "Esc to cancel") ||
		strings.Contains(line, "Do you want to") ||
		strings.Contains(line, "Tab to amend") ||
		strings.Contains(line, "Enter to confirm")
}

// isIdlePrompt checks if a line is Claude's idle input prompt.
// The idle prompt is ❯ optionally followed by suggestion text (e.g., "❯ Try something").
// Selection menu items like "❯ 1. Yes, I trust this folder" are NOT idle prompts.
func isIdlePrompt(line string) bool {
	// Must contain ❯
	idx := strings.Index(line, "❯")
	if idx < 0 {
		return false
	}
	// Check what follows ❯
	rest := strings.TrimSpace(line[idx+len("❯"):])
	if rest == "" {
		return true // bare ❯ prompt
	}
	// If followed by a digit, it's a menu item (e.g., "1. Yes, I trust this folder")
	if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		return false
	}
	// If the line also contains "Enter to confirm" or "Esc to cancel", it's a dialog
	if strings.Contains(line, "Enter to confirm") || strings.Contains(line, "Esc to cancel") {
		return false
	}
	return true
}

// ExtractLastResponse extracts text between the last two ❯ prompts,
// which is Claude's most recent response.
func ExtractLastResponse(capturedOutput string) string {
	lines := strings.Split(capturedOutput, "\n")

	// Find the last ❯ (current prompt) and second-to-last ❯ (previous prompt)
	var promptIndices []int
	for i, line := range lines {
		if isIdlePrompt(strings.TrimSpace(line)) {
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
