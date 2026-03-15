package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WorkspaceRow holds display data for one workspace.
type WorkspaceRow struct {
	ID           string
	Status       string
	Health       string // "pass", "fail", ""
	Activity     string // time since last event
	Drift        int    // commits behind base
	Agent        string // runtime name
	TaskStatus   string // dispatch status e.g. "d-0001 ▶"
	Branch       string
	Added        int
	Removed      int
	Staged       int
	Unstaged     int
	Untracked    int
	Merged       bool
	Pushed       bool
	Age          string
	RepoRoot     string
	BaseBranch   string
	BaseRef      string
	WorktreePath string
	ExitCode     *int
}

// view tracks which view is active.
type view int

const (
	viewDashboard view = iota
	viewDetail
	viewConfirmCleanup
)

// DashboardModel is the top-level bubbletea model.
type DashboardModel struct {
	workspaces []WorkspaceRow
	cursor     int
	width      int
	height     int
	view       view
	repoRoot   string

	// Detail view state.
	detailFiles  []string
	detailCursor int
	detailDiff   string

	// Cleanup confirmation state.
	cleanupWarnings []string
	cleanupSafe     bool // true if cleanup has no data loss risk

	// Store access for refreshing.
	repoStorePath string // repo-scoped store path (empty if started outside repo)
	allStorePath  string // all-repos directory path
	allRepos      bool   // current toggle state

	// Mission control context (from tmux env).
	planName   string
	runCost    string
	runElapsed string
}

// NewDashboardModel creates a new dashboard model.
func NewDashboardModel(repoRoot, repoStorePath, allStorePath string, allRepos bool) DashboardModel {
	return DashboardModel{
		repoRoot:      repoRoot,
		repoStorePath: repoStorePath,
		allStorePath:  allStorePath,
		allRepos:      allRepos,
	}
}

func (m DashboardModel) activeStorePath() string {
	if m.allRepos {
		return m.allStorePath
	}
	return m.repoStorePath
}

// tickMsg triggers periodic workspace refresh.
type tickMsg time.Time

// workspacesMsg carries refreshed workspace data.
type workspacesMsg []WorkspaceRow

// detailMsg carries file list and diff for detail view.
type detailMsg struct {
	files []string
	diff  string
}

// contextMsg carries mux context read from tmux environment variables.
type contextMsg struct {
	planName string
	cost     string
	elapsed  string
}

// Init starts the periodic refresh.
func (m DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		refreshWorkspaces(m.repoRoot, m.activeStorePath(), m.allRepos),
		readMuxContext(),
		tickEvery(2*time.Second),
	)
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func refreshWorkspaces(repoRoot, storePath string, allRepos bool) tea.Cmd {
	return func() tea.Msg {
		rows, err := loadWorkspaces(repoRoot, storePath, allRepos)
		if err != nil {
			return workspacesMsg(nil)
		}
		return workspacesMsg(rows)
	}
}

// readMuxContext reads plan name, cost, and elapsed time from tmux session env vars.
func readMuxContext() tea.Cmd {
	return func() tea.Msg {
		var plan, costStr, elapsed string
		if out, err := exec.Command("tmux", "show-environment", "-t", "towr-mux", "TOWR_PLAN").CombinedOutput(); err == nil {
			s := strings.TrimSpace(string(out))
			if idx := strings.IndexByte(s, '='); idx >= 0 {
				plan = s[idx+1:]
			}
		}
		if out, err := exec.Command("tmux", "show-environment", "-t", "towr-mux", "TOWR_COST").CombinedOutput(); err == nil {
			s := strings.TrimSpace(string(out))
			if idx := strings.IndexByte(s, '='); idx >= 0 {
				costStr = s[idx+1:]
			}
		}
		if out, err := exec.Command("tmux", "show-environment", "-t", "towr-mux", "TOWR_ELAPSED").CombinedOutput(); err == nil {
			s := strings.TrimSpace(string(out))
			if idx := strings.IndexByte(s, '='); idx >= 0 {
				elapsed = s[idx+1:]
			}
		}
		return contextMsg{planName: plan, cost: costStr, elapsed: elapsed}
	}
}

func loadWorkspaces(repoRoot, storePath string, allRepos bool) ([]WorkspaceRow, error) {
	if allRepos {
		return loadAllWorkspaces(storePath)
	}
	return loadRepoWorkspaces(repoRoot, storePath)
}

