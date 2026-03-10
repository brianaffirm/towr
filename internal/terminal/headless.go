package terminal

import "fmt"

// HeadlessBackend implements Backend for non-interactive use.
// It prints the worktree path instead of managing tmux panes.
type HeadlessBackend struct{}

// NewHeadlessBackend creates a headless terminal backend.
func NewHeadlessBackend() *HeadlessBackend {
	return &HeadlessBackend{}
}

func (h *HeadlessBackend) CreatePane(id, cwd, command string) error {
	// No-op in headless mode.
	return nil
}

func (h *HeadlessBackend) DestroyPane(id string) error {
	return nil
}

func (h *HeadlessBackend) Attach(id string) error {
	fmt.Println(id)
	return nil
}

func (h *HeadlessBackend) ListPanes() ([]PaneInfo, error) {
	return nil, nil
}

func (h *HeadlessBackend) IsPaneAlive(id string) (bool, error) {
	return false, nil
}

func (h *HeadlessBackend) SendKeys(id, keys string) error {
	return nil
}

func (h *HeadlessBackend) PasteBuffer(id, content string) error {
	return fmt.Errorf("paste-buffer not supported in headless mode")
}

func (h *HeadlessBackend) CapturePane(id string, lines int) (string, error) {
	return "", fmt.Errorf("capture-pane not supported in headless mode")
}

func (h *HeadlessBackend) IsHeadless() bool {
	return true
}
