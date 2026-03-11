package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProjectDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "/Users/brian.ho/.towr/worktrees/towr/models",
			want:  "-Users-brian-ho--towr-worktrees-towr-models",
		},
		{
			input: "/Users/brian.ho/.amux/worktrees/amux/a",
			want:  "-Users-brian-ho--amux-worktrees-amux-a",
		},
		{
			input: "/Users/brian.ho",
			want:  "-Users-brian-ho",
		},
		{
			input: "/tmp/simple",
			want:  "-tmp-simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ClaudeProjectDir(tt.input)
			if got != tt.want {
				t.Errorf("ClaudeProjectDir(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReadLastJSONLEntry(t *testing.T) {
	t.Run("assistant entry", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "test.jsonl")
		content := `{"type":"user","timestamp":"2026-03-11T00:06:00.000Z","sessionId":"abc123"}
{"type":"assistant","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"abc123","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}
`
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		entry, err := ReadLastJSONLEntry(f)
		if err != nil {
			t.Fatalf("ReadLastJSONLEntry: %v", err)
		}
		if entry.Type != "assistant" {
			t.Errorf("type = %q, want %q", entry.Type, "assistant")
		}
		if entry.SessionID != "abc123" {
			t.Errorf("sessionId = %q, want %q", entry.SessionID, "abc123")
		}
		if entry.Message == nil {
			t.Fatal("message is nil")
		}
		if entry.Message.Role != "assistant" {
			t.Errorf("message.role = %q, want %q", entry.Message.Role, "assistant")
		}
	})

	t.Run("last-prompt entry", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "test.jsonl")
		content := `{"type":"assistant","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"abc123"}
{"type":"last-prompt","lastPrompt":"Do something","sessionId":"abc123"}
`
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		entry, err := ReadLastJSONLEntry(f)
		if err != nil {
			t.Fatalf("ReadLastJSONLEntry: %v", err)
		}
		if entry.Type != "last-prompt" {
			t.Errorf("type = %q, want %q", entry.Type, "last-prompt")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(f, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := ReadLastJSONLEntry(f)
		if err == nil {
			t.Fatal("expected error for empty file")
		}
	})
}

