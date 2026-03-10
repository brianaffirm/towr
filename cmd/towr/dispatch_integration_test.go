package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
)

// TestDispatchOrchestrationFlow exercises the dispatch/report event flow
// through the store: comms directory creation, prompt writing, wrapper
// generation, event emission, and result archiving.
func TestDispatchOrchestrationFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// --- Setup ---
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)
	if err := config.EnsureTowrDirs(); err != nil {
		t.Fatalf("ensure towr dirs: %v", err)
	}

	repoRoot := "/tmp/fake-repo-" + t.Name()
	repoState := config.RepoStatePath(repoRoot)
	if err := os.MkdirAll(repoState, 0755); err != nil {
		t.Fatalf("create repo state dir: %v", err)
	}

	// Open store.
	s := store.NewSQLiteStore()
	dbPath := filepath.Join(repoState, "state.db")
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("init store: %v", err)
	}
	defer s.Close()

	// Create a workspace record with status READY.
	wsID := "dispatch-test"
	ws := &store.Workspace{
		ID:        wsID,
		RepoRoot:  repoRoot,
		Status:    "READY",
		Branch:    "towr/dispatch-test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.SaveWorkspace(ws); err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	// --- Test comms directory creation + prompt writing ---
	commsDir, err := dispatch.EnsureCommsDir(wsID)
	if err != nil {
		t.Fatalf("ensure comms dir: %v", err)
	}
	if _, err := os.Stat(commsDir); err != nil {
		t.Fatalf("comms dir not created: %v", err)
	}

	prompt := "Implement the frobnicator module"
	if err := dispatch.WritePrompt(commsDir, prompt); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	promptPath := filepath.Join(commsDir, "prompt.md")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if string(data) != prompt {
		t.Errorf("prompt content mismatch: got %q, want %q", string(data), prompt)
	}

	// --- Test wrapper generation ---
	dispatchID := "d-0001"
	wrapper := dispatch.BuildWrapper(wsID, dispatchID, commsDir)
	if wrapper == "" {
		t.Fatal("wrapper is empty")
	}
	if !strings.Contains(wrapper, wsID) {
		t.Errorf("wrapper should contain workspace ID %q", wsID)
	}
	if !strings.Contains(wrapper, dispatchID) {
		t.Errorf("wrapper should contain dispatch ID %q", dispatchID)
	}
	if !strings.Contains(wrapper, "towr report") {
		t.Error("wrapper should contain 'towr report' command")
	}

	// --- Emit task.dispatched event ---
	dispatchEvent := store.Event{
		ID:          "evt-dispatch-1",
		Kind:        store.EventTaskDispatched,
		WorkspaceID: wsID,
		RepoRoot:    repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"prompt":      prompt,
		},
	}
	if err := s.EmitEvent(dispatchEvent); err != nil {
		t.Fatalf("emit dispatch event: %v", err)
	}

	// Verify LatestDispatch returns it.
	latestDisp, err := s.LatestDispatch(repoRoot, wsID)
	if err != nil {
		t.Fatalf("latest dispatch: %v", err)
	}
	if latestDisp == nil {
		t.Fatal("expected latest dispatch event, got nil")
	}
	if latestDisp.Kind != store.EventTaskDispatched {
		t.Errorf("expected kind %s, got %s", store.EventTaskDispatched, latestDisp.Kind)
	}
	gotDispID, _ := latestDisp.Data["dispatch_id"].(string)
	if gotDispID != dispatchID {
		t.Errorf("expected dispatch_id %q, got %q", dispatchID, gotDispID)
	}

	// --- Emit task.completed event ---
	completedEvent := store.Event{
		ID:          "evt-completed-1",
		Kind:        store.EventTaskCompleted,
		WorkspaceID: wsID,
		RepoRoot:    repoRoot,
		Timestamp:   time.Now().UTC(),
		Data: map[string]interface{}{
			"dispatch_id": dispatchID,
			"exit_code":   0,
		},
	}
	if err := s.EmitEvent(completedEvent); err != nil {
		t.Fatalf("emit completed event: %v", err)
	}

	// Verify LatestTaskEvent returns it with correct kind.
	latestEvt, err := s.LatestTaskEvent(repoRoot, wsID, dispatchID)
	if err != nil {
		t.Fatalf("latest task event: %v", err)
	}
	if latestEvt == nil {
		t.Fatal("expected latest task event, got nil")
	}
	if latestEvt.Kind != store.EventTaskCompleted {
		t.Errorf("expected kind %s, got %s", store.EventTaskCompleted, latestEvt.Kind)
	}

	// --- Test result archiving ---
	// Create a fake result.json in commsDir.
	resultPath := filepath.Join(commsDir, "result.json")
	resultContent := `{"status":"success","output":"done"}`
	if err := os.WriteFile(resultPath, []byte(resultContent), 0644); err != nil {
		t.Fatalf("write result.json: %v", err)
	}

	archivePath, err := dispatch.ArchiveResult(commsDir, dispatchID)
	if err != nil {
		t.Fatalf("archive result: %v", err)
	}

	// Verify archive file exists with correct content.
	archivedData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archived result: %v", err)
	}
	if string(archivedData) != resultContent {
		t.Errorf("archived content mismatch: got %q, want %q", string(archivedData), resultContent)
	}

	// Verify original result.json is gone (moved, not copied).
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Error("expected result.json to be moved, but it still exists")
	}
}
