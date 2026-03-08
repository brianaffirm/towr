package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s := NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInitCreatesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s := NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer s.Close()

	// Audit log should also exist.
	auditPath := filepath.Join(dir, "audit.jsonl")
	if _, err := os.Stat(auditPath); err != nil {
		t.Fatalf("audit.jsonl not created: %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	s := newTestStore(t)
	ver, err := getSchemaVersion(s.db)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if ver != currentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion, ver)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	s := newTestStore(t)
	// Running migrate again should be a no-op.
	if err := migrate(s.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestEmitAndQueryEvents(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UTC()
	e := Event{
		ID:          "evt-001",
		Timestamp:   now,
		Kind:        EventWorkspaceCreated,
		WorkspaceID: "auth",
		RepoRoot:    "/repo",
		Runtime:     "claude-code",
		Actor:       "human:brian",
		Data:        map[string]interface{}{"source": "task", "base_branch": "main"},
	}

	if err := s.EmitEvent(e); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	// Query all events.
	events, err := s.QueryEvents(EventQuery{})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventWorkspaceCreated {
		t.Errorf("expected kind %s, got %s", EventWorkspaceCreated, events[0].Kind)
	}
	if events[0].Data["source"] != "task" {
		t.Errorf("expected data.source=task, got %v", events[0].Data["source"])
	}

	// Query by workspace.
	events, err = s.QueryEvents(EventQuery{WorkspaceID: "auth"})
	if err != nil {
		t.Fatalf("QueryEvents by workspace: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Query by kind.
	events, err = s.QueryEvents(EventQuery{Kind: EventWorkspaceLanded})
	if err != nil {
		t.Fatalf("QueryEvents by kind: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Query with limit.
	events, err = s.QueryEvents(EventQuery{Limit: 0})
	if err != nil {
		t.Fatalf("QueryEvents with limit: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestEventQuerySinceUntil(t *testing.T) {
	s := newTestStore(t)

	base := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		e := Event{
			ID:        fmt.Sprintf("evt-%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Hour),
			Kind:      EventWorkspaceCreated,
			RepoRoot:  "/repo",
		}
		if err := s.EmitEvent(e); err != nil {
			t.Fatalf("EmitEvent: %v", err)
		}
	}

	since := base.Add(30 * time.Minute)
	events, err := s.QueryEvents(EventQuery{Since: &since})
	if err != nil {
		t.Fatalf("QueryEvents since: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events since, got %d", len(events))
	}

	until := base.Add(90 * time.Minute)
	events, err = s.QueryEvents(EventQuery{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("QueryEvents since+until: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event in range, got %d", len(events))
	}
}

func TestSaveGetWorkspace(t *testing.T) {
	s := newTestStore(t)

	exitCode := 0
	w := &Workspace{
		ID:         "auth",
		RepoRoot:   "/repo",
		BaseBranch: "main",
		Branch:     "amux/auth",
		Status:     "READY",
		ExitCode:   &exitCode,
		Checkpoint: json.RawMessage(`{"progress":"50%"}`),
	}

	if err := s.SaveWorkspace(w); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	got, err := s.GetWorkspace("/repo", "auth")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got == nil {
		t.Fatal("expected workspace, got nil")
	}
	if got.Status != "READY" {
		t.Errorf("expected status READY, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", got.ExitCode)
	}
	if string(got.Checkpoint) != `{"progress":"50%"}` {
		t.Errorf("unexpected checkpoint: %s", got.Checkpoint)
	}
}

func TestSaveWorkspaceUpsert(t *testing.T) {
	s := newTestStore(t)

	w := &Workspace{ID: "auth", RepoRoot: "/repo", Status: "CREATING"}
	if err := s.SaveWorkspace(w); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	w.Status = "READY"
	if err := s.SaveWorkspace(w); err != nil {
		t.Fatalf("SaveWorkspace upsert: %v", err)
	}

	got, err := s.GetWorkspace("/repo", "auth")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got.Status != "READY" {
		t.Errorf("expected READY after upsert, got %s", got.Status)
	}
}

func TestGetWorkspaceNotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetWorkspace("/repo", "nonexistent")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing workspace, got %+v", got)
	}
}

func TestListWorkspaces(t *testing.T) {
	s := newTestStore(t)

	for _, ws := range []Workspace{
		{ID: "a", RepoRoot: "/repo1", Status: "READY"},
		{ID: "b", RepoRoot: "/repo1", Status: "RUNNING"},
		{ID: "c", RepoRoot: "/repo2", Status: "READY"},
	} {
		w := ws
		if err := s.SaveWorkspace(&w); err != nil {
			t.Fatalf("SaveWorkspace: %v", err)
		}
	}

	// All in repo1.
	list, err := s.ListWorkspaces("/repo1", ListFilter{})
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 workspaces in repo1, got %d", len(list))
	}

	// Filter by status.
	list, err = s.ListWorkspaces("/repo1", ListFilter{Status: "READY"})
	if err != nil {
		t.Fatalf("ListWorkspaces status: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 READY workspace, got %d", len(list))
	}

	// All repos.
	list, err = s.ListWorkspaces("", ListFilter{AllRepos: true})
	if err != nil {
		t.Fatalf("ListWorkspaces all repos: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 workspaces across all repos, got %d", len(list))
	}
}

func TestDeleteWorkspace(t *testing.T) {
	s := newTestStore(t)

	w := &Workspace{ID: "auth", RepoRoot: "/repo", Status: "READY"}
	if err := s.SaveWorkspace(w); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	if err := s.DeleteWorkspace("/repo", "auth"); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	got, err := s.GetWorkspace("/repo", "auth")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}

	// Deleting again should error.
	if err := s.DeleteWorkspace("/repo", "auth"); err == nil {
		t.Fatal("expected error deleting nonexistent workspace")
	}
}

func TestEnqueueAndGetQueue(t *testing.T) {
	s := newTestStore(t)

	item := QueueItem{
		ID:          "q-001",
		WorkspaceID: "auth",
		RepoRoot:    "/repo",
		Type:        "permission",
		Priority:    "high",
		Summary:     "Delete migration file",
	}

	if err := s.EnqueueApproval(item); err != nil {
		t.Fatalf("EnqueueApproval: %v", err)
	}

	items, err := s.GetQueue("/repo")
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 queue item, got %d", len(items))
	}
	if items[0].Summary != "Delete migration file" {
		t.Errorf("unexpected summary: %s", items[0].Summary)
	}
}

func TestResolveQueueItem(t *testing.T) {
	s := newTestStore(t)

	item := QueueItem{
		ID:       "q-001",
		RepoRoot: "/repo",
		Type:     "permission",
		Priority: "high",
	}
	if err := s.EnqueueApproval(item); err != nil {
		t.Fatalf("EnqueueApproval: %v", err)
	}

	res := Resolution{Action: "approved", ResolvedBy: "human:brian"}
	if err := s.ResolveQueueItem("q-001", res); err != nil {
		t.Fatalf("ResolveQueueItem: %v", err)
	}

	// Resolved items should not appear in GetQueue.
	items, err := s.GetQueue("/repo")
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 unresolved items, got %d", len(items))
	}

	// Resolving nonexistent item should error.
	if err := s.ResolveQueueItem("nonexistent", res); err == nil {
		t.Fatal("expected error resolving nonexistent item")
	}
}

func TestAuditLogWritten(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s := NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer s.Close()

	e := Event{
		ID:        "evt-audit",
		Timestamp: time.Now().UTC(),
		Kind:      EventWorkspaceLanded,
		Data:      map[string]interface{}{"merge_commit": "abc123"},
	}
	if err := s.EmitEvent(e); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	auditPath := filepath.Join(dir, "audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	var logged Event
	if err := json.Unmarshal(data, &logged); err != nil {
		t.Fatalf("unmarshal audit line: %v", err)
	}
	if logged.Kind != EventWorkspaceLanded {
		t.Errorf("expected kind %s in audit, got %s", EventWorkspaceLanded, logged.Kind)
	}
}

func TestStoreInterface(t *testing.T) {
	// Compile-time check that SQLiteStore implements Store.
	var _ Store = (*SQLiteStore)(nil)
}

// ---------- parseWorkspaceRef ----------

func TestParseWorkspaceRef(t *testing.T) {
	tests := []struct {
		ref      string
		wantRepo string
		wantID   string
	}{
		{"auth", "", "auth"},
		{"myapp:auth", "myapp", "auth"},
		{"backend:auth", "backend", "auth"},
		{"my-app:fix-bug", "my-app", "fix-bug"},
		{"repo:id:with:colons", "repo", "id:with:colons"},
		{":bare", "", ":bare"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			repo, id := parseWorkspaceRef(tt.ref)
			if repo != tt.wantRepo {
				t.Errorf("parseWorkspaceRef(%q) repo = %q, want %q", tt.ref, repo, tt.wantRepo)
			}
			if id != tt.wantID {
				t.Errorf("parseWorkspaceRef(%q) id = %q, want %q", tt.ref, id, tt.wantID)
			}
		})
	}
}

// ---------- FindWorkspaceByID ----------

// setupRepoDBs creates a reposDir with multiple repo databases containing workspaces.
// repos is a map of repoBasename -> []Workspace to save in that repo's DB.
func setupRepoDBs(t *testing.T, repos map[string][]Workspace) string {
	t.Helper()
	reposDir := t.TempDir()

	for repoName, workspaces := range repos {
		repoDir := filepath.Join(reposDir, repoName)
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", repoDir, err)
		}
		dbPath := filepath.Join(repoDir, "state.db")
		s := NewSQLiteStore()
		if err := s.Init(dbPath); err != nil {
			t.Fatalf("Init %s: %v", dbPath, err)
		}
		for i := range workspaces {
			if err := s.SaveWorkspace(&workspaces[i]); err != nil {
				t.Fatalf("SaveWorkspace: %v", err)
			}
		}
		s.Close()
	}
	return reposDir
}

func TestFindWorkspaceByID_BareID_SingleMatch(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
	})

	ws, err := FindWorkspaceByID(reposDir, "auth")
	if err != nil {
		t.Fatalf("FindWorkspaceByID: %v", err)
	}
	if ws.ID != "auth" {
		t.Errorf("expected ID auth, got %s", ws.ID)
	}
	if ws.RepoRoot != "/Users/you/myapp" {
		t.Errorf("expected RepoRoot /Users/you/myapp, got %s", ws.RepoRoot)
	}
}

