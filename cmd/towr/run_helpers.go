package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/control"
	"github.com/brianaffirm/towr/internal/cost"
	"github.com/brianaffirm/towr/internal/mux"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/router"
)

// stdLog implements control.Logger with colored timestamped stdout output.
type stdLog struct{}

func (l *stdLog) Log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := ansiDim + time.Now().Format("15:04:05") + ansiReset
	colored := colorizeLogMsg(msg)
	fmt.Printf(" %s  %s\n", ts, colored)
}

// colorizeLogMsg applies color based on log message content.
func colorizeLogMsg(msg string) string {
	switch {
	case strings.HasPrefix(msg, "completed"):
		return ansiGreen + "✓ " + msg + ansiReset
	case strings.HasPrefix(msg, "dispatched"):
		return ansiCyan + "▸ " + msg + ansiReset
	case strings.HasPrefix(msg, "escalating"):
		return ansiYellow + "↑ " + msg + ansiReset
	case strings.Contains(msg, "failed") || strings.Contains(msg, "error"):
		return ansiRed + "✗ " + msg + ansiReset
	case strings.HasPrefix(msg, "reconciled"):
		return ansiDim + "○ " + msg + ansiReset
	case strings.HasPrefix(msg, "budget"):
		return ansiYellow + "$ " + msg + ansiReset
	default:
		return msg
	}
}

func buildRunRequest(repoRoot string, plan *orchestrate.Plan) control.RunRequest {
	tasks := make([]control.TaskSpec, len(plan.Tasks))
	for i, t := range plan.Tasks {
		tasks[i] = control.TaskSpec{
			ID: t.ID, Prompt: t.Prompt, DependsOn: t.DependsOn,
			Agent: t.Agent, Model: t.Model,
		}
	}
	return control.RunRequest{
		RepoRoot:    repoRoot,
		PlanName:    plan.Name,
		PlanContent: plan.RawYAML(),
		Tasks:       tasks,
		Settings: control.SettingsSnapshot{
			DefaultAgent:   plan.Settings.DefaultAgent,
			DefaultModel:   plan.Settings.DefaultModel,
			AutoApprove:    plan.Settings.AutoApprove,
			MaxRetries:     plan.Settings.MaxRetries,
			PollInterval:   parsePollInterval(plan),
			CreatePR:       plan.Settings.CreatePR,
			ReactToReviews: plan.Settings.ReactToReviews,
			FullAuto:       plan.Settings.FullAuto,
			Budget:         plan.Settings.Budget,
			Web:            plan.Settings.Web,
			WebAddr:        plan.Settings.WebAddr,
			BaseBranch:     plan.Settings.BaseBranch,
		},
		Options: control.RunOptions{
			Budget:   plan.Settings.Budget,
			FullAuto: plan.Settings.FullAuto,
		},
	}
}

// ANSI color constants for run output.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiRed    = "\033[31m"
)


func formatPlanYAML(plan *orchestrate.Plan) string {
	raw := plan.RawYAML()
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		colored := highlightYAMLLine(line)
		b.WriteString("  " + colored + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// highlightYAMLLine applies simple YAML syntax highlighting to a line.
func highlightYAMLLine(line string) string {
	trimmed := strings.TrimSpace(line)
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

	switch {
	case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		return indent + ansiDim + trimmed + ansiReset
	case strings.HasPrefix(trimmed, "- id:"):
		return indent + ansiCyan + ansiBold + trimmed + ansiReset
	case strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "-"):
		parts := strings.SplitN(trimmed, ":", 2)
		return indent + ansiCyan + parts[0] + ansiReset + ":" + parts[1]
	default:
		return line
	}
}


func formatDryRun(planName string, items []control.PreRunItem) string {
	preItems := make([]cost.PreRunItem, len(items))
	for i, item := range items {
		preItems[i] = cost.PreRunItem{
			TaskID: item.TaskID,
			Decision: router.Decision{
				Model:           item.Decision.Model,
				Reason:          item.Decision.Reason,
				Tier:            item.Decision.Tier,
				CanEscalate:     item.Decision.CanEscalate,
				RequireApproval: item.Decision.RequireApproval,
			},
			EstCost: cost.Calculate(item.Decision.Model, cost.DefaultEstimate()),
		}
	}
	name := planName
	if name == "" {
		name = "plan"
	}
	return cost.FormatPreRun(name, preItems)
}

func startWebDashboard(addr string) {
	if addr == "" {
		addr = ":8090"
	}
	go func() {
		towrBin, _ := os.Executable()
		cmd := exec.Command(towrBin, "web", "--addr", addr)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}()
	fmt.Printf("[%s] Web dashboard: http://127.0.0.1%s\n", time.Now().Format("15:04:05"), addr)
}

func startMuxStatusUpdater(planName string, handle *control.RunHandle, rt *controlRuntime, tasks []orchestrate.Task) {
	session := mux.DefaultSessionName
	if !mux.SessionExists(session) {
		return
	}
	// Set plan name immediately.
	if planName != "" {
		_ = mux.SetSessionEnv(session, "TOWR_PLAN", planName)
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		startTime := time.Now()
		for range ticker.C {
			elapsed := int(time.Since(startTime).Minutes())
			_ = mux.SetSessionEnv(session, "TOWR_ELAPSED", fmt.Sprintf("%d", elapsed))
			if handle != nil {
				var completed int
				for _, st := range handle.TaskStates {
					if st == "completed" {
						completed++
					}
				}
				_ = mux.SetSessionEnv(session, "TOWR_COMPLETED", fmt.Sprintf("%d", completed))
			}
			// Compute aggregate cost across all tasks.
			if rt != nil {
				var totalCost float64
				for _, t := range tasks {
					model := t.Model
					if model == "" {
						model = "sonnet"
					}
					_, _, _, actual, _ := rt.ComputeCost(t.ID, model)
					totalCost += actual
				}
				if totalCost > 0 {
					_ = mux.SetSessionEnv(session, "TOWR_COST", fmt.Sprintf("%.2f", totalCost))
				}
			}
			_ = mux.UpdateStatusBar(session)
		}
	}()
}

func cleanupMuxEnv() {
	session := mux.DefaultSessionName
	if !mux.SessionExists(session) {
		return
	}
	_ = mux.SetSessionEnv(session, "TOWR_PLAN", "")
	_ = mux.SetSessionEnv(session, "TOWR_COST", "")
	_ = mux.SetSessionEnv(session, "TOWR_ELAPSED", "")
	_ = mux.SetSessionEnv(session, "TOWR_COMPLETED", "")
	_ = mux.UpdateStatusBar(session)
}

func parsePollInterval(plan *orchestrate.Plan) time.Duration {
	if plan.Settings.PollInterval != "" {
		if d, err := time.ParseDuration(plan.Settings.PollInterval); err == nil {
			return d
		}
	}
	return 10 * time.Second
}

func runWatchReact(app *appContext, interval time.Duration) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	fmt.Printf("[%s] Watching PRs for reviews and CI... (Ctrl-C to stop)\n", time.Now().Format("15:04:05"))
	towrBin, _ := os.Executable()
	cmd := exec.Command(towrBin, "watch", "--auto-approve", "--react", "--interval", interval.String())
	cmd.Dir = app.repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	go func() {
		<-sigCh
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGINT)
		}
	}()
	_ = cmd.Run()
}