func loadRepoWorkspaces(repoRoot, storePath string) ([]WorkspaceRow, error) {
	s := store.NewSQLiteStore()
	if err := s.Init(storePath); err != nil {
		return nil, err
	}
	defer s.Close()

	workspaces, err := s.ListWorkspaces(repoRoot, store.ListFilter{})
	if err != nil {
		return nil, err
	}

	// Lightweight reconciliation: detect STALE, MERGED, ORPHANED.
	staleThreshold := 7 * 24 * time.Hour
	for _, ws := range workspaces {
		result := workspace.ReconcileWorkspace(ws, staleThreshold)
		if result != nil {
			ws.Status = string(result.To)
			_ = s.SaveWorkspace(ws)
			_ = s.EmitEvent(store.Event{
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

	var rows []WorkspaceRow
	for _, ws := range workspaces {
		health := s.LastHookResult(ws.RepoRoot, ws.ID)
		row := buildWorkspaceRow(ws, health)
		row.TaskStatus = resolveTaskStatus(s, ws.RepoRoot, ws.ID)
		rows = append(rows, row)
	}
	return rows, nil
}

func loadAllWorkspaces(reposDir string) ([]WorkspaceRow, error) {
	workspaces, err := store.ListAllWorkspaces(reposDir)
	if err != nil {
		return nil, err
	}

	// Reconcile in all-repos mode too.
	staleThreshold := 7 * 24 * time.Hour
	for _, ws := range workspaces {
		result := workspace.ReconcileWorkspace(ws, staleThreshold)
		if result != nil {
			ws.Status = string(result.To)
			// Best-effort persist: open the workspace's store, save, close.
			if dbPath := reconcileDBPath(reposDir, ws); dbPath != "" {
				if rs := openAndSave(dbPath, ws, result); rs != nil {
					_ = rs
				}
			}
		}
	}

	var rows []WorkspaceRow
	for _, ws := range workspaces {
		// Health not available in all-repos mode (no single store to query).
		rows = append(rows, buildWorkspaceRow(ws, ""))
	}
	return rows, nil
}

// reconcileDBPath finds the state.db path for a workspace in all-repos mode.
func reconcileDBPath(reposDir string, ws *store.Workspace) string {
	if ws.RepoRoot != "" {
		return filepath.Join(config.RepoStatePath(ws.RepoRoot), "state.db")
	}
	return filepath.Join(filepath.Dir(reposDir), "global-state.db")
}

// openAndSave persists a reconciliation result to the workspace's store.
func openAndSave(dbPath string, ws *store.Workspace, result *workspace.ReconcileResult) error {
	s := store.NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		return err
	}
	defer s.Close()
	_ = s.SaveWorkspace(ws)
	_ = s.EmitEvent(store.Event{
		Kind:        store.EventWorkspaceAutoTransition,
		WorkspaceID: ws.ID,
		RepoRoot:    ws.RepoRoot,
		Data: map[string]interface{}{
			"from":   string(result.From),
			"to":     string(result.To),
			"reason": result.Reason,
		},
	})
	return nil
}

// resolveTaskStatus queries the store for the latest dispatch status of a workspace.
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

func buildWorkspaceRow(ws *store.Workspace, health string) WorkspaceRow {
	added, removed := getDiffCounts(ws.RepoRoot, ws.BaseBranch, ws.Branch)
	row := WorkspaceRow{
		ID:           ws.ID,
		Status:       ws.Status,
		Health:       health,
		Activity:     formatAge(ws.LastActivity),
		Drift:        workspace.DriftCount(ws.RepoRoot, ws.BaseBranch, ws.Branch),
		Agent:        ws.AgentRuntime,
		Branch:       ws.Branch,
		Added:        added,
		Removed:      removed,
		Age:          formatAge(ws.CreatedAt),
		RepoRoot:     ws.RepoRoot,
		BaseBranch:   ws.BaseBranch,
		BaseRef:      ws.BaseRef,
		WorktreePath: ws.WorktreePath,
		ExitCode:     ws.ExitCode,
		Merged:       workspace.IsBranchMerged(ws.RepoRoot, ws.BaseBranch, ws.Branch, ws.BaseRef),
		Pushed:       isBranchPushed(ws.RepoRoot, ws.Branch),
	}
	if ws.WorktreePath != "" && ws.RepoRoot != "" {
		ds, err := workspace.WorktreeDetailedStatus(ws.WorktreePath)
		if err == nil {
			row.Staged = ds.Staged
			row.Unstaged = ds.Unstaged
			row.Untracked = ds.Untracked
		}
	}
	return row
}

// isBranchPushed checks if the branch exists on the remote.
func isBranchPushed(repoRoot, branch string) bool {
	if repoRoot == "" || branch == "" {
		return false
	}
	_, err := git.RunGit(repoRoot, "rev-parse", "--verify", "origin/"+branch)
	return err == nil
}

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

func formatAge(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return "-"
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// loadDetail loads changed files and diff for a workspace.
// Checks both committed branch diff and uncommitted worktree changes.
func loadDetail(ws WorkspaceRow) tea.Cmd {
	return func() tea.Msg {
		if ws.WorktreePath == "" || ws.RepoRoot == "" {
			return detailMsg{}
		}

		var files []string
		var diff string

		// First try committed branch diff (base...branch).
		filesOut, err := git.RunGit(ws.RepoRoot, "diff", "--name-only", ws.BaseBranch+"..."+ws.Branch)
		if err == nil && filesOut != "" {
			files = strings.Split(filesOut, "\n")
		}
		diff, _ = git.RunGit(ws.RepoRoot, "diff", "--color=always", ws.BaseBranch+"..."+ws.Branch)

		// If no committed diff, fall back to uncommitted worktree changes.
		if len(files) == 0 {
			wtFiles, wtErr := git.RunGit(ws.WorktreePath, "diff", "--name-only")
			if wtErr == nil && wtFiles != "" {
				files = strings.Split(wtFiles, "\n")
			}
			// Also include staged files.
			stagedFiles, stErr := git.RunGit(ws.WorktreePath, "diff", "--cached", "--name-only")
			if stErr == nil && stagedFiles != "" {
				for _, f := range strings.Split(stagedFiles, "\n") {
					if f != "" {
						files = append(files, f)
					}
				}
			}
			// Deduplicate.
			files = uniqueStrings(files)

			wtDiff, _ := git.RunGit(ws.WorktreePath, "diff", "--color=always")
			stagedDiff, _ := git.RunGit(ws.WorktreePath, "diff", "--cached", "--color=always")
			if stagedDiff != "" && wtDiff != "" {
				diff = "=== Staged ===\n" + stagedDiff + "\n=== Unstaged ===\n" + wtDiff
			} else if stagedDiff != "" {
				diff = stagedDiff
			} else {
				diff = wtDiff
			}
		}

		return detailMsg{files: files, diff: diff}
	}
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var result []string
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// Update handles messages.
func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			refreshWorkspaces(m.repoRoot, m.activeStorePath(), m.allRepos),
			readMuxContext(),
			tickEvery(2*time.Second),
		)

	case workspacesMsg:
		m.workspaces = []WorkspaceRow(msg)
		if m.cursor >= len(m.workspaces) && len(m.workspaces) > 0 {
			m.cursor = len(m.workspaces) - 1
		}
		return m, nil

	case contextMsg:
		m.planName = msg.planName
		m.runCost = msg.cost
		m.runElapsed = msg.elapsed
		return m, nil

	case detailMsg:
		m.detailFiles = msg.files
		m.detailDiff = msg.diff
		m.detailCursor = 0
		return m, nil
	}

	return m, nil
}

func (m DashboardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewDashboard:
		return m.handleDashboardKey(msg)
	case viewDetail:
		return m.handleDetailKey(msg)
	case viewConfirmCleanup:
		return m.handleConfirmCleanupKey(msg)
	}
	return m, nil
}

func (m DashboardModel) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		if m.cursor < len(m.workspaces)-1 {
			m.cursor++
		}
		return m, nil

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "enter":
		if len(m.workspaces) > 0 {
			m.view = viewDetail
			return m, loadDetail(m.workspaces[m.cursor])
		}
		return m, nil

	case "l":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			return m, landWorkspace(ws)
		}
		return m, nil

	case "o":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			return m, openEditor(ws)
		}
		return m, nil

	case "a":
		// Toggle between repo-scoped and all-workspaces view.
		if m.repoStorePath != "" {
			m.allRepos = !m.allRepos
			m.cursor = 0
			return m, refreshWorkspaces(m.repoRoot, m.activeStorePath(), m.allRepos)
		}
		return m, nil

	case "d":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			return m, showDiff(ws)
		}
		return m, nil

	case "s":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			return m, switchToWorkspace(ws)
		}
		return m, nil

	case "c":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			warnings, safe := cleanupSafetyCheck(ws)
			m.cleanupWarnings = warnings
			m.cleanupSafe = safe
			m.view = viewConfirmCleanup
		}
		return m, nil
	}

	return m, nil
}

