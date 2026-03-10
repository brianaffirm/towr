// Package queue manages the approval queue for blockers that require human review.
package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// Manager wraps store.Store queue operations with higher-level logic
// such as priority calculation and context construction.
type Manager struct {
	store    store.Store
	repoRoot string
}

// NewManager creates a new queue Manager.
func NewManager(s store.Store, repoRoot string) *Manager {
	return &Manager{
		store:    s,
		repoRoot: repoRoot,
	}
}

// Enqueue creates a new queue item with computed priority and context,
// persists it through the store, and returns the created item.
func (m *Manager) Enqueue(workspaceID, blockerType, summary, detail string, filesAtStake []string, timeout time.Duration) (*store.QueueItem, error) {
	priority := calculatePriority(blockerType, filesAtStake)

	ctx := map[string]interface{}{
		"detail":        detail,
		"files_at_stake": filesAtStake,
	}
	ctxJSON, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("marshal context: %w", err)
	}

	timeoutStr := ""
	timeoutAction := "block"
	if timeout > 0 {
		timeoutStr = time.Now().Add(timeout).Format(time.RFC3339)
	}

	item := store.QueueItem{
		ID:            fmt.Sprintf("q-%s-%d", workspaceID, time.Now().UnixMilli()),
		WorkspaceID:   workspaceID,
		RepoRoot:      m.repoRoot,
		Type:          blockerType,
		Priority:      priority,
		Summary:       summary,
		Context:       ctxJSON,
		Timeout:       timeoutStr,
		TimeoutAction: timeoutAction,
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	if err := m.store.EnqueueApproval(item); err != nil {
		return nil, fmt.Errorf("enqueue approval: %w", err)
	}

	return &item, nil
}

// List returns all pending queue items for the configured repo.
func (m *Manager) List() ([]store.QueueItem, error) {
	return m.store.GetQueue(m.repoRoot)
}

// Approve resolves a queue item as approved.
func (m *Manager) Approve(queueID, resolvedBy string) error {
	return m.store.ResolveQueueItem(queueID, store.Resolution{
		Action:     "approved",
		ResolvedBy: resolvedBy,
	})
}

// Deny resolves a queue item as denied.
func (m *Manager) Deny(queueID, resolvedBy string) error {
	return m.store.ResolveQueueItem(queueID, store.Resolution{
		Action:     "denied",
		ResolvedBy: resolvedBy,
	})
}

// Respond resolves a queue item with a human's free-form response.
func (m *Manager) Respond(queueID, resolvedBy, response string) error {
	return m.store.ResolveQueueItem(queueID, store.Resolution{
		Action:     "responded",
		ResolvedBy: resolvedBy,
		Response:   response,
	})
}

// calculatePriority assigns a priority string based on blocker type and files.
// "!!" = critical, "!" = high, "." = normal.
func calculatePriority(blockerType string, filesAtStake []string) string {
	switch blockerType {
	case "permission":
		// Permission blockers involving many files are critical.
		if len(filesAtStake) > 3 {
			return "!!"
		}
		return "!"
	case "gate":
		return "!!"
	case "decision":
		return "!"
	case "external":
		return "."
	default:
		return "."
	}
}
