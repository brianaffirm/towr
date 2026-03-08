package store

import "encoding/json"

// Workspace represents the materialized state of a workspace.
type Workspace struct {
	ID                string          `json:"id"`
	RepoRoot          string          `json:"repo_root"`
	BaseBranch        string          `json:"base_branch,omitempty"`
	BaseRef           string          `json:"base_ref,omitempty"`
	Branch            string          `json:"branch,omitempty"`
	WorktreePath      string          `json:"worktree_path,omitempty"`
	SourceKind        string          `json:"source_kind,omitempty"`
	SourceValue       string          `json:"source_value,omitempty"`
	Status            string          `json:"status"`
	AgentRuntime      string          `json:"agent_runtime,omitempty"`
	AgentID           string          `json:"agent_id,omitempty"`
	AgentModelVersion string          `json:"agent_model_version,omitempty"`
	ExitCode          *int            `json:"exit_code,omitempty"`
	Error             string          `json:"error,omitempty"`
	MergeCommit       string          `json:"merge_commit,omitempty"`
	Checkpoint        json.RawMessage `json:"checkpoint,omitempty"`
	TerminalTarget    string          `json:"terminal_target,omitempty"`
	CreatedAt         string          `json:"created_at,omitempty"`
	UpdatedAt         string          `json:"updated_at,omitempty"`
}

// ListFilter controls workspace listing.
type ListFilter struct {
	Status  string // filter by status; empty = all
	AllRepos bool   // if true, ignore RepoRoot filter
}

// QueueItem represents an approval queue entry.
type QueueItem struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id,omitempty"`
	RepoRoot      string          `json:"repo_root,omitempty"`
	Type          string          `json:"type"`
	Priority      string          `json:"priority"`
	Summary       string          `json:"summary,omitempty"`
	Context       json.RawMessage `json:"context,omitempty"`
	Options       json.RawMessage `json:"options,omitempty"`
	Resolution    string          `json:"resolution,omitempty"`
	ResolvedBy    string          `json:"resolved_by,omitempty"`
	ResolvedAt    string          `json:"resolved_at,omitempty"`
	Timeout       string          `json:"timeout,omitempty"`
	TimeoutAction string          `json:"timeout_action,omitempty"`
	CreatedAt     string          `json:"created_at,omitempty"`
}

// Resolution contains the outcome of a queue item review.
type Resolution struct {
	Action     string `json:"action"`     // "approved", "denied", "responded"
	ResolvedBy string `json:"resolved_by"`
	Response   string `json:"response,omitempty"`
}

// Store is the primary state API for amux.
type Store interface {
	// Workspace CRUD (materialized view of events)
	SaveWorkspace(w *Workspace) error
	GetWorkspace(repoRoot, id string) (*Workspace, error)
	ListWorkspaces(repoRoot string, filter ListFilter) ([]*Workspace, error)
	DeleteWorkspace(repoRoot, id string) error

	// Events (append-only)
	EmitEvent(event Event) error
	QueryEvents(query EventQuery) ([]Event, error)

	// Queue (approval items)
	EnqueueApproval(item QueueItem) error
	GetQueue(repoRoot string) ([]QueueItem, error)
	ResolveQueueItem(id string, resolution Resolution) error

	// Lifecycle
	Init(dbPath string) error
	Close() error
}
