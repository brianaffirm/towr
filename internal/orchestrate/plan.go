package orchestrate

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Plan defines a declarative task graph for orchestrated execution.
type Plan struct {
	Name     string   `yaml:"name"`
	Tasks    []Task   `yaml:"tasks"`
	Settings Settings `yaml:"settings"`
}

// Task is a single unit of work in a plan.
type Task struct {
	ID        string   `yaml:"id"`
	Prompt    string   `yaml:"prompt"`
	DependsOn []string `yaml:"depends_on"`
	Agent     string   `yaml:"agent,omitempty"` // agent runtime override; defaults to settings.default_agent or "claude-code"
	Model     string   `yaml:"model,omitempty"` // model override: opus, sonnet, haiku, codex-mini, gpt-5.3-codex, gpt-5.4, cursor-auto, cursor-sonnet
}

// PolicyRule defines a routing policy that overrides heuristics.
// Lives in orchestrate (not router) to prevent import cycles.
type PolicyRule struct {
	Path            string `yaml:"path"`
	Keyword         string `yaml:"keyword"`
	Model           string `yaml:"model"`
	RequireApproval bool   `yaml:"require_approval"`
	Pin             bool   `yaml:"pin"`
}

// RoutingSettings configures smart model routing.
type RoutingSettings struct {
	Rules []PolicyRule `yaml:"rules"`
}

// Settings controls execution behavior for the plan.
type Settings struct {
	AutoApprove    bool   `yaml:"auto_approve"`
	MaxRetries     int    `yaml:"max_retries"`
	PollInterval   string `yaml:"poll_interval"`      // e.g. "10s"
	CreatePR       bool   `yaml:"create_pr"`          // auto-create PR on task completion
	LandPR         bool   `yaml:"land_pr"`            // deprecated alias for create_pr
	ReactToReviews bool   `yaml:"react_to_reviews"`   // monitor PRs for @towr comments + CI failures
	Web            bool   `yaml:"web"`                // start web dashboard
	WebAddr        string `yaml:"web_addr,omitempty"` // web dashboard address (default :8090)
	DefaultAgent   string `yaml:"default_agent,omitempty"`
	DefaultModel   string          `yaml:"default_model,omitempty"` // default model: opus, sonnet, etc.
	Routing        RoutingSettings `yaml:"routing"`
	Budget         float64         `yaml:"budget"`
}

// LoadPlan reads and parses a YAML plan file from the given path.
func LoadPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan file: %w", err)
	}

	var plan Plan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parse plan YAML: %w", err)
	}

	return &plan, nil
}

// Validate checks the plan for structural errors: duplicate IDs, missing
// dependency references, empty prompts, and circular dependencies.
func (p *Plan) Validate() error {
	if len(p.Tasks) == 0 {
		return fmt.Errorf("plan has no tasks")
	}

	// Check for duplicate IDs and build lookup set.
	ids := make(map[string]bool, len(p.Tasks))
	for _, t := range p.Tasks {
		if t.ID == "" {
			return fmt.Errorf("task has empty ID")
		}
		if ids[t.ID] {
			return fmt.Errorf("duplicate task ID: %q", t.ID)
		}
		ids[t.ID] = true
	}

	// Check each task.
	for _, t := range p.Tasks {
		if t.Prompt == "" {
			return fmt.Errorf("task %q has empty prompt", t.ID)
		}
		for _, dep := range t.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("task %q depends on unknown task %q", t.ID, dep)
			}
			if dep == t.ID {
				return fmt.Errorf("task %q depends on itself", t.ID)
			}
		}
	}

	// Detect cycles via topological sort (Kahn's algorithm).
	if err := checkCycles(p.Tasks); err != nil {
		return err
	}

	return nil
}

// checkCycles detects circular dependencies using Kahn's algorithm.
// Returns an error if a cycle is found.
func checkCycles(tasks []Task) error {
	// Build adjacency list and in-degree map.
	inDegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))

	for _, t := range tasks {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			dependents[dep] = append(dependents[dep], t.ID)
			inDegree[t.ID]++
		}
	}

	// Seed queue with zero in-degree nodes.
	var queue []string
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			queue = append(queue, t.ID)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if visited != len(tasks) {
		return fmt.Errorf("circular dependency detected in task graph")
	}
	return nil
}
