package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianaffirm/towr/internal/mux"
)

// TmuxBackend implements Backend using tmux.
// Each workspace gets its own tmux session named Prefix/<id>,
// unless a towr-mux session is active — then panes are created
// inside the mux window instead.
type TmuxBackend struct {
	Prefix string // session name prefix, e.g., "towr"

	// MuxSession is the tmux session name to check for mux integration.
	// Defaults to mux.DefaultSessionName ("towr-mux").
	// Set to "" to disable mux detection (useful in tests).
	MuxSession string

	// muxPanes maps workspace ID → tmux pane ID (e.g., "%5") for panes
	// created inside the mux window. Protected by mu.
	mu       sync.Mutex
	muxPanes map[string]string
}

// muxPanesPath returns the path to the mux pane mapping file.
func muxPanesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".towr", "mux-panes.json")
}

// NewTmuxBackend creates a tmux backend with the given prefix.
// Sessions are created as prefix/<workspace-id>.
// If a mux session is active, loads persisted pane mappings.
func NewTmuxBackend(prefix string) *TmuxBackend {
	if prefix == "" {
		prefix = "towr"
	}
	b := &TmuxBackend{
		Prefix:     prefix,
		MuxSession: mux.DefaultSessionName,
		muxPanes:   make(map[string]string),
	}
	b.loadMuxPanes()
	return b
}

// loadMuxPanes restores mux pane mappings from disk, discarding any
// entries whose tmux pane ID no longer exists.
func (t *TmuxBackend) loadMuxPanes() {
	if t.MuxSession == "" || !mux.SessionExists(t.MuxSession) {
		return
	}
	data, err := os.ReadFile(muxPanesPath())
	if err != nil {
		return
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	// Validate each pane still exists in tmux.
	for id, paneID := range saved {
		cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "ok")
		if cmd.Run() == nil {
			t.muxPanes[id] = paneID
		}
	}
}

// saveMuxPanes persists the current mux pane mapping to disk.
func (t *TmuxBackend) saveMuxPanes() {
	t.mu.Lock()
	snapshot := make(map[string]string, len(t.muxPanes))
	for k, v := range t.muxPanes {
		snapshot[k] = v
	}
	t.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	dir := filepath.Dir(muxPanesPath())
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(muxPanesPath(), data, 0644)
}

// sessionName returns the tmux session name for a workspace.
func (t *TmuxBackend) sessionName(id string) string {
	return t.Prefix + "/" + id
}

// isMuxPane returns true if the workspace has a pane inside the mux window.
func (t *TmuxBackend) isMuxPane(id string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	paneID, ok := t.muxPanes[id]
	return paneID, ok
}

// targetFor returns the tmux target for a workspace.
// If the workspace has a mux pane, returns the pane ID.
// Otherwise returns the traditional session:chat target.
func (t *TmuxBackend) targetFor(id string) string {
	if paneID, ok := t.isMuxPane(id); ok {
		return paneID
	}
	return t.sessionName(id) + ":chat"
}

// CreatePane creates a new tmux pane for the workspace.
// If a towr-mux session is active, creates a pane inside the mux window.
// Otherwise falls back to creating a separate tmux session.
func (t *TmuxBackend) CreatePane(id, cwd, command string) error {
	// Check for active mux session.
	if t.MuxSession != "" && mux.SessionExists(t.MuxSession) {
		info, err := mux.AddPane(t.MuxSession, cwd)
		if err != nil {
			return fmt.Errorf("mux add pane: %w", err)
		}
		t.mu.Lock()
		t.muxPanes[id] = info.PaneID
		t.mu.Unlock()
		t.saveMuxPanes()

		// If a command was specified, send it to the new pane.
		if command != "" {
			_ = t.tmuxRun("send-keys", "-t", info.PaneID, command, "C-m")
		}

		// Update the mux status bar.
		_ = mux.UpdateStatusBar(t.MuxSession)

		return nil
	}

	// Fallback: create separate tmux session (original behavior).
	session := t.sessionName(id)

	args := []string{"new-session", "-d", "-s", session, "-c", cwd}
	if command != "" {
		args = append(args, command)
	}
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Rename the initial window to "chat".
	if err := t.tmuxRun("rename-window", "-t", session+":0", "chat"); err != nil {
		return fmt.Errorf("tmux rename-window: %w", err)
	}

	// Create a second window named "code" with the same cwd.
	if err := t.tmuxRun("new-window", "-t", session, "-n", "code", "-c", cwd); err != nil {
		return fmt.Errorf("tmux new-window: %w", err)
	}

	// Select the "chat" window as active.
	if err := t.tmuxRun("select-window", "-t", session+":0"); err != nil {
		return fmt.Errorf("tmux select-window: %w", err)
	}

	return nil
}

