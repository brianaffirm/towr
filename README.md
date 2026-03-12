# towr

Ship features while you sleep — without giving agents the keys to the kingdom.

towr orchestrates parallel AI coding sessions with **granular permission control**: agents can edit files and run tests but can't `rm -rf` or force push. Every action is recorded in an immutable audit log. Every bypass requires a reason. Pre-land hooks block bad merges. Agents create PRs, not direct commits to main. Nothing lands without validation.

The agents work. You review PRs in the morning.

```yaml
# sprint.yaml — point at your Jira tickets, go to bed
name: "PROJ sprint 24"
tasks:
  - id: proj-1234
    prompt: "Read Jira ticket PROJ-1234 and implement what it describes. Run tests."
  - id: proj-1235
    prompt: "Read Jira ticket PROJ-1235 and implement it."
  - id: proj-1236
    prompt: "Read Jira ticket PROJ-1236. Depends on PROJ-1234."
    depends_on: [proj-1234]
settings:
  auto_approve: true    # scoped allowlist, not blanket permissions
  land_pr: true         # each task creates a PR, nothing merges to main
```

```bash
towr orchestrate sprint.yaml   # spawns 3 workspaces, agents read Jira tickets, create PRs
towr watch --react --all --auto-approve  # approves safe actions, monitors PRs, auto-fixes CI + reviews
# morning: 3 PRs ready for your review
```

The YAML can reference anything the agent can read — Jira tickets, GitHub issues, tech specs, design docs, Slack threads. Write it by hand or have Claude generate it from your sprint board.

## What towr does

**Orchestrate** — Define a YAML task graph. towr spawns isolated workspaces, dispatches prompts to Claude Code sessions, respects dependency order, merges upstream code into dependent tasks, auto-commits, and creates PRs.

**Watch** — Long-running monitor that auto-approves permission dialogs, detects CI failures on PRs and re-dispatches fixes, reads `@towr` review comments and dispatches replies, and notifies when PRs are ready to merge.

**Land safely** — Validated merge pipeline with pre-land hooks, rebase-ff, protected branch enforcement, and full cleanup. Not a git alias — a pipeline that blocks bad merges.

**Stay safe** — "Auto-approve" doesn't mean "approve everything." Agents get a `.claude/settings.json` allowlist: file edits, builds, and tests are pre-approved; `rm -rf`, `git push --force`, and network calls are blocked. This isn't `--dangerously-skip-permissions` — it's a scoped allowlist where you choose what's safe. Permissions that fall outside the allowlist are surfaced in the web dashboard and CLI, not silently skipped. Pre-land hooks run your test suite before any merge. Protected branches block direct pushes — agents create PRs, humans merge. Every `--force` or `--no-hooks` bypass is recorded in the audit log with a mandatory `--reason`. You can export the full trail with `towr audit --since 7d --csv` for compliance.

**Audit everything** — Every dispatch, approval, completion, and failure is recorded in an immutable event store. `towr audit --since 24h` exports the trail for compliance. `towr log <id>` shows per-workspace history. Bypass events (`--force`, `--no-hooks`) are flagged with `[BYPASS]`.

**See everything** — Web dashboard (`towr web`) with live workspace cards grouped by attention level, terminal streaming via SSE, activity feed, and action buttons. TUI dashboard (`towr`) for the terminal. `towr ls` for quick status.

## How towr fits with Claude Code Agent Teams

towr and Agent Teams solve different problems at different layers. They're designed to stack, not compete.

**Agent Teams** is a fast execution engine — one lead Claude spawns teammates that work in parallel within a single task. Great for: "implement this feature using 3 parallel workers." The teammates coordinate via SendMessage and task queues, share a session, and disappear when done.

**towr** is the operations layer above — it manages the workspaces those agents work in, the branches they commit to, the PRs they create, the CI that validates their code, and the review feedback loop that follows. It persists across sessions, works with any runtime, and keeps an audit trail.

They stack naturally:

```
You (or a master Claude session)
  │
  ├── towr dispatch auth "build auth" --agent claude-code
  │     └── Claude Code + Agent Teams (3 workers)
  │
  ├── towr dispatch billing "implement billing" --agent cursor
  │     └── Cursor CLI session
  │
  ├── towr dispatch docs "update API docs" --agent claude-code
  │     └── Claude Code (solo)
  │
  └── towr dispatch scripts "run migrations" --agent generic
        └── Plain bash

  towr watch --react --auto-approve
    → detects agent-specific permission dialogs (Enter for Claude, y for Cursor)
    → creates PRs when tasks complete
    → re-dispatches fixes when CI fails
    → replies to @towr review comments
```

