package workspace

import (
	"fmt"
	"sync"
)

// MemoryStore is an in-memory implementation of WorkspaceStore for tests.
type MemoryStore struct {
	mu         sync.RWMutex
	workspaces map[string]*Workspace
}

// NewMemoryStore creates a new in-memory workspace store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workspaces: make(map[string]*Workspace),
	}
}

func (s *MemoryStore) Save(ws *Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Store a copy to avoid aliasing.
	cp := *ws
	s.workspaces[ws.ID] = &cp
	return nil
}

func (s *MemoryStore) Get(id string) (*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.workspaces[id]
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", id)
	}
	cp := *ws
	return &cp, nil
}

func (s *MemoryStore) List(filter ListFilter) ([]*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Workspace
	for _, ws := range s.workspaces {
		if filter.RepoRoot != "" && ws.RepoRoot != filter.RepoRoot {
			continue
		}
		if filter.Status != "" && ws.Status != filter.Status {
			continue
		}
		cp := *ws
		result = append(result, &cp)
	}
	return result, nil
}

func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workspaces[id]; !ok {
		return fmt.Errorf("workspace %q not found", id)
	}
	delete(s.workspaces, id)
	return nil
}
