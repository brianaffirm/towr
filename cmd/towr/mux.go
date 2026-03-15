package main

import (
	"fmt"
	"os"

	"github.com/brianaffirm/towr/internal/mux"
	"github.com/spf13/cobra"
)

func newMuxCmd() *cobra.Command {
	var shell string

	cmd := &cobra.Command{
		Use:   "mux",
		Short: "Terminal multiplexer with tiling panes",
		Long: `Launch an interactive terminal multiplexer with a control sidebar,
master pane, and agent panes. Uses tmux for terminal rendering.

If a mux session already exists, reattaches to it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell == "" {
				shell = os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
			}

			sessionName := "towr-mux"

			// Reattach if session already exists.
			if mux.SessionExists(sessionName) {
				return mux.AttachSession(sessionName)
			}

			// Find towr binary path for the control pane.
			towrBin, err := os.Executable()
			if err != nil {
				towrBin = "towr" // fallback to PATH
			}

			cwd, _ := os.Getwd()

			cfg := mux.MuxConfig{
				SessionName: sessionName,
				Shell:       shell,
				WorkDir:     cwd,
				LeaderKey:   "C-a",
				TowrBin:     towrBin,
			}

			// Create the session.
			createCmds := mux.BuildCreateCommands(cfg)
			if err := mux.RunTmuxCmds(createCmds); err != nil {
				return fmt.Errorf("create mux session: %w", err)
			}

			// Configure keybindings.
			keyCmds := mux.BuildKeybindingCommands(cfg)
			if err := mux.RunTmuxCmds(keyCmds); err != nil {
				return fmt.Errorf("configure keybindings: %w", err)
			}

			// Configure status bar.
			statusCmds := mux.BuildStatusBarCommands(cfg, mux.StatusBarData{
				PaneCount:    2,
				RunningCount: 0,
				FocusName:    "master",
			})
			if err := mux.RunTmuxCmds(statusCmds); err != nil {
				return fmt.Errorf("configure status bar: %w", err)
			}

			// Attach to the session.
			return mux.AttachSession(sessionName)
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "", "Shell for master pane (default: $SHELL)")

	return cmd
}
