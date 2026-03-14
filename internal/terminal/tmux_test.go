package terminal

import (
	"os/exec"
	"strings"
	"testing"
)

// skipIfNoTmux skips the test if tmux is not installed.
func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// cleanupSession kills a tmux session if it exists.
func cleanupSession(t *testing.T, name string) {
	t.Helper()
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
}

// newStandaloneBackend creates a TmuxBackend with mux detection disabled.
func newStandaloneBackend(prefix string) *TmuxBackend {
	b := NewTmuxBackend(prefix)
	b.MuxSession = "" // disable mux detection so tests always create standalone sessions
	return b
}

func TestSessionName(t *testing.T) {
	b := NewTmuxBackend("towr")
	got := b.sessionName("my-workspace")
	want := "towr/my-workspace"
	if got != want {
		t.Errorf("sessionName = %q, want %q", got, want)
	}
}

func TestSessionNameWithRepoPrefix(t *testing.T) {
	b := NewTmuxBackend("towr/myrepo")
	got := b.sessionName("feat")
	want := "towr/myrepo/feat"
	if got != want {
		t.Errorf("sessionName = %q, want %q", got, want)
	}
}

func TestCreatePaneCreatesSession(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test")
	sessionName := b.sessionName("ws1")
	t.Cleanup(func() { cleanupSession(t, sessionName) })

	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane failed: %v", err)
	}

	// Verify a tmux session (not window) was created.
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		t.Errorf("expected tmux session %q to exist", sessionName)
	}
}

func TestCreatePaneCreatesTwoWindows(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-windows")
	sessionName := b.sessionName("ws1")
	t.Cleanup(func() { cleanupSession(t, sessionName) })

	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane failed: %v", err)
	}

	// List windows in the session.
	cmd := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_index}:#{window_name}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list-windows failed: %v", err)
	}

	windows := strings.TrimSpace(string(out))
	lines := strings.Split(windows, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 windows, got %d: %s", len(lines), windows)
	}

	if lines[0] != "0:chat" {
		t.Errorf("expected window 0 named 'chat', got %q", lines[0])
	}
	if lines[1] != "1:code" {
		t.Errorf("expected window 1 named 'code', got %q", lines[1])
	}

	// Verify the active window is "chat" (window 0).
	cmd = exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{window_index}")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("display-message failed: %v", err)
	}
	activeWindow := strings.TrimSpace(string(out))
	if activeWindow != "0" {
		t.Errorf("expected active window to be 0 (chat), got %q", activeWindow)
	}
}

func TestTwoWorkspacesGetSeparateSessions(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-separate")
	session1 := b.sessionName("ws1")
	session2 := b.sessionName("ws2")
	t.Cleanup(func() {
		cleanupSession(t, session1)
		cleanupSession(t, session2)
	})

	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane ws1: %v", err)
	}
	if err := b.CreatePane("ws2", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane ws2: %v", err)
	}

	// Killing ws1 should NOT affect ws2.
	if err := b.DestroyPane("ws1"); err != nil {
		t.Fatalf("DestroyPane ws1: %v", err)
	}

	alive, err := b.IsPaneAlive("ws2")
	if err != nil {
		t.Fatalf("IsPaneAlive ws2: %v", err)
	}
	if !alive {
		t.Error("ws2 should still be alive after killing ws1 — sessions are isolated")
	}
}

func TestDestroyPaneKillsSession(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-destroy")
	sessionName := b.sessionName("ws1")
	t.Cleanup(func() { cleanupSession(t, sessionName) })

	// Create the session first.
	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane failed: %v", err)
	}

	// Destroy it.
	if err := b.DestroyPane("ws1"); err != nil {
		t.Fatalf("DestroyPane failed: %v", err)
	}

	// Session should be gone.
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := cmd.Run(); err == nil {
		t.Errorf("session %q should not exist after DestroyPane", sessionName)
	}
}

func TestListPanesListsSessions(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-list")
	session1 := b.sessionName("ws-a")
	session2 := b.sessionName("ws-b")
	t.Cleanup(func() {
		cleanupSession(t, session1)
		cleanupSession(t, session2)
	})

	// Create two workspace sessions.
	if err := b.CreatePane("ws-a", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane ws-a: %v", err)
	}
	if err := b.CreatePane("ws-b", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane ws-b: %v", err)
	}

	panes, err := b.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}

	ids := make(map[string]bool)
	for _, p := range panes {
		ids[p.ID] = true
	}

	if !ids["ws-a"] {
		t.Errorf("expected ws-a in pane list, got %v", ids)
	}
	if !ids["ws-b"] {
		t.Errorf("expected ws-b in pane list, got %v", ids)
	}
}

func TestIsPaneAliveChecksSession(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-alive")
	sessionName := b.sessionName("ws1")
	t.Cleanup(func() { cleanupSession(t, sessionName) })

	// Before creating, should not be alive.
	alive, err := b.IsPaneAlive("ws1")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if alive {
		t.Error("expected ws1 to not be alive before creation")
	}

	// Create session.
	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}

	alive, err = b.IsPaneAlive("ws1")
	if err != nil {
		t.Fatalf("IsPaneAlive: %v", err)
	}
	if !alive {
		t.Error("expected ws1 to be alive after creation")
	}
}

