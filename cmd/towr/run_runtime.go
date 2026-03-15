package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/agent"
	"github.com/brianaffirm/towr/internal/control"
	"github.com/brianaffirm/towr/internal/cost"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/workspace"
)

// controlRuntime adapts appContext to the control.AgentRuntime interface.
type controlRuntime struct {
	app        *appContext
	baseBranch string // optional override from plan settings
}

func (r *controlRuntime) SpawnWorkspace(taskID, prompt, agentType, repoRoot string, depIDs []string) error {
	baseBranch := r.baseBranch
	if baseBranch == "" {
		baseBranch = "main"
		if detected, err := workspace.DetectDefaultBranch(repoRoot); err == nil {
			baseBranch = detected
		}
	}
	runtimeName := agentType
	if runtimeName == "" {
		runtimeName = "claude-code"
	}
	ws, err := r.app.manager.Create(workspace.CreateOpts{
		ID:         taskID,
		RepoRoot:   repoRoot,
		BaseBranch: baseBranch,
		Source:     workspace.SpawnSource{Kind: workspace.SpawnFromTask, Value: prompt},
		Agent:      &workspace.AgentIdentity{Runtime: runtimeName},
		CopyPaths:  r.app.cfg.Workspace.CopyPaths,
		LinkPaths:  r.app.cfg.Workspace.LinkPaths,
	})
	if err != nil {
		return err
	}
	if !r.app.term.IsHeadless() {
		_ = r.app.term.CreatePane(ws.ID, ws.WorktreePath, "")
	}
	for _, dep := range depIDs {
		branch := "towr/" + dep
		cmd := exec.Command("git", "-C", ws.WorktreePath, "merge", branch, "--no-edit", "-m",
			fmt.Sprintf("merge dependency %s", dep))
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("[%s] warning: %s merge dep %s failed — %s\n",
				time.Now().Format("15:04:05"), taskID, dep, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (r *controlRuntime) LaunchAndMonitor(taskID, prompt string, decision control.RoutingDecision, agentType string, fullAuto bool, done <-chan struct{}) {
	ag := agent.GetWithOpts(decision.Model, agentType, fullAuto)
	go func() {
		_ = r.app.term.SendInput(taskID, ag.LaunchCommand())
		for i := 0; i < 40; i++ {
			select {
			case <-done:
				return
			default:
			}
			time.Sleep(1500 * time.Millisecond)
			captured, _ := r.app.term.CaptureOutput(taskID, 50)
			if captured == "" {
				continue
			}
			for _, p := range ag.StartupDialogs() {
				if strings.Contains(captured, p) {
					_ = r.app.term.Approve(taskID, ag.StartupKey())
					time.Sleep(1 * time.Second)
					break
				}
			}
			if strings.Contains(captured, ag.IdlePattern()) {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
		_ = r.app.term.SendInput(taskID, prompt)
		if decision.RequireApproval {
			<-done
			return
		}
		indicators := ag.DialogIndicators()
		for {
			select {
			case <-done:
				return
			default:
			}
			time.Sleep(3 * time.Second)
			captured, err := r.app.term.CaptureOutput(taskID, 200)
			if err != nil {
				continue
			}
			for _, pattern := range indicators {
				if strings.Contains(captured, pattern) {
					approveKey := "Enter"
					if strings.Contains(captured, "Run this command?") || strings.Contains(captured, "Run (once)") {
						approveKey = "y"
					} else if strings.Contains(captured, "Trust this workspace") {
						approveKey = "a"
					}
					_ = r.app.term.Approve(taskID, approveKey)
					break
				}
			}
		}
	}()
}

func (r *controlRuntime) DetectState(taskID string) (string, string, error) {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, taskID)
	if err != nil || sw == nil {
		return "", "", fmt.Errorf("workspace not found")
	}
	ag := agent.Get(sw.AgentRuntime)
	if sw.WorktreePath != "" {
		jState, jSummary, jErr := ag.DetectActivity(sw.WorktreePath)
		if jErr == nil && dispatch.PaneState(jState) != dispatch.PaneEmpty {
			return jState, jSummary, nil
		}
	}
	captured, captErr := r.app.term.CaptureOutput(taskID, 200)
	if captErr != nil {
		return "", "", captErr
	}
	lastActivity := r.app.term.LastActivity(taskID)
	state := dispatch.DetectPaneStateWithPatterns(captured, ag.DialogIndicators(), ag.IdlePattern(), lastActivity, 15*time.Second)
	summary := dispatch.ExtractLastResponse(captured)
	return string(state), summary, nil
}

func (r *controlRuntime) ApproveDialog(taskID string) error {
	return r.app.term.Approve(taskID, "Enter")
}

func (r *controlRuntime) AutoCommit(taskID string) error {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, taskID)
	if err != nil || sw == nil || sw.WorktreePath == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", sw.WorktreePath, "status", "--porcelain")
	out, _ := cmd.Output()
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	_ = exec.Command("git", "-C", sw.WorktreePath, "add", "-A").Run()
	return exec.Command("git", "-C", sw.WorktreePath, "commit", "-m",
		fmt.Sprintf("feat(%s): auto-commit from towr run", taskID)).Run()
}

func (r *controlRuntime) CreatePR(taskID string) error {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, taskID)
	if err != nil || sw == nil {
		return fmt.Errorf("workspace not found")
	}
	towrBin, _ := os.Executable()
	cmd := exec.Command(towrBin, "land", taskID, "--pr", "--force", "--repo", r.app.repoRoot)
	cmd.Dir = r.app.repoRoot
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("land --pr: %w", err)
	}
	branch := "towr/" + taskID
	ghCmd := exec.Command("gh", "pr", "create",
		"--base", sw.BaseBranch, "--head", branch,
		"--title", fmt.Sprintf("feat(%s): from towr run", taskID),
		"--body", fmt.Sprintf("Auto-generated by `towr run`.\n\nTask: %s", taskID))
	ghCmd.Dir = r.app.repoRoot
	if out, err := ghCmd.CombinedOutput(); err == nil {
		prURL := strings.TrimSpace(string(out))
		fmt.Printf("[%s] PR created: %s — %s\n", time.Now().Format("15:04:05"), taskID, prURL)
	}
	return nil
}

func (r *controlRuntime) GetWorktreePath(taskID string) string {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, taskID)
	if err != nil || sw == nil {
		return ""
	}
	return sw.WorktreePath
}

func (r *controlRuntime) ComputeCost(taskID, model string) (int, int, string, float64, float64) {
	sw, err := r.app.store.GetWorkspace(r.app.repoRoot, taskID)
	if err != nil || sw == nil {
		return 0, 0, "unavailable", 0, 0
	}
	var usage cost.TokenUsage
	switch {
	case sw.WorktreePath != "" && (sw.AgentRuntime == "" || sw.AgentRuntime == "claude-code"):
		usage, _ = cost.ParseClaudeTokens(sw.WorktreePath)
	case sw.WorktreePath != "" && sw.AgentRuntime == "codex":
		usage, _ = cost.ParseCodexTokens(sw.WorktreePath)
		if usage.Source == "unavailable" {
			usage = cost.DefaultEstimate()
			usage.Source = "codex-estimated"
		}
	default:
		usage = cost.DefaultEstimate()
	}
	actualCost := cost.Calculate(model, usage)
	opusCost := cost.Calculate("opus", usage)
	return usage.InputTokens, usage.OutputTokens, usage.Source, actualCost, opusCost
}

func (r *controlRuntime) IsHeadless() bool {
	return r.app.term.IsHeadless()
}
