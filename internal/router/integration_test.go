package router_test

import (
	"testing"

	"github.com/brianaffirm/towr/internal/cost"
	"github.com/brianaffirm/towr/internal/orchestrate"
	"github.com/brianaffirm/towr/internal/router"
)

// TestEndToEnd_RoutingAndCost exercises the full routing → cost estimate pipeline
// without spawning any agents.
func TestEndToEnd_RoutingAndCost(t *testing.T) {
	plan := &orchestrate.Plan{
		Name: "e2e test",
		Tasks: []orchestrate.Task{
			{ID: "simple-fix", Prompt: "Fix the typo in README.md"},
			{ID: "api-endpoints", Prompt: "Implement the login handler in cmd/auth/handler.go and update cmd/auth/routes.go and internal/middleware/jwt.go"},
			{ID: "infra-change", Prompt: "Update infrastructure/terraform/main.tf with new VPC"},
			{ID: "arch-redesign", Prompt: "Refactor the entire auth system across cmd/auth/handler.go, cmd/auth/routes.go, internal/middleware/jwt.go, internal/middleware/cors.go, internal/middleware/rate.go, and internal/config/auth.go"},
			{ID: "explicit-opus", Prompt: "Simple task forced to opus", Model: "opus"},
			{ID: "cursor-task", Prompt: "Build the frontend UI", Agent: "cursor"},
		},
		Settings: orchestrate.Settings{
			Routing: orchestrate.RoutingSettings{
				Rules: []orchestrate.PolicyRule{
					{Path: "infrastructure/**", Model: "opus", RequireApproval: true},
					{Keyword: "documentation", Model: "haiku"},
				},
			},
			Budget: 25.00,
		},
	}

	type expected struct {
		model           string
		reasonPrefix    string
		canEscalate     bool
		requireApproval bool
	}

	expectations := map[string]expected{
		"simple-fix":    {model: "haiku", reasonPrefix: "heuristic:simple", canEscalate: true},
		"api-endpoints": {model: "sonnet", reasonPrefix: "heuristic:standard", canEscalate: true},
		"infra-change":  {model: "opus", reasonPrefix: "policy:infrastructure", requireApproval: true, canEscalate: true},
		"arch-redesign": {model: "opus", reasonPrefix: "heuristic:complex", canEscalate: false},
		"explicit-opus": {model: "opus", reasonPrefix: "explicit", canEscalate: false},
		"cursor-task":   {model: "cursor-auto", reasonPrefix: "external-agent:cursor", canEscalate: false},
	}

	var totalCost, opusTotalCost float64
	var preRunItems []cost.PreRunItem

	for _, task := range plan.Tasks {
		d := router.Route(task, plan.Settings)
		exp, ok := expectations[task.ID]
		if !ok {
			t.Fatalf("unexpected task %q", task.ID)
		}

		t.Run(task.ID, func(t *testing.T) {
			if d.Model != exp.model {
				t.Errorf("model = %q, want %q", d.Model, exp.model)
			}
			if len(d.Reason) < len(exp.reasonPrefix) || d.Reason[:len(exp.reasonPrefix)] != exp.reasonPrefix {
				t.Errorf("reason = %q, want prefix %q", d.Reason, exp.reasonPrefix)
			}
			if d.CanEscalate != exp.canEscalate {
				t.Errorf("canEscalate = %v, want %v", d.CanEscalate, exp.canEscalate)
			}
			if d.RequireApproval != exp.requireApproval {
				t.Errorf("requireApproval = %v, want %v", d.RequireApproval, exp.requireApproval)
			}
		})

		estUsage := cost.DefaultEstimate()
		estCost := cost.Calculate(d.Model, estUsage)
		opusCost := cost.Calculate("opus", estUsage)
		totalCost += estCost
		opusTotalCost += opusCost

		preRunItems = append(preRunItems, cost.PreRunItem{
			TaskID:   task.ID,
			Decision: d,
			EstCost:  estCost,
		})
	}

	// Verify cost savings exist.
	if totalCost >= opusTotalCost {
		t.Errorf("smart routing should be cheaper: total=$%.2f, opus=$%.2f", totalCost, opusTotalCost)
	}
	savings := (opusTotalCost - totalCost) / opusTotalCost * 100
	t.Logf("Cost: $%.2f routed vs $%.2f all-opus (%.0f%% savings)", totalCost, opusTotalCost, savings)

	// Verify pre-run report generates.
	report := cost.FormatPreRun(plan.Name, preRunItems)
	if report == "" {
		t.Error("FormatPreRun returned empty string")
	}
	t.Logf("Pre-run report:\n%s", report)

	// Verify budget would be respected.
	if totalCost > plan.Settings.Budget {
		t.Logf("Note: estimated cost $%.2f exceeds budget $%.2f", totalCost, plan.Settings.Budget)
	}

	// Test escalation chain.
	t.Run("escalation-chain", func(t *testing.T) {
		d := router.Route(plan.Tasks[0], plan.Settings) // haiku
		if d.Model != "haiku" {
			t.Fatalf("expected haiku, got %s", d.Model)
		}

		d2, ok := router.Escalate(d)
		if !ok {
			t.Fatal("haiku should escalate")
		}
		if d2.Model != "sonnet" {
			t.Errorf("escalated to %s, want sonnet", d2.Model)
		}

		d3, ok := router.Escalate(d2)
		if !ok {
			t.Fatal("sonnet should escalate")
		}
		if d3.Model != "opus" {
			t.Errorf("escalated to %s, want opus", d3.Model)
		}

		_, ok = router.Escalate(d3)
		if ok {
			t.Error("opus should not escalate further")
		}
	})
}
