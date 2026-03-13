# towr run — Architecture

## The One Command

```
$ towr run plan.yaml
```

## End-to-End Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│  YOU (terminal)                                                      │
│                                                                      │
│  1. Write plan.yaml (or ask Opus to write it)                       │
│  2. towr run plan.yaml                                               │
│  3. Go to sleep                                                      │
│  4. Morning: review PRs                                              │
└──────────────┬──────────────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────────────────────┐
│  TOWR RUN (single Go process)                                        │
│                                                                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │ Plan Parser  │  │ Task Loop   │  │ Web Server  │                 │
│  │ (YAML→tasks) │  │ (10s tick)  │  │ (:8090)     │                 │
│  └──────┬──────┘  └──────┬──────┘  └─────────────┘                 │
│         │                │                                           │
│         │         ┌──────┴──────────────────────┐                   │
│         │         │  Per tick:                    │                   │
│         │         │  1. Dispatch ready tasks      │                   │
│         │         │  2. Check running tasks       │                   │
│         │         │  3. Auto-approve (global)     │                   │
│         │         │  4. Check if all done         │                   │
│         │         └──────┬──────────────────────┘                   │
│         │                │                                           │
│         ▼                ▼                                           │
│  ┌─────────────────────────────────────────────┐                    │
│  │  Per Task: spawn → launch → approve → land   │                    │
│  │                                               │                    │
│  │  ┌─ Spawn ──────────────────────────────┐    │                    │
│  │  │ git worktree add → tmux session      │    │                    │
│  │  │ copy .claude/settings.json           │    │                    │
│  │  │ merge dependency branches            │    │                    │
│  │  └──────────────────────────────────────┘    │                    │
│  │                                               │                    │
│  │  ┌─ Launch (goroutine per task) ────────┐    │                    │
│  │  │ tmux paste: "claude --model sonnet"  │    │                    │
│  │  │ wait for idle prompt (❯/→/›)         │    │                    │
│  │  │ tmux paste: prompt text              │    │                    │
│  │  │                                      │    │                    │
│  │  │ Loop every 3s until task completes:  │    │                    │
│  │  │   capture-pane → detect dialog →     │    │                    │
│  │  │   send approval key (Enter/y/a)      │    │                    │
│  │  └──────────────────────────────────────┘    │                    │
│  │                                               │                    │
│  │  ┌─ Completion Detection ───────────────┐    │                    │
│  │  │ JSONL "last-prompt" (Claude only)    │    │                    │
│  │  │   OR                                 │    │                    │
│  │  │ capture-pane idle pattern + 15s quiet │    │                    │
│  │  │   OR                                 │    │                    │
│  │  │ process exit                         │    │                    │
│  │  └──────────────────────────────────────┘    │                    │
│  │                                               │                    │
│  │  ┌─ Post-Completion ───────────────────┐    │                    │
│  │  │ git add -A && git commit            │    │                    │
│  │  │ towr land --pr → git push           │    │                    │
│  │  │ gh pr create                        │    │                    │
│  │  └──────────────────────────────────────┘    │                    │
│  └───────────────────────────────────────────────┘                    │
└──────────────┬──────────────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────────────────────┐
│  INFRASTRUCTURE                                                      │
│                                                                      │
│  ┌── tmux ──────────────────────────────────────────────┐           │
│  │  session: towr/auth        session: towr/billing      │           │
│  │  ┌──────────────────┐     ┌──────────────────┐       │           │
│  │  │ claude --model   │     │ cursor-agent     │       │           │
│  │  │ sonnet           │     │                  │       │           │
│  │  │                  │     │                  │       │           │
│  │  │ ❯ implement JWT  │     │ → build the UI   │       │           │
│  │  │ ⏺ Read files... │     │ ⬢ Read go.mod   │       │           │
│  │  │ ⏺ Write jwt.go  │     │ $ go test -v    │       │           │
│  │  └──────────────────┘     └──────────────────┘       │           │
│  │                                                       │           │
│  │  session: towr/tests                                  │           │
│  │  ┌──────────────────┐                                │           │
│  │  │ codex            │                                │           │
│  │  │ --no-alt-screen  │                                │           │
│  │  │                  │                                │           │
│  │  │ › write tests    │                                │           │
│  │  │ • Added test.go  │                                │           │
│  │  └──────────────────┘                                │           │
│  └───────────────────────────────────────────────────────┘           │
│                                                                      │
│  ┌── git worktrees ─────────────────────────────────────┐           │
│  │  ~/.towr/worktrees/towr/auth/     ← isolated branch  │           │
│  │  ~/.towr/worktrees/towr/billing/  ← isolated branch  │           │
│  │  ~/.towr/worktrees/towr/tests/    ← isolated branch  │           │
│  │  Each has own .claude/settings.json (copied)          │           │
│  └───────────────────────────────────────────────────────┘           │
│                                                                      │
│  ┌── SQLite store ──────────────────────────────────────┐           │
│  │  ~/.towr/repos/<hash>/state.db                        │           │
│  │  Tables: workspaces, events                           │           │
│  │  Events: dispatched, started, approved, completed,    │           │
│  │          failed, blocked                              │           │
│  └───────────────────────────────────────────────────────┘           │
│                                                                      │
│  ┌── GitHub ────────────────────────────────────────────┐           │
│  │  PR #42: towr/auth → master                          │           │
│  │  PR #43: towr/billing → master                       │           │
│  │  PR #44: towr/tests → master                         │           │
│  └───────────────────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────────────────┘
```

## Data Flow

```
plan.yaml
  │
  ├─ parse → validate deps → topological sort
  │
  ├─ for each task (respecting dependencies):
  │    │
  │    ├─ resolve agent: model → ClaudeCode{sonnet}
  │    │                  agent: cursor → CursorAgent{}
  │    │                  agent: codex → CodexAgent{}
  │    │
  │    ├─ spawn workspace:
  │    │    git worktree add ~/.towr/worktrees/repo/task-id
  │    │    cp .claude/settings.json → worktree
  │    │    git merge dependency branches
  │    │    tmux new-session towr/task-id
  │    │    INSERT workspace INTO state.db
  │    │
  │    ├─ launch agent (goroutine):
  │    │    tmux paste-buffer: "claude --model sonnet"
  │    │    wait for idle prompt: ❯
  │    │    tmux paste-buffer: prompt text
  │    │    tmux send-keys: Enter
  │    │    │
  │    │    └─ approval loop (every 3s):
  │    │         tmux capture-pane → detect dialog?
  │    │         yes → tmux send-keys: Enter/y/a
  │    │              INSERT task.approved INTO events
  │    │
  │    ├─ detect completion (every 10s tick):
  │    │    JSONL check → ~/.claude/projects/<path>/*.jsonl
  │    │    OR capture-pane + activity timestamp
  │    │    │
  │    │    └─ on idle detected:
  │    │         git add -A && git commit
  │    │         git push origin towr/task-id
  │    │         gh pr create
  │    │         INSERT task.completed INTO events
  │    │
  │    └─ on failure:
  │         retry (re-dispatch) up to N times
  │         INSERT task.failed INTO events
  │
  └─ all tasks done → print summary → exit
```

## Agent Detection Methods

```
┌─────────────────────────────────────────────────────┐
│  Claude Code                                         │
│                                                      │
│  Primary: JSONL events                               │
│    ~/.claude/projects/<encoded-path>/*.jsonl          │
│    "last-prompt" type → definitively idle             │
│    file age < 60s → working                          │
│    file age 60-120s → inconclusive (check pane)      │
│    file age > 120s → stale/idle                      │
│                                                      │
│  Fallback: capture-pane                              │
│    ❯ prompt + 15s quiet → idle                       │
│    Dialog indicators → blocked                       │
│                                                      │
│  Dialog approval: Enter                              │
│  Startup trust: Enter                                │
├─────────────────────────────────────────────────────┤
│  Cursor CLI                                          │
│                                                      │
│  Detection: capture-pane only                        │
│    → prompt + 15s quiet → idle                       │
│    "Run this command?" → blocked                     │
│                                                      │
│  Dialog approval: y                                  │
│  Startup trust: a                                    │
├─────────────────────────────────────────────────────┤
│  Codex CLI                                           │
│                                                      │
│  Detection: capture-pane only                        │
│    › prompt + 15s quiet → idle                       │
│    "Would you like to run" → blocked                 │
│                                                      │
│  Dialog approval: Enter                              │
│  Startup trust: Enter                                │
│  Note: needs --no-alt-screen for tmux capture        │
└─────────────────────────────────────────────────────┘
```

## Safety Layers

```
plan.yaml
  │
  ▼
┌─ Layer 1: Workspace Isolation ──────────────────────┐
│  Each agent works in its own git worktree            │
│  Can't touch master, other workspaces, or unrelated  │
│  files. Changes are on a branch.                     │
└─────────────────────────────────────────────────────┘
  │
  ▼
┌─ Layer 2: Tool Allowlist ───────────────────────────┐
│  .claude/settings.json copied to each worktree       │
│  Pre-approves: Read, Write, Edit, go *, git *, gh *  │
│  Blocks: rm -rf, git push --force, git reset --hard  │
└─────────────────────────────────────────────────────┘
  │
  ▼
┌─ Layer 3: Agent Sandbox ────────────────────────────┐
│  Claude: scoped allowlist                            │
│  Cursor: workspace sandbox mode                      │
│  Codex: workspace-write sandbox                      │
└─────────────────────────────────────────────────────┘
  │
  ▼
┌─ Layer 4: Auto-Approve with Audit ──────────────────┐
│  Every approval recorded as task.approved event       │
│  Every bypass requires --reason and is flagged        │
│  Visible in dashboard + exportable via towr audit    │
└─────────────────────────────────────────────────────┘
  │
  ▼
┌─ Layer 5: PR Gate ──────────────────────────────────┐
│  Agents create PRs, never merge to main              │
│  Pre-land hooks run tests before merge               │
│  Humans review and merge                             │
└─────────────────────────────────────────────────────┘
```
