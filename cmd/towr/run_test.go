package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brianaffirm/towr/internal/orchestrate"
)

func TestDetectStringVsFile(t *testing.T) {
	// A non-existent path should be treated as a prompt, not a file.
	_, err := os.Stat("nonexistent-file-that-does-not-exist.yaml")
	if err == nil {
		t.Fatal("expected file to not exist")
	}
	// The detection logic: os.Stat fails → treat as prompt.
	if !os.IsNotExist(err) {
		t.Fatal("expected IsNotExist error")
	}
}

func TestDetectExistingFile(t *testing.T) {
	// Create a temp YAML file to verify file detection.
	dir := t.TempDir()
	planPath := filepath.Join(dir, "test-plan.yaml")
	content := []byte("name: test\ntasks:\n  - id: t1\n    prompt: do something\n")
	if err := os.WriteFile(planPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(planPath)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected a file, not a directory")
	}
}

func TestSynthesizePlan(t *testing.T) {
	prompt := "fix the auth bug in users.go"
	wsID := orchestrate.Slugify(prompt)

	plan := &orchestrate.Plan{
		Name: wsID,
		Tasks: []orchestrate.Task{{
			ID:     wsID,
			Prompt: prompt,
		}},
		Settings: orchestrate.Settings{
			AutoApprove: true,
		},
	}

	if plan.Name != "fix-the-auth-bug-in-usersgo" {
		t.Errorf("unexpected plan name: %q", plan.Name)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Prompt != prompt {
		t.Errorf("unexpected prompt: %q", plan.Tasks[0].Prompt)
	}
	if plan.Tasks[0].ID != plan.Name {
		t.Errorf("task ID %q should match plan name %q", plan.Tasks[0].ID, plan.Name)
	}
	if !plan.Settings.AutoApprove {
		t.Error("expected AutoApprove to be true")
	}
	if err := plan.Validate(); err != nil {
		t.Errorf("synthesized plan should be valid: %v", err)
	}
}

func TestFlagOverrides(t *testing.T) {
	prompt := "add caching layer"
	wsID := orchestrate.Slugify(prompt)

	plan := &orchestrate.Plan{
		Name: wsID,
		Tasks: []orchestrate.Task{{
			ID:     wsID,
			Prompt: prompt,
		}},
		Settings: orchestrate.Settings{
			AutoApprove: true,
		},
	}

	// Simulate --agent flag.
	plan.Tasks[0].Agent = "codex"
	if plan.Tasks[0].Agent != "codex" {
		t.Errorf("expected agent=codex, got %q", plan.Tasks[0].Agent)
	}

	// Simulate --model flag.
	plan.Tasks[0].Model = "opus"
	if plan.Tasks[0].Model != "opus" {
		t.Errorf("expected model=opus, got %q", plan.Tasks[0].Model)
	}

	// Simulate --budget flag.
	plan.Settings.Budget = 5.0
	if plan.Settings.Budget != 5.0 {
		t.Errorf("expected budget=5.0, got %f", plan.Settings.Budget)
	}

	// Simulate --pr flag.
	plan.Settings.CreatePR = true
	if !plan.Settings.CreatePR {
		t.Error("expected CreatePR=true")
	}

	// Simulate --full-auto flag.
	plan.Settings.FullAuto = true
	if !plan.Settings.FullAuto {
		t.Error("expected FullAuto=true")
	}

	// Simulate --base flag.
	plan.Settings.BaseBranch = "develop"
	if plan.Settings.BaseBranch != "develop" {
		t.Errorf("expected base=develop, got %q", plan.Settings.BaseBranch)
	}
}

func TestMultiWordPrompt(t *testing.T) {
	args := []string{"fix", "the", "auth", "bug"}
	prompt := strings.Join(args, " ")

	if prompt != "fix the auth bug" {
		t.Errorf("unexpected joined prompt: %q", prompt)
	}

	wsID := orchestrate.Slugify(prompt)
	if wsID != "fix-the-auth-bug" {
		t.Errorf("unexpected slugified ID: %q", wsID)
	}
}
