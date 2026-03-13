// Package store provides the event-sourced state store for towr.
package store

import "time"

// EventKind constants define the taxonomy of events in the system.
const (
	EventWorkspaceCreated       = "workspace.created"
	EventWorkspaceStatusChanged = "workspace.status_changed"
	EventWorkspacePaused        = "workspace.paused"
	EventWorkspaceResumed       = "workspace.resumed"
	EventWorkspaceHookStarted   = "workspace.hook.started"
	EventWorkspaceHookCompleted = "workspace.hook.completed"
	EventWorkspaceHookFailed    = "workspace.hook.failed"
	EventWorkspaceLandingStart  = "workspace.landing.started"
	EventWorkspaceLandingConfl  = "workspace.landing.conflict"
	EventWorkspaceLanded        = "workspace.landed"
	EventWorkspaceCleanup       = "workspace.cleanup"
	EventWorkspaceOrphaned      = "workspace.orphaned"
	EventWorkspaceRecovered     = "workspace.recovered"
	EventQueueCreated           = "queue.created"
	EventQueueResolved          = "queue.resolved"
	EventQueueTimeout           = "queue.timeout"
	EventPolicyEvaluated        = "policy.evaluated"
	EventLandingForced          = "workspace.landing.forced"
	EventLandingHooksSkipped    = "workspace.landing.hooks_skipped"
	EventCleanupForced          = "workspace.cleanup.forced"
	EventWorkspaceAdopted       = "workspace.adopted"
	EventWorkspaceAutoTransition = "workspace.auto_transition"

	// Dispatch orchestration events
	EventTaskDispatched = "task.dispatched"
	EventTaskStarted    = "task.started"
	EventTaskCompleted  = "task.completed"
	EventTaskFailed     = "task.failed"
	EventTaskBlocked    = "task.blocked"
	EventTaskPromoted   = "task.promoted"
	EventTaskCost       = "task.cost"
)

// Event represents an immutable state-change record.
type Event struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"ts"`
	Kind        string                 `json:"kind"`
	WorkspaceID string                 `json:"workspace_id,omitempty"`
	RepoRoot    string                 `json:"repo_root,omitempty"`
	Runtime     string                 `json:"runtime,omitempty"`
	Actor       string                 `json:"actor,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
}

// EventQuery defines filters for querying events.
type EventQuery struct {
	WorkspaceID string
	RepoRoot    string
	Kind        string
	Since       *time.Time
	Until       *time.Time
	Limit       int
}