Or in a plan — mix agents per task:

```yaml
tasks:
  - id: backend
    prompt: "Read Jira PROJ-101 and implement the API"
    agent: claude-code
  - id: frontend
    prompt: "Read Jira PROJ-102 and build the UI"
    agent: cursor
  - id: refactor
    prompt: "Read Jira PROJ-103 and refactor the data layer"
    agent: codex
  - id: tests
    prompt: "Write integration tests"
    depends_on: [backend, frontend, refactor]
settings:
  default_agent: claude-code
```

| | Agent Teams | towr |
|---|---|---|
| **What it does** | Fast parallel execution within one task | Lifecycle management across many tasks |
| **Scope** | Single workspace, single session | Multi-workspace, multi-session, multi-repo |
| **Persistence** | Ephemeral — gone when session ends | Event-sourced — survives crashes, reboots, sleep |
| **Runtimes** | Claude Code only | Claude Code, Cursor CLI, Codex CLI, Aider — mix per task |
| **Merge pipeline** | None — you merge manually | Validated: hooks → rebase → merge → cleanup |
| **PR workflow** | None | Auto-create PRs, monitor CI, respond to reviews |
| **Safety model** | `--dangerously-skip-permissions` (all or nothing) | Allowlist safe tools, block dangerous ones, audit bypasses |
| **State after crash** | Lost | `towr doctor` → full recovery in 60s |
| **Audit trail** | None | Immutable event log — every action, every bypass, exportable |

**Use Agent Teams when** you want 3 workers to build one feature fast.
**Use towr when** you want 5 features built overnight with PRs, CI, and review handling.
**Use both** when you want 5 features built overnight, each by a team of 3 workers.

```
$ towr spawn "refactor auth" --id auth
$ towr spawn "fix billing" --id billing
$ towr spawn "migrate tests" --id tests

$ towr ls
ID        STATUS   HEALTH   ACTIVITY   DRIFT   DIFF       TREE    AGENT    AGE
auth      READY    pass     4m         0       +142/-38   ~3      claude   12m
billing   READY    fail     15m        +3      +67/-12    clean   claude   45m
tests     READY    pass     2h         +12     +89/-204   ~1      —        2h

$ towr land auth
Landed workspace auth
  Strategy:     rebase-ff
  Merge commit: a1b2c3d
  Files:        8 changed
  Hooks:        pre_land passed (2.3s)
  Cleanup:      worktree removed, branch deleted
```

## Why not just `git worktree`?

| | git worktree | towr |
|---|---|---|
| Create isolated workspace | `git worktree add` | `towr spawn` |
| See what's active | `git worktree list` (no diff stats) | `towr ls` (health, activity, drift, diff, tree, agent) |
| Review changes | manual `git diff` per worktree | `towr diff`, `towr preview --diff` |
| Merge safely | manual rebase + merge + cleanup | `towr land` (rebase, hooks, merge, cleanup) |
| Block bad merges | nothing built-in | pre-land hooks, protected branches |
| Track what happened | nothing | event-sourced audit log |
| Clean up stale work | manual | `towr cleanup --stale`, `towr doctor` |
| Dashboard | nothing | TUI with live refresh |

towr is the workflow around worktrees that git doesn't give you.

## Install

### Homebrew (macOS and Linux)

```bash
brew tap brianaffirm/tap
brew install towr
```

### From source

```bash
git clone https://github.com/brianaffirm/towr.git
cd towr && go install ./cmd/towr/
```

Requires Go 1.21+ and git. tmux is optional (enables terminal management and preview panes).

## Quick start

```bash
cd ~/my-project

# Create workspaces
towr spawn "add authentication" --id auth
towr spawn "fix payment flow" --id payments
towr spawn                       # quick: auto-generates ws-0001
towr spawn "fix billing" --env SUBPROJECT=services/billing  # monolith

# Already working on a branch? Adopt it
towr adopt                       # adopt current branch
towr adopt feature/login         # adopt by branch name

# Check status
towr ls

# Review changes
towr diff auth
towr preview --diff          # show diff in tmux split pane

# Land when ready
towr land auth               # rebase, validate hooks, merge, clean up
towr land payments --pr      # push + print PR URL

# Clean up
towr cleanup payments
towr doctor                  # find orphaned state
```

