# amux

You're running 4 things at once — an auth refactor, a billing fix, a test migration, and an AI agent experimenting with a new API client. Each one needs its own branch, its own working directory, and its own terminal context. You're juggling `git stash`, tab names, and "wait, which branch am I on?" Every time you land one, you hold your breath.

amux gives you isolated git worktrees per task, tracks them in one place, and lands them safely.

```
$ amux spawn "refactor auth" --id auth
$ amux spawn "fix billing" --id billing
$ amux spawn "migrate tests" --id tests

$ amux ls
ID        STATUS   HEALTH   ACTIVITY   DRIFT   DIFF       TREE    AGENT    AGE
auth      READY    pass     4m         0       +142/-38   ~3      claude   12m
billing   READY    fail     15m        +3      +67/-12    clean   claude   45m
tests     READY    pass     2h         +12     +89/-204   ~1      —        2h

$ amux land auth
Landed workspace auth
  Strategy:     rebase-ff
  Merge commit: a1b2c3d
  Files:        8 changed
  Hooks:        pre_land passed (2.3s)
  Cleanup:      worktree removed, branch deleted
```

## Why not just `git worktree`?

| | git worktree | amux |
|---|---|---|
| Create isolated workspace | `git worktree add` | `amux spawn` |
| See what's active | `git worktree list` (no diff stats) | `amux ls` (health, activity, drift, diff, tree, agent) |
| Review changes | manual `git diff` per worktree | `amux diff`, `amux preview --diff` |
| Merge safely | manual rebase + merge + cleanup | `amux land` (rebase, hooks, merge, cleanup) |
| Block bad merges | nothing built-in | pre-land hooks, protected branches |
| Track what happened | nothing | event-sourced audit log |
| Clean up stale work | manual | `amux cleanup --stale`, `amux doctor` |
| Dashboard | nothing | TUI with live refresh |

amux is the workflow around worktrees that git doesn't give you.

## Install

### Homebrew (macOS and Linux)

```bash
brew tap brianaffirm/tap
brew install amux
```

### From source

```bash
git clone https://github.com/brianaffirm/amux.git
cd amux && go install ./cmd/amux/
```

Requires Go 1.21+ and git. tmux is optional (enables terminal management and preview panes).

## Quick start

```bash
cd ~/my-project

# Create workspaces
amux spawn "add authentication" --id auth
amux spawn "fix payment flow" --id payments
amux spawn                       # quick: auto-generates ws-0001

# Already working on a branch? Adopt it
amux adopt                       # adopt current branch
amux adopt feature/login         # adopt by branch name

# Check status
amux ls

# Review changes
amux diff auth
amux preview --diff          # show diff in tmux split pane

# Land when ready
amux land auth               # rebase, validate hooks, merge, clean up
amux land payments --pr      # push + print PR URL

# Clean up
amux cleanup payments
amux doctor                  # find orphaned state
```

## How `land` works

Landing is where amux earns its keep. `amux land` is not a convenience alias for `git merge` — it's a pipeline:

1. **Validate** — check workspace status and branch state
2. **Run pre-land hooks** — your tests, lints, whatever. If they fail, the workspace is marked BLOCKED and the merge is aborted. Nothing lands dirty.
3. **Rebase onto base** — fast-forward onto the latest base branch. If there are conflicts, the workspace is marked BLOCKED with conflict details. You fix them, then re-land.
4. **Merge** — using your configured strategy (rebase-ff, squash, ff-only, or merge commit)
5. **Run post-land hooks** — notifications, deploys, whatever. Non-blocking.
6. **Clean up** — remove worktree, delete branch, archive workspace

If anything fails at any step, the workspace stays intact for inspection. Nothing is silently lost.

### Landing options

```bash
amux land auth                # local merge (default: rebase-ff)
amux land auth --squash       # squash all commits into one
amux land auth --pr           # push + generate PR URL instead of local merge
amux land auth --dry-run      # preview: check conflicts, show files changed
amux land auth billing tests --chain  # land sequentially, rebase remaining onto updated base
amux land auth --force --reason "hotfix"  # bypass status check with audit trail
```