func (m DashboardModel) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		m.view = viewDashboard
		return m, nil

	case "j", "down":
		if m.detailCursor < len(m.detailFiles)-1 {
			m.detailCursor++
		}
		return m, nil

	case "k", "up":
		if m.detailCursor > 0 {
			m.detailCursor--
		}
		return m, nil

	case "o":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			return m, openEditor(ws)
		}
		return m, nil
	}

	return m, nil
}

// landWorkspace runs `towr land` for the workspace via a subprocess.
func (m DashboardModel) handleConfirmCleanupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if len(m.workspaces) > 0 {
			ws := m.workspaces[m.cursor]
			m.view = viewDashboard
			return m, doCleanup(ws)
		}
		m.view = viewDashboard
		return m, nil

	case "n", "esc", "q":
		m.view = viewDashboard
		return m, nil
	}
	return m, nil
}

// cleanupSafetyCheck returns warnings about potential data loss and whether cleanup is safe.
func cleanupSafetyCheck(ws WorkspaceRow) (warnings []string, safe bool) {
	// Non-repo workspace: always safe (files stay on disk).
	if ws.RepoRoot == "" {
		return []string{"Non-repo workspace — files at " + ws.WorktreePath + " will NOT be deleted."}, true
	}

	safe = true

	// Check for uncommitted changes.
	if ws.Staged > 0 || ws.Unstaged > 0 || ws.Untracked > 0 {
		var parts []string
		if ws.Unstaged > 0 {
			parts = append(parts, fmt.Sprintf("%d unstaged", ws.Unstaged))
		}
		if ws.Staged > 0 {
			parts = append(parts, fmt.Sprintf("%d staged", ws.Staged))
		}
		if ws.Untracked > 0 {
			parts = append(parts, fmt.Sprintf("%d untracked", ws.Untracked))
		}
		warnings = append(warnings, "Uncommitted changes: "+strings.Join(parts, ", "))
		safe = false
	}

	// Check if branch has commits not on base.
	if ws.RepoRoot != "" && ws.BaseBranch != "" && ws.Branch != "" {
		aheadOut, err := git.RunGit(ws.RepoRoot, "rev-list", "--count", ws.BaseBranch+".."+ws.Branch)
		if err == nil && strings.TrimSpace(aheadOut) != "0" {
			commits := strings.TrimSpace(aheadOut)

			if ws.Merged {
				warnings = append(warnings, commits+" commit(s) on branch — already merged into "+ws.BaseBranch+".")
			} else if ws.Pushed {
				warnings = append(warnings, commits+" commit(s) on branch — pushed to remote, not merged.")
			} else {
				warnings = append(warnings, commits+" commit(s) on branch — NOT pushed or merged. Will be lost!")
				safe = false
			}
		}
	}

	if safe && len(warnings) == 0 {
		warnings = append(warnings, "No uncommitted changes, no unmerged commits.")
	}

	return warnings, safe
}

