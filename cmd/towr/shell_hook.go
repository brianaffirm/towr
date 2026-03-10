package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newShellHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell-hook",
		Short: "Print shell integration code for towr nudge",
		Long: `Print shell hook code to integrate towr nudge into your prompt.
Add to your shell config:
  eval "$(towr shell-hook)"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := filepath.Base(os.Getenv("SHELL"))

			// Find towr binary path.
			towrBin, err := os.Executable()
			if err != nil {
				towrBin = "towr"
			}

			switch shell {
			case "zsh":
				fmt.Printf(`# towr shell integration
_towr_nudge() {
  %s _nudge 2>/dev/null
}
precmd_functions+=(_towr_nudge)
`, towrBin)
			case "bash":
				fmt.Printf(`# towr shell integration
_towr_nudge() {
  %s _nudge 2>/dev/null
}
PROMPT_COMMAND="_towr_nudge${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
`, towrBin)
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
				// Check if this branch is already tracked by towr.
				// Quick check: see if there's a workspace with this branch.
				if isTrackedByTowr(cwd, result.Branch) {
					return nil
				}
				fmt.Fprintln(os.Stderr, result.Message)
			}
			return nil
		},
	}
	return cmd
}

// isTrackedByTowr checks if a branch is already managed by towr.
// Uses a fast path: check if branch starts with "towr/" or if a workspace exists.
func isTrackedByTowr(cwd, branch string) bool {
	if strings.HasPrefix(branch, "towr/") {
		// towr-namespaced branches are likely tracked.
		// Quick heuristic — not perfect but fast.
		return true
	}
	// Could query the store here but that's too slow for prompt.
	// The nudge is advisory, so false positives are acceptable.
	return false
}
