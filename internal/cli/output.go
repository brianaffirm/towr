package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Color codes for terminal output.
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorCyan   = "\033[36m"
)

// NoColor returns true if color output should be suppressed.
func NoColor() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

// colorize wraps text in ANSI color codes, respecting NO_COLOR.
func colorize(text, color string) string {
	if NoColor() {
		return text
	}
	return color + text + colorReset
}

// StatusColor returns the appropriate color for a workspace status.
func StatusColor(status string) string {
	switch status {
	case "READY", "LANDED":
		return colorGreen
	case "RUNNING", "VALIDATING":
		return colorYellow
	case "BLOCKED", "ORPHANED":
		return colorRed
	case "ARCHIVED":
		return colorDim
	default:
		return ""
	}
}

// ColorStatus returns a status string with appropriate color.
func ColorStatus(status string) string {
	c := StatusColor(status)
	if c == "" {
		return status
	}
	return colorize(status, c)
}

// Column defines a table column.
type Column struct {
	Header string
	Width  int
}

// TablePrinter outputs aligned table rows to a writer.
type TablePrinter struct {
	w       io.Writer
	columns []Column
}

// NewTablePrinter creates a table printer for the given writer and columns.
func NewTablePrinter(w io.Writer, columns []Column) *TablePrinter {
	return &TablePrinter{w: w, columns: columns}
}

// PrintHeader prints the table header row.
func (t *TablePrinter) PrintHeader() {
	var parts []string
	for _, col := range t.columns {
		parts = append(parts, pad(colorize(col.Header, colorBold), col.Width))
	}
	fmt.Fprintln(t.w, strings.Join(parts, "  "))
}

// PrintRow prints a single data row. Values are strings; color is applied to the status column.
func (t *TablePrinter) PrintRow(values []string) {
	var parts []string
	for i, col := range t.columns {
		val := ""
		if i < len(values) {
			val = values[i]
		}
		parts = append(parts, pad(val, col.Width))
	}
	fmt.Fprintln(t.w, strings.Join(parts, "  "))
}

// pad right-pads s to width. If s contains ANSI codes, it accounts for their invisible width.
func pad(s string, width int) string {
	visible := stripANSI(s)
	if len(visible) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(visible))
}

// stripANSI removes ANSI escape sequences to compute visible length.
func stripANSI(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// FormatAge formats a time as a human-readable age string.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// FormatAgeFromString parses an RFC3339 timestamp and formats it as age.
func FormatAgeFromString(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return "-"
		}
	}
	return FormatAge(t)
}

// FormatDiff formats added/removed line counts as a colored diff string.
func FormatDiff(added, removed int) string {
	if added == 0 && removed == 0 {
		return colorize("-", colorDim)
	}
	plus := fmt.Sprintf("+%d", added)
	minus := fmt.Sprintf("-%d", removed)
	return colorize(plus, colorGreen) + "/" + colorize(minus, colorRed)
}

// FormatWorktreeStatus formats staged/unstaged/untracked counts as a compact string.
// Examples: "clean", "~3" (unstaged), "+1" (staged), "~3 +1" (both), "~3 +1 ?2" (all).
func FormatWorktreeStatus(staged, unstaged, untracked int) string {
	if staged == 0 && unstaged == 0 && untracked == 0 {
		return colorize("clean", colorDim)
	}
	var parts []string
	if unstaged > 0 {
		parts = append(parts, colorize(fmt.Sprintf("~%d", unstaged), colorYellow))
	}
	if staged > 0 {
		parts = append(parts, colorize(fmt.Sprintf("+%d", staged), colorGreen))
	}
	if untracked > 0 {
		parts = append(parts, colorize(fmt.Sprintf("?%d", untracked), colorDim))
	}
	return strings.Join(parts, " ")
}

// FormatMergeStatus returns a short indicator of whether a branch is merged.
func FormatMergeStatus(merged bool) string {
	if merged {
		return colorize("merged", colorGreen)
	}
	return ""
}

// PrintJSON marshals v to JSON and writes it to stdout.
func PrintJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