func TestApproveTargetsSession(t *testing.T) {
	skipIfNoTmux(t)

	b := newStandaloneBackend("towr-test-keys")
	sessionName := b.sessionName("ws1")
	t.Cleanup(func() { cleanupSession(t, sessionName) })

	if err := b.CreatePane("ws1", "/tmp", ""); err != nil {
		t.Fatalf("CreatePane: %v", err)
	}

	// Approve should not error when targeting the session.
	if err := b.Approve("ws1", "Enter"); err != nil {
		t.Errorf("Approve failed: %v", err)
	}
}

func TestAttachInsideTmuxUsesSwitchClient(t *testing.T) {
	// This test verifies the method signature works, but can't fully test
	// tmux switch-client without being inside a tmux session.
	// We just verify the session name format is correct.
	b := NewTmuxBackend("towr")
	name := b.sessionName("my-ws")
	if !strings.Contains(name, "/") {
		t.Errorf("session name %q should use / separator for tmux sessions", name)
	}
}

func TestTargetForStandaloneSession(t *testing.T) {
	b := NewTmuxBackend("towr")
	target := b.targetFor("my-ws")
	want := "towr/my-ws:chat"
	if target != want {
		t.Errorf("targetFor (standalone) = %q, want %q", target, want)
	}
}

func TestTargetForMuxPane(t *testing.T) {
	b := NewTmuxBackend("towr")
	b.mu.Lock()
	b.muxPanes["my-ws"] = "%42"
	b.mu.Unlock()

	target := b.targetFor("my-ws")
	if target != "%42" {
		t.Errorf("targetFor (mux pane) = %q, want '%%42'", target)
	}
}

func TestIsMuxPane(t *testing.T) {
	b := NewTmuxBackend("towr")

	// Not a mux pane.
	_, ok := b.isMuxPane("ws1")
	if ok {
		t.Error("ws1 should not be a mux pane")
	}

	// Register as mux pane.
	b.mu.Lock()
	b.muxPanes["ws1"] = "%10"
	b.mu.Unlock()

	paneID, ok := b.isMuxPane("ws1")
	if !ok {
		t.Error("ws1 should be a mux pane after registration")
	}
	if paneID != "%10" {
		t.Errorf("paneID = %q, want '%%10'", paneID)
	}
}

func TestCreatePaneMuxMode(t *testing.T) {
	skipIfNoTmux(t)

	// Create a mux session first.
	muxSession := "towr-mux-test-createpane"
	cmd := exec.Command("tmux", "new-session", "-d", "-s", muxSession, "-x", "200", "-y", "50")
	if err := cmd.Run(); err != nil {
		t.Fatalf("create mux session: %v", err)
	}
	// Rename window to "mux" to match what AddPane expects.
	_ = exec.Command("tmux", "rename-window", "-t", muxSession+":0", "mux").Run()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", muxSession).Run()
	})

	// Temporarily override the default session name by creating pane directly via mux package.
	// For the integration test we test AddPane directly since CreatePane checks "towr-mux" specifically.
	b := NewTmuxBackend("towr")

	// Count panes before.
	countBefore := countTmuxPanes(t, muxSession)

	// Use mux.AddPane directly to test pane creation in a mux session.
	info, err := addPaneForTest(muxSession, "/tmp")
	if err != nil {
		t.Fatalf("AddPane: %v", err)
	}

	// Register it in the backend.
	b.mu.Lock()
	b.muxPanes["test-ws"] = info
	b.mu.Unlock()

	// Count panes after.
	countAfter := countTmuxPanes(t, muxSession)
	if countAfter != countBefore+1 {
		t.Errorf("pane count: before=%d, after=%d, want %d", countBefore, countAfter, countBefore+1)
	}

	// Verify the backend targets the mux pane.
	target := b.targetFor("test-ws")
	if target != info {
		t.Errorf("targetFor = %q, want %q", target, info)
	}
}

func countTmuxPanes(t *testing.T, session string) int {
	t.Helper()
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

func addPaneForTest(session, cwd string) (string, error) {
	cmd := exec.Command("tmux", "split-window", "-t", session+":mux", "-h",
		"-c", cwd, "-P", "-F", "#{pane_id}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func TestDestroyMuxPane(t *testing.T) {
	skipIfNoTmux(t)

	muxSession := "towr-mux-test-destroy-muxpane"
	cmd := exec.Command("tmux", "new-session", "-d", "-s", muxSession, "-x", "200", "-y", "50")
	if err := cmd.Run(); err != nil {
		t.Fatalf("create mux session: %v", err)
	}
	_ = exec.Command("tmux", "rename-window", "-t", muxSession+":0", "mux").Run()
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", muxSession).Run()
	})

	// Add a pane.
	paneID, err := addPaneForTest(muxSession, "/tmp")
	if err != nil {
		t.Fatalf("addPaneForTest: %v", err)
	}

	b := NewTmuxBackend("towr")
	b.mu.Lock()
	b.muxPanes["test-ws"] = paneID
	b.mu.Unlock()

	// Destroy it.
	if err := b.DestroyPane("test-ws"); err != nil {
		t.Fatalf("DestroyPane: %v", err)
	}

	// Should no longer be tracked.
	_, ok := b.isMuxPane("test-ws")
	if ok {
		t.Error("test-ws should not be tracked after DestroyPane")
	}
}
