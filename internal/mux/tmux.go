package mux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// MuxConfig holds configuration for the multiplexer tmux session.
type MuxConfig struct {
	SessionName string // tmux session name (default: "towr-mux")
	Shell       string // shell for master pane (default: $SHELL)
	WorkDir     string // working directory
	LeaderKey   string // tmux prefix key (default: "C-a")
	TowrBin     string // path to towr binary (for control pane command)
}

// TmuxCmd represents a tmux command to execute.
type TmuxCmd struct {
	Args []string
}

// BuildCreateCommands returns the tmux commands to create the mux layout.
// Layout: control pane (left 20%) | master pane (right 80%).
// Additional agent panes are added later via AddPane.
func BuildCreateCommands(cfg MuxConfig) []TmuxCmd {
	session := cfg.SessionName
	shell := cfg.Shell

	var cmds []TmuxCmd

	// Create session with master pane (takes full window initially).
	cmds = append(cmds, TmuxCmd{Args: []string{
		"new-session", "-d", "-s", session, "-c", cfg.WorkDir, "-x", "200", "-y", "50", shell,
	}})

	// Rename master pane's window.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"rename-window", "-t", session + ":0", "mux",
	}})

	// Split: control pane on the left (20%), master stays right (80%).
	// -hb = horizontal split, -l = size as percentage, -b = new pane goes before (left).
	controlCmd := cfg.TowrBin + " tui"
	if cfg.TowrBin == "" {
		controlCmd = "towr tui"
	}
	cmds = append(cmds, TmuxCmd{Args: []string{
		"split-window", "-t", session + ":mux", "-hb", "-l", "20%", "-c", cfg.WorkDir, controlCmd,
	}})

	// Title the control pane.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"select-pane", "-t", session + ":mux.0", "-T", "towr",
	}})
	// Title the master pane.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"select-pane", "-t", session + ":mux.1", "-T", "master",
	}})

	// Select the master pane (right, pane index 1 after the split).
	cmds = append(cmds, TmuxCmd{Args: []string{
		"select-pane", "-t", session + ":mux.1",
	}})

	return cmds
}

// BuildKeybindingCommands returns tmux commands to configure leader key and bindings.
func BuildKeybindingCommands(cfg MuxConfig) []TmuxCmd {
	session := cfg.SessionName
	prefix := cfg.LeaderKey

	var cmds []TmuxCmd

	// Set prefix key (session-scoped).
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "prefix", prefix,
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "prefix2", "None",
	}})

	// Enable mouse support.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "mouse", "on",
	}})

	// Status bar.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "status", "on",
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "status-style", "bg=colour57,fg=white",
	}})

	// Key bindings — guarded with if-shell so they only fire inside the
	// towr-mux session. tmux bind-key is global (no session-scope flag),
	// so without the guard these would affect unrelated tmux sessions.
	guard := fmt.Sprintf(`[ "$(tmux display -p '#S')" = "%s" ]`, session)

	// Layout rebalance script — ensures control=20%, focused=55%, sidebar stacked.
	// Used after focus change, pane add, and pane close.
	layoutScript := `w=$(tmux display -p "#{window_width}"); ` +
		`cw=$((w*20/100)); [ "$cw" -lt 30 ] && cw=30; ` +
		`tmux resize-pane -t :.0 -x "$cw"; ` +
		`p=$(tmux display -p "#{pane_index}"); ` +
		`if [ "$p" != "0" ]; then ` +
		`  tmux resize-pane -x $((w*55/100)); ` +
		`fi`

	// Focus next/prev pane with layout rebalance.
	focusScript := `tmux select-pane -t :.%s; ` + layoutScript

	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "Right", "if-shell", guard, fmt.Sprintf("run-shell '%s'", fmt.Sprintf(focusScript, "+")), "select-pane -t :.+",
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "Left", "if-shell", guard, fmt.Sprintf("run-shell '%s'", fmt.Sprintf(focusScript, "-")), "select-pane -t .:.-",
	}})

	// Zoom toggle.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "Enter", "if-shell", guard, "resize-pane -Z", "",
	}})

	// New shell pane — splits last pane vertically (stacks in sidebar), then rebalances.
	addPaneScript := fmt.Sprintf(
		`last=$(tmux list-panes -F "#{pane_id}" | tail -1); `+
			`tmux split-window -t "$last" -v -c %s; %s`,
		cfg.WorkDir, layoutScript)
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "t", "if-shell", guard, fmt.Sprintf("run-shell '%s'", addPaneScript), "clock-mode",
	}})

	// Session/window picker (matches tmux Ctrl-b w convention).
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "w", "if-shell", guard, "choose-tree -s", "choose-window",
	}})

	// Close pane (matches tmux Ctrl-b x convention).
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "x", "if-shell", guard, "kill-pane", "confirm-before kill-pane",
	}})

	// Quit all.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "q", "if-shell", guard, "kill-session", "display-panes",
	}})

	// Pane border format — shows pane title on top border with color.
	// pane_title is set by SetPaneTitle to "name │ agent model │ +N/-N".
	// Use tmux format conditionals to colorize the active pane differently.
	borderFmt := " #[fg=#484f58]━━#[default] " +
		"#{?#{pane_active},#[fg=#58a6ff bold],#[fg=#8b949e]}#{pane_title}" +
		"#[default] #[fg=#484f58]━━#[default] "
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "pane-border-format", borderFmt,
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "pane-border-status", "top",
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "pane-border-style", "fg=#484f58",
	}})
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "pane-active-border-style", "fg=#58a6ff",
	}})
	// Distinct line characters for pane borders.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"set", "-t", session, "pane-border-lines", "heavy",
	}})

	return cmds
}

