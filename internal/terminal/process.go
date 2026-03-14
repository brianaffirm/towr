package terminal

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultRingSize = 500

// managedProcess holds the state for a single subprocess managed by ProcessBackend.
type managedProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu           sync.Mutex
	ring         []string // circular buffer of stdout lines
	ringPos      int      // next write position in ring
	ringCount    int      // total lines written (for computing available lines)
	lastActivity time.Time
	cwd          string
	command      string
	done         chan struct{} // closed when process exits
}

// writeRingLine appends a line to the ring buffer and updates lastActivity.
func (p *managedProcess) writeRingLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ring[p.ringPos] = line
	p.ringPos = (p.ringPos + 1) % len(p.ring)
	p.ringCount++
	p.lastActivity = time.Now()
}

// readRingLines returns the last n lines from the ring buffer.
func (p *managedProcess) readRingLines(n int) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	available := p.ringCount
	cap := len(p.ring)
	if available > cap {
		available = cap
	}
	if n > available {
		n = available
	}
	if n == 0 {
		return ""
	}

	lines := make([]string, n)
	// ringPos points to the next write slot, so the most recent line is at ringPos-1.
	start := (p.ringPos - n + cap) % cap
	for i := 0; i < n; i++ {
		lines[i] = p.ring[(start+i)%cap]
	}
	return strings.Join(lines, "\n") + "\n"
}

// getLastActivity returns the last stdout write time.
func (p *managedProcess) getLastActivity() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastActivity
}

// ProcessBackend implements Backend by managing agents as subprocesses.
// Each "pane" is an exec.Cmd with stdin/stdout pipes. No terminal multiplexer needed.
type ProcessBackend struct {
	mu    sync.Mutex
	procs map[string]*managedProcess
}

// NewProcessBackend creates a process-based backend.
func NewProcessBackend() *ProcessBackend {
	return &ProcessBackend{
		procs: make(map[string]*managedProcess),
	}
}

// CreatePane starts a subprocess for the given command in cwd.
// The command string is parsed by the shell (via sh -c).
func (b *ProcessBackend) CreatePane(id, cwd, command string) error {
	b.mu.Lock()
	if _, exists := b.procs[id]; exists {
		b.mu.Unlock()
		return fmt.Errorf("process %q already exists", id)
	}
	b.mu.Unlock()

	if command == "" {
		command = "sh"
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	// Create a new process group so we can signal the group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Merge stderr into stdout for capture.
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		return fmt.Errorf("start process: %w", err)
	}

	mp := &managedProcess{
		cmd:          cmd,
		stdin:        stdinPipe,
		ring:         make([]string, defaultRingSize),
		lastActivity: time.Now(),
		cwd:          cwd,
		command:      command,
		done:         make(chan struct{}),
	}

	// Read stdout in background and populate ring buffer.
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			mp.writeRingLine(scanner.Text())
		}
	}()

	// Wait for process exit in background.
	go func() {
		_ = cmd.Wait()
		close(mp.done)
	}()

	b.mu.Lock()
	b.procs[id] = mp
	b.mu.Unlock()

	return nil
}

// DestroyPane kills the subprocess for the given id.
func (b *ProcessBackend) DestroyPane(id string) error {
	b.mu.Lock()
	mp, ok := b.procs[id]
	if !ok {
		b.mu.Unlock()
		return nil // best-effort, like TmuxBackend
	}
	delete(b.procs, id)
	b.mu.Unlock()

	// Kill the process group.
	if mp.cmd.Process != nil {
		_ = syscall.Kill(-mp.cmd.Process.Pid, syscall.SIGKILL)
	}

	// Wait for cleanup.
	<-mp.done
	return nil
}

// IsPaneAlive checks if the subprocess is still running.
func (b *ProcessBackend) IsPaneAlive(id string) (bool, error) {
	b.mu.Lock()
	mp, ok := b.procs[id]
	b.mu.Unlock()

	if !ok {
		return false, nil
	}

	select {
	case <-mp.done:
		return false, nil
	default:
		return true, nil
	}
}

// ListPanes returns all tracked processes.
func (b *ProcessBackend) ListPanes() ([]PaneInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	panes := make([]PaneInfo, 0, len(b.procs))
	for id, mp := range b.procs {
		alive := true
		select {
		case <-mp.done:
			alive = false
		default:
		}
		panes = append(panes, PaneInfo{
			ID:      id,
			Alive:   alive,
			CWD:     mp.cwd,
			Command: mp.command,
		})
	}
	return panes, nil
}

// SendInput writes content followed by a newline to the process's stdin.
// Callers must NOT send Enter separately — SendInput auto-submits.
func (b *ProcessBackend) SendInput(id, content string) error {
	b.mu.Lock()
	mp, ok := b.procs[id]
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("process %q not found", id)
	}

	_, err := fmt.Fprintln(mp.stdin, content)
	if err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// Interrupt sends SIGINT to the subprocess.
func (b *ProcessBackend) Interrupt(id string) error {
	b.mu.Lock()
	mp, ok := b.procs[id]
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("process %q not found", id)
	}

	if mp.cmd.Process == nil {
		return fmt.Errorf("process %q has no OS process", id)
	}

	// Signal the process group so children also get SIGINT.
	return syscall.Kill(-mp.cmd.Process.Pid, syscall.SIGINT)
}

// Approve writes the approval key to stdin.
// Keys like "Enter", "y", "a" are translated: "Enter" becomes an empty SendInput
// (which appends \n), while single-char keys are sent as-is.
func (b *ProcessBackend) Approve(id, key string) error {
	switch key {
	case "Enter", "C-m":
		// SendInput auto-appends newline, so empty content = just press Enter.
		return b.SendInput(id, "")
	default:
		return b.SendInput(id, key)
	}
}

// CaptureOutput returns the last N lines from the process's stdout ring buffer.
func (b *ProcessBackend) CaptureOutput(id string, lines int) (string, error) {
	b.mu.Lock()
	mp, ok := b.procs[id]
	b.mu.Unlock()

	if !ok {
		return "", fmt.Errorf("process %q not found", id)
	}

	return mp.readRingLines(lines), nil
}

// LastActivity returns the timestamp of the last stdout write.
func (b *ProcessBackend) LastActivity(id string) time.Time {
	b.mu.Lock()
	mp, ok := b.procs[id]
	b.mu.Unlock()

	if !ok {
		return time.Time{}
	}

	return mp.getLastActivity()
}

// Attach returns an error — ProcessBackend is headless, there's no terminal to attach to.
func (b *ProcessBackend) Attach(id string) error {
	return fmt.Errorf("attach not supported: process backend is headless")
}

// IsHeadless returns false — ProcessBackend manages real processes and supports
// CreatePane/SendInput/CaptureOutput. It has no attach-able terminal, but it is
// not headless in the sense that HeadlessBackend is (which is a no-op stub).
func (b *ProcessBackend) IsHeadless() bool {
	return false
}
