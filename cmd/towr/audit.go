package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/spf13/cobra"
)

func newAuditCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		csvFlag       bool
		sinceFlag     string
		untilFlag     string
		workspaceFlag string
		kindFlag      string
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Export audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			query := store.EventQuery{
				RepoRoot: app.repoRoot,
			}

			if workspaceFlag != "" {
				query.WorkspaceID = workspaceFlag
			}
			if kindFlag != "" {
				query.Kind = kindFlag
			}

			if sinceFlag != "" {
				t, err := parseSinceFlag(sinceFlag)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", sinceFlag, err)
				}
				query.Since = &t
			}
			if untilFlag != "" {
				t, err := time.Parse("2006-01-02", untilFlag)
				if err != nil {
					return fmt.Errorf("invalid --until value %q: expected YYYY-MM-DD", untilFlag)
				}
				// End of day.
				t = t.Add(24*time.Hour - time.Nanosecond)
				query.Until = &t
			}

			events, err := app.store.QueryEvents(query)
			if err != nil {
				return fmt.Errorf("query events: %w", err)
			}

			if *jsonFlag {
				return cli.PrintJSON(events)
			}

			if csvFlag {
				return writeCSV(events)
			}

			return writeTable(events)
		},
	}

	cmd.Flags().BoolVar(&csvFlag, "csv", false, "output CSV")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "filter events after duration (24h) or date (2006-01-02)")
	cmd.Flags().StringVar(&untilFlag, "until", "", "filter events before date (2006-01-02)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "filter by workspace ID")
	cmd.Flags().StringVar(&kindFlag, "kind", "", "filter by event kind")

	return cmd
}

// parseSinceFlag parses a --since value as either a Go duration ("24h") or a date ("2006-01-02").
func parseSinceFlag(s string) (time.Time, error) {
	// Try duration first.
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	// Try date.
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected duration (e.g. 24h) or date (YYYY-MM-DD)")
	}
	return t, nil
}

// isBypassEvent returns true if the event kind indicates a bypass (forced or hooks_skipped).
func isBypassEvent(kind string) bool {
	lower := strings.ToLower(kind)
	return strings.Contains(lower, "forced") || strings.Contains(lower, "hooks_skipped")
}

// formatKind returns the display string for an event kind, prefixing bypass events.
func formatKind(kind string) string {
	if isBypassEvent(kind) {
		return "[BYPASS] " + kind
	}
	return kind
}

// dataSummary returns a compact string representation of event data.
func dataSummary(data map[string]interface{}) string {
	return formatEventData(data)
}

func writeTable(events []Event) error {
	if len(events) == 0 {
		fmt.Println("No audit events found.")
		return nil
	}

	columns := []cli.Column{
		{Header: "TIMESTAMP", Width: 20},
		{Header: "WORKSPACE", Width: 14},
		{Header: "KIND", Width: 35},
		{Header: "ACTOR", Width: 12},
		{Header: "DATA", Width: 40},
	}

	table := cli.NewTablePrinter(os.Stdout, columns)
	table.PrintHeader()

	for _, e := range events {
		ts := e.Timestamp.Format("2006-01-02 15:04:05")
		actor := e.Actor
		if actor == "" {
			actor = "-"
		}
		table.PrintRow([]string{
			ts,
			e.WorkspaceID,
			formatKind(e.Kind),
			actor,
			dataSummary(e.Data),
		})
	}
	return nil
}

func writeCSV(events []Event) error {
	w := csv.NewWriter(os.Stdout)

	if err := w.Write([]string{"timestamp", "workspace_id", "kind", "actor", "data"}); err != nil {
		return err
	}

	for _, e := range events {
		row := []string{
			e.Timestamp.Format(time.RFC3339),
			e.WorkspaceID,
			formatKind(e.Kind),
			e.Actor,
			dataSummary(e.Data),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}

	w.Flush()
	return w.Error()
}

// Event is a type alias to avoid repeating the import in function signatures.
type Event = store.Event
