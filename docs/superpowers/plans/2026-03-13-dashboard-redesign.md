# Dashboard Redesign Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current flat-card web dashboard with a Command Center layout — big number counters, urgency-sorted workspace list, click-to-expand structured step view, inline safety shields.

**Architecture:** Single-page vanilla HTML/CSS/JS app served from Go embed.FS. No build toolchain. All data from existing JSON/SSE API endpoints. One new field (`agent`) added to `/api/workspaces`. `terminal.js` deleted, replaced by `workspace.js`.

**Tech Stack:** Go (embed.FS, net/http), vanilla HTML/CSS/JS, SSE (EventSource)

**Spec:** `docs/superpowers/specs/2026-03-13-dashboard-redesign-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `cmd/towr/web.go` | Modify lines 274-280, 335-341 | Add `agent` field to webWorkspace struct and collectWorkspaces |
| `cmd/towr/web/index.html` | Rewrite (36 → ~40 lines) | Semantic shell: header, counters, workspace list, activity drawer |
| `cmd/towr/web/css/style.css` | Rewrite (145 → ~250 lines) | Design system with tokens, responsive, all component styles |
| `cmd/towr/web/js/app.js` | Rewrite (193 → ~200 lines) | Data fetch, score cards, urgency-sorted list, click-to-expand |
| `cmd/towr/web/js/workspace.js` | Create (~150 lines) | Expanded view: step parser, SSE connection, raw toggle, send message |
| `cmd/towr/web/js/activity.js` | Modify (101 → ~110 lines) | Improve event styling with approval/bypass highlighting |
| `cmd/towr/web/js/terminal.js` | Delete | Logic absorbed into workspace.js |

---

## Chunk 1: Backend + HTML Shell

### Task 1: Add `agent` field to webWorkspace API

**Files:**
- Modify: `cmd/towr/web.go:274-280` (webWorkspace struct)
- Modify: `cmd/towr/web.go:335-341` (collectWorkspaces)

- [ ] **Step 1: Add Agent field to webWorkspace struct**

In `cmd/towr/web.go`, change the struct at line 274:

```go
type webWorkspace struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Task   string `json:"task"`
	Diff   string `json:"diff"`
	Age    string `json:"age"`
	Agent  string `json:"agent"`
}
```

- [ ] **Step 2: Populate Agent in collectWorkspaces**

In `cmd/towr/web.go`, change the row construction at line 335:

```go
rows = append(rows, webWorkspace{
	ID:     ws.ID,
	Status: ws.Status,
	Task:   task,
	Diff:   diffStr,
	Age:    cli.FormatAgeFromString(ws.CreatedAt),
	Agent:  ws.AgentRuntime,
})
```

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/towr/ && go vet ./...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add cmd/towr/web.go
git commit -m "feat(web): add agent field to /api/workspaces response"
```

### Task 2: Rewrite index.html

**Files:**
- Rewrite: `cmd/towr/web/index.html`
- Delete: `cmd/towr/web/js/terminal.js`

- [ ] **Step 1: Write new index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>towr — dashboard</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'><text y='14' font-size='14'>🗼</text></svg>">
<link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
<header id="header">
  <div class="header-left">
    <h1>towr</h1>
    <span class="live-badge"><span class="pulse"></span>live</span>
  </div>
  <div class="header-right">
    <span class="header-meta" id="uptime"></span>
    <button class="btn-export" id="exportAudit">Export</button>
  </div>
</header>
<div class="counters" id="counters"></div>
<main class="workspace-list" id="workspaceList"></main>
<div class="activity-drawer" id="activityDrawer">
  <button class="activity-toggle" id="actToggle">
    <span class="arrow">▶</span> Activity <span class="badge" id="actCount">0</span>
  </button>
  <div class="activity-feed" id="actFeed"></div>
</div>
<script src="/static/js/workspace.js"></script>
<script src="/static/js/activity.js"></script>
<script src="/static/js/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Delete terminal.js**

```bash
rm cmd/towr/web/js/terminal.js
```

- [ ] **Step 3: Build to verify embed**

Run: `go build ./cmd/towr/`
Expected: clean build (embed picks up new index.html, no terminal.js reference needed)

