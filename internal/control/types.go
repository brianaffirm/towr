package control

import (
	"context"
	"sync"
	"time"
)

const (
	RunPending   = "pending"
	RunRunning   = "running"
	RunCompleted = "completed"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

type RunOptions struct {
	Budget   float64
	FullAuto bool
	DryRun   bool
	Quiet    bool
}

type RunRequest struct {
	RepoRoot    string
	PlanName    string
	PlanContent string
	Tasks       []TaskSpec
	Settings    SettingsSnapshot
	Options     RunOptions
}

type TaskSpec struct {
	ID        string
	Prompt    string
	DependsOn []string
	Agent     string
	Model     string
}

type SettingsSnapshot struct {
	DefaultAgent   string
	DefaultModel   string
	AutoApprove    bool
	MaxRetries     int
	PollInterval   time.Duration
	CreatePR       bool
	ReactToReviews bool
	FullAuto       bool
	Budget         float64
	Web            bool
	WebAddr        string
	BaseBranch     string
}

type RunHandle struct {
	ID         string
	Status     string
	TaskStates map[string]string
	Cancel     context.CancelFunc
	mu         sync.Mutex
}

// SetTaskState safely updates a task's state.
func (h *RunHandle) SetTaskState(taskID, state string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.TaskStates[taskID] = state
}

// CompletedCount returns the number of tasks in "completed" state.
func (h *RunHandle) CompletedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var n int
	for _, st := range h.TaskStates {
		if st == "completed" {
			n++
		}
	}
	return n
}

type RoutingDecision struct {
	Model           string
	Reason          string
	Tier            int
	CanEscalate     bool
	RequireApproval bool
}

type PreRunItem struct {
	TaskID   string
	Decision RoutingDecision
	EstCost  float64
}
