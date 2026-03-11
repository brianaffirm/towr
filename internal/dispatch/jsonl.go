package dispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// JSONLEntry represents a single line from Claude's JSONL event file.
type JSONLEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message,omitempty"`
}

// claudeProjectsDir is the base directory for Claude project JSONL files.
// Overridable in tests.
var claudeProjectsDir = ""

func getClaudeProjectsDir() string {
	if claudeProjectsDir != "" {
		return claudeProjectsDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// ClaudeProjectDir returns the ~/.claude/projects/ directory name for a worktree path.
// Encoding: replace both '/' and '.' with '-'.
// Example: /Users/brian.ho/.towr/worktrees/towr/models → -Users-brian-ho--towr-worktrees-towr-models
func ClaudeProjectDir(worktreePath string) string {
	// Replace both '/' and '.' with '-'
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(worktreePath)
}

// FindLatestJSONL finds the most recently modified .jsonl file in the Claude
// project directory for the given worktree path.
func FindLatestJSONL(worktreePath string) (string, error) {
	base := getClaudeProjectsDir()
	if base == "" {
		return "", fmt.Errorf("cannot determine Claude projects directory")
	}

	dirName := ClaudeProjectDir(worktreePath)
	pattern := filepath.Join(base, dirName, "*.jsonl")

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob %s: %w", pattern, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no JSONL files found in %s", filepath.Join(base, dirName))
	}

	// Find the most recently modified file.
	var newest string
	var newestMtime time.Time
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMtime) {
			newest = m
			newestMtime = info.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no readable JSONL files found")
	}
	return newest, nil
}

// ReadLastJSONLEntry reads the last non-empty line of a JSONL file and parses it.
// For files under 64KB, reads the entire file. For larger files, seeks from the end.
func ReadLastJSONLEntry(filePath string) (*JSONLEntry, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", filePath, err)
	}

	var lastLine string

	if info.Size() <= 64*1024 {
		// Small file: read entire contents.
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filePath, err)
		}
		lastLine = findLastLine(string(data))
	} else {
		// Large file: read last 16KB chunk (JSONL lines can be large).
		chunkSize := int64(16 * 1024)
		if chunkSize > info.Size() {
			chunkSize = info.Size()
		}
		buf := make([]byte, chunkSize)
		_, err := f.ReadAt(buf, info.Size()-chunkSize)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read tail of %s: %w", filePath, err)
		}
		lastLine = findLastLine(string(buf))

		// If the last line doesn't parse as JSON, the chunk may have split a line.
		// Fall back to reading a larger chunk.
		if lastLine != "" {
			var test JSONLEntry
			if json.Unmarshal([]byte(lastLine), &test) != nil && chunkSize < info.Size() {
				bigChunk := int64(256 * 1024)
				if bigChunk > info.Size() {
					bigChunk = info.Size()
				}
				buf = make([]byte, bigChunk)
				_, err = f.ReadAt(buf, info.Size()-bigChunk)
				if err != nil && err != io.EOF {
					return nil, fmt.Errorf("read larger tail of %s: %w", filePath, err)
				}
				lastLine = findLastLine(string(buf))
			}
		}
	}

	if lastLine == "" {
		return nil, fmt.Errorf("no non-empty lines in %s", filePath)
	}

	var entry JSONLEntry
	if err := json.Unmarshal([]byte(lastLine), &entry); err != nil {
		return nil, fmt.Errorf("parse JSONL entry: %w", err)
	}
	return &entry, nil
}

// findLastLine returns the last non-empty line from text.
func findLastLine(text string) string {
	text = strings.TrimRight(text, "\n\r ")
	if text == "" {
		return ""
	}
	idx := strings.LastIndex(text, "\n")
	if idx < 0 {
		return text
	}
	return text[idx+1:]
}

// DetectClaudeActivity determines Claude's state from its JSONL event file.
// Returns (state, summary, error). Summary is extracted from the last assistant
// message content if available.
//
// State logic:
//   - "last-prompt" type → PaneIdle (session waiting for input)
//   - file mtime > 120s ago → PaneIdle (stale)
//   - "progress" or "user" type AND mtime < 30s → PaneWorking
//   - "assistant" type AND mtime < 30s → PaneWorking (may still be generating)
//   - "assistant" type AND mtime > 30s → PaneIdle (finished responding)
//   - no JSONL file → PaneEmpty
func DetectClaudeActivity(worktreePath string) (PaneState, string, error) {
	jsonlPath, err := FindLatestJSONL(worktreePath)
	if err != nil {
		return PaneEmpty, "", err
	}

	entry, err := ReadLastJSONLEntry(jsonlPath)
	if err != nil {
		return PaneEmpty, "", err
	}

	// Check file modification time.
	info, err := os.Stat(jsonlPath)
	if err != nil {
		return PaneEmpty, "", fmt.Errorf("stat %s: %w", jsonlPath, err)
	}
	age := time.Since(info.ModTime())

	// Extract summary from assistant messages.
	summary := extractSummary(entry)

	// "last-prompt" always means idle (session ended or waiting for input).
	if entry.Type == "last-prompt" {
		return PaneIdle, summary, nil
	}

	// Stale file means idle regardless of last entry type.
	if age > 120*time.Second {
		return PaneIdle, summary, nil
	}

	// Recent activity — determine based on entry type.
	switch entry.Type {
	case "progress", "user":
		if age < 30*time.Second {
			return PaneWorking, summary, nil
		}
		return PaneIdle, summary, nil
	case "assistant":
		if age < 30*time.Second {
			return PaneWorking, summary, nil
		}
		return PaneIdle, summary, nil
	case "system", "file-history-snapshot":
		// System events shortly after assistant response — likely idle.
		if age < 30*time.Second {
			return PaneWorking, summary, nil
		}
		return PaneIdle, summary, nil
	default:
		// Unknown type — treat as working if recent, idle otherwise.
		if age < 30*time.Second {
			return PaneWorking, summary, nil
		}
		return PaneIdle, summary, nil
	}
}

// extractSummary pulls a text summary from a JSONLEntry.
// For assistant messages, extracts the first text content block.
func extractSummary(entry *JSONLEntry) string {
	if entry.Message == nil || entry.Message.Role != "assistant" {
		return ""
	}

	// Content can be a string or an array of content blocks.
	// Try string first.
	var textContent string
	if err := json.Unmarshal(entry.Message.Content, &textContent); err == nil {
		return truncateStr(textContent, 200)
	}

	// Try array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(entry.Message.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return truncateStr(b.Text, 200)
			}
		}
	}

	return ""
}

// truncateStr truncates a string to maxLen, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