func TestFindLatestJSONL(t *testing.T) {
	// Set up a fake Claude projects dir.
	dir := t.TempDir()
	oldDir := claudeProjectsDir
	claudeProjectsDir = dir
	t.Cleanup(func() { claudeProjectsDir = oldDir })

	worktreePath := "/Users/test/.towr/worktrees/towr/main"
	encoded := ClaudeProjectDir(worktreePath) // -Users-test--towr-worktrees-towr-main
	projDir := filepath.Join(dir, encoded)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two JSONL files with different mtimes.
	older := filepath.Join(projDir, "old-session.jsonl")
	newer := filepath.Join(projDir, "new-session.jsonl")

	if err := os.WriteFile(older, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set older file to past time.
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(newer, []byte(`{"type":"assistant"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindLatestJSONL(worktreePath)
	if err != nil {
		t.Fatalf("FindLatestJSONL: %v", err)
	}
	if got != newer {
		t.Errorf("got %q, want %q", got, newer)
	}
}

func TestDetectClaudeActivity(t *testing.T) {
	dir := t.TempDir()
	oldDir := claudeProjectsDir
	claudeProjectsDir = dir
	t.Cleanup(func() { claudeProjectsDir = oldDir })

	worktreePath := "/Users/test/.towr/worktrees/towr/detect"
	encoded := ClaudeProjectDir(worktreePath)
	projDir := filepath.Join(dir, encoded)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonlFile := filepath.Join(projDir, "session.jsonl")

	t.Run("no file returns empty", func(t *testing.T) {
		// Use a path with no JSONL files.
		state, _, err := DetectClaudeActivity("/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
		if state != PaneEmpty {
			t.Errorf("state = %q, want %q", state, PaneEmpty)
		}
	})

	t.Run("last-prompt returns idle", func(t *testing.T) {
		content := `{"type":"assistant","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"Done with the task"}]}}
{"type":"last-prompt","lastPrompt":"Do something","sessionId":"s1"}
`
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		state, _, err := DetectClaudeActivity(worktreePath)
		if err != nil {
			t.Fatalf("DetectClaudeActivity: %v", err)
		}
		if state != PaneIdle {
			t.Errorf("state = %q, want %q", state, PaneIdle)
		}
	})

	t.Run("recent assistant returns working", func(t *testing.T) {
		content := `{"type":"assistant","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"Working on it"}]}}
`
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		// Touch the file to now.
		now := time.Now()
		if err := os.Chtimes(jsonlFile, now, now); err != nil {
			t.Fatal(err)
		}

		state, summary, err := DetectClaudeActivity(worktreePath)
		if err != nil {
			t.Fatalf("DetectClaudeActivity: %v", err)
		}
		if state != PaneWorking {
			t.Errorf("state = %q, want %q", state, PaneWorking)
		}
		if summary != "Working on it" {
			t.Errorf("summary = %q, want %q", summary, "Working on it")
		}
	})

	t.Run("old assistant returns idle", func(t *testing.T) {
		content := `{"type":"assistant","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"All done"}]}}
`
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-30 * time.Second)
		if err := os.Chtimes(jsonlFile, past, past); err != nil {
			t.Fatal(err)
		}

		state, summary, err := DetectClaudeActivity(worktreePath)
		if err != nil {
			t.Fatalf("DetectClaudeActivity: %v", err)
		}
		if state != PaneIdle {
			t.Errorf("state = %q, want %q", state, PaneIdle)
		}
		if summary != "All done" {
			t.Errorf("summary = %q, want %q", summary, "All done")
		}
	})

	t.Run("stale file returns idle", func(t *testing.T) {
		content := `{"type":"progress","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"s1"}
`
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-5 * time.Minute)
		if err := os.Chtimes(jsonlFile, past, past); err != nil {
			t.Fatal(err)
		}

		state, _, err := DetectClaudeActivity(worktreePath)
		if err != nil {
			t.Fatalf("DetectClaudeActivity: %v", err)
		}
		if state != PaneIdle {
			t.Errorf("state = %q, want %q", state, PaneIdle)
		}
	})

	t.Run("recent progress returns working", func(t *testing.T) {
		content := `{"type":"progress","timestamp":"2026-03-11T00:06:05.000Z","sessionId":"s1"}
`
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if err := os.Chtimes(jsonlFile, now, now); err != nil {
			t.Fatal(err)
		}

		state, _, err := DetectClaudeActivity(worktreePath)
		if err != nil {
			t.Fatalf("DetectClaudeActivity: %v", err)
		}
		if state != PaneWorking {
			t.Errorf("state = %q, want %q", state, PaneWorking)
		}
	})
}

func TestExtractSummary(t *testing.T) {
	t.Run("text content array", func(t *testing.T) {
		entry := &JSONLEntry{
			Type: "assistant",
			Message: &struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}{
				Role:    "assistant",
				Content: json.RawMessage(`[{"type":"text","text":"Hello from Claude"}]`),
			},
		}
		got := extractSummary(entry)
		if got != "Hello from Claude" {
			t.Errorf("got %q, want %q", got, "Hello from Claude")
		}
	})

	t.Run("string content", func(t *testing.T) {
		entry := &JSONLEntry{
			Type: "assistant",
			Message: &struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}{
				Role:    "assistant",
				Content: json.RawMessage(`"Simple string response"`),
			},
		}
		got := extractSummary(entry)
		if got != "Simple string response" {
			t.Errorf("got %q, want %q", got, "Simple string response")
		}
	})

	t.Run("no message", func(t *testing.T) {
		entry := &JSONLEntry{Type: "progress"}
		got := extractSummary(entry)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
