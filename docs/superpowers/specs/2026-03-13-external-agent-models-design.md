# External Agent Model & Pricing Support

**Date:** 2026-03-13
**Status:** Approved

## Problem

When tasks are routed to external agents (Codex, Cursor), towr runs the Claude heuristic and assigns a Claude model name (e.g. "haiku"). This is wrong on two levels:
1. **Display**: the dashboard shows "haiku" when the task runs on codex-mini or cursor-auto
2. **Cost estimate**: uses Claude pricing when Codex/Cursor have their own rates

## Design

### 1. Pricing table expansion (`internal/cost/pricing.go`)

Add external agent models alongside Claude models:

```go
var Pricing = map[string]ModelPricing{
    // Claude (Anthropic)
    "opus":   {5.00, 25.00},
    "sonnet": {3.00, 15.00},
    "haiku":  {1.00, 5.00},
    // Codex (OpenAI)
    "codex-mini":    {0.25, 2.00},
    "gpt-5.3-codex": {1.75, 14.00},
    "gpt-5.4":       {2.50, 15.00},
    // Cursor (includes ~20% Cursor markup over base API rates)
    "cursor-auto":   {1.25, 6.00},
    "cursor-sonnet": {3.60, 18.00},
}
```

Default model per agent when none specified: `codex-mini` for codex, `cursor-auto` for cursor.

### 2. Agent structs — add ModelFlag to Codex and Cursor

Same pattern as `ClaudeCode`. Each agent appends its CLI model flag to the launch command when a model is set.

**CodexAgent:**
- `ModelFlag` field (e.g. `"codex-mini"`, `"gpt-5.3-codex"`)
- `LaunchCommand()` appends `-m {ModelFlag}` when set
- `Name()` returns `"codex:{ModelFlag}"` when set

**CursorAgent:**
- `ModelFlag` field (e.g. `"cursor-auto"`, `"cursor-sonnet"`)
- `LaunchCommand()` appends `-m {cursorCLIFlag}` when set, with a mapping from towr model names to Cursor CLI flag names:
  - `cursor-auto` → `auto`
  - `cursor-sonnet` → `sonnet-4`
- `Name()` returns `"cursor:{ModelFlag}"` when set

### 3. Registry — `GetWithModel` becomes agent-universal

Remove the claude-code-only guard. Switch on agent name to construct **new instances** (not mutate the registered singleton):
- `"codex"` → `&CodexAgent{ModelFlag: model}`
- `"cursor"` → `&CursorAgent{ModelFlag: model}`
- default → `&ClaudeCode{ModelFlag: model}`

### 4. Router — external agents use their own default model

Replace the `heuristic()` call for external agents with a default model lookup:

```go
var agentDefaultModel = map[string]string{
    "codex":  "codex-mini",
    "cursor": "cursor-auto",
}
```

The external-agent branch fires first in `Route()` (before the explicit-model check). So `task.Model` must be checked **inside** the branch:

```go
if task.Agent != "" && task.Agent != "claude-code" {
    model := task.Model                          // explicit override first
    if model == "" {
        model = agentDefaultModel[task.Agent]    // then agent default
    }
    return Decision{
        Model:       model,
        Reason:      fmt.Sprintf("external-agent:%s", task.Agent),
        CanEscalate: false,
    }
}
```

`Tier` is irrelevant for external agents (no escalation), so it stays at the zero value.

**Unknown model names:** `Calculate()` returns `0` for models not in the `Pricing` map. This is acceptable for now — user-specified model names in YAML are their responsibility. If a model typo produces $0.00 cost, the dashboard will show it.

### 5. UI — model badge colors

Add badge colors for new models in `app.js` `MODEL_COLORS`:
- `codex-mini`, `gpt-5.3-codex`, `gpt-5.4` → OpenAI green (`#10b981`)
- `cursor-auto`, `cursor-sonnet` → Cursor cyan (`#06b6d4`)

No other JS changes needed — badges already render `taskCost.model`.

### 6. YAML usage

```yaml
# Defaults — no model field needed
- id: fast-fix
  agent: codex              # → codex-mini ($0.25/$2.00)

- id: ui-work
  agent: cursor             # → cursor-auto ($1.25/$6.00)

# Explicit overrides
- id: heavy-codex
  agent: codex
  model: gpt-5.3-codex     # → gpt-5.3-codex ($1.75/$14.00)

- id: cursor-claude
  agent: cursor
  model: cursor-sonnet      # → cursor-sonnet ($3.60/$18.00)
```

## Files changed

| File | Change |
|------|--------|
| `internal/cost/pricing.go` | Add Codex + Cursor model pricing |
| `internal/agent/codex.go` | Add ModelFlag, update LaunchCommand/Name |
| `internal/agent/cursor.go` | Add ModelFlag, update LaunchCommand/Name, add CLI flag mapping |
| `internal/agent/registry.go` | Make GetWithModel agent-universal |
| `internal/router/router.go` | Add agentDefaultModel, use it for external agents |
| `cmd/towr/web/js/app.js` | Add MODEL_COLORS for new models |
| `internal/cost/cost_test.go` | Add test cases for new models |
| `internal/router/router_test.go` | Update external agent routing tests (add model assertion) |
| `internal/orchestrate/plan.go` | Update Task.Model field comment to list external model names |

## Edge cases

- **Unknown model name** (e.g. `model: codex_mini` typo): `Calculate()` returns $0.00. Acceptable — visible in dashboard as $0.00 cost.
- **Mismatched agent+model** (e.g. `agent: codex, model: cursor-sonnet`): passed through as-is. No validation in this phase (see non-goals).
- **No agent, no model**: unchanged — runs Claude heuristic as today.

## Non-goals

- Escalation for external agents (they don't use our model tiers)
- Token parsing for external agents (still estimated/unavailable)
- Validating model against agent's supported list (deferred to a later phase)
