package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPlan(t *testing.T) {
	yaml := `
name: "test plan"
tasks:
  - id: first
    prompt: "Do the first thing"
  - id: second
    prompt: "Do the second thing"
    depends_on: [first]
settings:
  auto_approve: true
  max_retries: 2
  poll_interval: "5s"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}

	if plan.Name != "test plan" {
		t.Errorf("name = %q, want %q", plan.Name, "test plan")
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "first" {
		t.Errorf("tasks[0].ID = %q, want %q", plan.Tasks[0].ID, "first")
	}
	if plan.Tasks[1].DependsOn[0] != "first" {
		t.Errorf("tasks[1].DependsOn = %v, want [first]", plan.Tasks[1].DependsOn)
	}
	if !plan.Settings.AutoApprove {
		t.Error("auto_approve should be true")
	}
	if plan.Settings.MaxRetries != 2 {
		t.Errorf("max_retries = %d, want 2", plan.Settings.MaxRetries)
	}
	if plan.Settings.PollInterval != "5s" {
		t.Errorf("poll_interval = %q, want %q", plan.Settings.PollInterval, "5s")
	}
}

func TestLoadPlan_FileNotFound(t *testing.T) {
	_, err := LoadPlan("/nonexistent/plan.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidate_Valid(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "b", Prompt: "do b", DependsOn: []string{"a"}},
			{ID: "c", Prompt: "do c", DependsOn: []string{"a", "b"}},
		},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_NoTasks(t *testing.T) {
	plan := &Plan{}
	if err := plan.Validate(); err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "a", Prompt: "do a again"},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
	if got := err.Error(); got != `duplicate task ID: "a"` {
		t.Errorf("error = %q", got)
	}
}

func TestValidate_EmptyPrompt(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: ""},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestValidate_MissingDep(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a", DependsOn: []string{"nonexistent"}},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestValidate_SelfDep(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a", DependsOn: []string{"a"}},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestValidate_Cycle(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a", DependsOn: []string{"c"}},
			{ID: "b", Prompt: "do b", DependsOn: []string{"a"}},
			{ID: "c", Prompt: "do c", DependsOn: []string{"b"}},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestValidate_EmptyID(t *testing.T) {
	plan := &Plan{
		Tasks: []Task{
			{ID: "", Prompt: "do something"},
		},
	}
	err := plan.Validate()
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestValidate_DiamondGraph(t *testing.T) {
	// Diamond: a -> b, a -> c, b -> d, c -> d
	plan := &Plan{
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "b", Prompt: "do b", DependsOn: []string{"a"}},
			{ID: "c", Prompt: "do c", DependsOn: []string{"a"}},
			{ID: "d", Prompt: "do d", DependsOn: []string{"b", "c"}},
		},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("diamond graph should be valid: %v", err)
	}
}

func TestLoadPlan_RoutingSettings(t *testing.T) {
	yamlContent := `
name: "routed plan"
tasks:
  - id: first
    prompt: "Do something"
settings:
  auto_approve: true
  routing:
    rules:
      - path: "infrastructure/**"
        model: opus
        require_approval: true
      - keyword: "test"
        model: haiku
  budget: 15.50
`
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}

	if len(plan.Settings.Routing.Rules) != 2 {
		t.Fatalf("routing rules = %d, want 2", len(plan.Settings.Routing.Rules))
	}
	r0 := plan.Settings.Routing.Rules[0]
	if r0.Path != "infrastructure/**" || r0.Model != "opus" || !r0.RequireApproval {
		t.Errorf("rule[0] = %+v", r0)
	}
	r1 := plan.Settings.Routing.Rules[1]
	if r1.Keyword != "test" || r1.Model != "haiku" {
		t.Errorf("rule[1] = %+v", r1)
	}
	if plan.Settings.Budget != 15.50 {
		t.Errorf("budget = %f, want 15.50", plan.Settings.Budget)
	}
}
