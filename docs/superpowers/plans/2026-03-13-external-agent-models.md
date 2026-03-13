# External Agent Model & Pricing Support — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** External agents (Codex, Cursor) get their own model names, pricing, and CLI flags instead of being assigned Claude model names.

**Architecture:** Extend the existing `ModelFlag` pattern from `ClaudeCode` to `CodexAgent` and `CursorAgent`. Add external model pricing to the cost table. Update the router to assign agent-specific default models instead of running Claude heuristics for external agents.

**Tech Stack:** Go, vanilla JS

**Spec:** `docs/superpowers/specs/2026-03-13-external-agent-models-design.md`

---

## Chunk 1: Pricing + Cost Tests

### Task 1: Add external model pricing

**Files:**
- Modify: `internal/cost/pricing.go`

- [ ] **Step 1: Write failing test for codex-mini pricing**

Add to `internal/cost/cost_test.go` inside the `TestCalculate` table:

```go
// Codex-mini: 10K × $0.25/M + 30K × $2.00/M = $0.0025 + $0.06 = $0.0625
{"codex-mini", TokenUsage{InputTokens: 10000, OutputTokens: 30000}, 0.0625},
// Cursor-auto: 10K × $1.25/M + 30K × $6.00/M = $0.0125 + $0.18 = $0.1925
{"cursor-auto", TokenUsage{InputTokens: 10000, OutputTokens: 30000}, 0.1925},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestCalculate ./internal/cost/`
Expected: FAIL — `codex-mini` and `cursor-auto` return 0.0000

- [ ] **Step 3: Add pricing entries**

In `internal/cost/pricing.go`, replace lines 10-19 (the comment block and `Pricing` variable):

```go
// Pricing reflects current model pricing as of March 2026.
//
// Claude (Anthropic) — https://docs.anthropic.com/en/docs/about-claude/models
//   Opus 4.6:   $5/M input,    $25/M output
//   Sonnet 4.6: $3/M input,    $15/M output
//   Haiku 4.5:  $1/M input,     $5/M output
//
// Codex (OpenAI) — https://developers.openai.com/codex/pricing/
//   codex-mini:    $0.25/M input,  $2/M output
//   gpt-5.3-codex: $1.75/M input, $14/M output
//   gpt-5.4:       $2.50/M input, $15/M output
//
// Cursor — https://cursor.com/docs/models (includes ~20% Cursor markup)
//   cursor-auto:   $1.25/M input,  $6/M output
//   cursor-sonnet: $3.60/M input, $18/M output
var Pricing = map[string]ModelPricing{
	"opus":          {5.00, 25.00},
	"sonnet":        {3.00, 15.00},
	"haiku":         {1.00, 5.00},
	"codex-mini":    {0.25, 2.00},
	"gpt-5.3-codex": {1.75, 14.00},
	"gpt-5.4":       {2.50, 15.00},
	"cursor-auto":   {1.25, 6.00},
	"cursor-sonnet": {3.60, 18.00},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestCalculate ./internal/cost/`
Expected: PASS — all rows including codex-mini and cursor-auto

- [ ] **Step 5: Commit**

```bash
git add internal/cost/pricing.go internal/cost/cost_test.go
git commit -m "feat(cost): add Codex and Cursor model pricing"
```

---

## Chunk 2: Agent Structs

### Task 2: Add ModelFlag to CodexAgent

**Files:**
- Modify: `internal/agent/codex.go`

- [ ] **Step 1: Add ModelFlag field and update Name()**

Replace the `CodexAgent` struct and `Name()` method in `internal/agent/codex.go`:

```go
type CodexAgent struct {
	ModelFlag string // e.g. "codex-mini", "gpt-5.3-codex", "gpt-5.4"
}

func (c *CodexAgent) Name() string {
	if c.ModelFlag != "" {
		return "codex:" + c.ModelFlag
	}
	return "codex"
}
```

- [ ] **Step 2: Update LaunchCommand() to pass -m flag**

Replace the `LaunchCommand()` method:

```go
func (c *CodexAgent) LaunchCommand() string {
	if c.ModelFlag != "" {
		return "codex --no-alt-screen -m " + c.ModelFlag
	}
	return "codex --no-alt-screen"
}
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./cmd/towr/`
Expected: compiles cleanly

- [ ] **Step 4: Commit**

```bash
git add internal/agent/codex.go
git commit -m "feat(agent): add ModelFlag to CodexAgent"
```

### Task 3: Add ModelFlag to CursorAgent

**Files:**
- Modify: `internal/agent/cursor.go`

