package workspace

import (
	"os"
	"strings"
	"testing"
)

func TestManager_Create(t *testing.T) {
	repo := initTestRepo(t)
	amuxHome := t.TempDir()
	t.Setenv("AMUX_HOME", amuxHome)

	store := NewMemoryStore()
	mgr := NewManager(store)

	ws, err := mgr.Create(CreateOpts{
		ID:         "feat-auth",
		RepoRoot:   repo,
		BaseBranch: "main",
		Source:     SpawnSource{Kind: SpawnFromTask, Value: "add auth"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if ws.Status != StatusReady {
		t.Errorf("status = %q, want READY", ws.Status)
	}
	if ws.Branch != "amux/feat-auth" {
		t.Errorf("branch = %q, want amux/feat-auth", ws.Branch)
	}
	if ws.BaseRef == "" {
		t.Error("base_ref should be set")
	}

	// Worktree should exist on disk.
	if _, err := os.Stat(ws.WorktreePath); os.IsNotExist(err) {
		t.Error("worktree path should exist")
	}

	// Branch should exist.
	exists, _ := BranchExists(repo, "amux/feat-auth")
	if !exists {
		t.Error("branch should exist after create")
	}
}

func TestManager_Create_DuplicateID(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	opts := CreateOpts{
		ID:         "dup",
		RepoRoot:   repo,
		BaseBranch: "main",
		Source:     SpawnSource{Kind: SpawnFromTask, Value: "task"},
	}

	_, err := mgr.Create(opts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Create(opts)
	if err == nil {
		t.Error("expected error on duplicate ID")
	}
}

func TestManager_Create_Validation(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store)

	tests := []struct {
		name string
		opts CreateOpts
	}{
		{"no ID", CreateOpts{RepoRoot: "/r", BaseBranch: "main"}},
		{"no repo", CreateOpts{ID: "x", BaseBranch: "main"}},
		{"no base", CreateOpts{ID: "x", RepoRoot: "/r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.Create(tt.opts)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestManager_Create_RejectsOverlappingPaths(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	_, err := mgr.Create(CreateOpts{
		ID:         "overlap-test",
		RepoRoot:   repo,
		BaseBranch: "main",
		Source:     SpawnSource{Kind: SpawnFromTask, Value: "test"},
		CopyPaths:  []string{"CLAUDE.md", ".coflow/"},
		LinkPaths:  []string{".coflow"},
	})
	if err == nil {
		t.Fatal("expected error for overlapping copy_paths and link_paths")
	}
	if got := err.Error(); !strings.Contains(got, "copy_paths") || !strings.Contains(got, "link_paths") {
		t.Errorf("error = %q, want mention of copy_paths and link_paths", got)
	}
}

func TestManager_Get(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	ws, _ := mgr.Create(CreateOpts{
		ID: "get-test", RepoRoot: repo, BaseBranch: "main",
		Source: SpawnSource{Kind: SpawnFromTask, Value: "t"},
	})

	got, err := mgr.Get(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ws.ID {
		t.Errorf("ID = %q, want %q", got.ID, ws.ID)
	}
}

func TestManager_List(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	for _, id := range []string{"a", "b", "c"} {
		_, err := mgr.Create(CreateOpts{
			ID: id, RepoRoot: repo, BaseBranch: "main",
			Source: SpawnSource{Kind: SpawnFromTask, Value: id},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	all, err := mgr.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("List(all) = %d, want 3", len(all))
	}
}

func TestManager_GetByRepo(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	_, _ = mgr.Create(CreateOpts{
		ID: "r1", RepoRoot: repo, BaseBranch: "main",
		Source: SpawnSource{Kind: SpawnFromTask, Value: "t"},
	})

	got, err := mgr.GetByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("GetByRepo = %d, want 1", len(got))
	}
}

func TestManager_UpdateStatus(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	ws, _ := mgr.Create(CreateOpts{
		ID: "status-test", RepoRoot: repo, BaseBranch: "main",
		Source: SpawnSource{Kind: SpawnFromTask, Value: "t"},
	})

	if err := mgr.UpdateStatus(ws.ID, StatusRunning, "agent started"); err != nil {
		t.Fatal(err)
	}

	got, _ := mgr.Get(ws.ID)
	if got.Status != StatusRunning {
		t.Errorf("status = %q, want RUNNING", got.Status)
	}
	if got.StatusDetail != "agent started" {
		t.Errorf("detail = %q, want 'agent started'", got.StatusDetail)
	}
}

func TestManager_UpdateStatus_Invalid(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	ws, _ := mgr.Create(CreateOpts{
		ID: "inv", RepoRoot: repo, BaseBranch: "main",
		Source: SpawnSource{Kind: SpawnFromTask, Value: "t"},
	})

	err := mgr.UpdateStatus(ws.ID, "BOGUS", "")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestManager_Delete(t *testing.T) {
	repo := initTestRepo(t)
	t.Setenv("AMUX_HOME", t.TempDir())

	store := NewMemoryStore()
	mgr := NewManager(store)

	ws, _ := mgr.Create(CreateOpts{
		ID: "del-test", RepoRoot: repo, BaseBranch: "main",
		Source: SpawnSource{Kind: SpawnFromTask, Value: "t"},
	})

	if err := mgr.Delete(ws.ID); err != nil {
		t.Fatal(err)
	}

	// Workspace should be gone from store.
	_, err := mgr.Get(ws.ID)
	if err == nil {
		t.Error("expected error after delete")
	}

	// Branch should be gone.
	exists, _ := BranchExists(repo, ws.Branch)
	if exists {
		t.Error("branch should be deleted")
	}
}
