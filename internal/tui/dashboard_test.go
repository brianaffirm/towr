package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewDashboardModel(t *testing.T) {
	m := NewDashboardModel("/tmp/repo", "/tmp/state.db", "/tmp/repos", false)
	if m.repoRoot != "/tmp/repo" {
		t.Errorf("repoRoot = %q, want /tmp/repo", m.repoRoot)
	}
	if m.repoStorePath != "/tmp/state.db" {
		t.Errorf("repoStorePath = %q, want /tmp/state.db", m.repoStorePath)
	}
	if m.allRepos {
		t.Error("allRepos should be false")
	}
	if m.view != viewDashboard {
		t.Error("initial view should be dashboard")
	}
}

func TestNewDashboardModelAllRepos(t *testing.T) {
	m := NewDashboardModel("", "", "/tmp/repos", true)
	if !m.allRepos {
		t.Error("allRepos should be true")
	}
}

func TestDashboardNavigationKeys(t *testing.T) {
	m := DashboardModel{
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "READY", Branch: "towr/auth"},
			{ID: "billing", Status: "RUNNING", Branch: "towr/billing"},
			{ID: "api", Status: "BLOCKED", Branch: "towr/api"},
		},
		cursor: 0,
		view:   viewDashboard,
	}

	// Move down.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(DashboardModel)
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}

	// Move down again.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(DashboardModel)
	if m.cursor != 2 {
		t.Errorf("after j: cursor = %d, want 2", m.cursor)
	}

	// Move down at bottom — should stay.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(DashboardModel)
	if m.cursor != 2 {
		t.Errorf("after j at bottom: cursor = %d, want 2", m.cursor)
	}

	// Move up.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(DashboardModel)
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}

	// Move up again.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(DashboardModel)
	if m.cursor != 0 {
		t.Errorf("after k: cursor = %d, want 0", m.cursor)
	}

	// Move up at top — should stay.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(DashboardModel)
	if m.cursor != 0 {
		t.Errorf("after k at top: cursor = %d, want 0", m.cursor)
	}
}

func TestEnterDetailView(t *testing.T) {
	m := DashboardModel{
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "READY", Branch: "towr/auth"},
		},
		cursor: 0,
		view:   viewDashboard,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(DashboardModel)
	if m.view != viewDetail {
		t.Errorf("after enter: view = %d, want viewDetail(%d)", m.view, viewDetail)
	}
}

func TestEscBackToDashboard(t *testing.T) {
	m := DashboardModel{
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "READY", Branch: "towr/auth"},
		},
		cursor: 0,
		view:   viewDetail,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(DashboardModel)
	if m.view != viewDashboard {
		t.Errorf("after esc: view = %d, want viewDashboard(%d)", m.view, viewDashboard)
	}
}

func TestDetailFileNavigation(t *testing.T) {
	m := DashboardModel{
		workspaces:   []WorkspaceRow{{ID: "auth"}},
		cursor:       0,
		view:         viewDetail,
		detailFiles:  []string{"a.go", "b.go", "c.go"},
		detailCursor: 0,
	}

	// Move down in file list.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(DashboardModel)
	if m.detailCursor != 1 {
		t.Errorf("after j: detailCursor = %d, want 1", m.detailCursor)
	}

	// Move up.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(DashboardModel)
	if m.detailCursor != 0 {
		t.Errorf("after k: detailCursor = %d, want 0", m.detailCursor)
	}
}

func TestWindowSizeMsg(t *testing.T) {
	m := DashboardModel{}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(DashboardModel)
	if m.width != 120 || m.height != 40 {
		t.Errorf("window size = %dx%d, want 120x40", m.width, m.height)
	}
}

func TestWorkspacesMsgUpdatesList(t *testing.T) {
	m := DashboardModel{cursor: 5}
	rows := []WorkspaceRow{
		{ID: "auth", Status: "READY"},
		{ID: "billing", Status: "RUNNING"},
	}
	updated, _ := m.Update(workspacesMsg(rows))
	m = updated.(DashboardModel)
	if len(m.workspaces) != 2 {
		t.Errorf("workspaces count = %d, want 2", len(m.workspaces))
	}
	// Cursor should clamp to valid range.
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (clamped)", m.cursor)
	}
}