// StatusBarData holds dynamic information for the tmux status bar.
type StatusBarData struct {
	PlanName       string
	PaneCount      int
	RunningCount   int
	CompletedCount int
	Cost           float64
	ElapsedMin     int
	FocusName      string
}

// BuildStatusBarCommands returns tmux commands to configure the status bar.
func BuildStatusBarCommands(cfg MuxConfig, data StatusBarData) []TmuxCmd {
	session := cfg.SessionName

	// Build left status with tmux styling.
	left := " #[fg=white,bg=colour57,bold] TOWR #[default]"
	if data.PlanName != "" {
		left += fmt.Sprintf(" #[fg=colour6]%s#[default] ", data.PlanName)
	}
	left += fmt.Sprintf(" #[fg=colour2]%d/%d agents ▶#[default]", data.RunningCount, data.PaneCount)
	if data.Cost > 0 {
		left += fmt.Sprintf("  #[fg=colour3]$%.2f#[default]", data.Cost)
	}
	if data.ElapsedMin > 0 {
		left += fmt.Sprintf("  #[fg=colour8]elapsed %dm#[default]", data.ElapsedMin)
	}

	right := " Ctrl-a ? help "

	return []TmuxCmd{
		{Args: []string{"set", "-t", session, "status-left-length", "120"}},
		{Args: []string{"set", "-t", session, "status-left", left}},
		{Args: []string{"set", "-t", session, "status-right", right}},
	}
}

