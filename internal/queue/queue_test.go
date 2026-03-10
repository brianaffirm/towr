package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// mockStore implements a minimal in-memory store.Store for queue testing.
type mockStore struct {
	items    []store.QueueItem
	resolved map[string]store.Resolution
}

func newMockStore() *mockStore {
	return &mockStore{
		resolved: make(map[string]store.Resolution),
	}
}

func (m *mockStore) EnqueueApproval(item store.QueueItem) error {
	m.items = append(m.items, item)
	return nil
}

func (m *mockStore) GetQueue(repoRoot string) ([]store.QueueItem, error) {
	var out []store.QueueItem
	for _, item := range m.items {
		if item.RepoRoot == repoRoot && item.Resolution == "" {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *mockStore) ResolveQueueItem(id string, res store.Resolution) error {
	m.resolved[id] = res
	for i := range m.items {
		if m.items[i].ID == id {
			m.items[i].Resolution = res.Action
			m.items[i].ResolvedBy = res.ResolvedBy
			m.items[i].ResolvedAt = time.Now().Format(time.RFC3339)
		}
	}
	return nil
}

// Unused Store interface methods — stubs.
func (m *mockStore) SaveWorkspace(w *store.Workspace) error                          { return nil }
func (m *mockStore) GetWorkspace(repoRoot, id string) (*store.Workspace, error)      { return nil, nil }
func (m *mockStore) ListWorkspaces(repoRoot string, f store.ListFilter) ([]*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) DeleteWorkspace(repoRoot, id string) error         { return nil }
func (m *mockStore) EmitEvent(event store.Event) error                 { return nil }
func (m *mockStore) QueryEvents(query store.EventQuery) ([]store.Event, error) { return nil, nil }
func (m *mockStore) Init(dbPath string) error                          { return nil }
func (m *mockStore) Close() error                                      { return nil }

func TestQueueManager_Enqueue(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	item, err := qm.Enqueue("ws-1", "permission", "Delete file", "wants to delete foo.go", []string{"foo.go"}, 6*time.Hour)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if item.WorkspaceID != "ws-1" {
		t.Errorf("expected workspace_id=ws-1, got %s", item.WorkspaceID)
	}
	if item.Type != "permission" {
		t.Errorf("expected type=permission, got %s", item.Type)
	}
	if item.Priority != "!" {
		t.Errorf("expected priority=!, got %s", item.Priority)
	}
	if item.Summary != "Delete file" {
		t.Errorf("expected summary='Delete file', got %s", item.Summary)
	}

	// Context should contain the detail and files.
	var ctx map[string]interface{}
	if err := json.Unmarshal(item.Context, &ctx); err != nil {
		t.Fatalf("unmarshal context: %v", err)
	}
	if ctx["detail"] != "wants to delete foo.go" {
		t.Errorf("unexpected context detail: %v", ctx["detail"])
	}
}

func TestQueueManager_List(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	_, _ = qm.Enqueue("ws-1", "permission", "Item 1", "", nil, 0)
	_, _ = qm.Enqueue("ws-2", "decision", "Item 2", "", nil, 0)

	items, err := qm.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestQueueManager_Approve(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	item, _ := qm.Enqueue("ws-1", "permission", "Test", "", nil, 0)
	err := qm.Approve(item.ID, "human")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	res := ms.resolved[item.ID]
	if res.Action != "approved" {
		t.Errorf("expected resolution=approved, got %s", res.Action)
	}
	if res.ResolvedBy != "human" {
		t.Errorf("expected resolved_by=human, got %s", res.ResolvedBy)
	}
}

func TestQueueManager_Deny(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	item, _ := qm.Enqueue("ws-1", "permission", "Test", "", nil, 0)
	err := qm.Deny(item.ID, "human")
	if err != nil {
		t.Fatalf("deny failed: %v", err)
	}

	res := ms.resolved[item.ID]
	if res.Action != "denied" {
		t.Errorf("expected resolution=denied, got %s", res.Action)
	}
}

func TestQueueManager_Respond(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	item, _ := qm.Enqueue("ws-1", "decision", "Choose timeout", "", nil, 0)
	err := qm.Respond(item.ID, "human", "Use 24h session expiry")
	if err != nil {
		t.Fatalf("respond failed: %v", err)
	}

	res := ms.resolved[item.ID]
	if res.Action != "responded" {
		t.Errorf("expected resolution=responded, got %s", res.Action)
	}
	if res.Response != "Use 24h session expiry" {
		t.Errorf("unexpected response: %s", res.Response)
	}
}

func TestQueueManager_PriorityCalculation(t *testing.T) {
	ms := newMockStore()
	qm := NewManager(ms, "/repo")

	// Permission with many files → critical
	item, _ := qm.Enqueue("ws-1", "permission", "test", "", []string{"a", "b", "c", "d"}, 0)
	if item.Priority != "!!" {
		t.Errorf("expected !!, got %s", item.Priority)
	}

	// Gate → critical
	item2, _ := qm.Enqueue("ws-2", "gate", "test", "", nil, 0)
	if item2.Priority != "!!" {
		t.Errorf("expected !!, got %s", item2.Priority)
	}

	// External → normal
	item3, _ := qm.Enqueue("ws-3", "external", "test", "", nil, 0)
	if item3.Priority != "." {
		t.Errorf("expected '.', got %s", item3.Priority)
	}
}
