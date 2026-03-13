# towr Web Dashboard Redesign

**Date:** 2026-03-13
**Status:** Approved

## Problem

The current dashboard is functional but unpolished — flat card layout, raw terminal text dump, noisy activity log, tiny safety shields. It serves developers but doesn't build trust with leadership. Needs to work for both audiences: quick glance at 7am (did the overnight run succeed?) and detailed monitoring during active work.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Layout | Command Center | Big numbers for glance, compact list for detail, drill-down for depth |
| Primary view | Full status sorted by urgency | Always show everything: blocked → working → completed |
| Terminal panel | Expand on click | Hidden by default, keeps dashboard clean |
| Terminal output | Structured summary | Parse steps (read → create → test → commit), not raw text dump |

## Architecture

### Layout Structure

```
┌─────────────────────────────────────────────────┐
│ HEADER: towr [live]  uptime 47m           [Export] │
├─────────────────────────────────────────────────┤
│ COUNTERS: 6 total | 2 working | 1 blocked | 3 done | 0 bypasses | 5 approvals │
├─────────────────────────────────────────────────┤
│ WORKSPACE LIST (sorted by urgency):              │
│  🔴 migration    codex    ⚠ permission  [Approve] │
│  🔵 auth-service claude   Running tests  🛡 safe  │
│  🔵 billing-api  cursor   Creating files 🛡 3 app │
│  🟢 tests        claude   ✓ PR #42      🛡 safe  │
│  🟢 docs         cursor   ✓ PR #41      🛡 safe  │
├─────────────────────────────────────────────────┤
│ EXPANDED VIEW (when workspace clicked):          │
│  ✓ Read project files         15s               │
│  ✓ Created pkg/auth/jwt.go    45s               │
│  ▶ Running tests              2s  [raw output]  │
│  ○ Commit & push              —                 │
│  [message input]              [Send] [Raw terminal] │
└─────────────────────────────────────────────────┘
```

### Score Cards

