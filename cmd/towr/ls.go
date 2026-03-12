package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newLsCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var allFlag bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, appErr := initApp()

			var workspaces []*store.Workspace
			showRepoColumn := false

			if appErr != nil || allFlag {
				// Outside repo or --all: show everything
				reposDir := filepath.Join(config.TowrHome(), "repos")
				var err error
				workspaces, err = store.ListAllWorkspaces(reposDir)
				if err != nil {
					if appErr != nil {
						return appErr
					}
					return err
				}
				showRepoColumn = true

				// Reconcile in all-repos mode.
				staleThreshold := 7 * 24 * time.Hour
				for _, ws := range workspaces {
					result := workspace.ReconcileWorkspace(ws, staleThreshold)
					if result != nil {
						ws.Status = string(result.To)
					}
				}
			} else {
				// Inside repo: show only this repo's workspaces
				var err error
				workspaces, err = app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
				if err != nil {
					return fmt.Errorf("list workspaces: %w", err)
				}

				// Lightweight reconciliation: detect STALE, MERGED, ORPHANED.
				staleThreshold := 7 * 24 * time.Hour
				for _, ws := range workspaces {
					result := workspace.ReconcileWorkspace(ws, staleThreshold)
					if result != nil {
						ws.Status = string(result.To)
						_ = app.store.SaveWorkspace(ws)
						_ = app.store.EmitEvent(store.Event{
							Kind:        store.EventWorkspaceAutoTransition,
							WorkspaceID: ws.ID,
							RepoRoot:    ws.RepoRoot,
							Data: map[string]interface{}{
								"from":   string(result.From),
								"to":     string(result.To),
								"reason": result.Reason,
							},
						})
					}
				}
			}

			// Build task status map for each workspace.
			taskStatusMap := make(map[string]string)
			if app != nil {
				for _, ws := range workspaces {
					taskStatusMap[ws.ID] = resolveTaskStatus(app.store, ws.RepoRoot, ws.ID)
				}
			}

			if *jsonFlag {
				type jsonWS struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Branch   string `json:"branch"`
					RepoRoot string `json:"repo_root,omitempty"`
					Task     string `json:"task,omitempty"`
					Diff     struct {
						Added   int `json:"added"`
						Removed int `json:"removed"`
					} `json:"diff"`
					Worktree struct {
						Staged    int `json:"staged"`
						Unstaged  int `json:"unstaged"`
						Untracked int `json:"untracked"`
					} `json:"worktree"`
					Merged bool   `json:"merged"`
					Age    string `json:"age"`
				}
				var items []jsonWS
				for _, ws := range workspaces {
					item := jsonWS{
						ID:     ws.ID,
						Status: ws.Status,
						Branch: ws.Branch,
						Age:    cli.FormatAgeFromString(ws.CreatedAt),
						Task:   taskStatusMap[ws.ID],
					}
					if showRepoColumn {
						item.RepoRoot = ws.RepoRoot
					}
					added, removed := getDiffCounts(ws.RepoRoot, ws.BaseBranch, ws.Branch)
					item.Diff.Added = added
					item.Diff.Removed = removed
					if ws.WorktreePath != "" && ws.RepoRoot != "" {
						ds, _ := workspace.WorktreeDetailedStatus(ws.WorktreePath)
						item.Worktree.Staged = ds.Staged
						item.Worktree.Unstaged = ds.Unstaged
						item.Worktree.Untracked = ds.Untracked
					}
					item.Merged = workspace.IsBranchMerged(ws.RepoRoot, ws.BaseBranch, ws.Branch, ws.BaseRef)
					items = append(items, item)
				}
				return cli.PrintJSON(items)
			}

			if len(workspaces) == 0 {
				fmt.Println("No workspaces found.")
				return nil
			}

			// Compute per-workspace overlap counts (repo-scoped only).
			// Use a set per workspace to avoid counting the same file multiple times
			// when it overlaps with multiple peers.
			overlapCounts := make(map[string]int)
			if !showRepoColumn {
				overlapFiles := make(map[string]map[string]bool) // wsID → set of files
				pairs := workspace.DetectOverlaps(workspaces)
				for _, p := range pairs {
					if overlapFiles[p.WorkspaceA] == nil {
						overlapFiles[p.WorkspaceA] = make(map[string]bool)
					}
					if overlapFiles[p.WorkspaceB] == nil {
						overlapFiles[p.WorkspaceB] = make(map[string]bool)
					}
					for _, f := range p.Files {
						overlapFiles[p.WorkspaceA][f] = true
						overlapFiles[p.WorkspaceB][f] = true
					}
				}
				for wsID, files := range overlapFiles {
					overlapCounts[wsID] = len(files)
				}
			}

			columns := []cli.Column{
				{Header: "ID", Width: 14},
				{Header: "STATUS", Width: 10},
				{Header: "TASK", Width: 12},
				{Header: "HEALTH", Width: 8},
				{Header: "ACTIVITY", Width: 10},
				{Header: "DRIFT", Width: 6},
				{Header: "OVERLAP", Width: 8},
				{Header: "DIFF", Width: 10},
				{Header: "TREE", Width: 10},
				{Header: "AGENT", Width: 8},
				{Header: "AGE", Width: 5},
			}
			if showRepoColumn {
				// Insert REPO column after ID.
				columns = append([]cli.Column{
					{Header: "ID", Width: 14},
					{Header: "REPO", Width: 12},
				}, columns[1:]...)
			}

			table := cli.NewTablePrinter(os.Stdout, columns)
			table.PrintHeader()

			// Query health for each workspace. Only available in repo-scoped mode
			// because hook events are stored per-repo — querying from the wrong
			// store would return incorrect results for cross-repo workspaces.
			var healthMap map[string]string
			if app != nil && !showRepoColumn {
				healthMap = make(map[string]string)
				for _, ws := range workspaces {
					healthMap[ws.ID] = app.store.LastHookResult(ws.RepoRoot, ws.ID)
				}
			}

			for _, ws := range workspaces {
				isNonRepo := ws.RepoRoot == ""

				var diffStr, treeStr, statusStr string
				if isNonRepo {
					diffStr = "-"
					treeStr = "-"
					statusStr = cli.ColorStatus(ws.Status)
				} else {
					added, removed := getDiffCounts(ws.RepoRoot, ws.BaseBranch, ws.Branch)
					diffStr = cli.FormatDiff(added, removed)

					treeStr = "-"
					if ws.WorktreePath != "" {
						ds, err := workspace.WorktreeDetailedStatus(ws.WorktreePath)
						if err == nil {
							treeStr = cli.FormatWorktreeStatus(ds.Staged, ds.Unstaged, ds.Untracked)
						}
					}

					statusStr = cli.ColorStatus(ws.Status)
					if workspace.IsBranchMerged(ws.RepoRoot, ws.BaseBranch, ws.Branch, ws.BaseRef) {
						statusStr = cli.FormatMergeStatus(true)
					}
				}

				// Health.
				healthStr := "-"
				if healthMap != nil {
					if h := healthMap[ws.ID]; h != "" {
						healthStr = h
					}
				}

				// Activity.
				activityStr := cli.FormatAgeFromString(ws.LastActivity)

				// Drift.
				driftStr := "0"
				if !isNonRepo {
					drift := workspace.DriftCount(ws.RepoRoot, ws.BaseBranch, ws.Branch)
					if drift > 0 {
						driftStr = fmt.Sprintf("+%d", drift)
					}
				}

				// Agent.
				agentStr := "-"
				if ws.AgentRuntime != "" {
					agentStr = ws.AgentRuntime
				}

				// Task.
				taskStr := "-"
				if ts := taskStatusMap[ws.ID]; ts != "" {
					taskStr = ts
				}

				age := cli.FormatAgeFromString(ws.CreatedAt)

				// Overlap.
				overlapStr := "-"
				if !showRepoColumn {
					overlapStr = "\033[2mclean\033[0m"
					if n := overlapCounts[ws.ID]; n > 0 {
						overlapStr = fmt.Sprintf("⚠ %d", n)
					}
				}

				row := []string{
					ws.ID,
					statusStr,
					taskStr,
					healthStr,
					activityStr,
					driftStr,
					overlapStr,
					diffStr,
					treeStr,
					agentStr,
					age,
				}
				if showRepoColumn {
					var repoName string
					if isNonRepo {
						if ws.WorktreePath != "" {
							repoName = "~" + filepath.Base(ws.WorktreePath)
						} else {
							repoName = "-"
						}
					} else {
						repoName = filepath.Base(ws.RepoRoot)
					}
					// Insert REPO after ID.
					row = append([]string{
						ws.ID,
						repoName,
					}, row[1:]...)
				}

				table.PrintRow(row)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&allFlag, "all", false, "show workspaces across all repos")

	return cmd
}