func TestDetailMsgSetsState(t *testing.T) {
	m := DashboardModel{}
	updated, _ := m.Update(detailMsg{
		files: []string{"a.go", "b.go"},
		diff:  "+added line",
	})
	m = updated.(DashboardModel)
	if len(m.detailFiles) != 2 {
		t.Errorf("detailFiles count = %d, want 2", len(m.detailFiles))
	}
	if m.detailDiff != "+added line" {
		t.Errorf("detailDiff = %q, want '+added line'", m.detailDiff)
	}
	if m.detailCursor != 0 {
		t.Errorf("detailCursor = %d, want 0", m.detailCursor)
	}
}

func TestRenderDashboardEmpty(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
	}
	output := m.View()
	if output == "" {
		t.Error("expected non-empty output for empty dashboard")
	}
}

func TestRenderDashboardWithWorkspaces(t *testing.T) {
	exitCode := 0
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "READY", Branch: "towr/auth", Added: 10, Removed: 3, Age: "5m", ExitCode: &exitCode},
			{ID: "billing", Status: "RUNNING", Branch: "towr/billing", Added: 5, Removed: 1, Age: "10m"},
		},
		cursor: 0,
		view:   viewDashboard,
	}
	output := m.View()
	if output == "" {
		t.Error("expected non-empty output")
	}
	// Should contain workspace IDs.
	if !containsStr(output, "auth") {
		t.Error("output should contain 'auth'")
	}
	if !containsStr(output, "billing") {
		t.Error("output should contain 'billing'")
	}
}

func TestRenderDetailView(t *testing.T) {
	m := DashboardModel{
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "READY", Branch: "towr/auth"},
		},
		cursor:      0,
		view:        viewDetail,
		detailFiles: []string{"src/main.go", "src/util.go"},
		detailDiff:  "+new line\n-old line",
		width:       100,
		height:      30,
	}
	output := m.View()
	if output == "" {
		t.Error("expected non-empty detail view output")
	}
}

