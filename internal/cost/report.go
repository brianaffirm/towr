package cost

import (
	"fmt"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/router"
)

type PreRunItem struct {
	TaskID   string
	Decision router.Decision
	EstCost  float64
}

type PostRunItem struct {
	TaskID     string
	Model      string
	Usage      TokenUsage
	ActualCost float64
	OpusCost   float64
}

// ANSI color helpers.
const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cBlue   = "\033[34m"
	cCyan   = "\033[36m"
	cWhite  = "\033[37m"
)

// modelColor returns an ANSI color for a model name.
func modelColor(model string) string {
	switch {
	case strings.Contains(model, "opus"):
		return "\033[35m" // magenta
	case strings.Contains(model, "sonnet"):
		return cBlue
	case strings.Contains(model, "haiku"):
		return cCyan
	case strings.Contains(model, "codex"):
		return cYellow
	default:
		return cWhite
	}
}

func FormatPreRun(planName string, items []PreRunItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s%s▸ Plan: %s%s (%d tasks)\n\n", cBold, cCyan, planName, cReset, len(items))
	fmt.Fprintf(&b, "  %s%-20s %-10s %-24s %s%s\n", cDim, "Task", "Model", "Reason", "Est. Cost", cReset)
	fmt.Fprintf(&b, "  %s%s%s\n", cDim, strings.Repeat("─", 66), cReset)

	var total, opusTotal float64
	for _, item := range items {
		opusEst := Calculate("opus", DefaultEstimate())
		mc := modelColor(item.Decision.Model)
		fmt.Fprintf(&b, "  %s%-20s%s %s%-10s%s %s%-24s%s %s~$%.2f%s\n",
			cBold, item.TaskID, cReset,
			mc, item.Decision.Model, cReset,
			cDim, item.Decision.Reason, cReset,
			cYellow, item.EstCost, cReset)
		total += item.EstCost
		opusTotal += opusEst
	}

	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "  %sEstimated:%s  %s~$%.2f%s\n", cDim, cReset, cYellow+cBold, total, cReset)
	fmt.Fprintf(&b, "  %sAll-opus:%s   %s~$%.2f%s\n", cDim, cReset, cDim, opusTotal, cReset)
	if opusTotal > 0 {
		pct := (opusTotal - total) / opusTotal * 100
		fmt.Fprintf(&b, "  %sSavings:%s    %s~%.0f%%%s\n", cDim, cReset, cGreen+cBold, pct, cReset)
	}
	return b.String()
}

func FormatPostRun(items []PostRunItem, totalTasks int, duration time.Duration) string {
	var b strings.Builder

	succeeded := len(items)
	fmt.Fprintf(&b, "\nRun complete: %d/%d tasks succeeded (%s)\n\n", succeeded, totalTasks, formatDuration(duration))
	fmt.Fprintf(&b, "  %-20s %-8s %-18s %-9s %s\n", "Task", "Model", "Tokens (in/out)", "Cost", "Saved")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("─", 70))

	var totalCost, totalOpus float64
	for _, item := range items {
		tokenStr := fmt.Sprintf("%s / %s", fmtTokens(item.Usage.InputTokens), fmtTokens(item.Usage.OutputTokens))
		saved := "—"
		if item.Model != "opus" && item.OpusCost > item.ActualCost {
			saved = fmt.Sprintf("$%.2f", item.OpusCost-item.ActualCost)
		}
		fmt.Fprintf(&b, "  %-20s %-8s %-18s $%-8.2f %s\n", item.TaskID, item.Model, tokenStr, item.ActualCost, saved)
		totalCost += item.ActualCost
		totalOpus += item.OpusCost
	}

	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "  Total:      $%.2f\n", totalCost)
	fmt.Fprintf(&b, "  All-opus:   $%.2f\n", totalOpus)
	totalSaved := totalOpus - totalCost
	if totalOpus > 0 {
		pct := totalSaved / totalOpus * 100
		fmt.Fprintf(&b, "  Saved:      $%.2f (%.0f%%)\n", totalSaved, pct)
	}
	return b.String()
}

func fmtTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
