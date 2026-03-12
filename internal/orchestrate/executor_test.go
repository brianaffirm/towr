package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// mockRuntime implements Runtime for testing.
type mockRuntime struct {
	mu            sync.Mutex
	spawned       []string
	dispatched    []string
	dispatchCount int
	// perWS tracks per-workspace call counts for stateFunc.
	perWS        map[string]int
	// stateFunc lets tests control what DetectState returns per workspace.
	stateFunc     func(wsID string, callNum int) (string, string, error)
	approveCount  int
	spawnErr      error
	dispatchErr   error
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		perWS: make(map[string]int),
	}
}

func (m *mockRuntime) SpawnWorkspace(id, task, agentType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawned = append(m.spawned, id)
	return m.spawnErr
}

func (m *mockRuntime) DispatchPrompt(wsID, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatched = append(m.dispatched, wsID)
	m.dispatchCount++
	return fmt.Sprintf("d-%04d", m.dispatchCount), m.dispatchErr
}

func (m *mockRuntime) DetectState(wsID string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.perWS[wsID]++
	callNum := m.perWS[wsID]
	if m.stateFunc != nil {
		return m.stateFunc(wsID, callNum)
	}
	return "idle", "done", nil
}

func (m *mockRuntime) SendApprove(wsID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approveCount++
	return nil
}

func (m *mockRuntime) GetWorktreePath(wsID string) string {
	return "/tmp/test/" + wsID
}

func (m *mockRuntime) MergeDeps(wsID string, depIDs []string) error {
	return nil
}

func (m *mockRuntime) AutoCommit(wsID string) error {
	return nil
}

func (m *mockRuntime) LandPR(wsID string) error {
	return nil
}

func (m *mockRuntime) EmitEvent(event store.Event) error {
	return nil
}

// mockLogger captures log output for assertions.
type mockLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *mockLogger) Log(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(format, args...))
}

func TestExecutor_SimpleLinear(t *testing.T) {
	// Two tasks: b depends on a. Each goes working->idle.
	plan := &Plan{
		Name: "linear test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "b", Prompt: "do b", DependsOn: []string{"a"}},
		},
		Settings: Settings{
			PollInterval: "50ms",
		},
	}

	rt := newMockRuntime()
	rt.stateFunc = func(wsID string, callNum int) (string, string, error) {
		// First call: working. Second+: idle with summary.
		if callNum <= 1 {
			return "working", "", nil
		}
		return "idle", "task " + wsID + " done", nil
	}
	logger := &mockLogger{}

	exec := NewExecutor(plan, rt, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exec.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.spawned) != 2 {
		t.Errorf("spawned %d workspaces, want 2", len(rt.spawned))
	}
	// a should be spawned before b (b depends on a).
	if len(rt.spawned) >= 2 && rt.spawned[0] != "a" {
		t.Errorf("first spawn = %q, want %q", rt.spawned[0], "a")
	}
}

func TestExecutor_ParallelTasks(t *testing.T) {
	// Two independent tasks should both be dispatched quickly and complete.
	plan := &Plan{
		Name: "parallel test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "b", Prompt: "do b"},
		},
		Settings: Settings{
			PollInterval: "50ms",
		},
	}

	rt := newMockRuntime()
	rt.stateFunc = func(wsID string, callNum int) (string, string, error) {
		if callNum <= 1 {
			return "working", "", nil
		}
		return "idle", "task " + wsID + " done", nil
	}
	logger := &mockLogger{}

	exec := NewExecutor(plan, rt, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exec.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.spawned) != 2 {
		t.Errorf("spawned %d workspaces, want 2", len(rt.spawned))
	}
}

func TestExecutor_SpawnFailure(t *testing.T) {
	plan := &Plan{
		Name: "fail test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
		},
		Settings: Settings{
			PollInterval: "50ms",
		},
	}

	rt := newMockRuntime()
	rt.spawnErr = fmt.Errorf("tmux not available")
	logger := &mockLogger{}

	exec := NewExecutor(plan, rt, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := exec.Run(ctx)
	if err == nil {
		t.Fatal("expected error for spawn failure")
	}
}

