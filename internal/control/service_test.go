package control

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func waitForRun(t *testing.T, handle *RunHandle, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run to finish, status=%s", handle.Status)
		default:
			if handle.Status != RunRunning {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func newTestClock() Clock {
	var mu sync.Mutex
	callCount := 0
	return func() time.Time {
		mu.Lock()
		callCount++
		c := callCount
		mu.Unlock()
		return time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC).Add(time.Duration(c) * time.Minute)
	}
}

// ---------------------------------------------------------------------------
// mockStore
// ---------------------------------------------------------------------------

type mockStore struct {
	mu     sync.Mutex
	runs   map[string]*store.Run
	events []store.Event
}

func newMockStore() *mockStore {
	return &mockStore{runs: make(map[string]*store.Run)}
}

func (m *mockStore) CreateRun(run *store.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID] = run
	return nil
}

func (m *mockStore) UpdateRun(run *store.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID] = run
	return nil
}

func (m *mockStore) GetRun(id string) (*store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("run %s not found", id)
	}
	return r, nil
}

func (m *mockStore) ListRuns(repoRoot string) ([]*store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.Run
	for _, r := range m.runs {
		if r.RepoRoot == repoRoot {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockStore) EmitEvent(event store.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockStore) QueryEvents(query store.EventQuery) ([]store.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Event
	for _, e := range m.events {
		if query.Kind != "" && e.Kind != query.Kind {
			continue
		}
		if query.RunID != "" && e.RunID != query.RunID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *mockStore) GetWorkspace(repoRoot, id string) (*store.Workspace, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockStore) eventsByKind(kind string) []store.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Event
	for _, e := range m.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func (m *mockStore) allEvents() []store.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

// ---------------------------------------------------------------------------
// mockRouter
// ---------------------------------------------------------------------------

type mockRouter struct{}

func (r *mockRouter) Route(task TaskSpec, defaultModel string, defaultAgent string) RoutingDecision {
	model := task.Model
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		model = "sonnet"
	}
	return RoutingDecision{
		Model:       model,
		Reason:      "mock",
		Tier:        1,
		CanEscalate: true,
	}
}

func (r *mockRouter) Escalate(prev RoutingDecision) (RoutingDecision, bool) {
	if prev.Tier < 2 {
		return RoutingDecision{
			Model:       "opus",
			Reason:      "escalated",
			Tier:        2,
			CanEscalate: false,
		}, true
	}
	return prev, false
}

// ---------------------------------------------------------------------------
// mockRuntime
// ---------------------------------------------------------------------------

type mockRuntime struct {
	mu            sync.Mutex
	spawned       map[string]bool
	stateOverride map[string]string // taskID -> state to return
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		spawned:       make(map[string]bool),
		stateOverride: make(map[string]string),
	}
}

func (m *mockRuntime) SpawnWorkspace(taskID string, prompt string, agentType string, repoRoot string, depIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawned[taskID] = true
	return nil
}

func (m *mockRuntime) LaunchAndMonitor(taskID string, prompt string, decision RoutingDecision, agentType string, fullAuto bool, done <-chan struct{}) {
}

func (m *mockRuntime) DetectState(taskID string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stateOverride[taskID]; ok {
		return s, "mock summary", nil
	}
	return "idle", "mock summary", nil
}

func (m *mockRuntime) ApproveDialog(taskID string) error { return nil }
func (m *mockRuntime) AutoCommit(taskID string) error    { return nil }
func (m *mockRuntime) CreatePR(taskID string) error      { return nil }
func (m *mockRuntime) GetWorktreePath(taskID string) string {
	return "/tmp/mock/" + taskID
}

func (m *mockRuntime) ComputeCost(taskID string, model string) (int, int, string, float64, float64) {
	return 10000, 30000, "test", 0.50, 1.00
}

func (m *mockRuntime) IsHeadless() bool { return true }

func (m *mockRuntime) wasSpawned(taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spawned[taskID]
}

// ---------------------------------------------------------------------------
// escalationRuntime — returns "empty" first, then "idle"
// ---------------------------------------------------------------------------

type escalationRuntime struct {
	mockRuntime
	callMu sync.Mutex
	calls  map[string]int
}

func newEscalationRuntime() *escalationRuntime {
	return &escalationRuntime{
		mockRuntime: mockRuntime{
			spawned:       make(map[string]bool),
			stateOverride: make(map[string]string),
		},
		calls: make(map[string]int),
	}
}

func (e *escalationRuntime) DetectState(taskID string) (string, string, error) {
	e.callMu.Lock()
	e.calls[taskID]++
	n := e.calls[taskID]
	e.callMu.Unlock()
	if n <= 1 {
		return "empty", "", nil
	}
	return "idle", "done after escalation", nil
}

// ---------------------------------------------------------------------------
// testLogger
// ---------------------------------------------------------------------------

type testLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *testLogger) Log(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------------------
// helper to build a service
// ---------------------------------------------------------------------------

func newTestService(s *mockStore, rt AgentRuntime) *RunService {
	return &RunService{
		Store:   s,
		Runtime: rt,
		Router:  &mockRouter{},
		Policy:  nil,
		Clock:   newTestClock(),
		Logger:  &testLogger{},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStartSingleTask(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "test-plan",
		Tasks: []TaskSpec{
			{ID: "task-1", Prompt: "do something"},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
		},
	}

	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	if handle.Status != RunCompleted {
		t.Fatalf("expected status %s, got %s", RunCompleted, handle.Status)
	}

	// Assert task.routed event emitted.
	routed := ms.eventsByKind(store.EventTaskRouted)
	if len(routed) == 0 {
		t.Fatal("expected task.routed event")
	}

	// Assert run.completed event emitted.
	completed := ms.eventsByKind(store.EventRunCompleted)
	if len(completed) == 0 {
		t.Fatal("expected run.completed event")
	}
}

func TestStartWithDependencies(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "dep-plan",
		Tasks: []TaskSpec{
			{ID: "task-a", Prompt: "first task"},
			{ID: "task-b", Prompt: "depends on a", DependsOn: []string{"task-a"}},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
		},
	}

	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	if handle.Status != RunCompleted {
		t.Fatalf("expected %s, got %s", RunCompleted, handle.Status)
	}

	if !rt.wasSpawned("task-a") {
		t.Fatal("task-a was not spawned")
	}
	if !rt.wasSpawned("task-b") {
		t.Fatal("task-b was not spawned")
	}
}

func TestStartTaskFailure(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	rt.stateOverride["task-fail"] = "empty"
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "fail-plan",
		Tasks: []TaskSpec{
			{ID: "task-fail", Prompt: "will fail"},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
			MaxRetries:   0,
		},
	}

	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	if handle.Status != RunFailed {
		t.Fatalf("expected %s, got %s", RunFailed, handle.Status)
	}
	if handle.TaskStates["task-fail"] != RunFailed {
		t.Fatalf("expected task-fail state %s, got %s", RunFailed, handle.TaskStates["task-fail"])
	}
}

