package control

import (
	"context"
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
