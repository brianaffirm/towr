package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/control"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/spf13/cobra"
)

func newRunCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var budgetOverride float64
	var quiet bool
	var dryRun bool
	var agentFlag string
	var modelFlag string
	var baseFlag string
	var prFlag bool
	var noAgentFlag bool
	var fullAutoFlag bool

	cmd := &cobra.Command{
		Use:   "run <plan.yaml | prompt...>",
		Short: "Execute a plan or single task",
		Long: `The single command for all work. Accepts a YAML plan file or a bare prompt string.

With a plan file:
  towr run plan.yaml

With a prompt (single-task shorthand):
  towr run "fix the auth bug in users.go"
  towr run fix the auth bug    # multi-word prompt joined automatically

Detection: if the argument is a readable file, it's loaded as a plan. Otherwise,
all arguments are joined into a single task prompt.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp()
			if err != nil {
				return err
			}

			var plan *orchestrate.Plan
			isPlanFile := false
			if len(args) == 1 {
				if _, err := os.Stat(args[0]); err == nil {
					isPlanFile = true
				}
			}

			if isPlanFile {
				promptOnlyFlags := map[string]bool{"agent": true, "model": true, "base": true, "pr": true, "no-agent": true}
				for name := range promptOnlyFlags {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s cannot be used with a plan file", name)
					}
				}
				plan, err = orchestrate.LoadPlan(args[0])
				if err != nil {
					return err
				}
				if err := plan.Validate(); err != nil {
					return fmt.Errorf("invalid plan: %w", err)
				}
				if plan.Settings.LandPR && !plan.Settings.CreatePR {
					plan.Settings.CreatePR = true
				}
			} else {
				prompt := strings.Join(args, " ")
				wsID := orchestrate.Slugify(prompt)
				plan = &orchestrate.Plan{
					Name:  wsID,
					Tasks: []orchestrate.Task{{ID: wsID, Prompt: prompt}},
					Settings: orchestrate.Settings{AutoApprove: true},
				}
				if agentFlag != "" {
					plan.Tasks[0].Agent = agentFlag
				}
				if modelFlag != "" {
					plan.Tasks[0].Model = modelFlag
				}
				if prFlag {
					plan.Settings.CreatePR = true
				}
				if fullAutoFlag {
					plan.Settings.FullAuto = true
				}
				if noAgentFlag {
					plan.Tasks[0].Prompt = ""
				}
				if baseFlag != "" {
					plan.Settings.BaseBranch = baseFlag
				}
			}
			if budgetOverride > 0 {
				plan.Settings.Budget = budgetOverride
			}

			svc := &control.RunService{Store: app.store, Runtime: &controlRuntime{app: app, baseBranch: plan.Settings.BaseBranch},
				Router: &control.RouterAdapter{}, Clock: time.Now, Logger: &stdLog{}}
			req := buildRunRequest(app.repoRoot, plan)

			if dryRun {
				fmt.Print(formatDryRun(plan.Name, svc.DryRun(req)))
				return nil
			}
			if !quiet {
				fmt.Print(formatDryRun(plan.Name, svc.DryRun(req)))
				if !plan.Settings.AutoApprove {
					fmt.Print("\nProceed? [Y/n] ")
					var answer string
					fmt.Scanln(&answer)
					if answer != "" && strings.ToLower(answer) != "y" {
						fmt.Println("Aborted.")
						return nil
					}
				}
			}

			_ = svc.ReconcileAll(context.Background(), app.repoRoot)
			if plan.Settings.Web {
				startWebDashboard(plan.Settings.WebAddr)
			}
			startMuxStatusUpdater()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				<-sigCh
				cancel()
			}()

			handle, err := svc.Start(ctx, req)
			if handle != nil {
				for handle.Status == control.RunRunning {
					time.Sleep(100 * time.Millisecond)
				}
				fmt.Printf("\nRun %s: %s\n", handle.ID, handle.Status)
			}
			if err == nil && plan.Settings.ReactToReviews {
				runWatchReact(app, parsePollInterval(plan))
			}
			return err
		},
	}

	cmd.Flags().Float64Var(&budgetOverride, "budget", 0, "Maximum USD budget for this run (0 = no limit)")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Skip pre-run routing summary display")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show routing table and cost estimate, then exit without executing")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "Agent runtime override (prompt mode only)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "Model override (prompt mode only)")
	cmd.Flags().StringVar(&baseFlag, "base", "", "Base branch for worktree (prompt mode only)")
	cmd.Flags().BoolVar(&prFlag, "pr", false, "Create PR on completion (prompt mode only)")
	cmd.Flags().BoolVar(&noAgentFlag, "no-agent", false, "Create workspace without dispatching agent (prompt mode only)")
	cmd.Flags().BoolVar(&fullAutoFlag, "full-auto", false, "Skip all permission prompts (prompt mode only)")

	return cmd
}