func TestStartEscalationRetry(t *testing.T) {
	ms := newMockStore()
	rt := newEscalationRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "escalation-plan",
		Tasks: []TaskSpec{
			{ID: "task-esc", Prompt: "needs escalation"},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
			MaxRetries:   2,
		},
	}

	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	if handle.Status != RunCompleted {
		t.Fatalf("expected %s, got %s", RunCompleted, handle.Status)
	}

	// Original dispatch emits task.dispatched; the retry path in checkTask
	// does not re-emit task.dispatched, but we can verify the escalation
	// happened by confirming DetectState was called at least twice.
	dispatched := ms.eventsByKind(store.EventTaskDispatched)
	if len(dispatched) < 1 {
		t.Fatalf("expected >= 1 task.dispatched events, got %d", len(dispatched))
	}

	rt.callMu.Lock()
	detectCalls := rt.calls["task-esc"]
	rt.callMu.Unlock()
	if detectCalls < 2 {
		t.Fatalf("expected >= 2 DetectState calls for escalation, got %d", detectCalls)
	}

	// Verify task.completed was emitted (task succeeded after escalation).
	completed := ms.eventsByKind(store.EventTaskCompleted)
	if len(completed) == 0 {
		t.Fatal("expected task.completed event after escalation retry")
	}
}

