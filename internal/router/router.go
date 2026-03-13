package router

import (
	"fmt"

	"github.com/brianaffirm/towr/internal/orchestrate"
)

// Decision captures the routing choice and why it was made.
type Decision struct {
	Model           string
	Reason          string
	Tier            int
	CanEscalate     bool
	RequireApproval bool
}

var modelTier = map[string]int{
	"haiku": 0, "sonnet": 1, "opus": 2,
}

var tierModel = map[int]string{
	0: "haiku", 1: "sonnet", 2: "opus",
}

// Route selects the model for a task. Precedence:
// 1. task.Model explicit → use it, no escalation
// 2. policy rule match → use rule's model
// 3. settings.DefaultModel (if set, overrides heuristic)
// 4. heuristic analysis of prompt
func Route(task orchestrate.Task, settings orchestrate.Settings) Decision {
	// Non-Claude agents manage their own models.
	if task.Agent != "" && task.Agent != "claude-code" {
		d := heuristic(task.Prompt)
		d.CanEscalate = false
		d.Reason = fmt.Sprintf("external-agent:%s", task.Agent)
		return d
	}

	// 1. Explicit model on task.
	if task.Model != "" {
		tier := modelTier[task.Model]
		return Decision{
			Model:       task.Model,
			Reason:      "explicit",
			Tier:        tier,
			CanEscalate: false,
		}
	}

	// 2. Policy rules.
	if d, ok := matchPolicy(task.Prompt, settings.Routing.Rules); ok {
		return d
	}

	// 3. Default model (overrides heuristic when set).
	if settings.DefaultModel != "" {
		tier := modelTier[settings.DefaultModel]
		return Decision{
			Model:       settings.DefaultModel,
			Reason:      "default",
			Tier:        tier,
			CanEscalate: true,
		}
	}

	// 4. Heuristic.
	return heuristic(task.Prompt)
}

// Escalate bumps the model one tier up. Consumes a retry count.
func Escalate(prev Decision) (Decision, bool) {
	if !prev.CanEscalate || prev.Tier >= 2 {
		return prev, false
	}
	next := prev
	next.Tier++
	next.Model = tierModel[next.Tier]
	next.Reason = fmt.Sprintf("escalated:%s→%s", prev.Model, next.Model)
	next.CanEscalate = next.Tier < 2
	return next, true
}
