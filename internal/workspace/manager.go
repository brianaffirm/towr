package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/brianho/amux/internal/config"
)

// WorkspaceStore is the persistence interface for workspaces.
// In-memory for now; SQLite in a later task.
type WorkspaceStore interface {
	Save(ws *Workspace) error
	Get(id string) (*Workspace, error)
	List(filter ListFilter) ([]*Workspace, error)
	Delete(id string) error
}

// Manager implements workspace CRUD operations.
type Manager struct {
	store WorkspaceStore
}

// NewManager creates a new workspace manager with the given store.
func NewManager(store WorkspaceStore) *Manager {
	return &Manager{store: store}
}

// Create provisions a new workspace: creates branch, worktree, and metadata record.
func (m *Manager) Create(opts CreateOpts) (*Workspace, error) {
	if opts.ID == "" {
		return nil, fmt.Errorf("workspace ID is required")
	}
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if opts.BaseBranch == "" {
		return nil, fmt.Errorf("base branch is required")
	}

	// Check for ID collision.
	if existing, err := m.store.Get(opts.ID); err == nil && existing != nil {
		return nil, fmt.Errorf("workspace %q already exists", opts.ID)
	}

	branch := BranchName(opts.ID)

	// Resolve base ref (HEAD of base branch).
	baseRef, err := GetHeadRef(opts.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base ref: %w", err)
	}

	// Determine worktree path.
	repoName := filepath.Base(opts.RepoRoot)
	worktreeRoot := config.WorktreeRoot()
	worktreePath := filepath.Join(worktreeRoot, repoName, opts.ID)

	now := time.Now()
	ws := &Workspace{
		ID:           opts.ID,
		RepoRoot:     opts.RepoRoot,
		BaseBranch:   opts.BaseBranch,
		BaseRef:      baseRef,
		Branch:       branch,
		WorktreePath: worktreePath,
		Source:       opts.Source,
		Status:       StatusCreating,
		Agent:        opts.Agent,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Persist early so we can track failures.
	if err := m.store.Save(ws); err != nil {
		return nil, fmt.Errorf("failed to save workspace: %w", err)
	}

	// Create the branch.
	if err := CreateBranch(opts.RepoRoot, branch, opts.BaseBranch); err != nil {
		_ = m.store.Delete(opts.ID)
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// Ensure parent directory for worktree exists.
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		_ = DeleteBranch(opts.RepoRoot, branch)
		_ = m.store.Delete(opts.ID)
		return nil, fmt.Errorf("failed to create worktree parent dir: %w", err)
	}

	// Create the worktree.
	if err := CreateWorktree(opts.RepoRoot, worktreePath, branch); err != nil {
		_ = DeleteBranch(opts.RepoRoot, branch)
		_ = m.store.Delete(opts.ID)
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Copy gitignored files (e.g. CLAUDE.md) into the worktree for context.
	// These stay invisible to git since the worktree shares the same .gitignore.
	_ = CopyGitIgnoredFiles(opts.RepoRoot, worktreePath)

	// Mark ready.
	ws.Status = StatusReady
	ws.UpdatedAt = time.Now()
	if err := m.store.Save(ws); err != nil {
		return nil, fmt.Errorf("failed to update workspace status: %w", err)
	}

	return ws, nil
}

// Get returns a workspace by ID.
func (m *Manager) Get(id string) (*Workspace, error) {
	return m.store.Get(id)
}

// List returns workspaces matching the given filter.
func (m *Manager) List(filter ListFilter) ([]*Workspace, error) {
	return m.store.List(filter)
}

// GetByRepo returns all workspaces for a given repository root.
func (m *Manager) GetByRepo(repoRoot string) ([]*Workspace, error) {
	return m.store.List(ListFilter{RepoRoot: repoRoot})
}

// UpdateStatus changes a workspace's status and optional detail message.
func (m *Manager) UpdateStatus(id string, status WorkspaceStatus, detail string) error {
	if !status.IsValid() {
		return fmt.Errorf("invalid status: %q", status)
	}
	ws, err := m.store.Get(id)
	if err != nil {
		return err
	}
	ws.Status = status
	ws.StatusDetail = detail
	ws.UpdatedAt = time.Now()
	return m.store.Save(ws)
}

// Delete removes a workspace: cleans up worktree, branch, and metadata.
func (m *Manager) Delete(id string) error {
	ws, err := m.store.Get(id)
	if err != nil {
		return err
	}

	// Remove worktree (best-effort — may already be gone).
	_ = RemoveWorktree(ws.RepoRoot, ws.WorktreePath)

	// Delete branch (best-effort).
	_ = DeleteBranch(ws.RepoRoot, ws.Branch)

	// Remove from store.
	return m.store.Delete(id)
}
