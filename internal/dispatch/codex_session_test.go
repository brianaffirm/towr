package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCodexSession_MatchByCwd(t *testing.T) {
	dir := t.TempDir()
	old := codexSessionsDir
	codexSessionsDir = dir
	t.Cleanup(func() { codexSessionsDir = old })

	dateDir := filepath.Join(dir, "2026", "03", "13")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(dateDir, "rollout-2026-03-13T15-15-19-abc123.jsonl")
	content := "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/Users/test/.towr/worktrees/towr/codex-task\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":100,\"output_tokens\":50}}}}\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindCodexSession("/Users/test/.towr/worktrees/towr/codex-task")
	if err != nil {
		t.Fatalf("FindCodexSession: %v", err)
	}
	if got != sessionFile {
		t.Errorf("got %q, want %q", got, sessionFile)
	}
}

func TestFindCodexSession_NoMatch(t *testing.T) {
	dir := t.TempDir()
	old := codexSessionsDir
	codexSessionsDir = dir
	t.Cleanup(func() { codexSessionsDir = old })

	_, err := FindCodexSession("/nonexistent/path")
	if err == nil {
		t.Error("expected error for no matching session")
	}
}

func TestFindCodexSession_WrongCwd(t *testing.T) {
	dir := t.TempDir()
	old := codexSessionsDir
	codexSessionsDir = dir
	t.Cleanup(func() { codexSessionsDir = old })

	dateDir := filepath.Join(dir, "2026", "03", "13")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(dateDir, "rollout-abc.jsonl")
	content := "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/some/other/path\"}}\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := FindCodexSession("/Users/test/.towr/worktrees/towr/codex-task")
	if err == nil {
		t.Error("expected error when cwd doesn't match")
	}
}
