package workspace

import (
	"testing"
	"time"
)

func newTestWorkspace(id, repo string, status WorkspaceStatus) *Workspace {
	return &Workspace{
		ID:       id,
		RepoRoot: repo,
		Status:   status,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestMemoryStore_SaveAndGet(t *testing.T) {
	s := NewMemoryStore()
	ws := newTestWorkspace("test-1", "/repo", StatusReady)

	if err := s.Save(ws); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get("test-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "test-1" {
		t.Errorf("ID = %q, want test-1", got.ID)
	}
}

func TestMemoryStore_Get_NotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Get("missing")
	if err == nil {
		t.Error("expected error for missing workspace")
	}
}

func TestMemoryStore_List(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Save(newTestWorkspace("a", "/repo1", StatusReady))
	_ = s.Save(newTestWorkspace("b", "/repo1", StatusRunning))
	_ = s.Save(newTestWorkspace("c", "/repo2", StatusReady))

	// All.
	all, _ := s.List(ListFilter{})
	if len(all) != 3 {
		t.Errorf("List(all) = %d items, want 3", len(all))
	}

	// Filter by repo.
	repo1, _ := s.List(ListFilter{RepoRoot: "/repo1"})
	if len(repo1) != 2 {
		t.Errorf("List(repo1) = %d items, want 2", len(repo1))
	}

	// Filter by status.
	ready, _ := s.List(ListFilter{Status: StatusReady})
	if len(ready) != 2 {
		t.Errorf("List(ready) = %d items, want 2", len(ready))
	}

	// Filter by both.
	both, _ := s.List(ListFilter{RepoRoot: "/repo1", Status: StatusReady})
	if len(both) != 1 {
		t.Errorf("List(repo1+ready) = %d items, want 1", len(both))
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Save(newTestWorkspace("del", "/repo", StatusReady))

	if err := s.Delete("del"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("del")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMemoryStore_Delete_NotFound(t *testing.T) {
	s := NewMemoryStore()
	err := s.Delete("missing")
	if err == nil {
		t.Error("expected error for deleting missing workspace")
	}
}

func TestMemoryStore_Isolation(t *testing.T) {
	// Verify that modifying a returned workspace doesn't affect the store.
	s := NewMemoryStore()
	ws := newTestWorkspace("iso", "/repo", StatusReady)
	_ = s.Save(ws)

	got, _ := s.Get("iso")
	got.Status = StatusBlocked

	got2, _ := s.Get("iso")
	if got2.Status != StatusReady {
		t.Errorf("store was mutated through returned pointer: status = %q", got2.Status)
	}
}
