package landing

import (
	"fmt"
	"time"

	"github.com/brianaffirm/towr/internal/git"
)

// ChainResult holds the outcome of landing multiple workspaces sequentially.
type ChainResult struct {
	Landed    []LandResult
	Failed    *ChainFailure // nil if all succeeded
	Duration  time.Duration
}

// ChainFailure describes a failure during chain landing.
type ChainFailure struct {
	WorkspaceID   string
	Error         error
	ConflictFiles []string
}

// ChainLand lands multiple workspaces sequentially. After each successful
// landing, subsequent workspaces are rebased onto the updated base branch.
// If a conflict occurs mid-chain, the chain stops and reports which
// workspaces succeeded and which failed. Completed landings are NOT rolled back.
func (p *LandingPipeline) ChainLand(ids []string, opts LandOpts) (*ChainResult, error) {
	start := time.Now()

	if len(ids) == 0 {
		return nil, fmt.Errorf("no workspace IDs provided")
	}

	result := &ChainResult{}

	for i, id := range ids {
		// Land the current workspace
		landResult, err := p.Land(id, opts)
		if err != nil {
			// Landing failed — record the failure and stop the chain
			failure := &ChainFailure{
				WorkspaceID: id,
				Error:       err,
			}

			// Try to detect conflict files for the report
			ws, wsErr := p.store.GetWorkspace(id)
			if wsErr == nil {
				conflicts, _ := git.HasConflictsWith(ws.RepoRoot, ws.BaseBranch, ws.Branch)
				failure.ConflictFiles = conflicts
			}

			result.Failed = failure
			result.Duration = time.Since(start)
			return result, nil // Return result (not error) so caller can see partial success
		}

		result.Landed = append(result.Landed, *landResult)

		// After a successful landing, rebase remaining workspaces onto updated base.
		// This ensures each subsequent workspace sees the changes from prior landings.
		if i < len(ids)-1 {
			for _, remainingID := range ids[i+1:] {
				ws, err := p.store.GetWorkspace(remainingID)
				if err != nil {
					continue // skip if we can't look up the workspace
				}

				// Rebase the remaining workspace branch onto the (now-updated) base branch
				if err := git.Rebase(ws.WorktreePath, ws.BaseBranch); err != nil {
					// Rebase conflict — abort and mark this workspace as the failure point
					_ = git.AbortRebase(ws.WorktreePath)
					_ = p.store.UpdateStatus(remainingID, StatusBlocked)

					conflicts, _ := git.HasConflictsWith(ws.RepoRoot, ws.BaseBranch, ws.Branch)
					result.Failed = &ChainFailure{
						WorkspaceID:   remainingID,
						Error:         fmt.Errorf("rebase conflict after landing %s: %w", id, err),
						ConflictFiles: conflicts,
					}
					result.Duration = time.Since(start)
					return result, nil
				}
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}
