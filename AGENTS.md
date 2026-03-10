# coflow Rules

These rules are managed by the coflow framework. Do not edit — they will be overwritten by `update-coflow.sh`.

## For AI Agents: Read in This Order

All project state lives in `.coflow/` (gitignored). Paths below are relative to the repo root.

### Always read first (< 50 lines):
1. **This file** — you're reading it now
2. **`.coflow/.dev/STATUS.md`** — current state, what's next, blockers

### Read when picking up new work:
3. **`.coflow/docs/inbox/`** — scan for items with `Status: ready` or `Status: design-resolved`

### Read only the sections relevant to your task:
4. **Project design doc** — single source of truth (read only what you need, not the whole file)

### Rarely needed:
5. `.coflow/docs/backlog.md` — future ideas; only check when inbox is empty
6. `.coflow/docs/archive/` — old iterations and meeting notes; only if explicitly asked
7. `.coflow/.dev/sessions/` — historical session logs; only for specific context

## Hard Gate

**Do NOT implement, plan, or write code for any feature or fix unless it has a corresponding inbox item in `.coflow/docs/inbox/` with `Status: ready` or `Status: design-resolved`.** No exceptions.

If the user asks to build something that isn't in the inbox, create a draft inbox item and ask the user to review/promote it before proceeding.

## Inbox Workflow

`.coflow/docs/inbox/` contains requirements. Lifecycle: `draft` → `ready` → `design-resolved` → archived.

- **draft**: Has open questions. Do NOT implement.
- **ready**: All questions resolved. Safe to implement.
- **design-resolved**: Design doc updated, code remains. (Design items only.)

### Item types
- **Design items** (`Type: design`): Update design doc first, then implement.
- **Bug/fix items** (`Type: fix`): Implement directly.

### Rules
1. Only implement items with `Status: ready` or `Status: design-resolved`
2. Do not modify Source, Context, or Requirements sections — they are human-authored
3. After implementation + tests pass, move item to `.coflow/docs/archive/inbox-resolved/`
4. Read the project's inbox workflow skill when picking up any inbox item

## Session Handoff

**Before ending any session**, update `.coflow/.dev/STATUS.md`:
- Overwrite the "Last Session" section with what you did
- Update "What's Next" with specific next steps
- Update "What's Broken" with any failing tests or known bugs
- Update "Blockers" if human input is needed
- If your session was substantial, write a detailed log to `.coflow/.dev/sessions/YYYY-MM-DD-<topic>.md`

Keep STATUS.md under 50 lines. Details go in session logs.

# amux

amux is a governed merge pipeline for AI-generated code: isolate, validate, audit, land — across any agent runtime.

## Project Details

- **Design doc**: `.coflow/docs/amux-design.md`
- **Inbox workflow skill**: `.coflow/skills/inbox-workflow-amux.md`
- **Release workflow**: `.coflow/RELEASING.md`

## Key Decisions

- Workspace operations layer, NOT an agent orchestrator
- Complements Claude Code Agent Teams — does not compete
- Event-sourced state with immutable audit log from day one
- Go binary, single install, zero dependencies
- Public repo: https://github.com/brianaffirm/amux

## Project Structure

```
cmd/amux/          CLI (cobra)
internal/
  workspace/       CRUD, git ops
  landing/         Merge pipeline, hooks, chain landing
  store/           SQLite event store, audit log
  git/             Low-level git
  config/          TOML loading, paths
  interruption/    Policy engine, 3-layer resolver
  queue/           Approval queue
  checkpoint/      Context preservation
  cli/             Output formatting, colors, JSON
  terminal/        tmux backend, headless mode
  tui/             Bubbletea dashboard + styles
```
