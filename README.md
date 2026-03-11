# towr

The operations layer for parallel AI-assisted development. Isolate, coordinate, validate, and land code changes across any number of agents and runtimes.

```
Claude Code, Cursor, Aider, Agent Teams, shell scripts, CI bots
                        |
                    towr (this tool)
            isolate | coordinate | validate | audit | land
                        |
                      git (source of truth)
```

**towr is NOT** an agent, a terminal emulator, or a Claude Code replacement. It's the layer beneath all of them — managing workspaces, coordinating work, enforcing validation gates, and keeping an audit trail.

## The problem

Running parallel AI coding agents produces code that needs to be safely isolated, validated, merged, and cleaned up. No tool owns this lifecycle. You end up with stale branches, orphaned worktrees, merge conflicts from file overlap, and no record of which agent changed what.

towr gives you isolated git worktrees per task, tracks them in one place, coordinates work across sessions, and lands them safely.

## How towr fits with Claude Code Agent Teams

Agent Teams coordinates work **within** a single project scope. towr coordinates work **across** project scopes. They stack:

```
Master Claude (you)
  └── towr dispatch auth "build auth with agent team"
        └── Claude Code → Agent Teams (3 teammates inside this workspace)
  └── towr dispatch billing "implement billing"
        └── Claude Code → Agent Teams (2 teammates inside this workspace)
  └── towr dispatch docs "update API docs"
        └── Claude Code (solo, no team needed)
```

| | Agent Teams | towr |
|---|---|---|
| **Scope** | Fast parallel work within one workspace | Cross-workspace lifecycle management |
| **Persistence** | Ephemeral — no resume, no memory | Event-sourced — full audit trail |
| **Runtimes** | Claude Code only | Any agent runtime |
| **Merge pipeline** | None — you merge manually | Validated landing with hooks |
| **Permission visibility** | Children block silently | Surfaces dialogs, selective approval |
| **State after crash** | Lost | `towr doctor` recovers in 60s |

Use Agent Teams for execution speed. Use towr for the lifecycle around it.

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

Completed task summaries are automatically injected as context into dependent prompts. Failed tasks retry up to `max_retries` before marking blocked.

The overnight workflow: `towr orchestrate plan.yaml`, go to sleep, check results in the morning.

| Command | Description |
|---------|-------------|
| `towr orchestrate <plan.yaml>` | Execute a declarative task plan with dependencies |
| `towr dispatch <id> "prompt"` | Send task to workspace (interactive default) |
| `towr dispatch <id> "prompt" --headless` | Autonomous mode via `claude -p` |
| `towr dispatch <id> "prompt" --wait` | Block until task completes or needs approval |
| `towr watch` | Monitor all workspaces, react to state changes |
| `towr watch --auto-approve` | Same, but auto-approve permission dialogs |
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
