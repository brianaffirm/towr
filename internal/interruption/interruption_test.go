package interruption

import (
	"testing"
)

// --- Policy Evaluate Tests ---

func TestPolicyEvaluate_AutoApprove(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Write to src file",
		FilesAtStake: []string{"src/auth/handler.go"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result, got nil")
	}
	if result.Action != "allow" {
		t.Errorf("expected action=allow, got %s", result.Action)
	}
}

func TestPolicyEvaluate_AutoDeny(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Modify CI config",
		FilesAtStake: []string{".github/workflows/ci.yml"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result, got nil")
	}
	if result.Action != "deny" {
		t.Errorf("expected action=deny, got %s", result.Action)
	}
}

func TestPolicyEvaluate_Queue(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Modify DB migration",
		FilesAtStake: []string{"db/migrations/003_add_users.sql"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result, got nil")
	}
	if result.Action != "queue" {
		t.Errorf("expected action=queue, got %s", result.Action)
	}
}

func TestPolicyEvaluate_AlwaysDeny(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Force push",
		AgentRequest: "git push --force origin main",
		FilesAtStake: []string{},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a result, got nil")
	}
	if result.Action != "deny" {
		t.Errorf("expected action=deny, got %s", result.Action)
	}
	if result.Enforcement != "hard" {
		t.Errorf("expected enforcement=hard, got %s", result.Enforcement)
	}
}

func TestPolicyEvaluate_NoMatch_Interactive(t *testing.T) {
	pe := NewPolicyEngine()
	// Default is "interactive" which has no rules.

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerDecision,
		Summary:      "Choose session length",
		FilesAtStake: []string{"src/config.go"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for interactive preset, got %+v", result)
	}
}

func TestPolicyEvaluate_Conservative_QueuesEverything(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("conservative")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Some change",
		FilesAtStake: []string{"anything/here.txt"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected queue result, got nil")
	}
	if result.Action != "queue" {
		t.Errorf("expected action=queue, got %s", result.Action)
	}
}

func TestPolicyEvaluate_Aggressive_ApprovesEverything(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("aggressive")

	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Some change",
		FilesAtStake: []string{"anything/here.txt"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected allow result, got nil")
	}
	if result.Action != "allow" {
		t.Errorf("expected action=allow, got %s", result.Action)
	}
}

func TestPolicyEvaluate_DenyTakesPrecedenceOverApprove(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	// File matches both auto_deny (.github/**) pattern.
	blocker := Blocker{
		WorkspaceID:  "test-ws",
		Type:         BlockerPermission,
		Summary:      "Modify workflow",
		FilesAtStake: []string{".github/workflows/deploy.yml"},
	}

	result, err := pe.Evaluate(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected deny result")
	}
	if result.Action != "deny" {
		t.Errorf("expected deny (auto_deny takes precedence), got %s", result.Action)
	}
}

// --- Preset Tests ---

func TestPresets_AllFourExist(t *testing.T) {
	presets := BuiltinPresets()
	if len(presets) != 4 {
		t.Fatalf("expected 4 presets, got %d", len(presets))
	}

	names := make(map[string]bool)
	for _, p := range presets {
		names[p.Name] = true
	}

	for _, expected := range []string{"overnight", "conservative", "aggressive", "interactive"} {
		if !names[expected] {
			t.Errorf("missing preset: %s", expected)
		}
	}
}

func TestPresets_OvernightHasRules(t *testing.T) {
	p := OvernightPreset()
	if len(p.AutoApprove) == 0 {
		t.Error("overnight should have auto_approve rules")
	}
	if len(p.AutoDeny) == 0 {
		t.Error("overnight should have auto_deny rules")
	}
	if len(p.Queue) == 0 {
		t.Error("overnight should have queue rules")
	}
	if !p.AutoLand {
		t.Error("overnight should have auto_land=true")
	}
}

func TestPresets_InteractiveHasNoRules(t *testing.T) {
	p := InteractivePreset()
	if len(p.AutoApprove) != 0 || len(p.AutoDeny) != 0 || len(p.Queue) != 0 || len(p.AlwaysDeny) != 0 {
		t.Error("interactive should have no rules")
	}
	if p.AutoLand {
		t.Error("interactive should not auto-land")
	}
}

func TestSetActive_UnknownPreset(t *testing.T) {
	pe := NewPolicyEngine()
	err := pe.SetActive("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}
	if _, ok := err.(*UnknownPresetError); !ok {
		t.Errorf("expected UnknownPresetError, got %T", err)
	}
}

// --- Resolver Layering Tests ---

func TestResolverLayering_PolicyMatch_SkipsQueue(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	r := NewResolver(pe, nil, nil, 0)

	blocker := Blocker{
		WorkspaceID:  "ws-1",
		Type:         BlockerPermission,
		Summary:      "Write src file",
		FilesAtStake: []string{"src/main.go"},
	}

	res, err := r.Resolve(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Layer != 1 {
		t.Errorf("expected layer=1 (policy), got %d", res.Layer)
	}
	if res.Action != "auto_decided" {
		t.Errorf("expected action=auto_decided, got %s", res.Action)
	}
}

func TestResolverLayering_PolicyDeny(t *testing.T) {
	pe := NewPolicyEngine()
	pe.SetActive("overnight")

	r := NewResolver(pe, nil, nil, 0)

	blocker := Blocker{
		WorkspaceID:  "ws-1",
		Type:         BlockerPermission,
		Summary:      "Modify infra",
		FilesAtStake: []string{"infrastructure/main.tf"},
	}

	res, err := r.Resolve(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Layer != 1 {
		t.Errorf("expected layer=1, got %d", res.Layer)
	}
	if res.Action != "denied" {
		t.Errorf("expected action=denied, got %s", res.Action)
	}
}

func TestResolverLayering_NoPolicy_NoQueue_SkipsToLayer3(t *testing.T) {
	pe := NewPolicyEngine()
	// interactive preset: no rules match

	r := NewResolver(pe, nil, nil, 0)

	blocker := Blocker{
		WorkspaceID:  "ws-1",
		Type:         BlockerDecision,
		Summary:      "Choose session length",
		FilesAtStake: []string{"src/config.go"},
	}

	res, err := r.Resolve(blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Layer != 3 {
		t.Errorf("expected layer=3 (skip), got %d", res.Layer)
	}
	if res.Action != "blocked" {
		t.Errorf("expected action=blocked, got %s", res.Action)
	}
}

// --- Glob Matching Tests ---

func TestMatchGlob_DoubleStarPrefix(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"src/auth/handler.go", "src/**", true},
		{"src/main.go", "src/**", true},
		{"tests/unit/auth_test.go", "tests/**", true},
		{"db/migrations/001.sql", "db/**", true},
		{"other/file.go", "src/**", false},
		{".github/workflows/ci.yml", ".github/**", true},
		{"infrastructure/main.tf", "infrastructure/**", true},
	}

	for _, tt := range tests {
		got := matchGlob(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchGlob_StandardPatterns(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"README.md", "*.md", true},
		{"src/main.go", "*.go", false}, // filepath.Match doesn't cross /
		{"config.toml", "config.*", true},
	}

	for _, tt := range tests {
		got := matchGlob(tt.path, tt.pattern)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}
