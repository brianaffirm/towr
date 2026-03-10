package workspace

import "time"

// WorkspaceStatus represents the current state of a workspace.
type WorkspaceStatus string

const (
	StatusCreating   WorkspaceStatus = "CREATING"
	StatusReady      WorkspaceStatus = "READY"
	StatusRunning    WorkspaceStatus = "RUNNING"
	StatusPaused     WorkspaceStatus = "PAUSED"
	StatusIdle       WorkspaceStatus = "IDLE"
	StatusValidating WorkspaceStatus = "VALIDATING"
	StatusLanding    WorkspaceStatus = "LANDING"
	StatusLanded     WorkspaceStatus = "LANDED"
	StatusArchived   WorkspaceStatus = "ARCHIVED"
	StatusBlocked    WorkspaceStatus = "BLOCKED"
	StatusOrphaned   WorkspaceStatus = "ORPHANED"
)

// ValidStatuses contains all valid workspace statuses.
var ValidStatuses = []WorkspaceStatus{
	StatusCreating, StatusReady, StatusRunning, StatusPaused, StatusIdle,
	StatusValidating, StatusLanding, StatusLanded, StatusArchived,
	StatusBlocked, StatusOrphaned,
}

// IsValid returns true if the status is a recognized value.
func (s WorkspaceStatus) IsValid() bool {
	for _, v := range ValidStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// SpawnSourceKind describes how a workspace was created.
type SpawnSourceKind string

const (
	SpawnFromTask   SpawnSourceKind = "task"
	SpawnFromBranch SpawnSourceKind = "branch"
	SpawnFromPR     SpawnSourceKind = "pr"
)

// SpawnSource records the origin of a workspace.
type SpawnSource struct {
	Kind  SpawnSourceKind `json:"kind"`
	Value string         `json:"value"`
}

// AgentIdentity tracks which agent runtime is working in a workspace.
type AgentIdentity struct {
	Runtime      string `json:"runtime"`
	AgentID      string `json:"agent_id,omitempty"`
	ModelVersion string `json:"model_version,omitempty"`
}

// Checkpoint preserves context for respawn across pauses and crashes.
type Checkpoint struct {
	// Implicit — towr captures automatically
	DiffSummary      string   `json:"diff_summary,omitempty"`
	FilesModified    []string `json:"files_modified,omitempty"`
	CommitsOnBranch  []string `json:"commits_on_branch,omitempty"`
	BlockerReason    string   `json:"blocker_reason,omitempty"`
	TimeWorked       string   `json:"time_worked,omitempty"`

	// Explicit — agent writes via hook/MCP
	ProgressSummary string   `json:"progress_summary,omitempty"`
	RemainingWork   string   `json:"remaining_work,omitempty"`
	KeyDecisions    []string `json:"key_decisions,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`

	// Resolution — filled when human responds
	HumanResolution string `json:"human_resolution,omitempty"`
}

// Workspace is the core domain object representing an isolated agent workspace.
type Workspace struct {
	ID             string          `json:"id"`
	RepoRoot       string          `json:"repo_root"`
	BaseBranch     string          `json:"base_branch"`
	BaseRef        string          `json:"base_ref"`
	Branch         string          `json:"branch"`
	WorktreePath   string          `json:"worktree_path"`
	Source         SpawnSource     `json:"source"`
	Status         WorkspaceStatus `json:"status"`
	StatusDetail   string          `json:"status_detail,omitempty"`
	Agent          *AgentIdentity  `json:"agent,omitempty"`
	ExitCode       *int            `json:"exit_code,omitempty"`
	Error          string          `json:"error,omitempty"`
	MergeCommit    string          `json:"merge_commit,omitempty"`
	Checkpoint     *Checkpoint     `json:"checkpoint,omitempty"`
	TerminalTarget string          `json:"terminal_target,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// CreateOpts holds the parameters for creating a new workspace.
type CreateOpts struct {
	ID         string          // Short user-provided or derived ID
	RepoRoot   string          // Absolute path to the git repository root
	BaseBranch string          // Branch to fork from (e.g. "main")
	Source     SpawnSource     // How the workspace was spawned
	Agent      *AgentIdentity  // Optional agent identity
	CopyPaths  []string        // Paths to copy from repo root into worktree
	LinkPaths  []string        // Paths to symlink from repo root into worktree
}

// ListFilter specifies criteria for listing workspaces.
type ListFilter struct {
	RepoRoot string          // Filter by repository root (empty = all)
	Status   WorkspaceStatus // Filter by status (empty = all)
}