// resolveTaskStatus queries the store for the latest dispatch status of a workspace
// and returns a display string like "d-0001 ▶" (running), "d-0001 ✓" (completed),
// "d-0001 ✗" (failed), or "d-0001 ◌" (dispatched/waiting).
func resolveTaskStatus(s *store.SQLiteStore, repoRoot, wsID string) string {
	disp, err := s.LatestDispatch(repoRoot, wsID)
	if err != nil || disp == nil {
		return ""
	}
	dispID, _ := disp.Data["dispatch_id"].(string)
	if dispID == "" {
		return ""
	}

	evt, err := s.LatestTaskEvent(repoRoot, wsID, dispID)
	if err != nil || evt == nil {
		// Dispatch exists but no follow-up event yet.
		return dispID + " ◌"
	}

	switch evt.Kind {
	case store.EventTaskStarted:
		return dispID + " ▶"
	case store.EventTaskCompleted:
		return dispID + " ✓"
	case store.EventTaskFailed:
		return dispID + " ✗"
	default:
		return dispID + " ◌"
	}
}

// getDiffCounts returns added/removed line counts between base and head branches.
func getDiffCounts(repoRoot, baseBranch, branch string) (int, int) {
	if repoRoot == "" || baseBranch == "" || branch == "" {
		return 0, 0
	}
	out, err := git.RunGit(repoRoot, "diff", "--shortstat", baseBranch+"..."+branch)
	if err != nil {
		return 0, 0
	}
	return parseShortstat(out)
}

// parseShortstat parses "3 files changed, 10 insertions(+), 2 deletions(-)" into (10, 2).
func parseShortstat(s string) (int, int) {
	var added, removed int
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "insertion") {
			fmt.Sscanf(p, "%d", &added)
		} else if strings.Contains(p, "deletion") {
			fmt.Sscanf(p, "%d", &removed)
		}
	}
	return added, removed
}
