# amux

Manage parallel AI agent workspaces with git. Spawn isolated worktrees, review changes, merge cleanly.

```
$ amux spawn "refactor auth" --id auth
$ amux spawn "fix billing" --id billing

$ amux ls
ID        STATUS  BASE    BRANCH          DIFF       TREE    AGE
auth      READY   main    amux/auth       +142/-38   ~3      12m
billing   READY   main    amux/billing    +67/-12    clean   45m

$ amux land auth
Landed workspace auth
  Merge commit: a1b2c3d
  Files:        8 changed
```

## What it does

- `amux spawn` creates an isolated git worktree + branch per task
- `amux ls` shows all workspaces with diff stats and worktree status
- `amux land` rebases, validates hooks, merges, and cleans up
- `amux` (no args) opens an interactive TUI dashboard
- `amux preview --diff` pushes diffs to a tmux split pane for agents

Works with Claude Code, Cursor, Aider, or anything that runs in a terminal.

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

# Check status
amux ls

# Review changes
amux diff auth
amux preview --diff          # show diff in tmux split pane

# Land when ready
amux land auth               # local merge (rebase-ff)
amux land payments --pr      # push + PR URL

# Clean up
amux cleanup payments
amux doctor                  # find orphaned state
```

## TUI Dashboard

Run `amux` with no arguments for an interactive dashboard:

- `j/k` navigate, `enter` detail view, `s` switch tmux session
- `d` full diff, `l` land, `c` cleanup with safety checks
- `a` toggle between current repo and all workspaces
- Polls for live updates every 2 seconds

## Commands

| Command | Description |
|---------|-------------|
| `amux spawn <task> --id <id>` | Create workspace (branch + worktree) |
| `amux ls` | List workspaces |
| `amux land <id>` | Validate, merge, clean up |
| `amux land <id> --pr` | Push + print PR URL |
| `amux land <id> --squash` | Squash commits before merge |
| `amux land <id> --dry-run` | Preview merge without executing |
| `amux diff <id>` | Show changes |
| `amux open <id>` | Switch to workspace tmux session |
| `amux preview --diff` | Show diff in tmux split pane |
| `amux cleanup <id>` | Remove workspace |
| `amux doctor` | Diagnose problems |
| `amux queue` | Show pending approval items |

All commands support `--json` for scripting.

## Configuration

Per-repo config at `~/.amux/repos/<hash>/config.toml`:

```toml
[defaults]
base_branch = "main"

[hooks]
post_create = "cd ${WORKTREE_PATH} && npm install"
pre_land = "cd ${WORKTREE_PATH} && npm test"
```

Pre-land hooks block the merge if they fail. Available variables: `${WORKSPACE_ID}`, `${WORKTREE_PATH}`, `${BRANCH}`, `${BASE_BRANCH}`, `${REPO_ROOT}`.

## How it works

State lives in `~/.amux/`, not in your repo. No files to gitignore, no risk of committing logs.

```
~/.amux/
  repos/<hash>/
    state.db        SQLite — workspace records + events
    audit.jsonl     Append-only audit trail
    config.toml     Per-repo config
  worktrees/<repo>/
    auth/           Git worktree for "auth" workspace
    billing/        Git worktree for "billing" workspace
```

## Requirements

- **git** 2.15+ (for `git worktree` support)
- **tmux** (optional) — enables `amux open`, `amux preview`, and TUI session switching. Without tmux, `open` prints the worktree path and `preview` is unavailable.

## License

MIT
