package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/agent"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newRunCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <plan.yaml>",
		Short: "Execute a plan: spawn, dispatch, approve, PR, watch — all in one",
		Long: `The single command for overnight workflows. Replaces orchestrate + watch + land.

Reads a YAML plan, spawns workspaces, dispatches to agents (with model routing),
auto-approves permissions, creates PRs on completion, and optionally monitors
PRs for CI failures and review comments.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			plan, err := orchestrate.LoadPlan(args[0])
			if err != nil {
				return err
			}
			if err := plan.Validate(); err != nil {
				return fmt.Errorf("invalid plan: %w", err)
			}

			// Backward compat: land_pr → create_pr
			if plan.Settings.LandPR && !plan.Settings.CreatePR {
				plan.Settings.CreatePR = true
			}

			return runPlan(app, plan, jsonFlag)
		},
	}
	return cmd
}

type runTaskState struct {
	status    string // pending, spawning, running, completed, failed
	retries   int
	dispID    string
	startedAt time.Time // when dispatch started — grace period before checking
}

func runPlan(app *appContext, plan *orchestrate.Plan, jsonFlag *bool) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Defaults.
	pollInterval := 10 * time.Second
	if plan.Settings.PollInterval != "" {
		if d, err := time.ParseDuration(plan.Settings.PollInterval); err == nil {
			pollInterval = d
		}
	}
	maxRetries := plan.Settings.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	// Start web dashboard if requested.
	if plan.Settings.Web {
		addr := plan.Settings.WebAddr
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
		fmt.Printf("[%s] Web dashboard: http://127.0.0.1%s\n", fmtTime(), addr)
	}

	// Task state tracking.
	states := make(map[string]*runTaskState)
	for _, t := range plan.Tasks {
		states[t.ID] = &runTaskState{status: "pending"}
	}

	name := plan.Name
	if name == "" {
		name = "plan"
	}
	fmt.Printf("[%s] Running %q — %d tasks\n", fmtTime(), name, len(plan.Tasks))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Printf("\n[%s] Interrupted. Summary:\n", fmtTime())
			printRunSummary(states)
			return nil
		case <-ticker.C:
			// 1. Dispatch ready tasks.
			for _, task := range plan.Tasks {
				st := states[task.ID]
				if st.status != "pending" {
					continue
				}
				// Check deps.
				ready := true
				for _, dep := range task.DependsOn {
					if states[dep].status != "completed" {
						ready = false
						break
					}
				}
				if !ready {
					continue
				}
				// Spawn + dispatch.
				runSpawnAndDispatch(app, plan, &task, st)
			}

			// 2. Check running tasks.
			for _, task := range plan.Tasks {
				st := states[task.ID]
				if st.status != "running" {
					continue
				}
				runCheckTask(app, plan, &task, st, maxRetries)
			}

			// 3. Auto-approve blocked workspaces.
			runAutoApprove(app, plan)

			// 4. Check if all done.
			allDone := true
			for _, st := range states {
				if st.status != "completed" && st.status != "failed" {
					allDone = false
					break
				}
			}
			if allDone {
				fmt.Printf("[%s] All tasks done.\n", fmtTime())
				printRunSummary(states)

				if plan.Settings.ReactToReviews {
					fmt.Printf("[%s] Watching PRs for reviews and CI... (Ctrl-C to stop)\n", fmtTime())
					// Hand off to watch --react loop.
					runWatchReact(app, sigCh, pollInterval)
				}
				return nil
			}
		}
	}
}

func runSpawnAndDispatch(app *appContext, plan *orchestrate.Plan, task *orchestrate.Task, st *runTaskState) {
	st.status = "spawning"

	// Resolve model → agent.
	model := task.Model
	if model == "" {
		model = plan.Settings.DefaultModel
	}
	agentType := task.Agent
	if agentType == "" {
		agentType = plan.Settings.DefaultAgent
	}
	ag := agent.GetWithModel(model, agentType)

	// Spawn workspace.
	baseBranch := "main"
	detected, err := workspace.DetectDefaultBranch(app.repoRoot)
	if err == nil {
		baseBranch = detected
	}

	var agentIdentity *workspace.AgentIdentity
	agentName := ag.Name()
	if agentName != "" {
		agentIdentity = &workspace.AgentIdentity{Runtime: agentName}
	}

	ws, err := app.manager.Create(workspace.CreateOpts{
		ID:         task.ID,
		RepoRoot:   app.repoRoot,
		BaseBranch: baseBranch,
		Source:     workspace.SpawnSource{Kind: workspace.SpawnFromTask, Value: task.Prompt},
		Agent:      agentIdentity,
		CopyPaths:  app.cfg.Workspace.CopyPaths,
		LinkPaths:  app.cfg.Workspace.LinkPaths,
	})
	if err != nil {
		fmt.Printf("[%s] ✗ %s: spawn failed — %v\n", fmtTime(), task.ID, err)
		st.status = "failed"
		return
	}

	// Create tmux pane.
	if !app.term.IsHeadless() {
		_ = app.term.CreatePane(ws.ID, ws.WorktreePath, "")
	}

	// Merge dependency branches.
	if len(task.DependsOn) > 0 {
		for _, dep := range task.DependsOn {
			branch := "towr/" + dep
			cmd := exec.Command("git", "-C", ws.WorktreePath, "merge", branch, "--no-edit", "-m",
				fmt.Sprintf("merge dependency %s", dep))
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Printf("[%s] ⚠ %s: merge dep %s failed — %s\n", fmtTime(), task.ID, dep, strings.TrimSpace(string(out)))
			}
		}
	}

	// Build prompt with self-management instructions.
	prompt := task.Prompt
	prompt += "\n\nWhen you are done:\n1. git add and commit all your changes with a descriptive message\n2. Do not leave uncommitted files."

	// Dispatch.
	fmt.Printf("[%s] ▶ %s: dispatched (%s)\n", fmtTime(), task.ID, ag.Name())

	dispID := fmt.Sprintf("d-%04d", 1)
	_ = app.store.EmitEvent(store.Event{
		ID: uuid.New().String(), Kind: store.EventTaskDispatched,
		WorkspaceID: task.ID, RepoRoot: app.repoRoot, Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{"dispatch_id": dispID, "prompt": prompt, "model": model, "agent": agentName},
	})

	// Launch agent and keep approving dialogs until task completes.
	go func() {
		id := task.ID
		indicators := ag.DialogIndicators()
		startupKey := ag.StartupKey()

		// Phase 1: launch agent.
		_ = app.term.PasteBuffer(id, ag.LaunchCommand())
		time.Sleep(500 * time.Millisecond)
		_ = app.term.SendKeys(id, "Enter")

		// Phase 2: wait for startup, handle trust dialogs.
		for i := 0; i < 40; i++ {
			time.Sleep(1500 * time.Millisecond)
			captured, _ := app.term.CapturePane(id, 50)
			if captured == "" {
				continue
			}
			for _, p := range ag.StartupDialogs() {
				if strings.Contains(captured, p) {
					_ = app.term.SendKeys(id, startupKey)
					time.Sleep(1 * time.Second)
					break
				}
			}
			if strings.Contains(captured, ag.IdlePattern()) {
				break
			}
		}

		// Phase 3: send prompt.
		time.Sleep(500 * time.Millisecond)
		_ = app.term.PasteBuffer(id, prompt)
		time.Sleep(500 * time.Millisecond)
		_ = app.term.SendKeys(id, "Enter")

		// Phase 4: keep approving dialogs every 3s until task completes.
		for {
			time.Sleep(3 * time.Second)
			if st.status == "completed" || st.status == "failed" {
				return
			}
			captured, err := app.term.CapturePane(id, 200)
			if err != nil {
				continue
			}
			// Check for blocked dialogs.
			for _, pattern := range indicators {
				if strings.Contains(captured, pattern) {
					approveKey := "Enter"
					if strings.Contains(captured, "Run this command?") || strings.Contains(captured, "Run (once)") {
						approveKey = "y"
					} else if strings.Contains(captured, "Trust this workspace") {
						approveKey = "a"
					}
					_ = app.term.SendKeys(id, approveKey)
					fmt.Printf("[%s] ✓ %s: auto-approved\n", fmtTime(), id)
					break
				}
			}
		}
	}()

	st.status = "running"
	st.dispID = dispID
	st.startedAt = time.Now()
}

func runCheckTask(app *appContext, plan *orchestrate.Plan, task *orchestrate.Task, st *runTaskState, maxRetries int) {
	// Grace period: don't check until agent has had time to start.
	if time.Since(st.startedAt) < 45*time.Second {
		return
	}

	sw, err := app.store.GetWorkspace(app.repoRoot, task.ID)
	if err != nil || sw == nil {
		return
	}

	// Resolve agent for this workspace.
	ag := agent.Get(sw.AgentRuntime)

	// Try agent-specific detection.
	var state dispatch.PaneState
	var summary string
	if sw.WorktreePath != "" {
		jState, jSummary, jErr := ag.DetectActivity(sw.WorktreePath)
		if jErr == nil && dispatch.PaneState(jState) != dispatch.PaneEmpty {
			state = dispatch.PaneState(jState)
			summary = jSummary
		}
	}

	// Fallback to capture-pane.
	if state == "" || state == dispatch.PaneEmpty {
		captured, captErr := app.term.CapturePane(task.ID, 200)
		if captErr == nil {
			lastActivity := app.term.PaneLastActivity(task.ID)
			state = dispatch.DetectPaneStateWithPatterns(captured, ag.DialogIndicators(), ag.IdlePattern(), lastActivity, 15*time.Second)
			if summary == "" {
				summary = truncate(dispatch.ExtractLastResponse(captured), 200)
			}
		}
	}

	switch state {
	case dispatch.PaneIdle:
		// Task completed. Auto-commit + create PR.
		fmt.Printf("[%s] ✓ %s: completed\n", fmtTime(), task.ID)

		// Auto-commit.
		if sw.WorktreePath != "" {
			cmd := exec.Command("git", "-C", sw.WorktreePath, "status", "--porcelain")
			if out, _ := cmd.Output(); len(strings.TrimSpace(string(out))) > 0 {
				_ = exec.Command("git", "-C", sw.WorktreePath, "add", "-A").Run()
				_ = exec.Command("git", "-C", sw.WorktreePath, "commit", "-m",
					fmt.Sprintf("feat(%s): auto-commit from towr run", task.ID)).Run()
			}
		}

		// Create PR.
		if plan.Settings.CreatePR {
			towrBin, _ := os.Executable()
			cmd := exec.Command(towrBin, "land", task.ID, "--pr", "--force", "--repo", app.repoRoot)
			cmd.Dir = app.repoRoot
			if out, err := cmd.CombinedOutput(); err == nil {
				// Create actual PR via gh.
				branch := "towr/" + task.ID
				ghCmd := exec.Command("gh", "pr", "create",
					"--base", sw.BaseBranch, "--head", branch,
					"--title", fmt.Sprintf("feat(%s): from towr run", task.ID),
					"--body", fmt.Sprintf("Auto-generated by `towr run`.\n\nTask: %s\nModel: %s", task.ID, task.Model))
				ghCmd.Dir = app.repoRoot
				if ghOut, ghErr := ghCmd.CombinedOutput(); ghErr == nil {
					prURL := strings.TrimSpace(string(ghOut))
					fmt.Printf("[%s] ✓ %s: PR created — %s\n", fmtTime(), task.ID, prURL)
				}
			} else {
				fmt.Printf("[%s] ⚠ %s: land --pr failed — %s\n", fmtTime(), task.ID, strings.TrimSpace(string(out)))
			}
		}

		st.status = "completed"
		_ = app.store.EmitEvent(store.Event{
			ID: uuid.New().String(), Kind: store.EventTaskCompleted,
			WorkspaceID: task.ID, RepoRoot: app.repoRoot, Timestamp: time.Now().UTC(),
			Data: map[string]interface{}{"dispatch_id": st.dispID, "summary": summary},
		})

	case dispatch.PaneWorking:
		// Still working, nothing to do.

	case dispatch.PaneBlocked:
		// Auto-approve handled separately in runAutoApprove.

	case dispatch.PaneEmpty:
		// Agent exited. Re-dispatch (workspace already exists) or fail.
		st.retries++
		if st.retries <= maxRetries {
			fmt.Printf("[%s] ⚠ %s: agent exited, re-dispatching (%d/%d)\n", fmtTime(), task.ID, st.retries, maxRetries)
			towrBin, _ := os.Executable()
			prompt := task.Prompt + "\n\nWhen you are done:\n1. git add and commit all your changes with a descriptive message\n2. Do not leave uncommitted files."
			go func() {
				cmd := exec.Command(towrBin, "dispatch", task.ID, prompt)
				cmd.Dir = app.repoRoot
				_ = cmd.Run()
			}()
			st.startedAt = time.Now()
		} else {
			fmt.Printf("[%s] ✗ %s: agent exited, no retries left\n", fmtTime(), task.ID)
			st.status = "failed"
		}
	}
}

func runAutoApprove(app *appContext, plan *orchestrate.Plan) {
	if !plan.Settings.AutoApprove {
		return
	}

	// Check ALL plan tasks for blocked dialogs — don't filter by store status.
	for _, task := range plan.Tasks {
		captured, err := app.term.CapturePane(task.ID, 200)
		if err != nil {
			continue
		}

		// Resolve agent from plan task.
		model := task.Model
		if model == "" {
			model = plan.Settings.DefaultModel
		}
		agentType := task.Agent
		if agentType == "" {
			agentType = plan.Settings.DefaultAgent
		}
		ag := agent.GetWithModel(model, agentType)
		lastActivity := app.term.PaneLastActivity(task.ID)
		state := dispatch.DetectPaneStateWithPatterns(captured, ag.DialogIndicators(), ag.IdlePattern(), lastActivity, 5*time.Second)

		if state == dispatch.PaneBlocked {
			// Pick the right key.
			approveKey := "Enter"
			if strings.Contains(captured, "Run this command?") || strings.Contains(captured, "Run (once)") {
				approveKey = "y"
			} else if strings.Contains(captured, "Trust this workspace") {
				approveKey = "a"
			}

			if err := app.term.SendKeys(task.ID, approveKey); err == nil {
				fmt.Printf("[%s] ✓ %s: auto-approved\n", fmtTime(), task.ID)
				_ = app.store.EmitEvent(store.Event{
					ID: uuid.New().String(), Kind: "task.approved",
					WorkspaceID: task.ID, RepoRoot: app.repoRoot, Timestamp: time.Now().UTC(),
					Data: map[string]interface{}{"approve_key": approveKey, "source": "run"},
				})
			}
		}
	}
}

func runWatchReact(app *appContext, sigCh chan os.Signal, pollInterval time.Duration) {
	// Simple PR watch loop — reuses existing watch --react logic via shell.
	towrBin, _ := os.Executable()
	cmd := exec.Command(towrBin, "watch", "--auto-approve", "--react", "--interval", pollInterval.String())
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

func printRunSummary(states map[string]*runTaskState) {
	for id, st := range states {
		icon := "▶"
		switch st.status {
		case "completed":
			icon = "✓"
		case "failed":
			icon = "✗"
		case "pending":
			icon = "○"
		}
		fmt.Printf("  %s %s: %s\n", icon, id, st.status)
	}
}

func fmtTime() string {
	return time.Now().Format("15:04:05")
}
