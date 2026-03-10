// Package interruption implements the three-layer resolution stack for
// handling blockers that agents encounter during autonomous operation.
package interruption

import (
	"fmt"
	"time"

	"github.com/brianaffirm/towr/internal/queue"
	"github.com/brianaffirm/towr/internal/store"
)

// BlockerType classifies the kind of interruption an agent encountered.
type BlockerType string

const (
	BlockerPermission BlockerType = "permission"
	BlockerDecision   BlockerType = "decision"
	BlockerGate       BlockerType = "gate"
	BlockerExternal   BlockerType = "external"
)

// Blocker describes something that is preventing an agent from continuing.
type Blocker struct {
	WorkspaceID  string      `json:"workspace_id"`
	Type         BlockerType `json:"type"`
	Summary      string      `json:"summary"`
	Detail       string      `json:"detail"`
	AgentRequest string      `json:"agent_request"`
	FilesAtStake []string    `json:"files_at_stake"`
}

// Resolution captures how a blocker was handled and by which layer.
type Resolution struct {
	Layer   int    `json:"layer"`    // 1=policy, 2=queue, 3=skip
	Action  string `json:"action"`   // "approved", "denied", "queued", "blocked", "auto_decided"
	Message string `json:"message"`
	QueueID string `json:"queue_id,omitempty"`
}

// Resolver attempts to handle blockers through the three-layer stack:
// Layer 1 (Policy) → Layer 2 (Queue) → Layer 3 (Skip/Timeout).
type Resolver struct {
	policy       *PolicyEngine
	queueManager *queue.Manager
	store        store.Store
	timeout      time.Duration
}

// NewResolver creates a Resolver with the given dependencies.
func NewResolver(policy *PolicyEngine, qm *queue.Manager, s store.Store, timeout time.Duration) *Resolver {
	return &Resolver{
		policy:       policy,
		queueManager: qm,
		store:        s,
		timeout:      timeout,
	}
}

// Resolve runs the blocker through the three-layer resolution stack and
// returns which layer handled it and the outcome.
func (r *Resolver) Resolve(blocker Blocker) (Resolution, error) {
	// Layer 1: Policy — can we auto-decide?
	result, err := r.policy.Evaluate(blocker)
	if err != nil {
		return Resolution{}, fmt.Errorf("policy evaluation failed: %w", err)
	}

	if result != nil {
		switch result.Action {
		case "allow":
			return Resolution{
				Layer:   1,
				Action:  "auto_decided",
				Message: fmt.Sprintf("Policy auto-approved: %s", result.Rule),
			}, nil
		case "deny":
			return Resolution{
				Layer:   1,
				Action:  "denied",
				Message: fmt.Sprintf("Policy auto-denied: %s", result.Rule),
			}, nil
		}
		// "queue" action falls through to layer 2
	}

	// Layer 2: Queue — park it for human review
	if r.queueManager != nil {
		item, err := r.queueManager.Enqueue(blocker.WorkspaceID, string(blocker.Type), blocker.Summary, blocker.Detail, blocker.FilesAtStake, r.timeout)
		if err != nil {
			return Resolution{}, fmt.Errorf("failed to enqueue blocker: %w", err)
		}

		return Resolution{
			Layer:   2,
			Action:  "queued",
			Message: fmt.Sprintf("Queued for human review (timeout: %s)", r.timeout),
			QueueID: item.ID,
		}, nil
	}

	// Layer 3: Skip — no queue available, mark as blocked
	return Resolution{
		Layer:   3,
		Action:  "blocked",
		Message: "No policy match and no queue available; blocking workspace",
	}, nil
}