// tmuxRun executes a tmux command and returns any error.
func (t *TmuxBackend) tmuxRun(args ...string) error {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DestroyPane kills the tmux pane/session for a workspace.
func (t *TmuxBackend) DestroyPane(id string) error {
	if paneID, ok := t.isMuxPane(id); ok {
		mux.RemovePane(paneID)
		t.mu.Lock()
		delete(t.muxPanes, id)
		t.mu.Unlock()
		t.saveMuxPanes()
		if t.MuxSession != "" {
			_ = mux.UpdateStatusBar(t.MuxSession)
		}
		return nil
	}

	session := t.sessionName(id)
	cmd := exec.Command("tmux", "kill-session", "-t", session)
	_ = cmd.Run() // best-effort
	return nil
}

// Attach switches to or attaches the workspace's tmux session.
func (t *TmuxBackend) Attach(id string) error {
	// If workspace is in mux, select its pane.
	if paneID, ok := t.isMuxPane(id); ok {
		return t.tmuxRun("select-pane", "-t", paneID)
	}

	session := t.sessionName(id)

	// If we're already inside tmux, switch to the workspace session.
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", session)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("tmux switch-client: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	// Not inside tmux — attach to the workspace session.
	cmd := exec.Command("tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux attach: %w", err)
	}
	return nil
}

// ListPanes lists all towr-managed sessions matching the prefix.
// Also includes panes from the mux window.
func (t *TmuxBackend) ListPanes() ([]PaneInfo, error) {
	var panes []PaneInfo

	// Include mux panes.
	t.mu.Lock()
	for wsID, paneID := range t.muxPanes {
		// Check if the pane is still alive.
		cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_current_path}")
		out, err := cmd.CombinedOutput()
		alive := err == nil
		p := PaneInfo{ID: wsID, Alive: alive}
		if alive {
			p.CWD = strings.TrimSpace(string(out))
		}
		panes = append(panes, p)
	}
	t.mu.Unlock()

	// Include standalone sessions.
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{pane_current_path}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return panes, nil
	}

	prefix := t.Prefix + "/"
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		wsID := strings.TrimPrefix(name, prefix)
		// Skip if already listed as mux pane.
		if _, ok := t.isMuxPane(wsID); ok {
			continue
		}
		p := PaneInfo{ID: wsID, Alive: true}
		if len(parts) > 2 {
			p.CWD = parts[2]
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// IsPaneAlive checks if the workspace's pane exists.
func (t *TmuxBackend) IsPaneAlive(id string) (bool, error) {
	if paneID, ok := t.isMuxPane(id); ok {
		cmd := exec.Command("tmux", "display-message", "-t", paneID, "-p", "ok")
		return cmd.Run() == nil, nil
	}

	session := t.sessionName(id)
	cmd := exec.Command("tmux", "has-session", "-t", session)
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

// SendInput loads content into a tmux paste buffer, pastes it, and sends Enter.
func (t *TmuxBackend) SendInput(id, content string) error {
	target := t.targetFor(id)
	tmpFile, err := os.CreateTemp("", "towr-paste-*.sh")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()
	if err := t.tmuxRun("load-buffer", "-b", "towr-dispatch", tmpFile.Name()); err != nil {
		return fmt.Errorf("load-buffer: %w", err)
	}
	if err := t.tmuxRun("paste-buffer", "-b", "towr-dispatch", "-t", target); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}
	// Brief delay to let the UI process the pasted text before sending Enter.
	time.Sleep(500 * time.Millisecond)
	if err := t.tmuxRun("send-keys", "-t", target, "C-m"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
}

// Interrupt sends Ctrl-C to the workspace's tmux session.
func (t *TmuxBackend) Interrupt(id string) error {
	return t.SendKeys(id, "C-c")
}

// Approve sends the given key to the workspace's tmux session to accept a dialog.
func (t *TmuxBackend) Approve(id, key string) error {
	return t.SendKeys(id, key)
}

// CaptureOutput captures the last N lines from the workspace's pane.
func (t *TmuxBackend) CaptureOutput(id string, lines int) (string, error) {
	target := t.targetFor(id)
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// LastActivity returns the time of the last output in the pane.
func (t *TmuxBackend) LastActivity(id string) time.Time {
	target := t.targetFor(id)
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_activity}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return time.Time{}
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return time.Time{}
	}
	var ts int64
	if _, err := fmt.Sscanf(s, "%d", &ts); err != nil {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

func (t *TmuxBackend) IsHeadless() bool {
	return false
}

// MuxPaneID returns the tmux pane ID for a workspace in mux mode, or "" if not in mux.
func (t *TmuxBackend) MuxPaneID(id string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.muxPanes[id]
}

// SendKeys sends raw keystrokes to the workspace's tmux pane.
func (t *TmuxBackend) SendKeys(id, keys string) error {
	target := t.targetFor(id)
	cmd := exec.Command("tmux", "send-keys", "-t", target, keys, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