func TestFormatDiffStr(t *testing.T) {
	tests := []struct {
		added, removed int
		wantEmpty      bool
	}{
		{0, 0, true},
		{10, 3, false},
		{0, 5, false},
		{100, 0, false},
	}
	for _, tt := range tests {
		result := formatDiffStr(tt.added, tt.removed)
		if tt.wantEmpty && result == "" {
			t.Errorf("formatDiffStr(%d, %d) should not be empty string", tt.added, tt.removed)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 2, "ab"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestParseShortstat(t *testing.T) {
	tests := []struct {
		input       string
		wantAdded   int
		wantRemoved int
	}{
		{"3 files changed, 10 insertions(+), 2 deletions(-)", 10, 2},
		{"1 file changed, 5 insertions(+)", 5, 0},
		{"1 file changed, 3 deletions(-)", 0, 3},
		{"", 0, 0},
	}
	for _, tt := range tests {
		added, removed := parseShortstat(tt.input)
		if added != tt.wantAdded || removed != tt.wantRemoved {
			t.Errorf("parseShortstat(%q) = (%d, %d), want (%d, %d)", tt.input, added, removed, tt.wantAdded, tt.wantRemoved)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "-"},
		{"invalid", "-"},
	}
	for _, tt := range tests {
		got := formatAge(tt.input)
		if got != tt.want {
			t.Errorf("formatAge(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestColorStatus(t *testing.T) {
	statuses := []string{"READY", "RUNNING", "VALIDATING", "BLOCKED", "ORPHANED", "LANDED", "ARCHIVED", "UNKNOWN"}
	for _, s := range statuses {
		result := colorStatus(s)
		if result == "" {
			t.Errorf("colorStatus(%q) returned empty string", s)
		}
	}
}

func TestNarrowThreshold(t *testing.T) {
	if narrowThreshold != 60 {
		t.Errorf("narrowThreshold = %d, want 60", narrowThreshold)
	}
}

func TestRenderNarrowDashboard(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "active", Branch: "towr/auth", Added: 10, Removed: 3, Agent: "claude-code"},
			{ID: "billing", Status: "ready", Branch: "towr/billing", Added: 5, Removed: 1},
		},
		cursor: 0,
		view:   viewDashboard,
		width:  35, // narrow mode
		height: 20,
	}
	output := m.View()
	if output == "" {
		t.Error("expected non-empty narrow dashboard output")
	}
	// Should contain workspace IDs.
	if !containsStr(output, "auth") {
		t.Error("narrow output should contain 'auth'")
	}
	if !containsStr(output, "billing") {
		t.Error("narrow output should contain 'billing'")
	}
	// Should NOT contain full column headers (narrow mode is compact).
	if containsStr(output, "ACTIVITY") {
		t.Error("narrow output should not contain full column headers like ACTIVITY")
	}
}

func TestRenderNarrowDashboardEmpty(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		width:    35,
		height:   20,
	}
	output := m.View()
	if !containsStr(output, "(no agents)") {
		t.Error("narrow empty dashboard should show '(no agents)'")
	}
}

func TestEstimateProgress(t *testing.T) {
	// Completed workspace (exit 0) -> 100%.
	exitZero := 0
	ws := WorkspaceRow{ExitCode: &exitZero, Age: "5m"}
	if got := estimateProgress(ws); got != 100 {
		t.Errorf("estimateProgress(exit 0) = %d, want 100", got)
	}

	// Failed workspace (exit 1) -> 0%.
	exitOne := 1
	ws2 := WorkspaceRow{ExitCode: &exitOne, Age: "5m"}
	if got := estimateProgress(ws2); got != 0 {
		t.Errorf("estimateProgress(exit 1) = %d, want 0", got)
	}

	// No age -> 5%.
	ws3 := WorkspaceRow{Age: ""}
	if got := estimateProgress(ws3); got != 5 {
		t.Errorf("estimateProgress(no age) = %d, want 5", got)
	}

	// 10m age -> should be between 5 and 95.
	ws4 := WorkspaceRow{Age: "10m"}
	if got := estimateProgress(ws4); got < 5 || got > 95 {
		t.Errorf("estimateProgress(10m) = %d, want between 5 and 95", got)
	}

	// Day-old -> 95%.
	ws5 := WorkspaceRow{Age: "2d"}
	if got := estimateProgress(ws5); got != 95 {
		t.Errorf("estimateProgress(2d) = %d, want 95", got)
	}

	// With code changes -> boosted to at least 50.
	ws6 := WorkspaceRow{Age: "1m", Added: 20, Removed: 5}
	if got := estimateProgress(ws6); got < 50 {
		t.Errorf("estimateProgress(1m, +20/-5) = %d, want >= 50", got)
	}

	// Pushed -> 95%.
	ws7 := WorkspaceRow{Age: "2m", Pushed: true}
	if got := estimateProgress(ws7); got != 95 {
		t.Errorf("estimateProgress(pushed) = %d, want 95", got)
	}
}

func TestRenderProgressBar(t *testing.T) {
	// 50% with width 10 -> 5 filled, 5 empty (10 runes total).
	bar := stripAnsi(renderProgressBar(50, 10))
	runeCount := len([]rune(bar))
	if runeCount != 10 {
		t.Errorf("progress bar rune count = %d, want 10", runeCount)
	}

	// 100% -> all filled.
	bar100 := stripAnsi(renderProgressBar(100, 10))
	runeCount100 := len([]rune(bar100))
	if runeCount100 != 10 {
		t.Errorf("progress bar 100%% rune count = %d, want 10", runeCount100)
	}

	// 0% -> all empty.
	bar0 := stripAnsi(renderProgressBar(0, 10))
	runeCount0 := len([]rune(bar0))
	if runeCount0 != 10 {
		t.Errorf("progress bar 0%% rune count = %d, want 10", runeCount0)
	}
}

func TestContextMsgUpdate(t *testing.T) {
	m := DashboardModel{}
	updated, _ := m.Update(contextMsg{planName: "my-plan", cost: "1.50", elapsed: "12m"})
	m = updated.(DashboardModel)
	if m.planName != "my-plan" {
		t.Errorf("planName = %q, want 'my-plan'", m.planName)
	}
	if m.runCost != "1.50" {
		t.Errorf("runCost = %q, want '1.50'", m.runCost)
	}
	if m.runElapsed != "12m" {
		t.Errorf("runElapsed = %q, want '12m'", m.runElapsed)
	}
}

func TestNarrowDashboardWithPlanName(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "active", Branch: "towr/auth", Added: 10, Removed: 3, Agent: "claude", Age: "5m"},
		},
		cursor:   0,
		view:     viewDashboard,
		width:    40,
		height:   30,
		planName: "refactor-auth",
	}
	output := m.View()
	if !containsStr(output, "refactor-auth") {
		t.Error("narrow dashboard with plan should contain plan name")
	}
	if !containsStr(output, "TOWR") {
		t.Error("narrow dashboard should contain TOWR header")
	}
}

