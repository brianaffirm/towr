package control

import (
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/router"
)

// RouterAdapter wraps the existing router package to implement the Router interface.
type RouterAdapter struct{}

func (a *RouterAdapter) Route(task TaskSpec, defaultModel, defaultAgent string) RoutingDecision {
	oTask := orchestrate.Task{
		ID:     task.ID,
		Prompt: task.Prompt,
		Agent:  task.Agent,
		Model:  task.Model,
	}
	settings := orchestrate.Settings{
		DefaultModel: defaultModel,
		DefaultAgent: defaultAgent,
	}
	d := router.Route(oTask, settings)
	return RoutingDecision{
		Model:           d.Model,
		Reason:          d.Reason,
		Tier:            d.Tier,
		CanEscalate:     d.CanEscalate,
		RequireApproval: d.RequireApproval,
	}
}

func (a *RouterAdapter) Escalate(prev RoutingDecision) (RoutingDecision, bool) {
	d := router.Decision{
		Model:       prev.Model,
		Reason:      prev.Reason,
		Tier:        prev.Tier,
		CanEscalate: prev.CanEscalate,
	}
	next, ok := router.Escalate(d)
	if !ok {
		return prev, false
	}
	return RoutingDecision{
		Model:       next.Model,
		Reason:      next.Reason,
		Tier:        next.Tier,
		CanEscalate: next.CanEscalate,
	}, true
}
