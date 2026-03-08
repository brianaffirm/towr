package terminal

// PaneInfo describes a terminal pane managed by amux.
type PaneInfo struct {
	ID      string
	Alive   bool
	CWD     string
	Command string
}

// Backend is the interface for terminal multiplexer operations.
type Backend interface {
	// CreatePane creates a new terminal pane for a workspace.
	CreatePane(id, cwd, command string) error
	// DestroyPane tears down the pane for a workspace.
	DestroyPane(id string) error
	// Attach switches focus to the given workspace pane.
	Attach(id string) error
	// ListPanes returns all amux-managed panes.
	ListPanes() ([]PaneInfo, error)
	// IsPaneAlive checks whether the pane for the given workspace is running.
	IsPaneAlive(id string) (bool, error)
	// SendKeys sends keystrokes to a pane (e.g., "C-c" for interrupt).
	SendKeys(id, keys string) error
	// IsHeadless returns true if the backend cannot manage terminal sessions.
	IsHeadless() bool
}