// doCleanup runs `towr cleanup --force <id>` as a subprocess.
func doCleanup(ws WorkspaceRow) tea.Cmd {
	return tea.ExecProcess(exec.Command(os.Args[0], "cleanup", "--force", ws.ID), func(err error) tea.Msg {
		return tickMsg(time.Now())
	})
}

func landWorkspace(ws WorkspaceRow) tea.Cmd {
	return tea.ExecProcess(exec.Command(os.Args[0], "land", ws.ID), func(err error) tea.Msg {
		return tickMsg(time.Now())
	})
}

// openEditor opens $EDITOR in the workspace's worktree.
func openEditor(ws WorkspaceRow) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, ".")
	cmd.Dir = ws.WorktreePath
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return tickMsg(time.Now())
	})
}

// showDiff shows the diff for the selected workspace using towr diff --full.
func showDiff(ws WorkspaceRow) tea.Cmd {
	return tea.ExecProcess(exec.Command(os.Args[0], "diff", "--full", ws.ID), func(err error) tea.Msg {
		return tickMsg(time.Now())
	})
}

// switchToWorkspace quits the TUI and runs `towr open` to switch tmux session.
func switchToWorkspace(ws WorkspaceRow) tea.Cmd {
	return tea.ExecProcess(exec.Command(os.Args[0], "open", ws.ID), func(err error) tea.Msg {
		// After switching, quit the TUI — user is now in the workspace session.
		return tea.QuitMsg{}
	})
}

// View renders the current view.
func (m DashboardModel) View() string {
	switch m.view {
	case viewDetail:
		return m.renderDetail()
	case viewConfirmCleanup:
		return m.renderConfirmCleanup()
	default:
		return m.renderDashboard()
	}
}

