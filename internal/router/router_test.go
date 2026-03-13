package router

import (
	"testing"

	"github.com/brianaffirm/towr/internal/orchestrate"
)

func TestHeuristic_SimplePrompt(t *testing.T) {
	d := heuristic("Fix the typo in main.go")
	if d.Model != "haiku" {
		t.Errorf("expected haiku, got %s", d.Model)
	}
	if d.Tier != 0 {
		t.Errorf("expected tier 0, got %d", d.Tier)
	}
	if !d.CanEscalate {
		t.Error("expected CanEscalate true")
	}
}

func TestHeuristic_StandardPrompt(t *testing.T) {
	// 3 file references, no arch keywords
	prompt := "Update the handler in cmd/towr/main.go, internal/config/config.go, and internal/dispatch/run.go to pass context"
	d := heuristic(prompt)
	if d.Model != "sonnet" {
		t.Errorf("expected sonnet, got %s", d.Model)
	}
	if d.Tier != 1 {
		t.Errorf("expected tier 1, got %d", d.Tier)
	}
	if !d.CanEscalate {
		t.Error("expected CanEscalate true")
	}
}

func TestHeuristic_ComplexPrompt_ManyFileRefs(t *testing.T) {
	// More than 5 file references
	prompt := "Update cmd/towr/main.go, internal/config/config.go, internal/dispatch/run.go, internal/queue/queue.go, internal/store/store.go, internal/tui/tui.go to use new interface"
	d := heuristic(prompt)
	if d.Model != "opus" {
		t.Errorf("expected opus, got %s", d.Model)
	}
	if d.Tier != 2 {
		t.Errorf("expected tier 2, got %d", d.Tier)
	}
	if d.CanEscalate {
		t.Error("expected CanEscalate false for opus")
	}
}

func TestHeuristic_ArchKeyword_Architect(t *testing.T) {
	d := heuristic("Help me architect a new module for the system")
	if d.Model != "opus" {
		t.Errorf("expected opus for 'architect' keyword, got %s", d.Model)
	}
	if d.Tier != 2 {
		t.Errorf("expected tier 2, got %d", d.Tier)
	}
	if d.CanEscalate {
		t.Error("expected CanEscalate false for opus")
	}
}

func TestHeuristic_ArchKeyword_Refactor(t *testing.T) {
	d := heuristic("Refactor the entire authentication system")
	if d.Model != "opus" {
		t.Errorf("expected opus for 'refactor' keyword, got %s", d.Model)
	}
	if d.Tier != 2 {
		t.Errorf("expected tier 2, got %d", d.Tier)
	}
	if d.CanEscalate {
		t.Error("expected CanEscalate false for opus")
	}
}

func TestHeuristic_ArchKeyword_Migration(t *testing.T) {
	d := heuristic("Plan the database migration for production")
	if d.Model != "opus" {
		t.Errorf("expected opus for 'migration' keyword, got %s", d.Model)
	}
	if d.Tier != 2 {
		t.Errorf("expected tier 2, got %d", d.Tier)
	}
	if d.CanEscalate {
		t.Error("expected CanEscalate false for opus")
	}
}

func TestCountFileReferences_Zero(t *testing.T) {
	n := countFileReferences("Fix the typo please")
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestCountFileReferences_One(t *testing.T) {
	n := countFileReferences("Update internal/config/config.go to add a new field")
	if n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestCountFileReferences_Two(t *testing.T) {
	n := countFileReferences("Compare cmd/towr/main.go with internal/dispatch/run.go")
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestCountFileReferences_Three(t *testing.T) {
	n := countFileReferences("Update cmd/towr/main.go, internal/config/config.go, and internal/store/store.go")
	if n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
}

func TestMatchPolicy_PathRule(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Path: "infrastructure/**", Model: "opus"},
	}
	d, ok := matchPolicy("Update infrastructure/terraform/main.tf with new VPC config", rules)
	if !ok {
		t.Fatal("expected policy match")
	}
	if d.Model != "opus" {
		t.Errorf("model = %q, want opus", d.Model)
	}
	if d.Reason != "policy:infrastructure/**" {
		t.Errorf("reason = %q", d.Reason)
	}
}

func TestMatchPolicy_KeywordRule(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Keyword: "documentation", Model: "haiku"},
	}
	d, ok := matchPolicy("Write documentation for the API endpoints", rules)
	if !ok {
		t.Fatal("expected policy match")
	}
	if d.Model != "haiku" {
		t.Errorf("model = %q, want haiku", d.Model)
	}
}

func TestMatchPolicy_NoMatch(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Path: "infrastructure/**", Model: "opus"},
	}
	_, ok := matchPolicy("Fix a bug in cmd/towr/run.go", rules)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestMatchPolicy_PinPreventsEscalation(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Keyword: "security", Model: "opus", Pin: true},
	}
	d, ok := matchPolicy("Fix the security vulnerability", rules)
	if !ok {
		t.Fatal("expected match")
	}
	if d.CanEscalate {
		t.Error("pinned policy should not be escalatable")
	}
}

func TestMatchPolicy_RequireApproval(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Path: "infrastructure/**", Model: "opus", RequireApproval: true},
	}
	d, ok := matchPolicy("Update infrastructure/k8s/deploy.yaml", rules)
	if !ok {
		t.Fatal("expected match")
	}
	if !d.RequireApproval {
		t.Error("expected RequireApproval = true")
	}
}

func TestMatchPolicy_FirstMatchWins(t *testing.T) {
	rules := []orchestrate.PolicyRule{
		{Keyword: "test", Model: "haiku"},
		{Keyword: "test", Model: "opus"},
	}
	d, ok := matchPolicy("Write a test", rules)
	if !ok {
		t.Fatal("expected match")
	}
	if d.Model != "haiku" {
		t.Errorf("model = %q, want haiku (first match)", d.Model)
	}
}
