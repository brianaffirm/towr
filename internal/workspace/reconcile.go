package workspace

import (
	"fmt"
	"os"
	"time"

	"github.com/brianaffirm/towr/internal/store"
)

// ReconcileResult describes a detected state change.
type ReconcileResult struct {
	WorkspaceID string
	From        WorkspaceStatus
	To          WorkspaceStatus
	Reason      string
}

// ReconcileWorkspace checks actual state vs stored state and returns a needed
// transition if one is detected. Does NOT mutate — caller decides whether to apply.
func ReconcileWorkspace(ws *store.Workspace, staleThreshold time.Duration) *ReconcileResult {
	status := WorkspaceStatus(ws.Status)

	// Skip terminal statuses.
	if status == StatusLanded || status == StatusArchived {
		return nil
	}

	// Check orphaned (worktree missing on disk).
	if ws.WorktreePath != "" && ws.RepoRoot != "" && status != StatusOrphaned {
		if _, err := os.Stat(ws.WorktreePath); os.IsNotExist(err) {
			return &ReconcileResult{ws.ID, status, StatusOrphaned, "worktree missing from disk"}
		}
	}

	// Check merged (branch merged into base).
	if ws.RepoRoot != "" && ws.BaseBranch != "" && ws.Branch != "" && status != StatusMerged {
		if IsBranchMerged(ws.RepoRoot, ws.BaseBranch, ws.Branch, ws.BaseRef) {
			return &ReconcileResult{ws.ID, status, StatusMerged, "branch merged into " + ws.BaseBranch}
		}
	}

	// Check stale (no activity past threshold).
	if ws.LastActivity != "" && status != StatusStale && status != StatusMerged && status != StatusOrphaned {
		lastAct, err := time.Parse(time.RFC3339, ws.LastActivity)
		if err == nil && time.Since(lastAct) > staleThreshold {
			if status == StatusReady || status == StatusRunning || status == StatusIdle || status == StatusPaused {
				return &ReconcileResult{ws.ID, status, StatusStale,
					fmt.Sprintf("no activity for %s", time.Since(lastAct).Truncate(time.Hour))}
			}
		}
	}

	return nil
}
