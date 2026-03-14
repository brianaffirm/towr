package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestProcessBackendImplementsBackend(t *testing.T) {
	var _ Backend = (*ProcessBackend)(nil)
}

func TestProcessCreateAndDestroy(t *testing.T) {
	b := NewProcessBackend()

	if err := b.CreatePane("test1", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}

	alive, err := b.IsPaneAlive("test1")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if !alive {
		t.Error("expected process to be alive after creation")
	}

	if err := b.DestroyPane("test1"); err != nil {
		t.Fatalf("DestroyPane: %v", err)
	}

	alive, err = b.IsPaneAlive("test1")
	if err != nil {
		t.Fatalf("IsPaneAlive after destroy: %v", err)
	}
	if alive {
		t.Error("expected process to be dead after destroy")
	}
}

func TestProcessCreateDuplicateErrors(t *testing.T) {
	b := NewProcessBackend()

	if err := b.CreatePane("dup", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("dup") })

	if err := b.CreatePane("dup", "/tmp", "cat"); err == nil {
		t.Error("expected error creating duplicate process")
	}
}

func TestProcessSendInputAndCapture(t *testing.T) {
	b := NewProcessBackend()

	// cat echoes stdin to stdout.
	if err := b.CreatePane("echo-test", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("echo-test") })

	if err := b.SendInput("echo-test", "hello world"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// Give cat time to echo.
	time.Sleep(100 * time.Millisecond)

	output, err := b.CaptureOutput("echo-test", 10)
	if err != nil {
		t.Fatalf("CaptureOutput: %v", err)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got %q", output)
	}
}

func TestProcessSendInputAutoSubmits(t *testing.T) {
	b := NewProcessBackend()

	// Use cat to verify newline is appended — each SendInput becomes one line.
	if err := b.CreatePane("newline-test", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("newline-test") })

	b.SendInput("newline-test", "line1")
	b.SendInput("newline-test", "line2")

	time.Sleep(100 * time.Millisecond)

	output, err := b.CaptureOutput("newline-test", 10)
	if err != nil {
		t.Fatalf("CaptureOutput: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %q", len(lines), output)
	}
	if lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("expected [line1, line2], got %v", lines)
	}
}

func TestProcessCaptureOutputRingBuffer(t *testing.T) {
	b := NewProcessBackend()

	if err := b.CreatePane("ring-test", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("ring-test") })

	// Write more lines than the ring buffer holds.
	for i := 0; i < defaultRingSize+50; i++ {
		b.SendInput("ring-test", "x")
	}

	time.Sleep(200 * time.Millisecond)

	output, err := b.CaptureOutput("ring-test", defaultRingSize+100)
	if err != nil {
		t.Fatalf("CaptureOutput: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != defaultRingSize {
		t.Errorf("expected %d lines (ring buffer max), got %d", defaultRingSize, len(lines))
	}
}

func TestProcessListPanes(t *testing.T) {
	b := NewProcessBackend()

	b.CreatePane("ws-a", "/tmp", "cat")
	b.CreatePane("ws-b", "/tmp", "cat")
	t.Cleanup(func() {
		b.DestroyPane("ws-a")
		b.DestroyPane("ws-b")
	})

	panes, err := b.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}

	ids := make(map[string]bool)
	for _, p := range panes {
		ids[p.ID] = true
	}

	if !ids["ws-a"] || !ids["ws-b"] {
		t.Errorf("expected ws-a and ws-b, got %v", ids)
	}
}

func TestProcessIsPaneAliveAfterExit(t *testing.T) {
	b := NewProcessBackend()

	// "true" exits immediately.
	if err := b.CreatePane("short", "/tmp", "true"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("short") })

	// Wait for exit.
	time.Sleep(200 * time.Millisecond)

	alive, err := b.IsPaneAlive("short")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Error("expected process to be dead after 'true' exits")
	}
}

func TestProcessIsPaneAliveUnknown(t *testing.T) {
	b := NewProcessBackend()

	alive, err := b.IsPaneAlive("nonexistent")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Error("expected unknown process to not be alive")
	}
}

func TestProcessInterrupt(t *testing.T) {
	b := NewProcessBackend()

	// sleep will be interrupted by SIGINT.
	if err := b.CreatePane("int-test", "/tmp", "sleep 60"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("int-test") })

	if err := b.Interrupt("int-test"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	// Process should exit after SIGINT.
	time.Sleep(200 * time.Millisecond)

	alive, err := b.IsPaneAlive("int-test")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Error("expected process to exit after interrupt")
	}
}

func TestProcessLastActivity(t *testing.T) {
	b := NewProcessBackend()

	if err := b.CreatePane("activity-test", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("activity-test") })

	before := time.Now()
	time.Sleep(10 * time.Millisecond)

	b.SendInput("activity-test", "ping")
	time.Sleep(100 * time.Millisecond)

	last := b.LastActivity("activity-test")
	if last.Before(before) {
		t.Errorf("expected LastActivity after %v, got %v", before, last)
	}
}

func TestProcessLastActivityUnknown(t *testing.T) {
	b := NewProcessBackend()
	last := b.LastActivity("nonexistent")
	if !last.IsZero() {
		t.Errorf("expected zero time for unknown process, got %v", last)
	}
}

func TestProcessApproveEnterSendsNewline(t *testing.T) {
	b := NewProcessBackend()

	// cat echoes stdin to stdout. "Enter" should send just a newline, not the word "Enter".
	if err := b.CreatePane("approve-test", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("approve-test") })

	// Send some text first, then approve with "Enter".
	b.SendInput("approve-test", "before")
	b.Approve("approve-test", "Enter")
	b.SendInput("approve-test", "after")

	time.Sleep(100 * time.Millisecond)

	output, _ := b.CaptureOutput("approve-test", 10)
	// Should see "before", an empty line (from Enter), and "after" — NOT the word "Enter".
	if strings.Contains(output, "Enter") {
		t.Errorf("Approve(Enter) should send newline, not literal 'Enter': %q", output)
	}
	if !strings.Contains(output, "before") || !strings.Contains(output, "after") {
		t.Errorf("expected 'before' and 'after' in output, got %q", output)
	}
}

