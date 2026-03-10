package landing

import (
	"fmt"
	"os"
	"time"

	"github.com/brianaffirm/towr/internal/git"
)

// MergeStrategy defines how a workspace branch is merged into the base branch.
type MergeStrategy string

const (
	StrategyRebaseFF MergeStrategy = "rebase-ff"
	StrategySquash   MergeStrategy = "squash"
	StrategyFFOnly   MergeStrategy = "ff-only"
	StrategyMerge    MergeStrategy = "merge"
)

// WorkspaceStatus represents the status of a workspace.
type WorkspaceStatus string

const (
	StatusCreating   WorkspaceStatus = "CREATING"
	StatusReady      WorkspaceStatus = "READY"
	StatusRunning    WorkspaceStatus = "RUNNING"
	StatusPaused     WorkspaceStatus = "PAUSED"
	StatusIdle       WorkspaceStatus = "IDLE"
	StatusValidating WorkspaceStatus = "VALIDATING"
	StatusLanding    WorkspaceStatus = "LANDING"
	StatusLanded     WorkspaceStatus = "LANDED"
	StatusArchived   WorkspaceStatus = "ARCHIVED"
	StatusBlocked    WorkspaceStatus = "BLOCKED"
	StatusOrphaned   WorkspaceStatus = "ORPHANED"
)

// Workspace is a minimal representation of a workspace for the landing pipeline.
// The full workspace type will be defined in the workspace package.
type Workspace struct {
	ID           string
	RepoRoot     string
	BaseBranch   string
	Branch       string
	WorktreePath string
	Status       WorkspaceStatus
	Task         string // task description for commit messages
}

// WorkspaceStore is the interface for looking up and updating workspaces.
// Implemented by the workspace/store packages (Task 01/03).
type WorkspaceStore interface {
	GetWorkspace(id string) (*Workspace, error)
	UpdateStatus(id string, status WorkspaceStatus) error
	SetMergeCommit(id string, sha string) error
}

// WorkspaceOps provides workspace lifecycle operations (cleanup, etc).
// Implemented by the workspace package (Task 01).
type WorkspaceOps interface {
	RemoveWorktree(id string) error
	DeleteBranch(id string) error
}

// HookConfig provides hook commands for a workspace.
type HookConfig interface {
	GetHook(hookType HookType) string
}

// EventEmitter emits audit events. Implemented by the store package.
type EventEmitter interface {
	EmitBypassEvent(kind, workspaceID, repoRoot, actor, reason string, data map[string]interface{}) error
}

// LandOpts controls landing behavior.
type LandOpts struct {
	Strategy MergeStrategy
	DryRun   bool
	Force    bool // bypass status check (creates audit event)
	Push     bool // push branch instead of local merge
	PR       bool // push branch for PR creation (same as Push)
	NoHooks  bool // skip hooks (creates audit event)
	Reason   string // audit reason for bypass
}

// LandResult holds the outcome of a successful land operation.
type LandResult struct {
	WorkspaceID  string
	MergeCommit  string
	PushedBranch string // set when Push or PR mode is used
	FilesChanged []string
	Strategy     MergeStrategy
	HookResults  map[HookType]*HookResult
	Duration     time.Duration
}

// DryRunResult holds the outcome of a dry-run (non-destructive check).
type DryRunResult struct {
	WorkspaceID   string
	CanLand       bool
	ConflictFiles []string
	FilesChanged  []string
	DiffStat      string
	Strategy      MergeStrategy
	HooksPassed   bool
	Issues        []string
}

// LandingPipeline orchestrates the full landing flow:
// pre-land hooks -> rebase -> merge -> post-land hooks -> cleanup.
type LandingPipeline struct {
	store      WorkspaceStore
	ops        WorkspaceOps
	hookRunner *HookRunner
	hooks      HookConfig
	emitter    EventEmitter
}

// NewLandingPipeline creates a new pipeline with the given dependencies.
func NewLandingPipeline(store WorkspaceStore, ops WorkspaceOps, hooks HookConfig, hookTimeout time.Duration) *LandingPipeline {
	return &LandingPipeline{
		store:      store,
		ops:        ops,
		hookRunner: NewHookRunner(hookTimeout),
		hooks:      hooks,
	}
}

// SetEventEmitter sets the event emitter for audit events.
func (p *LandingPipeline) SetEventEmitter(e EventEmitter) {
	p.emitter = e
}

