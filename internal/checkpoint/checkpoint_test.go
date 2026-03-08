package checkpoint

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/workspace"
)

// mockStore implements store.Store for checkpoint testing.
type mockStore struct {
	workspaces map[string]*store.Workspace
}

func newMockStore() *mockStore {
	return &mockStore{workspaces: make(map[string]*store.Workspace)}
}

func (m *mockStore) SaveWorkspace(w *store.Workspace) error {
	m.workspaces[w.RepoRoot+"/"+w.ID] = w
	return nil
}

func (m *mockStore) GetWorkspace(repoRoot, id string) (*store.Workspace, error) {
	w, ok := m.workspaces[repoRoot+"/"+id]
	if !ok {
		return nil, nil
	}
	return w, nil
}

func (m *mockStore) ListWorkspaces(repoRoot string, f store.ListFilter) ([]*store.Workspace, error) {
	return nil, nil
}
func (m *mockStore) DeleteWorkspace(repoRoot, id string) error                       { return nil }
func (m *mockStore) EmitEvent(event store.Event) error                               { return nil }
func (m *mockStore) QueryEvents(query store.EventQuery) ([]store.Event, error)       { return nil, nil }
func (m *mockStore) EnqueueApproval(item store.QueueItem) error                      { return nil }
func (m *mockStore) GetQueue(repoRoot string) ([]store.QueueItem, error)             { return nil, nil }
func (m *mockStore) ResolveQueueItem(id string, res store.Resolution) error          { return nil }
func (m *mockStore) Init(dbPath string) error                                        { return nil }
func (m *mockStore) Close() error                                                    { return nil }

func TestCreateImplicit(t *testing.T) {
	// Stub git commands to avoid needing a real repo.
	origGit := gitCommand
	defer func() { gitCommand = origGit }()

	gitCommand = func(dir string, args ...string) (string, error) {
		switch args[0] {
		case "diff":
			if len(args) > 1 && args[1] == "--stat" {
				return " src/main.go | 10 +++++++---\n 2 files changed, 7 insertions(+), 3 deletions(-)\n", nil
			}
			if len(args) > 1 && args[1] == "--name-only" {
				return "src/main.go\nsrc/util.go\n", nil
			}
		case "log":
			return "abc1234 Add auth handler\ndef5678 Fix token validation\n", nil
		}
		return "", nil
	}

	ms := newMockStore()
	mgr := NewManager(ms)

	ws := &workspace.Workspace{
		ID:           "test-ws",
		RepoRoot:     "/repo",
		BaseBranch:   "main",
		WorktreePath: "/worktree/test-ws",
		Error:        "permission denied: delete migrations",
	}

	cp, err := mgr.CreateImplicit(ws)
	if err != nil {
		t.Fatalf("CreateImplicit failed: %v", err)
	}

	if cp.DiffSummary == "" {
		t.Error("expected non-empty diff summary")
	}
	if len(cp.CommitsOnBranch) != 2 {
		t.Errorf("expected 2 commits, got %d", len(cp.CommitsOnBranch))
	}
	if len(cp.FilesModified) != 2 {
		t.Errorf("expected 2 files modified, got %d", len(cp.FilesModified))
	}
	if cp.BlockerReason != "permission denied: delete migrations" {
		t.Errorf("unexpected blocker reason: %s", cp.BlockerReason)
	}
}

