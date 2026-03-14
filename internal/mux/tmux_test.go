package mux

import "testing"

func TestMuxSessionCommands(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
		TowrBin:     "/usr/local/bin/towr",
	}

	cmds := BuildCreateCommands(cfg)

	// Should create a new tmux session.
	if cmds[0].Args[0] != "new-session" {
		t.Errorf("first command should be new-session, got %s", cmds[0].Args[0])
	}

	// Session name should match.
	found := false
	for _, arg := range cmds[0].Args {
		if arg == "towr-mux" {
			found = true
		}
	}
	if !found {
		t.Error("session name not found in new-session args")
	}
}

func TestMuxSessionHasControlPane(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
		TowrBin:     "/usr/local/bin/towr",
	}

	cmds := BuildCreateCommands(cfg)

	// Should have a split-window for the control pane.
	hasSplit := false
	for _, c := range cmds {
		if c.Args[0] == "split-window" {
			hasSplit = true
		}
	}
	if !hasSplit {
		t.Error("should have split-window for control pane")
	}
}

func TestMuxSessionControlPaneRunsTowrTUI(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
		TowrBin:     "/usr/local/bin/towr",
	}

	cmds := BuildCreateCommands(cfg)

	// The split-window command should contain the towr tui command.
	found := false
	for _, c := range cmds {
		if c.Args[0] == "split-window" {
			for _, arg := range c.Args {
				if arg == "/usr/local/bin/towr tui" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("control pane should run 'towr tui' with full binary path")
	}
}

func TestMuxSessionControlPaneFallback(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
		TowrBin:     "", // empty — should fallback
	}

	cmds := BuildCreateCommands(cfg)

	found := false
	for _, c := range cmds {
		if c.Args[0] == "split-window" {
			for _, arg := range c.Args {
				if arg == "towr tui" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("control pane should fallback to 'towr tui' when TowrBin is empty")
	}
}

func TestMuxSessionConfiguresLeaderKey(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
	}

	cmds := BuildKeybindingCommands(cfg)

	// Should set prefix key.
	hasPrefixSet := false
	for _, c := range cmds {
		if c.Args[0] == "set" {
			for _, arg := range c.Args {
				if arg == "prefix" {
					hasPrefixSet = true
				}
			}
		}
	}
	if !hasPrefixSet {
		t.Error("should configure tmux prefix key")
	}
}

func TestMuxSessionKeybindings(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
	}

	cmds := BuildKeybindingCommands(cfg)

	// Should have bindings for: Right, Left, Enter, t, w, q
	expectedKeys := map[string]bool{
		"Right": false,
		"Left":  false,
		"Enter": false,
		"t":     false,
		"w":     false,
		"q":     false,
	}

	for _, c := range cmds {
		if c.Args[0] == "bind" {
			// Key is after "-t" and session name
			for i, arg := range c.Args {
				if _, ok := expectedKeys[arg]; ok && i > 0 {
					expectedKeys[arg] = true
				}
			}
		}
	}

	for key, found := range expectedKeys {
		if !found {
			t.Errorf("missing keybinding for %q", key)
		}
	}
}

func TestMuxSessionEnablesMouse(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/tmp/test",
		LeaderKey:   "C-a",
	}

	cmds := BuildKeybindingCommands(cfg)

	hasMouse := false
	for _, c := range cmds {
		for i, arg := range c.Args {
			if arg == "mouse" && i+1 < len(c.Args) && c.Args[i+1] == "on" {
				hasMouse = true
			}
		}
	}
	if !hasMouse {
		t.Error("should enable mouse support")
	}
}

func TestBuildStatusBarCommands(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
	}

	cmds := BuildStatusBarCommands(cfg, 3, 2, "agent-1")

	if len(cmds) != 3 {
		t.Fatalf("expected 3 status bar commands, got %d", len(cmds))
	}

	// Check status-left contains pane info.
	found := false
	for _, c := range cmds {
		for _, arg := range c.Args {
			if arg == "status-left" {
				found = true
			}
		}
	}
	if !found {
		t.Error("should set status-left")
	}
}

func TestBuildCreateCommandsWorkDir(t *testing.T) {
	cfg := MuxConfig{
		SessionName: "towr-mux",
		Shell:       "/bin/zsh",
		WorkDir:     "/home/user/project",
		LeaderKey:   "C-a",
		TowrBin:     "towr",
	}

	cmds := BuildCreateCommands(cfg)

	// new-session should use the work dir.
	foundWorkDir := false
	for i, arg := range cmds[0].Args {
		if arg == "-c" && i+1 < len(cmds[0].Args) && cmds[0].Args[i+1] == "/home/user/project" {
			foundWorkDir = true
		}
	}
	if !foundWorkDir {
		t.Error("new-session should set working directory")
	}
}
