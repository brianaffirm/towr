# towr

You're running 4 things at once — an auth refactor, a billing fix, a test migration, and an AI agent experimenting with a new API client. Each one needs its own branch, its own working directory, and its own terminal context. You're juggling `git stash`, tab names, and "wait, which branch am I on?" Every time you land one, you hold your breath.

towr gives you isolated git worktrees per task, tracks them in one place, and lands them safely.

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

towr works with Claude Code, Cursor, Aider, or anything that runs in a terminal. Each agent gets its own isolated worktree — no branch conflicts, no stash juggling.

```bash
# Give an agent its own workspace
towr spawn "implement caching layer" --id cache --agent claude-code

# While it works, start another task yourself
towr spawn "update API docs" --id docs

# Check on both
towr ls

# Review the agent's work before landing
towr diff cache
towr land cache --dry-run    # preview what would merge

# Land it if it looks good
towr land cache
```

The `--agent` flag tags the workspace with the runtime identifier, tracked in the audit log. `towr preview --diff` pushes diffs into a tmux split pane so agents can see changes without switching context.

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