// RunTmuxCmds executes a list of tmux commands.
func RunTmuxCmds(cmds []TmuxCmd) error {
	for _, c := range cmds {
		cmd := exec.Command("tmux", c.Args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("tmux %s: %s: %w", c.Args[0], strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// SessionExists checks if the mux tmux session already exists.
func SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// DefaultSessionName is the standard mux session name.
const DefaultSessionName = "towr-mux"

// MuxPaneInfo describes a pane created inside the mux window.
type MuxPaneInfo struct {
	PaneID string // tmux pane ID (e.g., "%5") — globally unique
	Index  int    // pane index within the mux window
}

// AddPane creates a new pane inside the mux window via split-window.
// Returns the tmux pane ID for the new pane. The pane runs a shell in cwd.
//
// Layout strategy:
//   - 2 panes (control + master): first agent REUSES master pane (pane 1) as the hero
//   - 3 panes (control + hero + 1): second agent splits hero horizontally → sidebar column
//   - 4+ panes: subsequent agents split the last sidebar pane vertically (stacked)
//
// After adding, applies focus layout so the focused pane stays large.
func AddPane(session, cwd string) (MuxPaneInfo, error) {
	paneCount := CountMuxPanes(session)
	return addPaneSplit(session, cwd, paneCount)
}

// addPaneSplit creates a new pane via split-window.
func addPaneSplit(session, cwd string, paneCount int) (MuxPaneInfo, error) {
	var splitArgs []string
	if paneCount <= 2 {
		// First agent: split master horizontally to create the hero pane.
		splitArgs = []string{"split-window", "-t", session + ":mux.1", "-h",
			"-c", cwd, "-P", "-F", "#{pane_id}\t#{pane_index}"}
	} else {
		// Subsequent: split the last pane vertically to stack in sidebar.
		lastPane := findLastPane(session)
		if lastPane == "" {
			lastPane = session + ":mux"
		}
		splitArgs = []string{"split-window", "-t", lastPane, "-v",
			"-c", cwd, "-P", "-F", "#{pane_id}\t#{pane_index}"}
	}

	cmd := exec.Command("tmux", splitArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return MuxPaneInfo{}, fmt.Errorf("tmux split-window: %s: %w", strings.TrimSpace(string(out)), err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	info := MuxPaneInfo{PaneID: parts[0]}
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &info.Index)
	}

	// Apply focus layout.
	applyMuxLayout(session, paneCount+1)

	return info, nil
}

// paneTitle returns the current title of a tmux pane.
func paneTitle(paneID string) string {
	cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_title}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// getPaneID returns the tmux pane ID for a given pane index in the mux window.
func getPaneID(session string, index int) string {
	cmd := exec.Command("tmux", "list-panes", "-t", session+":mux",
		"-F", "#{pane_id}\t#{pane_index}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			var idx int
			fmt.Sscanf(parts[1], "%d", &idx)
			if idx == index {
				return parts[0]
			}
		}
	}
	return ""
}

// findLastPane returns the pane ID of the highest-index pane in the mux window.
func findLastPane(session string) string {
	cmd := exec.Command("tmux", "list-panes", "-t", session+":mux",
		"-F", "#{pane_id}\t#{pane_index}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var lastPaneID string
	var maxIdx int
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		var idx int
		fmt.Sscanf(parts[1], "%d", &idx)
		if idx >= maxIdx {
			maxIdx = idx
			lastPaneID = parts[0]
		}
	}
	return lastPaneID
}

// applyMuxLayout resizes panes to the mission control layout.
//
// With agents present (4+ panes):
//
//	pane 0 (control/TUI) = 18%
//	pane 1 (master/log)  = minimal (~25 cols, just enough for status)
//	pane 2 (hero agent)  = ~50%
//	pane 3+ (sidebar)    = remaining, stacked vertically
//
// Without agents (2-3 panes): control 20% | master 80%.
func applyMuxLayout(session string, totalPanes int) {
	if totalPanes <= 2 {
		return
	}

	// Get terminal width.
	widthCmd := exec.Command("tmux", "display-message", "-t", session+":mux", "-p", "#{window_width}")
	out, err := widthCmd.CombinedOutput()
	if err != nil {
		return
	}
	var termW int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &termW)
	if termW < 80 {
		return
	}

	controlW := termW * 18 / 100
	if controlW < 28 {
		controlW = 28
	}

	// Resize control pane.
	_ = exec.Command("tmux", "resize-pane", "-t", session+":mux.0", "-x", fmt.Sprintf("%d", controlW)).Run()

	if totalPanes <= 3 {
		// No agents yet — master gets remaining space.
		return
	}

	// Agents present: shrink master to minimal log strip, hero gets the space.
	masterW := 30
	_ = exec.Command("tmux", "resize-pane", "-t", session+":mux.1", "-x", fmt.Sprintf("%d", masterW)).Run()

	// Hero agent (pane 2) gets ~50% of terminal.
	heroW := termW * 50 / 100
	_ = exec.Command("tmux", "resize-pane", "-t", session+":mux.2", "-x", fmt.Sprintf("%d", heroW)).Run()
}

// RemovePane kills a pane in the mux window by its tmux pane ID.
func RemovePane(paneID string) error {
	cmd := exec.Command("tmux", "kill-pane", "-t", paneID)
	_ = cmd.Run() // best-effort
	return nil
}

// CountMuxPanes returns the number of panes in the mux window.
func CountMuxPanes(session string) int {
	cmd := exec.Command("tmux", "list-panes", "-t", session+":mux", "-F", "#{pane_id}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, l := range lines {
		if l != "" {
			count++
		}
	}
	return count
}

// UpdateStatusBar queries current mux state and updates the tmux status bar.
func UpdateStatusBar(session string) error {
	paneCount := CountMuxPanes(session)

	// Get focused pane title.
	cmd := exec.Command("tmux", "display-message", "-t", session+":mux", "-p", "#{pane_title}")
	out, _ := cmd.CombinedOutput()
	focusName := strings.TrimSpace(string(out))
	if focusName == "" {
		focusName = "shell"
	}

	// Agent count = total panes minus control (pane 0) and master (pane 1).
	agentCount := paneCount - 2
	if agentCount < 0 {
		agentCount = 0
	}

	// Read enhanced data from session env if available.
	planName := GetSessionEnv(session, "TOWR_PLAN")
	var costVal float64
	if cs := GetSessionEnv(session, "TOWR_COST"); cs != "" {
		fmt.Sscanf(cs, "%f", &costVal)
	}
	var elapsed int
	if es := GetSessionEnv(session, "TOWR_ELAPSED"); es != "" {
		fmt.Sscanf(es, "%d", &elapsed)
	}
	var completed int
	if cs := GetSessionEnv(session, "TOWR_COMPLETED"); cs != "" {
		fmt.Sscanf(cs, "%d", &completed)
	}

	data := StatusBarData{
		PlanName:       planName,
		PaneCount:      agentCount,
		RunningCount:   agentCount,
		CompletedCount: completed,
		Cost:           costVal,
		ElapsedMin:     elapsed,
		FocusName:      focusName,
	}

	cmds := BuildStatusBarCommands(MuxConfig{SessionName: session}, data)
	return RunTmuxCmds(cmds)
}

// SetPaneTitle sets the title of a tmux pane (shown in pane-border-format).
func SetPaneTitle(paneID, title string) error {
	cmd := exec.Command("tmux", "select-pane", "-t", paneID, "-T", title)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("set pane title: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// SetSessionEnv sets a tmux environment variable on the session.
func SetSessionEnv(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", session, key, value)
	_, err := cmd.CombinedOutput()
	return err
}

// GetSessionEnv reads a tmux environment variable from the session.
func GetSessionEnv(session, key string) string {
	cmd := exec.Command("tmux", "show-environment", "-t", session, key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	// Output is "KEY=VALUE\n"
	s := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(s, '='); idx >= 0 {
		return s[idx+1:]
	}
	return ""
}

// AttachSession attaches to an existing mux session.
func AttachSession(name string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", name)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