- [ ] **Step 4: Commit**

```bash
git add cmd/towr/web/index.html
git rm cmd/towr/web/js/terminal.js
git commit -m "feat(web): rewrite index.html shell, remove terminal.js"
```

---

## Chunk 2: Design System (CSS)

### Task 3: Rewrite style.css

**Files:**
- Rewrite: `cmd/towr/web/css/style.css`

- [ ] **Step 1: Write the complete design system**

Write `cmd/towr/web/css/style.css` with these sections:
1. **Design tokens** as CSS custom properties on `:root`
2. **Reset + base** — dark background, system font
3. **Header** — flex row, logo, live badge with pulse animation, export button
4. **Counters** — flex row with 1px gaps, big numbers, responsive wrap
5. **Workspace list** — vertical stack of rows
6. **Workspace row** — flex row with status dot, left border, hover state, opacity for completed
7. **Expanded view** — border highlight, step list, raw output area, action bar
8. **Activity drawer** — fixed bottom, toggle button, scrollable feed
9. **Buttons** — ghost style (export), accent style (send, approve)
10. **Responsive** — breakpoints at 1024px and 768px
11. **Animations** — pulse for live dot, fade-in for new rows

Design tokens from spec:
```css
:root {
  --bg-page: #0a0a0a;
  --bg-card: #161b22;
  --bg-input: #0d1117;
  --border: #21262d;
  --border-bright: #30363d;
  --text-primary: #e6edf3;
  --text-secondary: #8b949e;
  --status-working: #58a6ff;
  --status-blocked: #f85149;
  --status-completed: #3fb950;
  --status-approval: #d29922;
  --accent: #1f6feb;
  --font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
  --font-mono: 'SF Mono', 'Cascadia Code', Consolas, monospace;
  --radius: 8px;
}
```

