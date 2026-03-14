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

	// Focus next/prev pane with dynamic resizing.
	// Shell one-liner: select pane, then resize focused to 60%, control to 20%.
	// Only resizes when 3+ panes exist (control + 2 others).
	focusScript := `tmux select-pane -t :.%s; ` +
		`total=$(tmux list-panes | wc -l | tr -d " "); ` +
		`if [ "$total" -gt 2 ]; then ` +
		`  w=$(tmux display -p "#{window_width}"); ` +
		`  p=$(tmux display -p "#{pane_index}"); ` +
		`  cw=$((w*20/100)); [ "$cw" -lt 30 ] && cw=30; ` +
		`  if [ "$p" = "0" ]; then ` +
		`    tmux resize-pane -t :.0 -x "$cw"; ` +
		`  else ` +
		`    tmux resize-pane -x $((w*60/100)); ` +
		`    tmux resize-pane -t :.0 -x "$cw"; ` +
		`  fi; ` +
		`fi`

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

	// New shell pane.
	cmds = append(cmds, TmuxCmd{Args: []string{
		"bind", "t", "if-shell", guard, "split-window -h -c " + cfg.WorkDir, "clock-mode",
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

	return cmds
}

// BuildStatusBarCommands returns tmux commands to configure the status bar.
func BuildStatusBarCommands(cfg MuxConfig, paneCount, runningCount int, focusName string) []TmuxCmd {
	session := cfg.SessionName

	left := fmt.Sprintf(" TOWR │ %d panes │ %d running │ focus: %s", paneCount, runningCount, focusName)
	right := " Ctrl-a ? help "

	return []TmuxCmd{
		{Args: []string{"set", "-t", session, "status-left-length", "80"}},
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
func AddPane(session, cwd string) (MuxPaneInfo, error) {
	// Split horizontally from the last pane in the mux window.
	cmd := exec.Command("tmux", "split-window", "-t", session+":mux", "-h",
		"-c", cwd, "-P", "-F", "#{pane_id}\t#{pane_index}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return MuxPaneInfo{}, fmt.Errorf("tmux split-window: %s: %w", strings.TrimSpace(string(out)), err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	info := MuxPaneInfo{PaneID: parts[0]}
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &info.Index)
	}
	return info, nil
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

	// Count running — pane_count minus control (pane 0).
	running := paneCount - 1
	if running < 0 {
		running = 0
	}

	cmds := BuildStatusBarCommands(MuxConfig{SessionName: session}, paneCount, running, focusName)
	return RunTmuxCmds(cmds)
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
