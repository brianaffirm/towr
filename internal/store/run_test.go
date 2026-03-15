package store

import (
	"testing"
	"time"
)

func TestCreateAndGetRun(t *testing.T) {
	s := newTestStore(t)

	run := &Run{
		ID:          "run-001",
		RepoRoot:    "/repo",
		PlanName:    "deploy.yaml",
		PlanContent: "tasks:\n  - name: build",
		Status:      "pending",
		OwnerPID:    12345,
		FullAuto:    true,
		Budget:      5.0,
		CreatedAt:   "2026-03-14T10:00:00Z",
		UpdatedAt:   "2026-03-14T10:00:00Z",
	}

	if err := s.CreateRun(run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	got, err := s.GetRun("run-001")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected run, got nil")
	}
	if got.ID != "run-001" {
		t.Errorf("ID = %q, want run-001", got.ID)
	}
	if got.RepoRoot != "/repo" {
		t.Errorf("RepoRoot = %q, want /repo", got.RepoRoot)
	}
	if got.PlanName != "deploy.yaml" {
		t.Errorf("PlanName = %q, want deploy.yaml", got.PlanName)
	}
	if got.PlanContent != "tasks:\n  - name: build" {
		t.Errorf("PlanContent = %q, want tasks content", got.PlanContent)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if got.OwnerPID != 12345 {
		t.Errorf("OwnerPID = %d, want 12345", got.OwnerPID)
	}
	if !got.FullAuto {
		t.Error("FullAuto = false, want true")
	}
	if got.Budget != 5.0 {
		t.Errorf("Budget = %f, want 5.0", got.Budget)
	}
	if got.StartedAt != "" {
		t.Errorf("StartedAt = %q, want empty", got.StartedAt)
	}
	if got.FinishedAt != "" {
		t.Errorf("FinishedAt = %q, want empty", got.FinishedAt)
	}
}

func TestUpdateRun(t *testing.T) {
	s := newTestStore(t)

	run := &Run{
		ID:          "run-002",
		RepoRoot:    "/repo",
		PlanName:    "test.yaml",
		PlanContent: "tasks: []",
		Status:      "pending",
		CreatedAt:   "2026-03-14T10:00:00Z",
		UpdatedAt:   "2026-03-14T10:00:00Z",
	}
	if err := s.CreateRun(run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	run.Status = "running"
	run.StartedAt = "2026-03-14T10:01:00Z"
	run.UpdatedAt = "2026-03-14T10:01:00Z"
	if err := s.UpdateRun(run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, err := s.GetRun("run-002")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.StartedAt != "2026-03-14T10:01:00Z" {
		t.Errorf("StartedAt = %q, want 2026-03-14T10:01:00Z", got.StartedAt)
	}
}

func TestListRuns(t *testing.T) {
	s := newTestStore(t)

	runs := []*Run{
		{ID: "run-a", RepoRoot: "/repo1", PlanName: "a.yaml", PlanContent: "a", Status: "done", CreatedAt: "2026-03-14T10:00:00Z", UpdatedAt: "2026-03-14T10:00:00Z"},
		{ID: "run-b", RepoRoot: "/repo1", PlanName: "b.yaml", PlanContent: "b", Status: "running", CreatedAt: "2026-03-14T11:00:00Z", UpdatedAt: "2026-03-14T11:00:00Z"},
		{ID: "run-c", RepoRoot: "/repo2", PlanName: "c.yaml", PlanContent: "c", Status: "pending", CreatedAt: "2026-03-14T12:00:00Z", UpdatedAt: "2026-03-14T12:00:00Z"},
	}
	for _, r := range runs {
		if err := s.CreateRun(r); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}

	list, err := s.ListRuns("/repo1")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 runs for /repo1, got %d", len(list))
	}
	// Should be ordered by created_at DESC.
	if list[0].ID != "run-b" {
		t.Errorf("first run = %q, want run-b (most recent)", list[0].ID)
	}

	list, err = s.ListRuns("/repo2")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 run for /repo2, got %d", len(list))
	}
}

func TestGetRunNotFound(t *testing.T) {
	s := newTestStore(t)

	got, err := s.GetRun("nonexistent")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for nonexistent run, got %+v", got)
	}
}

func TestEmitEventWithRunID(t *testing.T) {
	s := newTestStore(t)

	e := Event{
		ID:        "evt-run-001",
		Timestamp: time.Now().UTC(),
		Kind:      EventRunCreated,
		RepoRoot:  "/repo",
		RunID:     "run-xyz",
		Actor:     "human:brian",
		Data:      map[string]interface{}{"plan": "deploy.yaml"},
	}
	if err := s.EmitEvent(e); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	// Query by RunID.
	events, err := s.QueryEvents(EventQuery{RunID: "run-xyz"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event for run-xyz, got %d", len(events))
	}
	if events[0].RunID != "run-xyz" {
		t.Errorf("RunID = %q, want run-xyz", events[0].RunID)
	}
	if events[0].Kind != EventRunCreated {
		t.Errorf("Kind = %q, want %s", events[0].Kind, EventRunCreated)
	}

	// Emit another event without RunID — should not appear in run query.
	e2 := Event{
		ID:        "evt-no-run",
		Timestamp: time.Now().UTC(),
		Kind:      EventWorkspaceCreated,
		RepoRoot:  "/repo",
	}
	if err := s.EmitEvent(e2); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	events, err = s.QueryEvents(EventQuery{RunID: "run-xyz"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected still 1 event for run-xyz, got %d", len(events))
	}
}