func TestStoreExplicit(t *testing.T) {
	ms := newMockStore()
	// Pre-populate a workspace.
	ms.workspaces["/repo/ws-1"] = &store.Workspace{
		ID:       "ws-1",
		RepoRoot: "/repo",
		Status:   "RUNNING",
	}

	mgr := NewManager(ms)

	explicit := workspace.Checkpoint{
		ProgressSummary: "Completed auth handler",
		RemainingWork:   "Need to add tests",
		KeyDecisions:    []string{"Using JWT over sessions"},
		OpenQuestions:    []string{"Token expiry: 24h or 7d?"},
	}

	err := mgr.StoreExplicit("/repo", "ws-1", explicit)
	if err != nil {
		t.Fatalf("StoreExplicit failed: %v", err)
	}

	// Verify the checkpoint was stored.
	ws := ms.workspaces["/repo/ws-1"]
	if ws.Checkpoint == nil {
		t.Fatal("expected checkpoint to be stored")
	}

	var cp workspace.Checkpoint
	if err := json.Unmarshal(ws.Checkpoint, &cp); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if cp.ProgressSummary != "Completed auth handler" {
		t.Errorf("unexpected progress: %s", cp.ProgressSummary)
	}
	if len(cp.KeyDecisions) != 1 {
		t.Errorf("expected 1 key decision, got %d", len(cp.KeyDecisions))
	}
}

func TestBuildRespawnPrompt_AllSections(t *testing.T) {
	ms := newMockStore()
	mgr := NewManager(ms)

	cp := &workspace.Checkpoint{
		DiffSummary:     "src/main.go | 10 +++---",
		CommitsOnBranch: []string{"abc1234 Add auth", "def5678 Fix token"},
		BlockerReason:   "Permission denied: delete migrations",
		ProgressSummary: "Auth handler done",
		RemainingWork:   "Add integration tests",
		KeyDecisions:    []string{"JWT over sessions"},
		OpenQuestions:    []string{"Token expiry?"},
	}

	ws := &workspace.Workspace{
		ID:       "auth",
		RepoRoot: "/repo",
		Source: workspace.SpawnSource{
			Kind:  workspace.SpawnFromTask,
			Value: "Refactor auth middleware",
		},
		Checkpoint: cp,
	}

	prompt, err := mgr.BuildRespawnPrompt(ws, "Approved: go ahead with JWT")
	if err != nil {
		t.Fatalf("BuildRespawnPrompt failed: %v", err)
	}

	// Verify all required sections are present.
	sections := []string{
		"Original Task",
		"Refactor auth middleware",
		"What Was Already Done",
		"src/main.go",
		"abc1234 Add auth",
		"Why It Paused",
		"Permission denied",
		"Human's Decision",
		"Approved: go ahead with JWT",
		"Previous Agent's Notes",
		"Auth handler done",
		"Add integration tests",
		"JWT over sessions",
		"Token expiry?",
		"Instructions",
		"Do NOT redo work",
	}

	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing expected section/content: %q", s)
		}
	}
}

func TestBuildRespawnPrompt_MinimalCheckpoint(t *testing.T) {
	ms := newMockStore()
	mgr := NewManager(ms)

	ws := &workspace.Workspace{
		ID:       "fix-ci",
		RepoRoot: "/repo",
		Source: workspace.SpawnSource{
			Kind:  workspace.SpawnFromTask,
			Value: "Fix CI pipeline",
		},
	}

	prompt, err := mgr.BuildRespawnPrompt(ws, "Denied")
	if err != nil {
		t.Fatalf("BuildRespawnPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "Fix CI pipeline") {
		t.Error("prompt should contain the original task")
	}
	if !strings.Contains(prompt, "Denied") {
		t.Error("prompt should contain the resolution")
	}
}

func TestNotificationEvent_WebhookFilter(t *testing.T) {
	wh := NewWebhookNotifier("http://example.com/hook")

	// Tier 2 should not fire webhooks.
	err := wh.Notify(NotificationEvent{
		Tier:    2,
		Title:   "Test",
		Message: "Should not fire",
	})
	if err != nil {
		t.Errorf("expected no error for tier 2 webhook, got %v", err)
	}
}

func TestNotificationEvent_SystemFilter(t *testing.T) {
	sn := NewSystemNotifier()

	// Tier 1 (passive) should not fire system notifications.
	err := sn.Notify(NotificationEvent{
		Tier:    1,
		Title:   "Test",
		Message: "Should not fire",
	})
	if err != nil {
		t.Errorf("expected no error for tier 1 system notif, got %v", err)
	}
}
