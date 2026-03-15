package control

import (
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

type ControlStore interface {
	CreateRun(run *store.Run) error
	UpdateRun(run *store.Run) error
	GetRun(id string) (*store.Run, error)
	ListRuns(repoRoot string) ([]*store.Run, error)
	EmitEvent(event store.Event) error
	QueryEvents(query store.EventQuery) ([]store.Event, error)
	GetWorkspace(repoRoot, id string) (*store.Workspace, error)
	SaveWorkspace(w *store.Workspace) error
}

type Router interface {
	Route(task TaskSpec, defaultModel string, defaultAgent string) RoutingDecision
	Escalate(prev RoutingDecision) (RoutingDecision, bool)
}

type PolicyEngine interface {
	Evaluate(task TaskSpec) error
}

type AgentRuntime interface {
	SpawnWorkspace(taskID string, prompt string, agentType string, repoRoot string, depIDs []string) error
	LaunchAndMonitor(taskID string, prompt string, decision RoutingDecision, agentType string, fullAuto bool, done <-chan struct{})
	DetectState(taskID string) (state string, summary string, err error)
	ApproveDialog(taskID string) error
	AutoCommit(taskID string) error
	CreatePR(taskID string) error
	GetWorktreePath(taskID string) string
	ComputeCost(taskID string, model string) (inputTokens, outputTokens int, source string, actualCost, opusCost float64)
	IsHeadless() bool
}

type Clock func() time.Time
