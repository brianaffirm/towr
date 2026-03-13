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

func FormatPreRun(planName string, items []PreRunItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nPlan: %s (%d tasks)\n\n", planName, len(items))
	fmt.Fprintf(&b, "  %-20s %-8s %-28s %s\n", "Task", "Model", "Reason", "Est. Cost")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("─", 70))

	var total, opusTotal float64
	for _, item := range items {
		opusEst := Calculate("opus", DefaultEstimate())
		fmt.Fprintf(&b, "  %-20s %-8s %-28s ~$%.2f\n", item.TaskID, item.Decision.Model, item.Decision.Reason, item.EstCost)
		total += item.EstCost
		opusTotal += opusEst
	}

	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "  Estimated:  ~$%.2f\n", total)
	fmt.Fprintf(&b, "  All-opus:   ~$%.2f\n", opusTotal)
	if opusTotal > 0 {
		pct := (opusTotal - total) / opusTotal * 100
		fmt.Fprintf(&b, "  Savings:    ~%.0f%%\n", pct)
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
