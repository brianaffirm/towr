package terminal

import (
	"os"
	"os/exec"
	"time"
)

// PaneInfo describes a terminal pane managed by towr.
type PaneInfo struct {
	ID      string
	Alive   bool
	CWD     string
	Command string
}

// Backend is the interface for agent session management.
// Methods express intent, not transport mechanism — implementations
// may use tmux, raw subprocesses, containers, or remote APIs.
type Backend interface {
	// Lifecycle
	CreatePane(id, cwd, command string) error
	DestroyPane(id string) error
	IsPaneAlive(id string) (bool, error)
	ListPanes() ([]PaneInfo, error)

	// Communication — intent, not mechanism.
	// SendInput delivers content and submits it (auto-Enter).
	// Callers must NOT send Enter separately after SendInput.
	SendInput(id, content string) error
	// Interrupt stops the agent's current work (tmux: C-c, process: SIGINT).
	Interrupt(id string) error
	// Approve accepts a permission/confirmation dialog using the given key.
	// The key is agent-specific (e.g., "Enter" for Claude, "y" or "a" for Cursor).
	Approve(id, key string) error

	// Observation
	CaptureOutput(id string, lines int) (string, error)
	LastActivity(id string) time.Time

	// Human access
	Attach(id string) error
	IsHeadless() bool
}

// NewBackend returns a Backend based on the TOWR_BACKEND env var:
//   - "process": ProcessBackend (subprocess with stdin/stdout pipes)
//   - "headless": HeadlessBackend (stub, no-op)
//   - "tmux" or unset: TmuxBackend if tmux is installed, HeadlessBackend otherwise
func NewBackend() Backend {
	switch os.Getenv("TOWR_BACKEND") {
	case "process":
		return NewProcessBackend()
	case "headless":
		return NewHeadlessBackend()
	default:
		if _, err := exec.LookPath("tmux"); err != nil {
			return NewHeadlessBackend()
		}
		return NewTmuxBackend("towr")
	}
}