func TestFindWorkspaceByID_BareID_Ambiguous(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
		"backend": {
			{ID: "auth", RepoRoot: "/Users/you/backend", Status: "RUNNING"},
		},
	})

	_, err := FindWorkspaceByID(reposDir, "auth")
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "multiple repos") {
		t.Errorf("expected ambiguity message, got: %s", errStr)
	}
	// Should suggest repo:id syntax.
	if !strings.Contains(errStr, ":auth") {
		t.Errorf("expected repo:id suggestion in error, got: %s", errStr)
	}
}

func TestFindWorkspaceByID_RepoPrefix_Disambiguates(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
		"backend": {
			{ID: "auth", RepoRoot: "/Users/you/backend", Status: "RUNNING"},
		},
	})

	ws, err := FindWorkspaceByID(reposDir, "myapp:auth")
	if err != nil {
		t.Fatalf("FindWorkspaceByID: %v", err)
	}
	if ws.RepoRoot != "/Users/you/myapp" {
		t.Errorf("expected RepoRoot /Users/you/myapp, got %s", ws.RepoRoot)
	}

	ws, err = FindWorkspaceByID(reposDir, "backend:auth")
	if err != nil {
		t.Fatalf("FindWorkspaceByID: %v", err)
	}
	if ws.RepoRoot != "/Users/you/backend" {
		t.Errorf("expected RepoRoot /Users/you/backend, got %s", ws.RepoRoot)
	}
}

