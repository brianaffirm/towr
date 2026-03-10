package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PasteBuffer loads content into a tmux paste buffer and sends it to the workspace's chat window.
func (t *TmuxBackend) PasteBuffer(id, content string) error {
	target := t.sessionName(id)
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
	if err := t.tmuxRun("paste-buffer", "-b", "towr-dispatch", "-t", target+":chat"); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}
	if err := t.tmuxRun("send-keys", "-t", target+":chat", "C-m"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
}

// TmuxBackend implements Backend using tmux.
// Each workspace gets its own tmux session named Prefix/<id>.
type TmuxBackend struct {
	Prefix string // session name prefix, e.g., "towr"
}

// NewTmuxBackend creates a tmux backend with the given prefix.
// Sessions are created as prefix/<workspace-id>.
func NewTmuxBackend(prefix string) *TmuxBackend {
	if prefix == "" {
		prefix = "towr"
	}
	return &TmuxBackend{Prefix: prefix}
}

// sessionName returns the tmux session name for a workspace.
func (t *TmuxBackend) sessionName(id string) string {
	return t.Prefix + "/" + id
}

// CreatePane creates a new tmux session for the workspace.
// The session gets two windows: "chat" (window 0, active) and "code" (window 1),
// both with cwd set to the worktree path.
func (t *TmuxBackend) CreatePane(id, cwd, command string) error {
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

// DestroyPane kills the tmux session for a workspace.
func (t *TmuxBackend) DestroyPane(id string) error {
	session := t.sessionName(id)
	cmd := exec.Command("tmux", "kill-session", "-t", session)
	_ = cmd.Run() // best-effort
	return nil
}

// Attach switches to or attaches the workspace's tmux session.
func (t *TmuxBackend) Attach(id string) error {
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
func (t *TmuxBackend) ListPanes() ([]PaneInfo, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{pane_current_path}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil // no tmux server or no sessions
	}

	prefix := t.Prefix + "/"
	var panes []PaneInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// Extract the workspace ID by stripping the prefix.
		wsID := strings.TrimPrefix(name, prefix)
		p := PaneInfo{ID: wsID, Alive: true}
		if len(parts) > 2 {
			p.CWD = parts[2]
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// IsPaneAlive checks if the tmux session for the workspace exists.
func (t *TmuxBackend) IsPaneAlive(id string) (bool, error) {
	session := t.sessionName(id)
	cmd := exec.Command("tmux", "has-session", "-t", session)
	if err := cmd.Run(); err != nil {
		return false, nil // session doesn't exist
	}
	return true, nil
}

// CapturePane captures the last N lines from the workspace's chat window.
func (t *TmuxBackend) CapturePane(id string, lines int) (string, error) {
	session := t.sessionName(id)
	cmd := exec.Command("tmux", "capture-pane", "-t", session+":chat", "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

func (t *TmuxBackend) IsHeadless() bool {
	return false
}

// SendKeys sends keystrokes to the workspace's tmux session.
func (t *TmuxBackend) SendKeys(id, keys string) error {
	session := t.sessionName(id)
	cmd := exec.Command("tmux", "send-keys", "-t", session, keys, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