## How `land` works

Landing is where towr earns its keep. `towr land` is not a convenience alias for `git merge` — it's a pipeline:

1. **Validate** — check workspace status and branch state
2. **Run pre-land hooks** — your tests, lints, whatever. If they fail, the workspace is marked BLOCKED and the merge is aborted. Nothing lands dirty.
3. **Rebase onto base** — fast-forward onto the latest base branch. If there are conflicts, the workspace is marked BLOCKED with conflict details. You fix them, then re-land.
4. **Merge** — using your configured strategy (rebase-ff, squash, ff-only, or merge commit)
5. **Run post-land hooks** — notifications, deploys, whatever. Non-blocking.
6. **Clean up** — remove worktree, delete branch, archive workspace

If anything fails at any step, the workspace stays intact for inspection. Nothing is silently lost.

### Landing options

```bash
towr land auth                # local merge (default: rebase-ff)
towr land auth --squash       # squash all commits into one
towr land auth --pr           # push + generate PR URL instead of local merge
towr land auth --dry-run      # preview: check conflicts, show files changed
towr land auth billing tests --chain  # land sequentially, rebase remaining onto updated base
towr land auth --force --reason "hotfix"  # bypass status check with audit trail
```

Protected branches (`main`, `master`, `develop`, `release/*`) block local merge by default — use `--pr` or `--push` instead.

## Working with AI agents

towr works with Claude Code, Cursor, Aider, or anything that runs in a terminal. Each agent gets its own isolated worktree — no branch conflicts, no stash juggling, no "which agent is editing which file?"

```bash
# Give agents their own workspaces
towr spawn "implement caching layer" --id cache
towr spawn "update API docs" --id docs
towr spawn "fix flaky tests" --id tests

# One dashboard for everything
towr ls
# ID      STATUS    TASK          DIFF        TREE    AGE
# cache   RUNNING   d-0001 ▶    +142/-38    ~3      12m
# docs    IDLE      d-0001 ✓    +67/-12     clean   45m
# tests   RUNNING   d-0001 ▶    +89/-204    ~1      2h

# Check for file overlaps before merging (catch conflicts early)
towr overlap

# Review and land
towr diff cache
towr land cache              # rebase, validate hooks, merge, clean up
towr land docs --pr          # push + print PR URL
```

Every action is recorded in an immutable audit log — which agent changed what, when, and why. `towr log <id>` shows the full history.

## Dispatch orchestration

Use one "master" Claude Code session to coordinate multiple child sessions across workspaces. The master dispatches tasks, monitors progress, approves permissions, and sequences dependent work.

```bash
# Spawn workspaces
towr spawn "auth middleware" --id auth
towr spawn "billing service" --id billing

# Dispatch tasks (interactive mode — launches Claude REPL in each tmux session)
towr dispatch auth "Implement JWT middleware in internal/auth/jwt.go"
towr dispatch billing "Add Stripe webhook handler"

# Check progress
towr ls
# ID        STATUS    TASK          DIFF
# auth      RUNNING   d-0001 ▶    +0/-0
# billing   RUNNING   d-0001 ▶    +0/-0

# Wait for a workspace — surfaces permission dialogs
towr wait auth
# ⚠ auth d-0001: Do you want to create jwt.go?
#   Run: towr send auth --approve

# Approve and continue waiting
towr send auth --approve
towr wait auth
# ✓ auth d-0001: Created jwt.go with middleware...

# Send follow-up to a running session
towr send auth "Now add unit tests for the JWT middleware" --wait

# Headless mode for fully autonomous tasks (no permission prompts)
towr dispatch billing "refactor handlers" --headless
```

### Autonomous monitoring

Instead of manually waiting and approving each workspace, `towr watch` monitors all workspaces and reacts automatically:

```bash
# Dispatch tasks, then let watch handle the rest
towr dispatch auth "implement JWT middleware"
towr dispatch billing "add Stripe webhooks"
towr dispatch tests "write integration tests"

# Monitor all workspaces, auto-approve permission dialogs
towr watch --auto-approve
# [19:30:15] Watching 3 workspaces (poll: 10s, auto-approve: on)
# [19:30:25] ▶ auth: working
# [19:30:35] ⚠ auth: permission dialog — "Do you want to create jwt.go?"
# [19:30:35] ✓ auth: auto-approved
# [19:30:55] ✓ auth d-0001: completed — "Created jwt.go with middleware..."
# [19:31:25] ✓ tests d-0001: completed
# [19:31:25] All workspaces idle.
```