func TestFindWorkspaceByID_RepoPrefix_NotFound(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
	})

	_, err := FindWorkspaceByID(reposDir, "myapp:nonexistent")
	if err == nil {
		t.Fatal("expected not found error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", err.Error())
	}
}

// ---------- ListAllWorkspaces ----------

func TestListAllWorkspaces_MultipleRepos(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
			{ID: "login", RepoRoot: "/Users/you/myapp", Status: "RUNNING"},
		},
		"backend": {
			{ID: "api", RepoRoot: "/Users/you/backend", Status: "READY"},
		},
	})

	wss, err := ListAllWorkspaces(reposDir)
	if err != nil {
		t.Fatalf("ListAllWorkspaces: %v", err)
	}
	if len(wss) != 3 {
		t.Fatalf("expected 3 workspaces, got %d", len(wss))
	}

	// Verify we got workspaces from both repos.
	ids := map[string]bool{}
	for _, ws := range wss {
		ids[ws.ID] = true
	}
	for _, id := range []string{"auth", "login", "api"} {
		if !ids[id] {
			t.Errorf("expected workspace %q in results", id)
		}
	}
}

func TestListAllWorkspaces_EmptyReposDir(t *testing.T) {
	reposDir := t.TempDir()

	wss, err := ListAllWorkspaces(reposDir)
	if err != nil {
		t.Fatalf("ListAllWorkspaces: %v", err)
	}
	if len(wss) != 0 {
		t.Fatalf("expected 0 workspaces, got %d", len(wss))
	}
}

