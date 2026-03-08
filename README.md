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

```bash
go install github.com/brianho/amux/cmd/amux@latest
```

Or from source:

```bash
git clone https://github.com/brianho/amux.git
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
| `amux diff <id>` | Show changes |
| `amux open <id>` | Switch to workspace tmux session |
| `amux preview --diff` | Show diff in tmux split pane |
| `amux cleanup <id>` | Remove workspace |
| `amux doctor` | Diagnose problems |

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

Pre-land hooks block the merge if they fail.

## License

MIT
