package main

import (
	"fmt"
	"path/filepath"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICmd(initApp func() (*appContext, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "tui",
		Short:  "Open interactive TUI dashboard",
		Hidden: true, // Also invoked as the default command (no subcommand).
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(initApp)
		},
	}
	return cmd
}

func runTUI(initApp func() (*appContext, error)) error {
	app, appErr := initApp()

	reposDir := filepath.Join(config.TowrHome(), "repos")
	var repoRoot, repoStorePath string
	var allRepos bool

	if appErr != nil {
		// Not inside a repo — show all workspaces.
		allRepos = true
	} else {
		repoRoot = app.repoRoot
		repoState := config.RepoStatePath(repoRoot)
		repoStorePath = filepath.Join(repoState, "state.db")
	}

	model := tui.NewDashboardModel(repoRoot, repoStorePath, reposDir, allRepos)
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	_ = finalModel
	return nil
}