### Declarative task plans

For multi-step projects with dependencies, define a YAML plan and let towr execute it end-to-end:

```yaml
# plan.yaml
name: "build todo app"
tasks:
  - id: models
    prompt: "Create todo/store.go with Todo struct and CRUD methods"
  - id: tests
    prompt: "Create todo/store_test.go with table-driven tests"
  - id: cli
    prompt: "Create cobra CLI with add/list/complete/delete commands"
    depends_on: [models]
  - id: integration
    prompt: "Run go test ./... and fix any failures"
    depends_on: [models, tests, cli]
settings:
  auto_approve: true
  max_retries: 2
  land_pr: true       # auto-push + create PR on task completion (workspace stays alive)
```

```bash
towr orchestrate plan.yaml
# [19:30:00] Orchestrating "build todo app" (4 tasks, auto-approve: on)
# [19:30:02] ▶ models: dispatched
# [19:30:02] ▶ tests: dispatched (no deps)
# [19:30:30] ✓ models: completed — "Created todo/store.go..."
# [19:30:30] ▶ cli: dispatched (deps: models ✓)
# [19:31:00] ✓ tests: completed
# [19:31:15] ✓ cli: completed
# [19:31:15] ▶ integration: dispatched (deps: models ✓, tests ✓, cli ✓)
# [19:31:45] ✓ integration: completed — "All tests pass"
# Plan "build todo app" completed: 4/4 tasks succeeded.
```

When a task's dependencies complete, towr automatically:
- **Merges dependency branches** into the dependent workspace (so the child Claude has upstream code)
- **Injects completion summaries** as context into the prompt
- **Auto-commits** any uncommitted files when a task finishes
- **Retries** failed tasks up to `max_retries` before marking blocked

### PR monitoring and auto-fix

`towr watch --react` monitors open PRs on `towr/*` branches and auto-reacts to CI failures and review feedback:

```bash
# Run from anywhere — monitors all repos
towr watch --react --all --auto-approve

# [19:35:00] ✗ PR #42 (towr/auth): CI failed — dispatching fix
# [19:38:00] ✓ auth d-0002: completed (CI fix pushed)
# [19:40:00] 💬 PR #42 (towr/auth): changes requested — dispatching fix
# [19:43:00] ✓ auth d-0003: completed (review fixes pushed)
# [19:45:00] ✓ PR #42 (towr/auth): approved + CI passing — ready to merge
```

The overnight workflow:

```bash
# Start the watcher (runs forever in its own tmux session)
towr spawn "control plane" --id watcher --path /tmp/towr-watcher
tmux send-keys -t towr/watcher:chat "towr watch --react --all --auto-approve" Enter

# Orchestrate your work plan
towr orchestrate plan.yaml
# → tasks complete → PRs created → orchestrate exits
# → reviewer comments next morning → watch auto-dispatches fixes
# → CI fails → watch auto-dispatches fixes
# → approved + green → watch notifies
```

| Command | Description |
|---------|-------------|
| `towr orchestrate <plan.yaml>` | Execute a declarative task plan with dependencies |
| `towr dispatch <id> "prompt"` | Send task to workspace (interactive default) |
| `towr dispatch <id> "prompt" --headless` | Autonomous mode via `claude -p` |
| `towr dispatch <id> "prompt" --wait` | Block until task completes or needs approval |
| `towr watch --react --all` | Monitor all workspaces + PRs, auto-react to feedback |
| `towr watch --auto-approve` | Auto-approve permission dialogs |
| `towr send <id> "message"` | Send follow-up to interactive session |
| `towr send <id> --approve` | Approve a permission dialog |
| `towr wait <id>` | Wait for current task (`--any`/`--all` for multi-workspace) |
| `towr promote <id>` | Attach to tmux session for hands-on debugging |

## TUI Dashboard

Run `towr` with no arguments for an interactive dashboard:

<!-- TODO: add screenshot (see inbox 013) -->

- `j/k` navigate, `enter` detail view, `s` switch tmux session
- `d` full diff, `l` land, `c` cleanup with safety checks
- `a` toggle between current repo and all workspaces
- Live refresh every 2 seconds

## Web Dashboard

`towr web` starts a local HTTP server with a real-time browser dashboard:

```bash
towr web                    # http://127.0.0.1:8090
towr web --addr :9000       # custom port
```

