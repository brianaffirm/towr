package main

import (
	"fmt"

	"github.com/brianho/amux/internal/cli"
	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/workspace"
	"github.com/spf13/cobra"
)

func newOverlapCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overlap",
		Short: "Detect file overlaps between workspaces",
		Long:  "Show which workspaces are editing the same files, indicating merge conflict risk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			workspaces, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
			if err != nil {
				return fmt.Errorf("list workspaces: %w", err)
			}

			overlaps := workspace.DetectOverlaps(workspaces)

			if *jsonFlag {
				return cli.PrintJSON(overlaps)
			}

			if len(overlaps) == 0 {
				fmt.Println("No file overlaps detected.")
				return nil
			}

			for _, o := range overlaps {
				fmt.Printf("%s ↔ %s: %d shared file(s)\n", o.WorkspaceA, o.WorkspaceB, len(o.Files))
				for _, f := range o.Files {
					fmt.Printf("  %s\n", f)
				}
				fmt.Println()
			}
			return nil
		},
	}
	return cmd
}