Six counters across the top, separated by 1px borders:
- **Total** (white) — all workspaces
- **Working** (blue #58a6ff) — RUNNING status
- **Blocked** (red #f85149) — BLOCKED or needs attention
- **Completed** (green #3fb950) — READY/IDLE with completed tasks
- **Bypasses** (green when 0, red when >0) — audit safety signal
- **Approvals** (white) — total auto-approvals across all workspaces

### Workspace Rows

Each workspace is a single row with:
- **Status dot** — 8px circle, colored by state, red gets a glow shadow
- **Workspace name** — bold
- **Agent badge** — muted text (claude-code, cursor, codex)
- **Current activity** — shown only in expanded view (derived from SSE stream). Collapsed rows show the `task` field from the API (e.g., "d-0001 ▶")
- **Safety shield** — inline text: "🛡 sandboxed" (green), "🛡 3 approved" (yellow), "🛡 bypass" (red)
- **Age** — relative time since creation
- **Left border** — 3px colored line matching status

Sorting: blocked first (red), working second (blue), completed last (green).

Completed workspace opacity:
- Completed < 30 min ago: opacity 0.7
- Completed 30min–2h ago: opacity 0.5
- Completed > 2h ago: opacity 0.3

Blocked rows get an inline **Approve** button.

### API Changes Required

**Extend `/api/workspaces` response** to include `agent` field:
```json
{
  "id": "auth-service",
  "status": "RUNNING",
  "task": "d-0001 ▶",
  "diff": "+142/-38",
  "age": "12m",
  "agent": "claude-code"
}
```

Change: add `agent` field from `ws.AgentRuntime` in `collectWorkspaces()` in web.go.

No other API changes needed — safety data comes from the existing `/api/workspace/:id/safety` endpoint, fetched per-card on load.

### Expanded Workspace View

Clicking a row expands it inline (pushes rows below down) showing:

**Structured Steps (Claude Code only):**
Each tool call / action from the agent parsed into a step:
- ✓ Completed step — green check, description, files affected, duration
- ▶ Current step — blue arrow, description, shows compact raw output (max 80px height, scrollable)
- ○ Pending step — gray circle, dimmed

**Step parsing heuristic** (from SSE stream at `/stream/:id`):
- Lines starting with `⏺` or `•` = new step (Claude Code / Codex patterns)
- `Read`, `Explored`, `Searched` = "Read files" step
- `Write`, `Added`, `Edited`, `Update` = "Created/Modified file" step
- `Bash(go test` = "Running tests" step
- `Bash(git add` / `git commit` = "Commit" step
- `Bash(gh pr` = "Creating PR" step

**Fallback for non-Claude agents (Cursor, Codex, Generic):**
If no structured steps are detected after 3 SSE updates, fall back to raw terminal view — show the SSE stream as scrollable monospace text (same as current behavior). This handles:
- Cursor CLI which uses `⬢` and `$` markers (different from Claude's `⏺`)
- Codex which uses `•` markers (partially compatible)
- Generic shell which has no markers

The raw terminal fallback is always available via the "Raw terminal" toggle button regardless of agent.

**Note:** The SSE endpoint is `/stream/:id` (not `/api/stream/:id`). The existing handler at line ~206 of web.go calls `term.CapturePane(id, 50)` every 2 seconds.

**Step history limitation:** The SSE stream is a 50-line rolling window, not a persistent log. The structured step view accumulates steps client-side as they arrive via SSE. Steps seen before the page was opened are not available — the view starts from "now." This is acceptable because the primary use case is monitoring active work, not reviewing completed work (use `towr log` for history).

**Action Bar** at bottom of expanded view:
- Text input: "Send a message to this agent..."
- Send button (POST /api/workspaces/:id/send)
- "Raw terminal" toggle — switches to raw SSE stream output (for developers)

### Header

- **towr** logo (bold text)
- **Live indicator** — green dot with pulse animation + "live" text
- **Uptime** — time since the dashboard page was loaded (client-side `Date.now() - pageLoadTime`). Not orchestration time — the dashboard doesn't know what plan is running.
- **Export button** — downloads CSV via `/api/audit/export?format=csv&since=7d`

### Safety Integration

No separate safety panel. Safety is woven into every row:
- Shield text is always visible inline on each workspace row
- Score cards include bypass and approval counts
- Blocked workspaces show the dialog text right in the row
- Color coding: green = clean, yellow = approvals, red = bypass

### Responsive Design

- **Desktop (>1024px):** Full layout as designed
- **Tablet (768-1024px):** Score cards wrap to 2 rows of 3
- **Mobile (<768px):** Score cards stack 2 per row, workspace rows become cards, expanded view goes full-width below

### Design Tokens

```css
--bg-page: #0a0a0a
--bg-card: #161b22
--bg-input: #0d1117
--border: #21262d
--border-bright: #30363d
--text-primary: #e6edf3
--text-secondary: #8b949e
--status-working: #58a6ff
--status-blocked: #f85149
--status-completed: #3fb950
--status-approval: #d29922
--accent: #1f6feb
--font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif
--font-mono: 'SF Mono', 'Cascadia Code', Consolas, monospace
--radius: 8px
```

## File Structure

```
cmd/towr/web/
  index.html         — semantic HTML shell
  css/
    style.css        — full design system + responsive
  js/
    app.js           — data fetching, score cards, workspace list rendering
    workspace.js     — expanded view, structured step parsing, raw terminal toggle
    activity.js      — activity log (keep existing, improve styling)
```

`terminal.js` is **removed** — its SSE connection and display logic is absorbed into `workspace.js` as part of the expanded view.

## What Changes

| File | Change |
|---|---|
| `index.html` | New structure: header, counters, list container. Remove sidebar/terminal panel divs. |
| `style.css` | Complete rewrite with design tokens, responsive breakpoints |
| `app.js` | Rewrite: score card rendering, urgency-sorted list, click-to-expand, safety fetch per card |
| `workspace.js` | New: replaces terminal.js. Step parser, expanded view renderer, raw terminal toggle, message input, SSE connection |
| `terminal.js` | **Deleted** — logic moved to workspace.js |
| `activity.js` | Minor: better event styling with approval/bypass highlighting, keep existing logic |
| `web.go` | Add `agent` field to `webWorkspace` struct and `collectWorkspaces()` |

## What Stays the Same

- API endpoints: /api/workspaces, /api/events, /api/workspace/:id/safety, /api/audit/export, /api/workspaces/:id/approve, /api/workspaces/:id/send
- SSE endpoint: /stream/:id
- Go embed.FS approach
- No external dependencies — still vanilla HTML/CSS/JS
- Single `go install` binary

## Out of Scope

- ANSI color rendering (replaced by structured steps + raw fallback)
- Authentication / multi-user
- Persistent layout preferences
- Notification sounds
- Plan name display (no data source — would need new API)