func TestNarrowDashboardProgressAndStats(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "active", Branch: "towr/auth", Added: 176, Removed: 38, Agent: "claude", Age: "16m"},
			{ID: "api-tests", Status: "blocked", Branch: "towr/api", Added: 127, Removed: 12, Agent: "codex", Age: "12m"},
		},
		cursor:     0,
		view:       viewDashboard,
		width:      40,
		height:     30,
		runCost:    "1.65",
		runElapsed: "18m",
	}
	output := m.View()
	// Should show agent count.
	if !containsStr(output, "2 agents") {
		t.Error("narrow dashboard should show agent count")
	}
	// Should show blocked count.
	if !containsStr(output, "1 blocked") {
		t.Error("narrow dashboard should show blocked count")
	}
}

func TestRenderWideDashboardAboveThreshold(t *testing.T) {
	m := DashboardModel{
		repoRoot: "/tmp/myrepo",
		workspaces: []WorkspaceRow{
			{ID: "auth", Status: "active", Branch: "towr/auth"},
		},
		cursor: 0,
		view:   viewDashboard,
		width:  120, // wide mode
		height: 30,
	}
	output := m.View()
	// Wide mode should have column headers.
	if !containsStr(output, "STATUS") {
		t.Error("wide output should contain column header 'STATUS'")
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		merged bool
		want   string
	}{
		{"active", false, "A"},
		{"ready", false, "R"},
		{"blocked", false, "B"},
		{"completed", false, "D"},
		{"stale", false, "S"},
		{"unknown", false, "?"},
		{"active", true, "M"},
	}
	for _, tt := range tests {
		got := stripAnsi(statusIcon(tt.status, tt.merged))
		if got != tt.want {
			t.Errorf("statusIcon(%q, %v) = %q, want %q", tt.status, tt.merged, got, tt.want)
		}
	}
}

func TestNarrowStatus(t *testing.T) {
	// With task status.
	ws := WorkspaceRow{TaskStatus: "d-0001 ▶", Activity: "5m"}
	got := narrowStatus(ws)
	if got != "d-0001 ▶" {
		t.Errorf("narrowStatus with task = %q, want 'd-0001 ▶'", got)
	}

	// Without task status, with activity.
	ws2 := WorkspaceRow{Activity: "5m"}
	got2 := stripAnsi(narrowStatus(ws2))
	if got2 != "5m" {
		t.Errorf("narrowStatus with activity = %q, want '5m'", got2)
	}

	// No status or activity.
	ws3 := WorkspaceRow{}
	got3 := narrowStatus(ws3)
	if got3 != "" {
		t.Errorf("narrowStatus empty = %q, want empty", got3)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