Protected branches (`main`, `master`, `develop`, `release/*`) block local merge by default — use `--pr` or `--push` instead.

## Working with AI agents

amux works with Claude Code, Cursor, Aider, or anything that runs in a terminal. Each agent gets its own isolated worktree — no branch conflicts, no stash juggling.

```bash
# Give an agent its own workspace
amux spawn "implement caching layer" --id cache --agent claude-code

# While it works, start another task yourself
amux spawn "update API docs" --id docs

# Check on both
amux ls

# Review the agent's work before landing
amux diff cache
amux land cache --dry-run    # preview what would merge

# Land it if it looks good
amux land cache
```

The `--agent` flag tags the workspace with the runtime identifier, tracked in the audit log. `amux preview --diff` pushes diffs into a tmux split pane so agents can see changes without switching context.

## TUI Dashboard

Run `amux` with no arguments for an interactive dashboard:

<!-- TODO: add screenshot (see inbox 013) -->

- `j/k` navigate, `enter` detail view, `s` switch tmux session
- `d` full diff, `l` land, `c` cleanup with safety checks
- `a` toggle between current repo and all workspaces
- Live refresh every 2 seconds

## Commands

| Command | Description |
|---------|-------------|
| `amux spawn [task]` | Create workspace (branch + worktree). No args = auto-ID |
| `amux adopt [path-or-branch]` | Adopt existing worktree/branch as amux workspace |
| `amux ls` | List workspaces with diff stats and tree status |
| `amux land <id>` | Validate, rebase, merge, clean up |
| `amux land <id> --pr` | Push + print PR URL |
| `amux land <id> --squash` | Squash commits before merge |
| `amux land <id> --dry-run` | Preview merge without executing |
| `amux land <id1> <id2> --chain` | Land multiple workspaces sequentially |
| `amux diff <id>` | Show changes against base |
| `amux log <id>` | Show workspace event history |
| `amux open <id>` | Switch to workspace tmux session |
| `amux preview --diff` | Show diff in tmux split pane |
| `amux cleanup <id>` | Remove workspace |
| `amux cleanup --stale` | Remove workspaces older than threshold |
| `amux cleanup --merged` | Remove workspaces whose branches are merged |
| `amux doctor` | Diagnose orphaned worktrees, missing branches |
| `amux queue` | Show pending approval items |
| `amux approve/deny/respond <id>` | Resolve approval items |
| `amux shell-hook` | Print shell integration for prompt nudges |

All commands support `--json` for scripting.

## Shell integration

Add to your `~/.zshrc` or `~/.bashrc`:

```bash
eval "$(amux shell-hook)"
```

When you're on an untracked branch with uncommitted changes, amux nudges:

```
amux: untracked work on feat/auth — 'amux adopt' to track
```

## Configuration

Global config at `~/.amux/global-config.toml`, per-repo config at `.amux.toml` in your repo root (repo config overlays global):

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

State lives in `~/.amux/`, not in your repo. No files to gitignore, no risk of committing logs.

```
~/.amux/
  repos/<hash>/
    state.db        SQLite — workspace records + event-sourced state
    audit.jsonl     Append-only audit trail (every spawn, land, hook, conflict)
    config.toml     Per-repo config
  worktrees/<repo>/
    auth/           Git worktree for "auth" workspace
    billing/        Git worktree for "billing" workspace
```

Every action — spawn, land, hook execution, conflict, cleanup — is recorded as an immutable event. `amux log <id>` shows the full history. `audit.jsonl` is machine-readable for compliance or debugging.

## Requirements

- **git** 2.15+ (for `git worktree` support)
- **tmux** (optional) — enables `amux open`, `amux preview`, and TUI session switching. Without tmux, amux falls back gracefully: `open` prints the worktree path, `preview` is unavailable.

## License

MIT
