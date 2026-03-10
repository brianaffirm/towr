package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/brianaffirm/towr/internal/git"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/terminal"
	"github.com/spf13/cobra"
)

func newPreviewCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		diffFlag  bool
		closeFlag bool
	)

	cmd := &cobra.Command{
		Use:   "preview [file]",
		Short: "Show file or diff in a tmux preview pane",
		Long: `Push file contents or diffs to a tmux preview pane adjacent to the chat.
Agents call this after making changes — no special protocol needed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			if app.term.IsHeadless() {
				return fmt.Errorf("preview requires tmux (headless mode)")
			}

			// Determine which workspace we're in by checking cwd against worktree paths.
			ws, err := findCurrentWorkspace(app)
			if err != nil {
				return err
			}

			tmuxBackend, ok := app.term.(*terminal.TmuxBackend)
			if !ok {
				return fmt.Errorf("preview requires tmux backend")
			}

			if closeFlag {
				return closePreviewPane(tmuxBackend, ws.ID)
			}

			// Build the content command to display.
			// For diffs: show worktree diff first, fall back to branch diff.
			var contentCmd string
			if diffFlag {
				if len(args) > 0 {
					// File-specific: worktree diff, then staged, then branch.
					contentCmd = fmt.Sprintf(
						"{ git -C %s diff --color -- %s 2>/dev/null || true; git -C %s diff --cached --color -- %s 2>/dev/null || true; } | less -R",
						shellQuote(ws.WorktreePath), shellQuote(args[0]),
						shellQuote(ws.WorktreePath), shellQuote(args[0]))
				} else {
					// Full workspace diff: worktree + staged, fall back to branch diff.
					contentCmd = fmt.Sprintf(
						"{ wt=$(git -C %s diff --color 2>/dev/null); st=$(git -C %s diff --cached --color 2>/dev/null); "+
							"if [ -n \"$wt\" ] || [ -n \"$st\" ]; then "+
							"[ -n \"$st\" ] && echo '=== Staged ===' && echo \"$st\"; "+
							"[ -n \"$wt\" ] && echo '=== Unstaged ===' && echo \"$wt\"; "+
							"else git -C %s diff --color %s...%s 2>/dev/null; fi; } | less -R",
						shellQuote(ws.WorktreePath), shellQuote(ws.WorktreePath),
						shellQuote(ws.WorktreePath), shellQuote(ws.BaseBranch), shellQuote(ws.Branch))
				}
			} else {
				if len(args) == 0 {
					return fmt.Errorf("file argument required (or use --diff)")
				}
				contentCmd = fmt.Sprintf("less -N -R %s/%s",
					shellQuote(ws.WorktreePath), shellQuote(args[0]))
			}

			// Build header line.
			header := buildPreviewHeader(ws.ID, diffFlag, args)

			return showInPreviewPane(tmuxBackend, ws.ID, header, contentCmd)
		},
	}

	cmd.Flags().BoolVar(&diffFlag, "diff", false, "show git diff instead of file contents")
	cmd.Flags().BoolVar(&closeFlag, "close", false, "dismiss the preview pane")

	return cmd
}

// findCurrentWorkspace finds the workspace whose worktree contains the current directory.
func findCurrentWorkspace(app *appContext) (*store.Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get cwd: %w", err)
	}

	workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	for _, ws := range workspaces {
		if ws.WorktreePath != "" && strings.HasPrefix(cwd, ws.WorktreePath) {
			return ws, nil
		}
	}

	// If only one active workspace, use it.
	var active []*store.Workspace
	for _, ws := range workspaces {
		if ws.Status != "LANDED" && ws.Status != "ARCHIVED" {
			active = append(active, ws)
		}
	}
	if len(active) == 1 {
		return active[0], nil
	}

	return nil, fmt.Errorf("cannot determine current workspace (run from within a worktree or specify workspace ID)")
}

// previewPaneID returns the tmux pane target for a workspace's preview pane.
func previewPaneID(workspaceID string) string {
	return "towr/" + workspaceID + ":preview"
}

// showInPreviewPane creates a split pane that runs the content command.
// The pane auto-closes when the command (less) exits — no lingering shell.
// If a preview pane already exists, it's killed first.
func showInPreviewPane(tmux *terminal.TmuxBackend, wsID, header, contentCmd string) error {
	session := "towr/" + wsID
	chatWindow := session + ":chat"

	// Kill existing preview pane if present (pane 1).
	checkCmd := exec.Command("tmux", "list-panes", "-t", chatWindow, "-F", "#{pane_index}")
	out, err := checkCmd.CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "1" {
				killCmd := exec.Command("tmux", "kill-pane", "-t", chatWindow+".1")
				_ = killCmd.Run()
				break
			}
		}
	}

	// Make sure the chat window is active/visible.
	_ = exec.Command("tmux", "select-window", "-t", chatWindow).Run()

	// Split right with the content command. When less quits, the pane closes automatically.
	display := fmt.Sprintf("echo '%s' && %s", header, contentCmd)
	splitCmd := exec.Command("tmux", "split-window", "-t", chatWindow, "-h", "-l", "50%", "sh", "-c", display)
	if splitOut, splitErr := splitCmd.CombinedOutput(); splitErr != nil {
		return fmt.Errorf("create preview pane: %s: %w", strings.TrimSpace(string(splitOut)), splitErr)
	}

	fmt.Fprintf(os.Stderr, "Preview opened (split pane)\n")
	return nil
}


// closePreviewPane kills the preview pane for a workspace.
func closePreviewPane(tmux *terminal.TmuxBackend, wsID string) error {
	session := "towr/" + wsID
	previewPane := session + ":chat.1"

	killCmd := exec.Command("tmux", "kill-pane", "-t", previewPane)
	if out, err := killCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("close preview: %s: %w", strings.TrimSpace(string(out)), err)
	}

	fmt.Fprintf(os.Stderr, "Preview closed for %s\n", wsID)
	return nil
}

// buildPreviewHeader creates the header line for the preview pane.
func buildPreviewHeader(wsID string, isDiff bool, args []string) string {
	var filename string
	if len(args) > 0 {
		filename = args[0]
	} else if isDiff {
		filename = "workspace diff"
	}

	// Format: ── w3 │ src/auth/handler.go ──────
	header := fmt.Sprintf("── %s │ %s ", wsID, filename)
	// Pad with ─ to fill ~60 chars.
	pad := 60 - len(header)
	if pad < 3 {
		pad = 3
	}
	header += strings.Repeat("─", pad)
	return header
}

// shellQuote wraps a string in single quotes for shell safety.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// getDiffOutput runs git diff and returns the colored output.
func getDiffOutput(repoRoot, baseBranch, branch string) (string, error) {
	return git.RunGit(repoRoot, "diff", "--color=always", baseBranch+"..."+branch)
}
