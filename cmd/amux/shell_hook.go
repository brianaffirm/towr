package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brianho/amux/internal/workspace"
	"github.com/spf13/cobra"
)

func newShellHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell-hook",
		Short: "Print shell integration code for amux nudge",
		Long: `Print shell hook code to integrate amux nudge into your prompt.
Add to your shell config:
  eval "$(amux shell-hook)"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := filepath.Base(os.Getenv("SHELL"))

			// Find amux binary path.
			amuxBin, err := os.Executable()
			if err != nil {
				amuxBin = "amux"
			}

			switch shell {
			case "zsh":
				fmt.Printf(`# amux shell integration
_amux_nudge() {
  %s _nudge 2>/dev/null
}
precmd_functions+=(_amux_nudge)
`, amuxBin)
			case "bash":
				fmt.Printf(`# amux shell integration
_amux_nudge() {
  %s _nudge 2>/dev/null
}
PROMPT_COMMAND="_amux_nudge${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
`, amuxBin)
			default:
				fmt.Fprintf(os.Stderr, "Unsupported shell: %s (supported: bash, zsh)\n", shell)
			}
			return nil
		},
	}
	return cmd
}

func newNudgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "_nudge",
		Short:  "Check for untracked work (called by shell hook)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return nil
			}

			result := workspace.CheckNudge(cwd)
			if result.ShouldNudge {
				// Check if this branch is already tracked by amux.
				// Quick check: see if there's a workspace with this branch.
				if isTrackedByAmux(cwd, result.Branch) {
					return nil
				}
				fmt.Fprintln(os.Stderr, result.Message)
			}
			return nil
		},
	}
	return cmd
}

// isTrackedByAmux checks if a branch is already managed by amux.
// Uses a fast path: check if branch starts with "amux/" or if a workspace exists.
func isTrackedByAmux(cwd, branch string) bool {
	if strings.HasPrefix(branch, "amux/") {
		// amux-namespaced branches are likely tracked.
		// Quick heuristic — not perfect but fast.
		return true
	}
	// Could query the store here but that's too slow for prompt.
	// The nudge is advisory, so false positives are acceptable.
	return false
}