func TestReconcileDeadPID(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	// Create a run with a dead PID.
	run := &store.Run{
		ID:       "dead-run",
		RepoRoot: "/tmp/test-repo",
		PlanName: "dead-plan",
		Status:   RunRunning,
		OwnerPID: 99999999,
	}
	if err := ms.CreateRun(run); err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}

	if err := svc.Reconcile(context.Background(), "dead-run"); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updated, err := ms.GetRun("dead-run")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if updated.Status != RunFailed {
		t.Fatalf("expected run status %s, got %s", RunFailed, updated.Status)
	}

	recovered := ms.eventsByKind(store.EventRunRecovered)
	if len(recovered) == 0 {
		t.Fatal("expected run.recovered event")
	}
}

func TestDryRun(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		Tasks: []TaskSpec{
			{ID: "task-1", Prompt: "first", Model: "opus"},
			{ID: "task-2", Prompt: "second"},
		},
		Settings: SettingsSnapshot{
			DefaultModel: "sonnet",
		},
	}

	items := svc.DryRun(req)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].Decision.Model != "opus" {
		t.Fatalf("expected task-1 model opus, got %s", items[0].Decision.Model)
	}
	if items[1].Decision.Model != "sonnet" {
		t.Fatalf("expected task-2 model sonnet, got %s", items[1].Decision.Model)
	}
}

func TestBudgetExhaustedTerminatesRun(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "budget-plan",
		Tasks: []TaskSpec{
			{ID: "task-1", Prompt: "first task"},
			{ID: "task-2", Prompt: "second task", DependsOn: []string{"task-1"}},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
		},
		Options: RunOptions{
			Budget: 0.01, // very low budget
		},
	}

	// ComputeCost returns $0.50 per task, so after task-1 the budget is exceeded.
	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	// Run should terminate (not hang forever).
	if handle.Status != RunFailed {
		t.Fatalf("expected run status %s, got %s", RunFailed, handle.Status)
	}

	// task-2 should be failed due to budget exhaustion.
	if handle.TaskStates["task-2"] != RunFailed {
		t.Fatalf("expected task-2 status %s, got %s", RunFailed, handle.TaskStates["task-2"])
	}

	// Verify budget exhaustion event emitted.
	failed := ms.eventsByKind(store.EventTaskFailed)
	budgetFailed := false
	for _, ev := range failed {
		if ev.Data["task_id"] == "task-2" && ev.Data["reason"] == "budget exhausted" {
			budgetFailed = true
		}
	}
	if !budgetFailed {
		t.Fatal("expected task.failed event with reason 'budget exhausted' for task-2")
	}
}

func TestRunEventsHaveRunID(t *testing.T) {
	ms := newMockStore()
	rt := newMockRuntime()
	svc := newTestService(ms, rt)

	req := RunRequest{
		RepoRoot: "/tmp/test-repo",
		PlanName: "event-plan",
		Tasks: []TaskSpec{
			{ID: "task-ev", Prompt: "check events"},
		},
		Settings: SettingsSnapshot{
			PollInterval: 10 * time.Millisecond,
		},
	}

	handle, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForRun(t, handle, 5*time.Second)

	events := ms.allEvents()
	if len(events) == 0 {
		t.Fatal("expected events to be emitted")
	}

	for _, ev := range events {
		if ev.RunID != handle.ID {
			t.Fatalf("event %s (kind=%s) has RunID=%s, expected %s", ev.ID, ev.Kind, ev.RunID, handle.ID)
		}
	}
}