func TestExecutor_AutoApprove(t *testing.T) {
	plan := &Plan{
		Name: "approve test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
		},
		Settings: Settings{
			AutoApprove:  true,
			PollInterval: "50ms",
		},
	}

	rt := newMockRuntime()
	rt.stateFunc = func(wsID string, callNum int) (string, string, error) {
		switch {
		case callNum <= 1:
			return "working", "", nil
		case callNum == 2:
			return "blocked", "", nil
		default:
			return "idle", "done after approve", nil
		}
	}
	logger := &mockLogger{}

	exec := NewExecutor(plan, rt, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exec.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.approveCount == 0 {
		t.Error("expected auto-approve to fire")
	}
}

func TestExecutor_DepContext(t *testing.T) {
	// Verify that dependency context is included in the dispatched prompt.
	plan := &Plan{
		Name: "context test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
			{ID: "b", Prompt: "do b", DependsOn: []string{"a"}},
		},
		Settings: Settings{
			PollInterval: "50ms",
		},
	}

	var capturedPrompts []string
	var promptMu sync.Mutex

	rt := newMockRuntime()
	rt.stateFunc = func(wsID string, callNum int) (string, string, error) {
		if callNum <= 1 {
			return "working", "", nil
		}
		return "idle", "completed work for " + wsID, nil
	}

	// Wrap to capture prompts.
	wrapper := &promptCapturingRuntime{
		mockRuntime:     rt,
		capturedPrompts: &capturedPrompts,
		mu:              &promptMu,
	}
	logger := &mockLogger{}

	exec := NewExecutor(plan, wrapper, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = exec.Run(ctx)

	promptMu.Lock()
	defer promptMu.Unlock()

	// The second dispatch (for "b") should contain context from "a".
	if len(capturedPrompts) < 2 {
		t.Fatalf("expected 2 dispatches, got %d", len(capturedPrompts))
	}
	bPrompt := capturedPrompts[1]
	if !strings.Contains(bPrompt, "Context from completed tasks") {
		t.Errorf("b's prompt should contain dep context, got: %s", bPrompt)
	}
	if !strings.Contains(bPrompt, "completed work for a") {
		t.Errorf("b's prompt should contain a's summary, got: %s", bPrompt)
	}
}

// promptCapturingRuntime wraps mockRuntime to capture dispatched prompts.
type promptCapturingRuntime struct {
	*mockRuntime
	capturedPrompts *[]string
	mu              *sync.Mutex
}

func (r *promptCapturingRuntime) DispatchPrompt(wsID, prompt string) (string, error) {
	r.mu.Lock()
	*r.capturedPrompts = append(*r.capturedPrompts, prompt)
	r.mu.Unlock()
	return r.mockRuntime.DispatchPrompt(wsID, prompt)
}

func TestExecutor_RetryOnExit(t *testing.T) {
	plan := &Plan{
		Name: "retry test",
		Tasks: []Task{
			{ID: "a", Prompt: "do a"},
		},
		Settings: Settings{
			MaxRetries:   1,
			PollInterval: "50ms",
		},
	}

	rt := newMockRuntime()
	rt.stateFunc = func(wsID string, callNum int) (string, string, error) {
		switch callNum {
		case 1:
			// Agent exited immediately without working — should trigger retry.
			return "empty", "", nil
		case 2:
			// After retry, working.
			return "working", "", nil
		default:
			return "idle", "done on retry", nil
		}
	}
	logger := &mockLogger{}

	exec := NewExecutor(plan, rt, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exec.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Should have spawned twice (initial + retry).
	if len(rt.spawned) != 2 {
		t.Errorf("spawned %d times, want 2 (initial + retry)", len(rt.spawned))
	}
}
