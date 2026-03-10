package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCommsDir(t *testing.T) {
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)

	dir, err := EnsureCommsDir("ws-1")
	if err != nil {
		t.Fatalf("EnsureCommsDir: %v", err)
	}

	want := filepath.Join(towrHome, "comms", "ws-1")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat comms dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("comms dir is not a directory")
	}
}

func TestWritePrompt(t *testing.T) {
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)

	commsDir, err := EnsureCommsDir("ws-2")
	if err != nil {
		t.Fatalf("EnsureCommsDir: %v", err)
	}

	err = WritePrompt(commsDir, "do the thing")
	if err != nil {
		t.Fatalf("WritePrompt: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(commsDir, "prompt.md"))
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if string(data) != "do the thing" {
		t.Errorf("prompt content = %q, want %q", string(data), "do the thing")
	}
}

func TestArchiveResult(t *testing.T) {
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)

	commsDir, err := EnsureCommsDir("ws-3")
	if err != nil {
		t.Fatalf("EnsureCommsDir: %v", err)
	}

	// Create a fake result file.
	resultPath := filepath.Join(commsDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write result: %v", err)
	}

	archivePath, err := ArchiveResult(commsDir, "dispatch-abc")
	if err != nil {
		t.Fatalf("ArchiveResult: %v", err)
	}

	// Archive should be under commsDir/archive/dispatch-abc/result.json
	wantDir := filepath.Join(commsDir, "archive", "dispatch-abc")
	if filepath.Dir(archivePath) != wantDir {
		t.Errorf("archive dir = %q, want %q", filepath.Dir(archivePath), wantDir)
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("archive content = %q", string(data))
	}

	// Original result should be gone.
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Error("original result.json should have been removed")
	}
}

func TestCleanCommsDir(t *testing.T) {
	towrHome := t.TempDir()
	t.Setenv("TOWR_HOME", towrHome)

	commsDir, err := EnsureCommsDir("ws-4")
	if err != nil {
		t.Fatalf("EnsureCommsDir: %v", err)
	}

	// Create some files.
	os.WriteFile(filepath.Join(commsDir, "prompt.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(commsDir, "result.json"), []byte("y"), 0o644)
	os.WriteFile(filepath.Join(commsDir, "heartbeat"), []byte("z"), 0o644)

	err = CleanCommsDir(commsDir)
	if err != nil {
		t.Fatalf("CleanCommsDir: %v", err)
	}

	// Directory should still exist but be empty of non-archive files.
	entries, _ := os.ReadDir(commsDir)
	for _, e := range entries {
		if e.Name() != "archive" {
			t.Errorf("unexpected file after clean: %s", e.Name())
		}
	}
}