func TestListAllWorkspaces_SkipsBadDB(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
	})

	// Create a corrupt database.
	badDir := filepath.Join(reposDir, "corrupt")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "state.db"), []byte("not a db"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	wss, err := ListAllWorkspaces(reposDir)
	if err != nil {
		t.Fatalf("ListAllWorkspaces: %v", err)
	}
	// Should still get the workspace from the good repo.
	if len(wss) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(wss))
	}
	if wss[0].ID != "auth" {
		t.Errorf("expected workspace auth, got %s", wss[0].ID)
	}
}

// ---------- Non-repo workspace support ----------

func TestSaveAndGetNonRepoWorkspace(t *testing.T) {
	s := newTestStore(t)

	ws := &Workspace{
		ID:           "brainstorm",
		RepoRoot:     "", // non-repo workspace
		WorktreePath: "/Users/me/notes/q2",
		SourceKind:   "task",
		SourceValue:  "brainstorm auth redesign",
		Status:       "READY",
	}
	if err := s.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetWorkspace("", "brainstorm")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected workspace, got nil")
	}
	if got.WorktreePath != "/Users/me/notes/q2" {
		t.Errorf("WorktreePath = %q, want /Users/me/notes/q2", got.WorktreePath)
	}
	if got.RepoRoot != "" {
		t.Errorf("RepoRoot = %q, want empty", got.RepoRoot)
	}
}

func TestSaveNonRepoWorkspaceUpsert(t *testing.T) {
	s := newTestStore(t)

	ws := &Workspace{
		ID:       "brainstorm",
		RepoRoot: "",
		Status:   "CREATING",
	}
	if err := s.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	ws.Status = "READY"
	if err := s.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetWorkspace("", "brainstorm")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected workspace after upsert, got nil")
	}
	if got.Status != "READY" {
		t.Errorf("Status = %q after upsert, want READY", got.Status)
	}
}

func TestListWorkspacesIncludesNonRepo(t *testing.T) {
	s := newTestStore(t)

	for _, ws := range []Workspace{
		{ID: "repo-ws", RepoRoot: "/repo", Status: "READY"},
		{ID: "non-repo-ws", RepoRoot: "", Status: "READY", WorktreePath: "/Users/me/notes"},
	} {
		w := ws
		if err := s.SaveWorkspace(&w); err != nil {
			t.Fatalf("SaveWorkspace: %v", err)
		}
	}

	// AllRepos=true should include both repo and non-repo workspaces.
	list, err := s.ListWorkspaces("", ListFilter{AllRepos: true})
	if err != nil {
		t.Fatalf("ListWorkspaces AllRepos: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 workspaces with AllRepos, got %d", len(list))
	}

	// Filtering by specific repo should NOT include non-repo workspaces.
	list, err = s.ListWorkspaces("/repo", ListFilter{})
	if err != nil {
		t.Fatalf("ListWorkspaces /repo: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 workspace for /repo, got %d", len(list))
	}
	if list[0].ID != "repo-ws" {
		t.Errorf("expected repo-ws, got %s", list[0].ID)
	}

	// Empty repoRoot with AllRepos=false — should return only non-repo workspaces.
	list, err = s.ListWorkspaces("", ListFilter{})
	if err != nil {
		t.Fatalf("ListWorkspaces empty repo: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 non-repo workspace, got %d", len(list))
	}
	if list[0].ID != "non-repo-ws" {
		t.Errorf("expected non-repo-ws, got %s", list[0].ID)
	}
}

func TestFindWorkspaceByID_NonRepoInMultiRepoDB(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
			{ID: "brainstorm", RepoRoot: "", Status: "READY", WorktreePath: "/Users/me/notes"},
		},
	})

	ws, err := FindWorkspaceByID(reposDir, "brainstorm")
	if err != nil {
		t.Fatalf("FindWorkspaceByID: %v", err)
	}
	if ws.ID != "brainstorm" {
		t.Errorf("expected ID brainstorm, got %s", ws.ID)
	}
	if ws.RepoRoot != "" {
		t.Errorf("expected empty RepoRoot, got %q", ws.RepoRoot)
	}
}

func TestDeleteNonRepoWorkspace(t *testing.T) {
	s := newTestStore(t)

	ws := &Workspace{ID: "brainstorm", RepoRoot: "", Status: "READY"}
	if err := s.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteWorkspace("", "brainstorm"); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	got, err := s.GetWorkspace("", "brainstorm")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestFindWorkspaceByID_NonexistentRepo(t *testing.T) {
	reposDir := setupRepoDBs(t, map[string][]Workspace{
		"myapp": {
			{ID: "auth", RepoRoot: "/Users/you/myapp", Status: "READY"},
		},
	})

	_, err := FindWorkspaceByID(reposDir, "nonexistent:auth")
	if err == nil {
		t.Fatal("expected not found error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", err.Error())
	}
}