- **Workspace cards** grouped by attention zone: Working (blue), Needs Attention (red), Completed (green)
- **Live terminal view** — click any card to stream its tmux output in real-time
- **Activity feed** — reverse-chronological event log with color-coded entries
- **Stats bar** — total/working/blocked/completed counts at a glance
- **Action buttons** — approve permission dialogs and send messages from the browser
- **Auto-refresh** every 5s, dark theme, responsive, zero external dependencies

API endpoints:
- `GET /api/workspaces` — JSON workspace list for scripting
- `GET /api/events` — recent audit events
- `GET /api/stream/<id>` — SSE live terminal output
- `POST /api/workspaces/<id>/approve` — approve permission dialog
- `POST /api/workspaces/<id>/send` — send message to workspace

All static assets embedded in the Go binary via `embed.FS` — single `go install`, nothing else needed.

## Commands

| Command | Description |
|---------|-------------|
| `towr spawn [task] [--env K=V]` | Create workspace (branch + worktree). No args = auto-ID |
| `towr adopt [path-or-branch]` | Adopt existing worktree/branch as towr workspace |
| `towr ls` | List workspaces with diff stats and tree status |
| `towr land <id>` | Validate, rebase, merge, clean up |
| `towr land <id> --pr` | Push + print PR URL |
| `towr land <id> --squash` | Squash commits before merge |
| `towr land <id> --dry-run` | Preview merge without executing |
| `towr land <id1> <id2> --chain` | Land multiple workspaces sequentially |
| `towr diff <id>` | Show changes against base |
| `towr log <id>` | Show workspace event history |
| `towr open <id>` | Switch to workspace tmux session |
| `towr preview --diff` | Show diff in tmux split pane |
| `towr cleanup <id>` | Remove workspace |
| `towr cleanup --stale` | Remove workspaces older than threshold |
| `towr cleanup --merged` | Remove workspaces whose branches are merged |
| `towr doctor` | Diagnose orphaned worktrees, missing branches |
| `towr queue` | Show pending approval items |
| `towr approve/deny/respond <id>` | Resolve approval items |
| `towr overlap` | Detect file overlaps between workspaces (merge conflict risk) |
| `towr web` | Start local HTTP dashboard (JSON API + SSE streaming) |
| `towr shell-hook` | Print shell integration for prompt nudges |

All commands support `--json` for scripting.

## Shell integration

Add to your `~/.zshrc` or `~/.bashrc`:

```bash
eval "$(towr shell-hook)"
```

When you're on an untracked branch with uncommitted changes, towr nudges:

```
towr: untracked work on feat/auth — 'towr adopt' to track
```

## Configuration

Global config at `~/.towr/global-config.toml`, per-repo config at `.towr.toml` in your repo root (repo config overlays global):

```toml
[defaults]
base_branch = "main"
merge_strategy = "rebase-ff"   # rebase-ff | squash | ff-only | merge

[hooks]
post_create = "cd ${WORKTREE_PATH} && npm install"
pre_land = "cd ${WORKTREE_PATH} && npm test"

[landing]
protected_branches = ["main", "master", "develop", "release/*"]

[workspace]
copy_paths = [".env.local"]         # copied into each worktree
link_paths = ["node_modules"]       # symlinked to save disk/time

[cleanup]
stale_threshold = "7d"
```

Hook variables: `${WORKSPACE_ID}`, `${WORKTREE_PATH}`, `${BRANCH}`, `${BASE_BRANCH}`, `${REPO_ROOT}`.

Pre-land hooks block the merge if they fail. Post-land hooks are non-blocking.

## How it works

State lives in `~/.towr/`, not in your repo. No files to gitignore, no risk of committing logs.

```
~/.towr/
  repos/<hash>/
    state.db        SQLite — workspace records + event-sourced state
    audit.jsonl     Append-only audit trail (every spawn, land, hook, conflict)
    config.toml     Per-repo config
  worktrees/<repo>/
    auth/           Git worktree for "auth" workspace
    billing/        Git worktree for "billing" workspace
```

Every action — spawn, land, hook execution, conflict, cleanup — is recorded as an immutable event. `towr log <id>` shows the full history. `audit.jsonl` is machine-readable for compliance or debugging.

## Requirements

- **git** 2.15+ (for `git worktree` support)
- **tmux** (optional) — enables `towr open`, `towr preview`, and TUI session switching. Without tmux, towr falls back gracefully: `open` prints the worktree path, `preview` is unavailable.

## License

MIT