Key styles:
- `.workspace-row` — `background: var(--bg-card); border-left: 3px solid <status-color>; border-radius: var(--radius); padding: 14px 16px; display: flex; align-items: center; gap: 12px; cursor: pointer; transition: opacity 0.3s;`
- `.workspace-row.blocked .status-dot` — `box-shadow: 0 0 8px var(--status-blocked);`
- `.workspace-row.completed` — `opacity: 0.7` (recent), `0.5` (older), `0.3` (old)
- `.expanded` — `border: 1px solid <status-color>44;`
- `.step` — flex row: icon + description + duration
- `.counter` — big number (32px bold) + small label below
- `@keyframes pulse` — scale 1→1.5→1 on the live dot

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/towr/`

- [ ] **Step 3: Commit**

```bash
git add cmd/towr/web/css/style.css
git commit -m "feat(web): design system with tokens, responsive breakpoints"
```

---

## Chunk 3: Core Application JS

### Task 4: Rewrite app.js

**Files:**
- Rewrite: `cmd/towr/web/js/app.js`

- [ ] **Step 1: Write app.js with these functions**

Structure:
```javascript
(function() {
  "use strict";
  var POLL_MS = 4000;
  var pageLoadTime = Date.now();

  // --- Score Cards ---
  function renderCounters(workspaces) { /* count by status, render 6 counter divs */ }

  // --- Urgency Sort ---
  function urgencySort(a, b) { /* blocked=0, running=1, idle/ready=2, other=3 */ }

  // --- Workspace List ---
  function renderWorkspaces(workspaces) {
    // Sort by urgency
    // For each workspace, build a row div
    // Row contains: status dot, name, agent badge, task, safety shield, age
    // Completed rows get opacity based on age
    // Blocked rows get Approve button
    // Click handler calls window.expandWorkspace(id)
    // Only rebuild if data changed (compare JSON string)
  }

  // --- Safety Shields ---
  function fetchSafety(id) {
    // GET /api/workspace/{id}/safety
    // Update shield text inline on the row
  }

  // --- Uptime ---
  function updateUptime() {
    var elapsed = Math.floor((Date.now() - pageLoadTime) / 1000);
    // Format as "Xm" or "Xh Ym"
  }

  // --- Poll Loop ---
  function poll() {
    Promise.all([
      fetch("/api/workspaces").then(function(r) { return r.json(); }),
      fetch("/api/events").then(function(r) { return r.json(); })
    ]).then(function(results) {
      renderCounters(results[0]);
      renderWorkspaces(results[0]);
      // Fetch safety for each workspace
      results[0].forEach(function(ws) { fetchSafety(ws.id); });
      // Render activity
      if (window.renderActivity) window.renderActivity(results[1]);
      updateUptime();
    }).catch(function() {});
    setTimeout(poll, POLL_MS);
  }

  // --- Export ---
  document.getElementById("exportAudit").addEventListener("click", function() {
    window.location.href = "/api/audit/export?format=csv&since=7d";
  });

  poll();
})();
```

Key details:
- `urgencySort`: blocked (status contains "BLOCK")=0, running=1, idle/ready with completed task=2, everything else=3
- Completed opacity: parse age string, <30m → 0.7, 30m-2h → 0.5, >2h → 0.3
- Safety fetch is per-workspace, updates a `[data-safety="<id>"]` span
- `renderWorkspaces` compares `JSON.stringify(data)` to previous and skips rebuild if unchanged
- Approve button POSTs to `/api/workspaces/<id>/approve`

- [ ] **Step 2: Build**

Run: `go build ./cmd/towr/`

- [ ] **Step 3: Commit**

```bash
git add cmd/towr/web/js/app.js
git commit -m "feat(web): command center layout with counters and urgency-sorted list"
```

---

## Chunk 4: Workspace Expanded View

### Task 5: Create workspace.js

**Files:**
- Create: `cmd/towr/web/js/workspace.js`

- [ ] **Step 1: Write workspace.js**

Structure:
```javascript
(function() {
  "use strict";
  var activeId = null;
  var evtSource = null;
  var steps = [];
  var rawMode = false;
  var rawLines = [];

  // --- Step Parser ---
  // Parse SSE lines into structured steps
  function parseStep(line) {
    // Claude Code patterns:
    // "⏺ Read" / "⏺ Explored" / "⏺ Searched" → type: "read"
    // "⏺ Write" / "⏺ Added" / "⏺ Edited" / "⏺ Update" → type: "write"
    // "⏺ Bash(go test" → type: "test"
    // "⏺ Bash(git add" / "git commit" → type: "commit"
    // "⏺ Bash(gh pr" → type: "pr"
    // "⏺ Bash(" → type: "shell"
    // "⏺" or "•" with other text → type: "action"
    // Codex: "•" prefix → same mapping
    // Return null if no step marker found
  }

  function stepIcon(step) {
    // completed: "✓" (green), current: "▶" (blue), pending: "○" (gray)
  }

  function stepLabel(step) {
    // "Read project files", "Created jwt.go (+87 lines)", "Running tests", etc.
  }

  // --- Expand/Collapse ---
  window.expandWorkspace = function(id) {
    if (activeId === id) { collapseWorkspace(); return; }
    collapseWorkspace();
    activeId = id;
    steps = [];
    rawLines = [];
    rawMode = false;
    // Insert expanded div after the row
    // Connect SSE to /stream/<id>
    connectSSE(id);
    renderExpanded();
  };

  function collapseWorkspace() {
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    var el = document.querySelector(".workspace-expanded");
    if (el) el.remove();
  }

  // --- SSE Connection ---
  function connectSSE(id) {
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    var noStructuredCount = 0;
    evtSource.onmessage = function(e) {
      rawLines.push(e.data);
      if (rawLines.length > 500) rawLines.shift();
      var step = parseStep(e.data);
      if (step) {
        // Mark previous current step as completed
        if (steps.length > 0) steps[steps.length-1].status = "completed";
        step.status = "current";
        steps.push(step);
        noStructuredCount = 0;
      } else {
        noStructuredCount++;
        // If current step exists, append line to its output
        if (steps.length > 0 && steps[steps.length-1].status === "current") {
          steps[steps.length-1].output = (steps[steps.length-1].output || "") + e.data + "\n";
        }
      }
      // Fallback: if 3+ SSE updates with no structured steps, switch to raw
      if (noStructuredCount >= 3 && steps.length === 0) rawMode = true;
      renderExpanded();
    };
    evtSource.onerror = function() {
      if (evtSource && evtSource.readyState === EventSource.CLOSED) {
        // Stream ended
      }
    };
  }

  // --- Render ---
  function renderExpanded() {
    var container = document.querySelector(".workspace-expanded");
    if (!container) {
      container = document.createElement("div");
      container.className = "workspace-expanded";
      var row = document.querySelector('[data-id="' + activeId + '"]');
      if (row) row.after(container);
    }

    if (rawMode) {
      // Show raw terminal output
      container.innerHTML = renderRaw();
    } else {
      // Show structured steps + action bar
      container.innerHTML = renderSteps() + renderActionBar();
    }
    // Auto-scroll raw output
    var output = container.querySelector(".raw-output");
    if (output) output.scrollTop = output.scrollHeight;
  }

  function renderSteps() {
    // For each step: icon + label + duration + compact output for current step
  }

  function renderRaw() {
    // Scrollable pre with last 100 raw lines
  }

  function renderActionBar() {
    // Input + Send button + Raw terminal toggle
    // Send POSTs to /api/workspaces/<id>/send
    // Raw toggle flips rawMode and re-renders
  }

  // Expose collapse for external use
  window.collapseWorkspace = collapseWorkspace;
})();
```

- [ ] **Step 2: Build**

Run: `go build ./cmd/towr/`

- [ ] **Step 3: Commit**

```bash
git add cmd/towr/web/js/workspace.js
git commit -m "feat(web): workspace expanded view with structured step parsing"
```

---

## Chunk 5: Activity Feed + Integration Test

### Task 6: Update activity.js

**Files:**
- Modify: `cmd/towr/web/js/activity.js`

- [ ] **Step 1: Update activity event styling**

Keep existing logic but improve rendering:
- `task.approved` events: green "✓" prefix, show dialog text
- `task.completed` events: green dot
- `task.failed` events: red dot
- `task.dispatched` / `task.started`: blue dot
- Events containing "forced" or "hooks_skipped": red left-border + `[BYPASS]` tag
- Use CSS classes from the new design system

- [ ] **Step 2: Build**

Run: `go build ./cmd/towr/`

- [ ] **Step 3: Commit**

```bash
git add cmd/towr/web/js/activity.js
git commit -m "feat(web): improve activity feed with approval and bypass styling"
```

### Task 7: Integration test

- [ ] **Step 1: Full build and vet**

```bash
go build ./cmd/towr/ && go vet ./... && go test ./...
```

All must pass.

- [ ] **Step 2: Manual smoke test**

```bash
# Start the server
timeout 10 towr web --addr :18093 &
sleep 2
# Test endpoints
curl -s http://localhost:18093/ | grep -c "towr"
curl -s http://localhost:18093/static/css/style.css | grep -c "bg-page"
curl -s http://localhost:18093/static/js/app.js | grep -c "renderCounters"
curl -s http://localhost:18093/static/js/workspace.js | grep -c "expandWorkspace"
curl -s http://localhost:18093/api/workspaces | python3 -c "import sys,json; d=json.loads(sys.stdin.read()); print('agent' in d[0] if d else 'no workspaces')"
kill %1 2>/dev/null
```

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "feat(web): dashboard redesign — command center layout complete"
```

---

## Summary

| Task | Description | Files | Estimated |
|---|---|---|---|
| 1 | Add agent field to API | web.go | 2 min |
| 2 | Rewrite index.html + delete terminal.js | index.html, terminal.js | 3 min |
| 3 | Rewrite style.css | style.css | 5 min |
| 4 | Rewrite app.js | app.js | 5 min |
| 5 | Create workspace.js | workspace.js | 5 min |
| 6 | Update activity.js | activity.js | 3 min |
| 7 | Integration test | — | 3 min |

**Total: 7 tasks, ~26 minutes of agent work**

Dependencies: Task 1 before Tasks 3-6 (needs agent field). Task 2 before Tasks 3-6 (needs new HTML structure). Tasks 3-6 can run in sequence (all modify different files but depend on shared HTML element IDs).
