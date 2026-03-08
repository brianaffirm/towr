package main

import (
	"fmt"
	"os"

	"github.com/brianho/amux/internal/cli"
	"github.com/brianho/amux/internal/store"
	"github.com/spf13/cobra"
)

func newQueueCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "queue",
		Aliases: []string{"q"},
		Short:   "Show pending approval items",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			items, err := app.store.GetQueue(app.repoRoot)
			if err != nil {
				return fmt.Errorf("get queue: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(items)
			}

			if len(items) == 0 {
				fmt.Println("No pending approval items.")
				return nil
			}

			table := cli.NewTablePrinter(os.Stdout, []cli.Column{
				{Header: "PRI", Width: 4},
				{Header: "WORKSPACE", Width: 14},
				{Header: "TYPE", Width: 14},
				{Header: "AGE", Width: 6},
				{Header: "SUMMARY", Width: 40},
			})
			table.PrintHeader()

			for _, item := range items {
				pri := prioritySymbol(item.Priority)
				table.PrintRow([]string{
					pri,
					item.WorkspaceID,
					item.Type,
					cli.FormatAgeFromString(item.CreatedAt),
					item.Summary,
				})
			}

			return nil
		},
	}

	return cmd
}

func newApproveCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve <queue-id>",
		Short: "Approve a pending queue item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			queueID := args[0]
			resolution := store.Resolution{
				Action:     "approved",
				ResolvedBy: "cli",
			}

			if err := app.store.ResolveQueueItem(queueID, resolution); err != nil {
				return fmt.Errorf("approve failed: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]string{
					"action": "approved",
					"id":     queueID,
				})
			}

			fmt.Printf("Approved: %s\n", queueID)
			return nil
		},
	}

	return cmd
}

func newDenyCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deny <queue-id>",
		Short: "Deny a pending queue item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			queueID := args[0]
			resolution := store.Resolution{
				Action:     "denied",
				ResolvedBy: "cli",
			}

			if err := app.store.ResolveQueueItem(queueID, resolution); err != nil {
				return fmt.Errorf("deny failed: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]string{
					"action": "denied",
					"id":     queueID,
				})
			}

			fmt.Printf("Denied: %s\n", queueID)
			return nil
		},
	}

	return cmd
}

func newRespondCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "respond <queue-id> <message>",
		Short: "Respond to a pending queue item with a message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			queueID := args[0]
			message := args[1]

			resolution := store.Resolution{
				Action:     "responded",
				ResolvedBy: "cli",
				Response:   message,
			}

			if err := app.store.ResolveQueueItem(queueID, resolution); err != nil {
				return fmt.Errorf("respond failed: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(map[string]string{
					"action":   "responded",
					"id":       queueID,
					"response": message,
				})
			}

			fmt.Printf("Responded to %s: %s\n", queueID, message)
			return nil
		},
	}

	return cmd
}

func prioritySymbol(p string) string {
	switch p {
	case "critical":
		return "!!"
	case "high":
		return "!"
	default:
		return "."
	}
}