- [ ] **Step 1: Add ModelFlag, CLI flag mapping, and update Name()**

Replace the `CursorAgent` struct and `Name()` method in `internal/agent/cursor.go`:

```go
type CursorAgent struct {
	ModelFlag string // e.g. "cursor-auto", "cursor-sonnet"
}

func (c *CursorAgent) Name() string {
	if c.ModelFlag != "" {
		return "cursor:" + c.ModelFlag
	}
	return "cursor"
}
```

Add a mapping function (before `LaunchCommand`):

```go
// cursorCLIFlag maps towr model names to Cursor's CLI -m flag values.
var cursorCLIFlags = map[string]string{
	"cursor-auto":   "auto",
	"cursor-sonnet": "sonnet-4",
}

func cursorCLIFlag(model string) string {
	if f, ok := cursorCLIFlags[model]; ok {
		return f
	}
	return model // pass through unknown names
}
```

- [ ] **Step 2: Update LaunchCommand() to pass -m flag**

Replace the `LaunchCommand()` method:

```go
func (c *CursorAgent) LaunchCommand() string {
	if c.ModelFlag != "" {
		return "cursor-agent -m " + cursorCLIFlag(c.ModelFlag)
	}
	return "cursor-agent"
}
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./cmd/towr/`
Expected: compiles cleanly

- [ ] **Step 4: Commit**

```bash
git add internal/agent/cursor.go
git commit -m "feat(agent): add ModelFlag to CursorAgent with CLI flag mapping"
```

### Task 4: Make GetWithModel agent-universal

**Files:**
- Modify: `internal/agent/registry.go`

- [ ] **Step 1: Replace GetWithModel**

In `internal/agent/registry.go`, replace the `GetWithModel` function:

```go
// GetWithModel returns an agent with an optional model override.
// Constructs a new instance (not the registered singleton) so ModelFlag
// is set without shared-state mutation.
func GetWithModel(model, agentName string) Agent {
	switch agentName {
	case "codex":
		return &CodexAgent{ModelFlag: model}
	case "cursor":
		return &CursorAgent{ModelFlag: model}
	default: // claude-code or empty
		if model != "" {
			return &ClaudeCode{ModelFlag: model}
		}
		return Default()
	}
}
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add internal/agent/registry.go
git commit -m "feat(agent): make GetWithModel agent-universal"
```

---

## Chunk 3: Router

### Task 5: Route external agents to their own default models

**Files:**
- Modify: `internal/router/router.go`
- Modify: `internal/router/router_test.go`
- Modify: `internal/router/integration_test.go`

- [ ] **Step 1: Write failing test for codex default model**

Add to `internal/router/router_test.go`:

```go
func TestRoute_ExternalAgent_DefaultModel(t *testing.T) {
	task := orchestrate.Task{ID: "t1", Prompt: "build the UI", Agent: "codex"}
	d := Route(task, orchestrate.Settings{})
	if d.Model != "codex-mini" {
		t.Errorf("model = %q, want codex-mini", d.Model)
	}
	if d.Reason != "external-agent:codex" {
		t.Errorf("reason = %q, want external-agent:codex", d.Reason)
	}
	if d.CanEscalate {
		t.Error("external agent should not be escalatable")
	}
}

func TestRoute_ExternalAgent_ExplicitModel(t *testing.T) {
	task := orchestrate.Task{ID: "t1", Prompt: "heavy task", Agent: "codex", Model: "gpt-5.3-codex"}
	d := Route(task, orchestrate.Settings{})
	if d.Model != "gpt-5.3-codex" {
		t.Errorf("model = %q, want gpt-5.3-codex", d.Model)
	}
	if d.Reason != "external-agent:codex" {
		t.Errorf("reason = %q, want external-agent:codex", d.Reason)
	}
}

```

Note: `TestRoute_NonClaudeAgent` (which tests cursor default) is updated in Step 4 below — no separate cursor default test needed to avoid duplication.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestRoute_ExternalAgent" ./internal/router/`
Expected: FAIL — models are "haiku" instead of "codex-mini"/"cursor-auto"

- [ ] **Step 3: Update router.go**

In `internal/router/router.go`, add the default model map after `tierModel`:

```go
// agentDefaultModel maps external agent names to their default (cheapest) model.
var agentDefaultModel = map[string]string{
	"codex":  "codex-mini",
	"cursor": "cursor-auto",
}
```

Replace the external-agent branch in `Route()` (lines 32-38):

