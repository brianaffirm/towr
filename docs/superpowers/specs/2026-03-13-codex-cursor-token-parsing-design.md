# Codex/Cursor Token Parsing (Inbox 050)

**Date:** 2026-03-13
**Status:** Approved

## Problem

After `towr run` completes, external agent tasks show $0.00 actual cost because towr can't parse their token usage. The post-run report shows zeros, undermining the cost intelligence story.

## Research findings

- **Codex**: Writes JSONL session logs to `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` with `token_count` events containing `input_tokens`, `output_tokens`, `cached_input_tokens`, and `reasoning_output_tokens`.
- **Cursor**: No token data in CLI output. Open feature request on Cursor forum. `stream-json` mode strips token deltas. No viable parsing path today.

## Design

### 1. Codex token parser

Add `ParseCodexTokens(worktreePath string)` to `internal/cost/tokens.go`. Same pattern as `ParseClaudeTokens`:

- Find session files at `~/.codex/sessions/` (glob `**/rollout-*.jsonl`)
- Pick the most recently modified file within a reasonable time window
- Scan for lines containing `"token_count"`, parse the last one
- Extract `total_token_usage.input_tokens` and `total_token_usage.output_tokens`
- Return `TokenUsage{Source: "codex-jsonl"}`
- Use same 1MB scanner buffer as Claude parser (Codex lines can be 2-3KB)
- Take only the **last** `token_count` entry (Codex totals are cumulative, not incremental)

#### Codex JSONL structure

```json
{
  "timestamp": "2026-03-13T19:04:01.046Z",
  "type": "event_msg",
  "payload": {
    "type": "token_count",
    "info": {
      "total_token_usage": {
        "input_tokens": 8951,
        "cached_input_tokens": 7552,
        "output_tokens": 206,
        "reasoning_output_tokens": 23,
        "total_tokens": 9157
      }
    }
  }
}
```

#### Session discovery

Add `FindCodexSession(worktreePath string)` to `internal/dispatch/` (alongside existing Claude JSONL helpers):

- Codex sessions dir: `~/.codex/sessions/`
- Glob: `**/rollout-*.jsonl`
- **Match by `cwd`**: read the first line (`session_meta` event) of each candidate file, check if `payload.cwd` matches the worktree path. This disambiguates concurrent Codex tasks.
- Fallback: if no `cwd` match, use most recently modified file (for backward compat with older Codex versions)
- Overridable in tests via `SetCodexSessionsDirOverride()`

The `session_meta` event on the first line of every Codex JSONL file contains:

```json
{"type":"session_meta","payload":{"cwd":"/Users/brian.ho/.towr/worktrees/towr/codex-task",...}}
```

This is verified against real session files from towr runs — Codex records the worktree path it was launched in.

**Why not timestamp-only**: towr runs agents in parallel. Multiple Codex tasks finish around the same time. Timestamp matching would misattribute sessions. The `cwd` field gives exact workspace-to-session mapping, same precision as Claude's per-project directory structure.

### 2. Cursor estimation improvement

No parser possible. Improve transparency:

- When agent is Cursor, return `Source: "cursor-estimated"` (instead of generic `"estimated"`)
- UI already handles estimated sources with the `est` badge — no dashboard changes needed
- When Cursor ships token data in `stream-json` (open feature request), add a parser

### 3. Wire into run.go

Replace the binary Claude/other check in `runCheckTask` with a switch:

```go
switch sw.AgentRuntime {
case "", "claude-code":
    usage, _ = cost.ParseClaudeTokens(sw.WorktreePath)
case "codex":
    usage, _ = cost.ParseCodexTokens(sw.WorktreePath)
default: // cursor, others — no token parsing available
    usage = cost.EstimateTokens(task.Prompt)
    usage.Source = sw.AgentRuntime + "-estimated"
}
```

### 4. No other changes

Pricing, router, dashboard, and web.go are unchanged. The existing pipeline already:
- Calls `cost.Calculate(model, usage)` with whatever tokens are returned
- Stores the result in `task.cost` events
- Displays in the dashboard with source-aware badges

Once `ParseCodexTokens` returns real tokens, actual costs flow through automatically.

## Files changed

| File | Change |
|------|--------|
| `internal/cost/tokens.go` | Add `ParseCodexTokens()`, codex JSONL structs |
| `internal/dispatch/codex_session.go` | New: `FindLatestCodexSession()`, dir override for tests |
| `internal/cost/cost_test.go` | Add tests for `ParseCodexTokens` with fixture data |
| `cmd/towr/run.go` | Switch on `AgentRuntime` for token parsing |

## Non-goals

- Cursor token parsing (blocked on Cursor shipping the feature)
- Codex `cached_input_tokens` or `reasoning_output_tokens` tracking (use total input/output only for now)
- Changing the estimation heuristic (current word-count × 1.3 is fine)