func (m DashboardModel) renderConfirmCleanup() string {
	if m.cursor >= len(m.workspaces) {
		return "No workspace selected"
	}
	ws := m.workspaces[m.cursor]

	var b strings.Builder

	title := fmt.Sprintf(" Cleanup %s ", ws.ID)
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if ws.RepoRoot != "" {
		b.WriteString(fmt.Sprintf("  Branch:   %s (base: %s)\n", ws.Branch, ws.BaseBranch))
		b.WriteString(fmt.Sprintf("  Worktree: %s\n", ws.WorktreePath))
	} else {
		b.WriteString(fmt.Sprintf("  Path: %s\n", ws.WorktreePath))
	}
	b.WriteString("\n")

	for _, w := range m.cleanupWarnings {
		icon := "  ✓ "
		style := statusReady
		if !m.cleanupSafe {
			// Check if this specific warning is the dangerous one.
			if strings.Contains(w, "NOT pushed") || strings.Contains(w, "Uncommitted") {
				icon = "  ✗ "
				style = statusBlocked
			} else {
				icon = "  ! "
				style = statusRunning
			}
		}
		b.WriteString(icon + style.Render(w) + "\n")
	}

	b.WriteString("\n")
	if ws.RepoRoot == "" {
		b.WriteString(dimStyle.Render("  This will remove the workspace entry and tmux session."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Files on disk will NOT be deleted."))
	} else if m.cleanupSafe {
		b.WriteString(dimStyle.Render("  This will remove the worktree, branch, and tmux session."))
	} else {
		b.WriteString(statusBlocked.Render("  WARNING: You may lose uncommitted or unpushed work!"))
	}

	b.WriteString("\n\n")
	b.WriteString(footerStyle.Render(" y confirm  n/esc cancel"))
	b.WriteString("\n")

	return b.String()
}

// narrowThreshold is the width below which the dashboard switches to
// narrow/compact mode, suitable for the mux control pane (~30-50 cols).
const narrowThreshold = 60

func (m DashboardModel) renderDashboard() string {
	if m.width > 0 && m.width < narrowThreshold {
		return m.renderNarrowDashboard()
	}
	return m.renderWideDashboard()
}

// renderNarrowDashboard renders a mission-control-style layout for the mux
// control pane (~30-50 columns wide). Shows agent cards with status icons,
// diff stats, progress bars, and a summary footer.
func (m DashboardModel) renderNarrowDashboard() string {
	var b strings.Builder
	maxW := m.width
	if maxW <= 0 {
		maxW = 40
	}

	// Header.
	b.WriteString(headerStyle.Render("TOWR"))
	b.WriteString("\n")

	// Plan name box (if available).
	if m.planName != "" {
		planDisplay := truncate(m.planName, maxW-4)
		borderW := len(planDisplay) + 2
		if borderW > maxW-2 {
			borderW = maxW - 2
		}
		border := strings.Repeat("─", borderW)
		b.WriteString(dimStyle.Render("┌" + border + "┐"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("│") + " " + planNameStyle.Render(planDisplay) + " " + dimStyle.Render("│"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("└" + border + "┘"))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Agent cards.
	if len(m.workspaces) == 0 {
		b.WriteString(dimStyle.Render(" (no agents)"))
		b.WriteString("\n")
	} else {
		for i, ws := range m.workspaces {
			// Status icon + name.
			var icon string
			switch {
			case ws.Status == "BLOCKED" || ws.Status == "blocked":
				icon = statusBlocked.Render("✗")
			case ws.ExitCode != nil && *ws.ExitCode == 0:
				icon = statusReady.Render("✓")
			case ws.ExitCode != nil:
				icon = statusBlocked.Render("✗")
			default:
				icon = statusRunning.Render("▶")
			}

			// Agent name (bold if selected).
			name := ws.ID
			if len(name) > maxW-4 {
				name = name[:maxW-4]
			}
			if i == m.cursor {
				b.WriteString(icon + " " + boldStyle.Render(name))
			} else {
				b.WriteString(icon + " " + name)
			}
			b.WriteString("\n")

			// Agent runtime info.
			agentInfo := ws.Agent
			if agentInfo == "" {
				agentInfo = "claude"
			}
			b.WriteString("  " + dimStyle.Render(agentInfo))
			b.WriteString("\n")

			// Diff stats + time.
			colored := diffAdded.Render(fmt.Sprintf("+%d", ws.Added)) + "/" + diffRemoved.Render(fmt.Sprintf("-%d", ws.Removed))
			timeStr := ws.Activity
			if timeStr == "" || timeStr == "-" {
				timeStr = ws.Age
			}
			if timeStr == "" || timeStr == "-" {
				timeStr = ""
			}
			if timeStr != "" {
				b.WriteString("  " + colored + " · " + dimStyle.Render(timeStr))
			} else {
				b.WriteString("  " + colored)
			}
			b.WriteString("\n")

			// Progress bar.
			pct := estimateProgress(ws)
			bar := renderProgressBar(pct, 10)
			b.WriteString("  " + bar + " " + dimStyle.Render(fmt.Sprintf("%d%%", pct)))
			b.WriteString("\n")

			// Blank line between cards (but not after last).
			if i < len(m.workspaces)-1 {
				b.WriteString("\n")
			}
		}
	}

	// Summary footer.
	b.WriteString("\n")
	sepW := maxW - 2
	if sepW < 1 {
		sepW = 1
	}
	b.WriteString(dimStyle.Render(strings.Repeat("─", sepW)))
	b.WriteString("\n")

	agentCount := len(m.workspaces)
	costStr := ""
	if m.runCost != "" {
		costStr = " · " + statusRunning.Render("$"+m.runCost)
	}
	b.WriteString(fmt.Sprintf("%d agents%s", agentCount, costStr))
	b.WriteString("\n")

	// Blocked count + elapsed.
	var blockedCount int
	for _, ws := range m.workspaces {
		if ws.Status == "BLOCKED" || ws.Status == "blocked" {
			blockedCount++
		}
	}
	blockedStr := fmt.Sprintf("%d blocked", blockedCount)
	elapsedStr := ""
	if m.runElapsed != "" {
		elapsedStr = " · " + m.runElapsed
	}
	b.WriteString(dimStyle.Render(blockedStr + elapsedStr))
	b.WriteString("\n\n")

	// Footer.
	b.WriteString(footerStyle.Render("[a]dd [j/k]nav [?]help"))
	b.WriteString("\n")

	return b.String()
}

// estimateProgress returns a 0-100 estimate based on workspace age, status, and diff stats.
func estimateProgress(ws WorkspaceRow) int {
	if ws.ExitCode != nil {
		if *ws.ExitCode == 0 {
			return 100
		}
		return 0
	}

	// If the workspace has committed changes (pushed or merged), it's likely done.
	if ws.Pushed || ws.Merged {
		return 95
	}

	// Estimate based on activity duration — typical task takes ~15 min.
	age := ws.Age
	if age == "" || age == "-" {
		return 5
	}
	var minutes float64
	if strings.HasSuffix(age, "s") {
		fmt.Sscanf(age, "%f", &minutes)
		minutes = minutes / 60
	} else if strings.HasSuffix(age, "m") {
		fmt.Sscanf(age, "%f", &minutes)
	} else if strings.HasSuffix(age, "h") {
		var h float64
		fmt.Sscanf(age, "%f", &h)
		minutes = h * 60
	} else if strings.HasSuffix(age, "d") {
		return 95
	}

	// Asymptotic curve: approaches 95% over ~20 minutes.
	pct := int(95 * (1 - 1/(1+minutes/8)))

	// Boost estimate if there are actual code changes — agent is making progress.
	totalChanges := ws.Added + ws.Removed
	if totalChanges > 0 {
		// Having changes means at least 50% done; scale up with more changes.
		changeBoost := 50 + min(totalChanges, 45)
		if changeBoost > pct {
			pct = changeBoost
		}
	}

	if pct < 5 {
		pct = 5
	}
	if pct > 95 {
		pct = 95
	}
	return pct
}

// renderProgressBar renders a text progress bar with filled and empty segments.
func renderProgressBar(pct, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled
	return statusReady.Render(strings.Repeat("█", filled)) + dimStyle.Render(strings.Repeat("░", empty))
}

// statusIcon returns a compact status icon for narrow mode.
func statusIcon(status string, merged bool) string {
	if merged {
		return mergedStyle.Render("M")
	}
	switch status {
	case "active":
		return statusRunning.Render("A")
	case "ready":
		return statusReady.Render("R")
	case "blocked":
		return statusBlocked.Render("B")
	case "completed":
		return statusReady.Render("D")
	case "stale":
		return dimStyle.Render("S")
	default:
		return dimStyle.Render("?")
	}
}

// narrowStatus returns a compact status string for narrow mode.
func narrowStatus(ws WorkspaceRow) string {
	if ws.TaskStatus != "" {
		return ws.TaskStatus
	}
	if ws.Activity != "" && ws.Activity != "-" {
		return dimStyle.Render(ws.Activity)
	}
	return ""
}

func (m DashboardModel) renderWideDashboard() string {
	var b strings.Builder

	// Header.
	repoName := filepath.Base(m.repoRoot)
	if m.allRepos {
		repoName = "all repos"
	}
	title := fmt.Sprintf(" towr ── %s ── %d workspaces ", repoName, len(m.workspaces))
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if len(m.workspaces) == 0 {
		b.WriteString(dimStyle.Render("  No workspaces found."))
		b.WriteString("\n")
	} else {
		sep := dimStyle.Render("│")
		cols := []int{14, 10, 12, 8, 8, 6, 10, 10, 8, 5}

		// Column header.
		headers := []string{
			headerColStyle.Render("ID"),
			headerColStyle.Render("STATUS"),
			headerColStyle.Render("TASK"),
			headerColStyle.Render("HEALTH"),
			headerColStyle.Render("ACTIVITY"),
			headerColStyle.Render("DRIFT"),
			headerColStyle.Render("DIFF"),
			headerColStyle.Render("TREE"),
			headerColStyle.Render("AGENT"),
			headerColStyle.Render("AGE"),
		}
		b.WriteString("  ")
		for i, h := range headers {
			b.WriteString(padAnsi(h, cols[i]))
			if i < len(headers)-1 {
				b.WriteString(" " + sep + " ")
			}
		}
		b.WriteString("\n")

		// Separator line under header.
		b.WriteString("  ")
		for i, w := range cols {
			b.WriteString(dimStyle.Render(strings.Repeat("─", w)))
			if i < len(cols)-1 {
				b.WriteString(dimStyle.Render("─┼─"))
			}
		}
		b.WriteString("\n")

		// Workspace rows.
		for i, ws := range m.workspaces {
			prefix := "  "
			if i == m.cursor {
				prefix = "> "
			}

			diffStr := formatDiffStr(ws.Added, ws.Removed)
			treeStr := formatTreeStr(ws.Staged, ws.Unstaged, ws.Untracked)
			statusStr := colorStatus(ws.Status)
			if ws.Merged {
				statusStr = mergedStyle.Render("merged")
			}

			// Health column.
			healthStr := dimStyle.Render("—")
			if ws.Health == "pass" {
				healthStr = statusReady.Render("pass")
			} else if ws.Health == "fail" {
				healthStr = statusBlocked.Render("fail")
			}

			// Activity column.
			activityStr := ws.Activity
			if activityStr == "" || activityStr == "-" {
				activityStr = dimStyle.Render("—")
			}

			// Drift column.
			driftStr := dimStyle.Render("0")
			if ws.Drift > 0 && ws.Drift <= 5 {
				driftStr = statusRunning.Render(fmt.Sprintf("+%d", ws.Drift))
			} else if ws.Drift > 5 {
				driftStr = statusBlocked.Render(fmt.Sprintf("+%d", ws.Drift))
			}

			// Agent column.
			agentStr := dimStyle.Render("—")
			if ws.Agent != "" {
				agentStr = ws.Agent
			}

			// Task column.
			taskStr := dimStyle.Render("—")
			if ws.TaskStatus != "" {
				taskStr = ws.TaskStatus
			}

			cells := []string{
				ws.ID,
				statusStr,
				taskStr,
				healthStr,
				activityStr,
				driftStr,
				diffStr,
				treeStr,
				truncate(agentStr, 8),
				ws.Age,
			}

			b.WriteString(prefix)
			for j, cell := range cells {
				b.WriteString(padAnsi(cell, cols[j]))
				if j < len(cells)-1 {
					b.WriteString(" " + sep + " ")
				}
			}
			b.WriteString("\n")
		}
	}

	// Footer: selected workspace summary.
	b.WriteString("\n")
	if len(m.workspaces) > 0 && m.cursor < len(m.workspaces) {
		ws := m.workspaces[m.cursor]

		// Show repo or path.
		var location string
		if ws.RepoRoot != "" {
			location = filepath.Base(ws.RepoRoot)
		} else if ws.WorktreePath != "" {
			location = ws.WorktreePath
		}

		summary := fmt.Sprintf(" %s", ws.ID)
		if location != "" {
			summary += fmt.Sprintf(" @ %s", location)
		}
		if ws.WorktreePath != "" && ws.RepoRoot != "" {
			summary += fmt.Sprintf(" → %s", ws.WorktreePath)
		}
		if ws.ExitCode != nil {
			summary += fmt.Sprintf(" | exit %d", *ws.ExitCode)
		}
		summary += fmt.Sprintf(" | %s/-%s", diffAdded.Render(fmt.Sprintf("+%d", ws.Added)), diffRemoved.Render(fmt.Sprintf("%d", ws.Removed)))
		b.WriteString(footerStyle.Render(summary))
		b.WriteString("\n")
	}

	// Key help.
	help := " j/k nav  enter detail  s switch  l land  d diff  c cleanup  o editor  a all/repo  q quit"
	b.WriteString(footerStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

func (m DashboardModel) renderDetail() string {
	if m.cursor >= len(m.workspaces) {
		return "No workspace selected"
	}
	ws := m.workspaces[m.cursor]

	var b strings.Builder

	// Title.
	title := fmt.Sprintf(" %s ── %s ── detail ", ws.ID, ws.Branch)
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if len(m.detailFiles) == 0 && m.detailDiff == "" {
		b.WriteString(dimStyle.Render("  No changes found."))
		b.WriteString("\n")
	} else {
		// Calculate available height for content.
		contentHeight := m.height - 6 // header + footer
		if contentHeight < 10 {
			contentHeight = 10
		}

		// Split: file list (left 30%), diff (right 70%).
		fileListWidth := 30
		if m.width > 0 {
			fileListWidth = m.width * 3 / 10
			if fileListWidth < 20 {
				fileListWidth = 20
			}
		}

		// Render file list.
		var fileList strings.Builder
		fileList.WriteString(boldStyle.Render("Files"))
		fileList.WriteString("\n")
		for i, f := range m.detailFiles {
			style := normalFileStyle
			prefix := "  "
			if i == m.detailCursor {
				style = activeFileStyle
				prefix = "> "
			}
			line := prefix + style.Render(truncate(filepath.Base(f), fileListWidth-4))
			fileList.WriteString(line)
			fileList.WriteString("\n")
		}

		// Render diff panel (show diff for selected file if possible).
		var diffPanel strings.Builder
		diffPanel.WriteString(boldStyle.Render("Diff"))
		diffPanel.WriteString("\n")

		diffContent := m.detailDiff
		if m.detailCursor < len(m.detailFiles) {
			file := m.detailFiles[m.detailCursor]
			// Try committed branch diff first, then worktree diff.
			fileDiff, err := git.RunGit(ws.RepoRoot, "diff", ws.BaseBranch+"..."+ws.Branch, "--", file)
			if (err != nil || fileDiff == "") && ws.WorktreePath != "" {
				fileDiff, _ = git.RunGit(ws.WorktreePath, "diff", "--", file)
				if fileDiff == "" {
					fileDiff, _ = git.RunGit(ws.WorktreePath, "diff", "--cached", "--", file)
				}
			}
			if fileDiff != "" {
				diffContent = fileDiff
			}
		}

		// Limit diff lines to available height.
		diffLines := strings.Split(diffContent, "\n")
		maxLines := contentHeight - 2
		if maxLines < 5 {
			maxLines = 5
		}
		if len(diffLines) > maxLines {
			diffLines = diffLines[:maxLines]
			diffLines = append(diffLines, dimStyle.Render("... (press d for full diff)"))
		}
		for _, line := range diffLines {
			diffPanel.WriteString(line)
			diffPanel.WriteString("\n")
		}

		// Join panels side by side.
		left := fileListStyle.Width(fileListWidth).Render(fileList.String())
		right := diffPanelStyle.Render(diffPanel.String())
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	}

	b.WriteString("\n")
	help := " j/k files  d full diff  o editor  esc back  q quit"
	b.WriteString(footerStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

func formatTreeStr(staged, unstaged, untracked int) string {
	if staged == 0 && unstaged == 0 && untracked == 0 {
		return dimStyle.Render("clean")
	}
	var parts []string
	if unstaged > 0 {
		parts = append(parts, statusRunning.Render(fmt.Sprintf("~%d", unstaged)))
	}
	if staged > 0 {
		parts = append(parts, diffAdded.Render(fmt.Sprintf("+%d", staged)))
	}
	if untracked > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("?%d", untracked)))
	}
	return strings.Join(parts, " ")
}

func formatDiffStr(added, removed int) string {
	if added == 0 && removed == 0 {
		return dimStyle.Render("-")
	}
	return diffAdded.Render(fmt.Sprintf("+%d", added)) + "/" + diffRemoved.Render(fmt.Sprintf("-%d", removed))
}

// padAnsi right-pads a string to width, accounting for invisible ANSI escape codes.
func padAnsi(s string, width int) string {
	visible := stripAnsi(s)
	if len(visible) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(visible))
}

// stripAnsi removes ANSI escape sequences to compute visible length.
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// shortenPath returns a display-friendly short path.
// $HOME → "~", $HOME/foo → "~/foo", otherwise basename.
func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Base(p)
	}
	if p == home {
		return "~"
	}
	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(p)
	}
	return "~/" + rel
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