// Land performs the full landing flow for a workspace.
func (p *LandingPipeline) Land(id string, opts LandOpts) (*LandResult, error) {
	start := time.Now()
	hookResults := make(map[HookType]*HookResult)

	// 1. Look up workspace
	ws, err := p.store.GetWorkspace(id)
	if err != nil {
		return nil, fmt.Errorf("workspace lookup failed: %w", err)
	}

	// 2. Check status (unless --force)
	if !opts.Force {
		if !isLandableStatus(ws.Status) {
			return nil, fmt.Errorf("workspace %s has status %s, must be READY, IDLE, or PAUSED to land", id, ws.Status)
		}
	} else if p.emitter != nil {
		_ = p.emitter.EmitBypassEvent(
			"workspace.landing.forced",
			id, ws.RepoRoot, os.Getenv("USER"), opts.Reason,
			map[string]interface{}{
				"skipped_status_check": true,
				"original_status":     string(ws.Status),
			},
		)
	}

	strategy := opts.Strategy
	if strategy == "" {
		strategy = StrategyRebaseFF
	}

	// 3. Run pre-land hooks (blocking)
	if opts.NoHooks && p.emitter != nil {
		_ = p.emitter.EmitBypassEvent(
			"workspace.landing.hooks_skipped",
			id, ws.RepoRoot, os.Getenv("USER"), opts.Reason,
			map[string]interface{}{
				"skipped_hooks": []string{"pre-land", "post-land"},
			},
		)
	}
	if !opts.NoHooks {
		if err := p.store.UpdateStatus(id, StatusValidating); err != nil {
			return nil, fmt.Errorf("failed to update status: %w", err)
		}

		hookCmd := p.hooks.GetHook(HookPreLand)
		if hookCmd != "" {
			vars := makeHookVars(ws)
			result, err := p.hookRunner.Run(hookCmd, vars)
			if err != nil {
				_ = p.store.UpdateStatus(id, StatusBlocked)
				return nil, fmt.Errorf("pre-land hook error: %w", err)
			}
			hookResults[HookPreLand] = result
			if result.ExitCode != 0 {
				_ = p.store.UpdateStatus(id, StatusBlocked)
				return nil, fmt.Errorf("pre-land hook failed (exit %d):\nstdout: %s\nstderr: %s",
					result.ExitCode, result.Stdout, result.Stderr)
			}
		}
	}

	// 4. Update status to LANDING
	if err := p.store.UpdateStatus(id, StatusLanding); err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	// Push/PR mode: rebase + push, skip local merge
	if opts.Push || opts.PR {
		// Rebase onto latest base
		if err := git.Rebase(ws.WorktreePath, ws.BaseBranch); err != nil {
			_ = git.AbortRebase(ws.WorktreePath)
			_ = p.store.UpdateStatus(id, StatusBlocked)
			return nil, fmt.Errorf("rebase conflict: %w", err)
		}

		// Push to remote
		if err := git.Push(ws.WorktreePath, "origin", ws.Branch); err != nil {
			_ = p.store.UpdateStatus(id, StatusBlocked)
			return nil, fmt.Errorf("push failed: %w", err)
		}

		// Workspace stays READY — user cleans up after PR merges
		if err := p.store.UpdateStatus(id, StatusReady); err != nil {
			return nil, fmt.Errorf("failed to update status: %w", err)
		}

		filesChanged, _ := git.DiffFiles(ws.RepoRoot, ws.BaseBranch, ws.Branch)

		return &LandResult{
			WorkspaceID:  id,
			PushedBranch: ws.Branch,
			FilesChanged: filesChanged,
			Strategy:     strategy,
			HookResults:  hookResults,
			Duration:     time.Since(start),
		}, nil
	}

	// 5. Execute merge strategy
	mergeCommit, err := p.executeMerge(ws, strategy)
	if err != nil {
		_ = p.store.UpdateStatus(id, StatusBlocked)
		return nil, fmt.Errorf("merge failed: %w", err)
	}

	// 6. Record merge commit
	if err := p.store.SetMergeCommit(id, mergeCommit); err != nil {
		// Non-fatal but worth noting
		_ = err
	}

	// 7. Get files changed for the result
	filesChanged, _ := git.DiffFiles(ws.RepoRoot, ws.BaseBranch+"~1", ws.BaseBranch)

	// 8. Run post-land hooks (non-blocking)
	if !opts.NoHooks {
		hookCmd := p.hooks.GetHook(HookPostLand)
		if hookCmd != "" {
			vars := makeHookVars(ws)
			result, _ := p.hookRunner.Run(hookCmd, vars)
			if result != nil {
				hookResults[HookPostLand] = result
			}
		}
	}

	// 9. Update status to LANDED
	if err := p.store.UpdateStatus(id, StatusLanded); err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	return &LandResult{
		WorkspaceID:  id,
		MergeCommit:  mergeCommit,
		FilesChanged: filesChanged,
		Strategy:     strategy,
		HookResults:  hookResults,
		Duration:     time.Since(start),
	}, nil
}