func TestProcessApproveCharKey(t *testing.T) {
	b := NewProcessBackend()

	if err := b.CreatePane("approve-char", "/tmp", "cat"); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("approve-char") })

	// Approving with "a" (Cursor's key) should send literal "a".
	b.Approve("approve-char", "a")
	time.Sleep(100 * time.Millisecond)

	output, _ := b.CaptureOutput("approve-char", 10)
	if !strings.Contains(output, "a") {
		t.Errorf("expected 'a' in output, got %q", output)
	}
}

func TestProcessAttachErrors(t *testing.T) {
	b := NewProcessBackend()
	if err := b.Attach("anything"); err == nil {
		t.Error("expected Attach to return error for process backend")
	}
}

func TestProcessIsNotHeadless(t *testing.T) {
	b := NewProcessBackend()
	if b.IsHeadless() {
		t.Error("ProcessBackend should not be headless — it manages real processes")
	}
}

func TestProcessDestroyUnknownNoError(t *testing.T) {
	b := NewProcessBackend()
	if err := b.DestroyPane("nonexistent"); err != nil {
		t.Errorf("expected no error destroying unknown process, got %v", err)
	}
}

func TestProcessSendInputToUnknownErrors(t *testing.T) {
	b := NewProcessBackend()
	if err := b.SendInput("nonexistent", "hello"); err == nil {
		t.Error("expected error sending to unknown process")
	}
}

func TestProcessCaptureOutputUnknownErrors(t *testing.T) {
	b := NewProcessBackend()
	if _, err := b.CaptureOutput("nonexistent", 10); err == nil {
		t.Error("expected error capturing from unknown process")
	}
}

func TestProcessDefaultCommand(t *testing.T) {
	b := NewProcessBackend()

	// Empty command should default to "sh".
	if err := b.CreatePane("default-cmd", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("default-cmd") })

	alive, _ := b.IsPaneAlive("default-cmd")
	if !alive {
		t.Error("expected default shell to be alive")
	}
}

func TestProcessTwoWorkspacesIsolated(t *testing.T) {
	b := NewProcessBackend()

	b.CreatePane("ws1", "/tmp", "cat")
	b.CreatePane("ws2", "/tmp", "cat")
	t.Cleanup(func() {
		b.DestroyPane("ws1")
		b.DestroyPane("ws2")
	})

	// Killing ws1 should NOT affect ws2.
	b.DestroyPane("ws1")

	alive, _ := b.IsPaneAlive("ws2")
	if !alive {
		t.Error("ws2 should still be alive after killing ws1")
	}
}

// TestProcessSmokeIntegration exercises the full lifecycle: spawn a script that
// produces output, send input, capture output, check activity, interrupt, destroy.
func TestProcessSmokeIntegration(t *testing.T) {
	b := NewProcessBackend()

	// Spawn a script that prints numbered lines, then waits on cat for input.
	script := `sh -c 'for i in 1 2 3 4 5; do echo "output line $i"; done; echo "READY"; cat'`
	if err := b.CreatePane("smoke", "/tmp", script); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}
	t.Cleanup(func() { b.DestroyPane("smoke") })

	// Wait for script output.
	time.Sleep(500 * time.Millisecond)

	// Capture should contain the script's output.
	out, err := b.CaptureOutput("smoke", 10)
	if err != nil {
		t.Fatalf("CaptureOutput: %v", err)
	}
	if !strings.Contains(out, "output line 5") {
		t.Errorf("expected 'output line 5' in output, got %q", out)
	}
	if !strings.Contains(out, "READY") {
		t.Errorf("expected 'READY' in output, got %q", out)
	}

	// Process should be alive (cat is blocking).
	alive, _ := b.IsPaneAlive("smoke")
	if !alive {
		t.Fatal("expected process to be alive while cat blocks")
	}

	// LastActivity should be recent.
	activity := b.LastActivity("smoke")
	if time.Since(activity) > 5*time.Second {
		t.Errorf("LastActivity too old: %v", activity)
	}

	// Send input — cat will echo it back.
	if err := b.SendInput("smoke", "hello from towr"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	out, _ = b.CaptureOutput("smoke", 10)
	if !strings.Contains(out, "hello from towr") {
		t.Errorf("expected 'hello from towr' in output after SendInput, got %q", out)
	}

	// ListPanes should show our process.
	panes, _ := b.ListPanes()
	found := false
	for _, p := range panes {
		if p.ID == "smoke" && p.Alive {
			found = true
		}
	}
	if !found {
		t.Error("expected 'smoke' in ListPanes as alive")
	}

	// Interrupt should kill cat.
	if err := b.Interrupt("smoke"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	alive, _ = b.IsPaneAlive("smoke")
	if alive {
		t.Error("expected process to exit after interrupt")
	}
}

func TestNewBackendProcessEnvVar(t *testing.T) {
	t.Setenv("TOWR_BACKEND", "process")
	b := NewBackend()
	if _, ok := b.(*ProcessBackend); !ok {
		t.Errorf("expected *ProcessBackend, got %T", b)
	}
}

func TestNewBackendHeadlessEnvVar(t *testing.T) {
	t.Setenv("TOWR_BACKEND", "headless")
	b := NewBackend()
	if _, ok := b.(*HeadlessBackend); !ok {
		t.Errorf("expected *HeadlessBackend, got %T", b)
	}
}