```go
	// Non-Claude agents use their own model namespace.
	if task.Agent != "" && task.Agent != "claude-code" {
		model := task.Model
		if model == "" {
			model = agentDefaultModel[task.Agent]
		}
		return Decision{
			Model:       model,
			Reason:      fmt.Sprintf("external-agent:%s", task.Agent),
			CanEscalate: false,
		}
	}
```

- [ ] **Step 4: Update existing TestRoute_NonClaudeAgent to assert model**

In `internal/router/router_test.go`, replace `TestRoute_NonClaudeAgent`:

```go
func TestRoute_NonClaudeAgent(t *testing.T) {
	task := orchestrate.Task{ID: "t1", Prompt: "build the UI", Agent: "cursor"}
	settings := orchestrate.Settings{}
	d := Route(task, settings)
	if d.Model != "cursor-auto" {
		t.Errorf("model = %q, want cursor-auto", d.Model)
	}
	if d.CanEscalate {
		t.Error("cursor agent should not be escalatable")
	}
	if d.Reason != "external-agent:cursor" {
		t.Errorf("reason = %q, want external-agent:cursor", d.Reason)
	}
}
```

- [ ] **Step 5: Update integration test expectation**

In `internal/router/integration_test.go`, change the `cursor-task` expectation (line 48):

```go
"cursor-task":   {model: "cursor-auto", reasonPrefix: "external-agent:cursor", canEscalate: false},
```

- [ ] **Step 6: Run all router tests**

Run: `go test -v ./internal/router/`
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add internal/router/router.go internal/router/router_test.go internal/router/integration_test.go
git commit -m "feat(router): route external agents to their own default models"
```

---

## Chunk 4: UI + Plan Comment + Full Test

### Task 6: Add model badge colors for external models

**Files:**
- Modify: `cmd/towr/web/js/app.js`

- [ ] **Step 1: Update MODEL_COLORS map**

In `cmd/towr/web/js/app.js`, replace the `MODEL_COLORS` object (lines 15-19):

```js
  var MODEL_COLORS = {
      haiku: "#58a6ff",
      sonnet: "#a78bfa",
      opus: "#f59e0b",
      "codex-mini": "#10b981",
      "gpt-5.3-codex": "#10b981",
      "gpt-5.4": "#10b981",
      "cursor-auto": "#06b6d4",
      "cursor-sonnet": "#06b6d4"
  };
```

- [ ] **Step 2: Commit**

```bash
git add cmd/towr/web/js/app.js
git commit -m "feat(web): add model badge colors for Codex and Cursor models"
```

### Task 7: Update Task.Model field comment

**Files:**
- Modify: `internal/orchestrate/plan.go`

- [ ] **Step 1: Update comment**

In `internal/orchestrate/plan.go`, replace line 23:

```go
	Model     string   `yaml:"model,omitempty"` // model override: opus, sonnet, haiku, codex-mini, gpt-5.3-codex, gpt-5.4, cursor-auto, cursor-sonnet
```

- [ ] **Step 2: Run build**

Run: `go build ./cmd/towr/`
Expected: compiles cleanly

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrate/plan.go
git commit -m "docs: update Task.Model comment with external model names"
```

### Task 8: Full test suite + dry-run validation

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: all packages pass

- [ ] **Step 2: Validate dry-run with mixed agent plan**

Create `testdata/e2e-mixed-agents.yaml` (if not already present):

```yaml
name: "mixed-agent-cost-test"
tasks:
  - id: claude-haiku-task
    prompt: "Fix a typo"

  - id: codex-default
    prompt: "Add a comment"
    agent: codex

  - id: codex-heavy
    prompt: "Refactor module"
    agent: codex
    model: gpt-5.3-codex

  - id: cursor-default
    prompt: "Build UI"
    agent: cursor

  - id: cursor-sonnet
    prompt: "Complex UI work"
    agent: cursor
    model: cursor-sonnet

  - id: claude-opus-task
    prompt: "Architect the system"
    model: opus

settings:
  auto_approve: true
```

Run: `./towr run --dry-run testdata/e2e-mixed-agents.yaml`

Expected output should show:
- `claude-haiku-task` → haiku, ~$0.16
- `codex-default` → codex-mini, ~$0.06
- `codex-heavy` → gpt-5.3-codex, ~$0.44
- `cursor-default` → cursor-auto, ~$0.19
- `cursor-sonnet` → cursor-sonnet, ~$0.56
- `claude-opus-task` → opus, ~$0.80

- [ ] **Step 3: Commit test fixture**

```bash
git add testdata/e2e-mixed-agents.yaml
git commit -m "test: add mixed-agent plan fixture for e2e validation"
```