// DryRun performs a non-destructive check of whether landing would succeed.
func (p *LandingPipeline) DryRun(id string, opts LandOpts) (*DryRunResult, error) {
	ws, err := p.store.GetWorkspace(id)
	if err != nil {
		return nil, fmt.Errorf("workspace lookup failed: %w", err)
	}

	strategy := opts.Strategy
	if strategy == "" {
		strategy = StrategyRebaseFF
	}

	result := &DryRunResult{
		WorkspaceID: id,
		CanLand:     true,
		Strategy:    strategy,
		HooksPassed: true,
	}

	// Check status
	if !opts.Force && !isLandableStatus(ws.Status) {
		result.CanLand = false
		result.Issues = append(result.Issues, fmt.Sprintf("status is %s, must be READY, IDLE, or PAUSED", ws.Status))
	}

	// Check for conflicts using merge-tree (non-destructive)
	conflicts, err := git.HasConflictsWith(ws.RepoRoot, ws.BaseBranch, ws.Branch)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("conflict check failed: %v", err))
	}
	if len(conflicts) > 0 {
		result.CanLand = false
		result.ConflictFiles = conflicts
		result.Issues = append(result.Issues, fmt.Sprintf("%d conflicting files", len(conflicts)))
	}

	// Get diff stats
	filesChanged, err := git.DiffFiles(ws.RepoRoot, ws.BaseBranch, ws.Branch)
	if err == nil {
		result.FilesChanged = filesChanged
	}

	diffStat, err := git.DiffStat(ws.RepoRoot, ws.BaseBranch, ws.Branch)
	if err == nil {
		result.DiffStat = diffStat.Summary
	}

	return result, nil
}

// executeMerge performs the actual merge operation based on the strategy.
func (p *LandingPipeline) executeMerge(ws *Workspace, strategy MergeStrategy) (string, error) {
	switch strategy {
	case StrategyRebaseFF:
		return p.mergeRebaseFF(ws)
	case StrategySquash:
		return p.mergeSquash(ws)
	case StrategyFFOnly:
		return p.mergeFFOnly(ws)
	case StrategyMerge:
		return p.mergeMergeCommit(ws)
	default:
		return "", fmt.Errorf("unknown merge strategy: %s", strategy)
	}
}

// mergeRebaseFF: rebase workspace branch onto base, then ff-merge into base.
func (p *LandingPipeline) mergeRebaseFF(ws *Workspace) (string, error) {
	// Rebase the workspace branch onto the base branch
	if err := git.Rebase(ws.WorktreePath, ws.BaseBranch); err != nil {
		// On conflict, abort rebase to leave clean state
		_ = git.AbortRebase(ws.WorktreePath)
		return "", fmt.Errorf("rebase conflict: %w", err)
	}

	// Switch to base branch in the repo root and ff-merge
	if err := git.CheckoutBranch(ws.RepoRoot, ws.BaseBranch); err != nil {
		return "", fmt.Errorf("checkout base branch: %w", err)
	}

	if err := git.MergeFF(ws.RepoRoot, ws.Branch); err != nil {
		return "", fmt.Errorf("fast-forward merge: %w", err)
	}

	sha, err := git.HeadRef(ws.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("get merge commit: %w", err)
	}

	return sha, nil
}

// mergeSquash: squash all commits from workspace branch into one on base.
func (p *LandingPipeline) mergeSquash(ws *Workspace) (string, error) {
	if err := git.CheckoutBranch(ws.RepoRoot, ws.BaseBranch); err != nil {
		return "", fmt.Errorf("checkout base branch: %w", err)
	}

	message := ws.Task
	if message == "" {
		message = fmt.Sprintf("Land workspace %s", ws.ID)
	}

	if err := git.MergeSquash(ws.RepoRoot, ws.Branch, message); err != nil {
		return "", fmt.Errorf("squash merge: %w", err)
	}

	sha, err := git.HeadRef(ws.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("get merge commit: %w", err)
	}

	return sha, nil
}

// mergeFFOnly: fast-forward only, fail if not possible.
func (p *LandingPipeline) mergeFFOnly(ws *Workspace) (string, error) {
	if err := git.CheckoutBranch(ws.RepoRoot, ws.BaseBranch); err != nil {
		return "", fmt.Errorf("checkout base branch: %w", err)
	}

	if err := git.MergeFF(ws.RepoRoot, ws.Branch); err != nil {
		return "", fmt.Errorf("ff-only merge: %w", err)
	}

	sha, err := git.HeadRef(ws.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("get merge commit: %w", err)
	}

	return sha, nil
}

// mergeMergeCommit: regular merge with merge commit.
func (p *LandingPipeline) mergeMergeCommit(ws *Workspace) (string, error) {
	if err := git.CheckoutBranch(ws.RepoRoot, ws.BaseBranch); err != nil {
		return "", fmt.Errorf("checkout base branch: %w", err)
	}

	message := fmt.Sprintf("Merge workspace %s", ws.ID)
	if ws.Task != "" {
		message = ws.Task
	}

	if err := git.MergeCommit(ws.RepoRoot, ws.Branch, message); err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}

	sha, err := git.HeadRef(ws.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("get merge commit: %w", err)
	}

	return sha, nil
}

func isLandableStatus(status WorkspaceStatus) bool {
	return status == StatusReady || status == StatusIdle || status == StatusPaused
}

func makeHookVars(ws *Workspace) HookVars {
	return HookVars{
		WorkspaceID:  ws.ID,
		WorktreePath: ws.WorktreePath,
		Branch:       ws.Branch,
		BaseBranch:   ws.BaseBranch,
		RepoRoot:     ws.RepoRoot,
	}
}
