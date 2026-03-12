package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianaffirm/towr/internal/cli"
	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/terminal"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/spf13/cobra"
)

func newWebCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start a local HTTP dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, appErr := initApp()

			mux := http.NewServeMux()

			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if err := dashboardTmpl.Execute(w, nil); err != nil {
					http.Error(w, err.Error(), 500)
				}
			})

			mux.HandleFunc("/api/workspaces", func(w http.ResponseWriter, r *http.Request) {
				rows, err := collectWorkspaces(app, appErr)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(rows)
			})

			mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
			if app == nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]store.Event{})
				return
			}
			events, err := app.store.QueryEvents(store.EventQuery{})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// Take the last 50 (newest) and reverse to newest-first.
			if len(events) > 50 {
				events = events[len(events)-50:]
			}
			for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
				events[i], events[j] = events[j], events[i]
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(events)
		})

		mux.HandleFunc("/stream/", func(w http.ResponseWriter, r *http.Request) {
				id := strings.TrimPrefix(r.URL.Path, "/stream/")
				if id == "" {
					http.Error(w, "missing workspace id", 400)
					return
				}

				var term terminal.Backend
				if app != nil {
					term = app.term
				} else {
					if _, err := lookupTmux(); err != nil {
						http.Error(w, "tmux not available", 500)
						return
					}
					term = terminal.NewTmuxBackend("towr")
				}

				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming not supported", 500)
					return
				}

				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")

				ctx := r.Context()
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()

				// Send initial capture immediately.
				sendCapture(w, flusher, term, id)

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						sendCapture(w, flusher, term, id)
					}
				}
			})

			fmt.Printf("towr web dashboard: http://%s\n", addr)
			return http.ListenAndServe(addr, mux)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8090", "listen address (use 0.0.0.0:8090 to expose on all interfaces)")
	return cmd
}

func sendCapture(w http.ResponseWriter, flusher http.Flusher, term terminal.Backend, id string) {
	output, err := term.CapturePane(id, 50)
	if err != nil {
		fmt.Fprintf(w, "data: [error: %s]\n\n", err.Error())
		flusher.Flush()
		return
	}
	for _, line := range strings.Split(output, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	flusher.Flush()
}

type webWorkspace struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Task   string `json:"task"`
	Diff   string `json:"diff"`
	Age    string `json:"age"`
}

func collectWorkspaces(app *appContext, appErr error) ([]webWorkspace, error) {
	var workspaces []*store.Workspace

	if appErr != nil {
		// Outside repo: show all.
		reposDir := filepath.Join(config.TowrHome(), "repos")
		var err error
		workspaces, err = store.ListAllWorkspaces(reposDir)
		if err != nil {
			return nil, err
		}
		staleThreshold := 7 * 24 * time.Hour
		for _, ws := range workspaces {
			result := workspace.ReconcileWorkspace(ws, staleThreshold)
			if result != nil {
				ws.Status = string(result.To)
			}
		}
	} else {
		var err error
		workspaces, err = app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
		if err != nil {
			return nil, err
		}
		staleThreshold := 7 * 24 * time.Hour
		for _, ws := range workspaces {
			result := workspace.ReconcileWorkspace(ws, staleThreshold)
			if result != nil {
				ws.Status = string(result.To)
			}
		}
	}

	taskStatusMap := make(map[string]string)
	if app != nil {
		for _, ws := range workspaces {
			taskStatusMap[ws.ID] = resolveTaskStatus(app.store, ws.RepoRoot, ws.ID)
		}
	}

	var rows []webWorkspace
	for _, ws := range workspaces {
		diffStr := "-"
		if ws.RepoRoot != "" && ws.BaseBranch != "" && ws.Branch != "" {
			added, removed := getDiffCounts(ws.RepoRoot, ws.BaseBranch, ws.Branch)
			diffStr = formatDiffPlain(added, removed)
		}

		task := "-"
		if ts := taskStatusMap[ws.ID]; ts != "" {
			task = ts
		}

		rows = append(rows, webWorkspace{
			ID:     ws.ID,
			Status: ws.Status,
			Task:   task,
			Diff:   diffStr,
			Age:    cli.FormatAgeFromString(ws.CreatedAt),
		})
	}
	return rows, nil
}

// formatDiffPlain returns a plain-text diff summary like "+10 / -2".
func formatDiffPlain(added, removed int) string {
	if added == 0 && removed == 0 {
		return "-"
	}
	return fmt.Sprintf("+%d / -%d", added, removed)
}

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>towr — dashboard</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    background: #0d1117; color: #c9d1d9; min-height: 100vh;
  }
  header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.5rem; border-bottom: 1px solid #21262d;
  }
  header h1 { font-size: 1.1rem; color: #58a6ff; font-weight: 600; }
  .header-meta { font-size: 0.7rem; color: #484f58; }
  .header-meta .dot { display: inline-block; width: 6px; height: 6px;
    border-radius: 50%; background: #3fb950; margin-right: 4px; vertical-align: middle; }

  .layout { display: flex; height: calc(100vh - 53px); }
  .sidebar { flex: 1; overflow-y: auto; padding: 1rem; min-width: 0; }
  .terminal-panel {
    width: 0; overflow: hidden; border-left: 1px solid #21262d;
    background: #010409; transition: width 0.2s ease; display: flex; flex-direction: column;
  }
  .terminal-panel.open { width: 45%; min-width: 320px; }
  .terminal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.6rem 1rem; border-bottom: 1px solid #21262d; background: #161b22;
  }
  .terminal-header span { font-size: 0.8rem; color: #58a6ff; }
  .terminal-close {
    background: none; border: 1px solid #30363d; color: #8b949e;
    border-radius: 4px; padding: 2px 8px; cursor: pointer; font-family: inherit; font-size: 0.75rem;
  }
  .terminal-close:hover { color: #c9d1d9; border-color: #484f58; }
  .terminal-body {
    flex: 1; overflow-y: auto; padding: 0.75rem; font-size: 0.75rem;
    line-height: 1.4; white-space: pre-wrap; color: #8b949e;
  }

  .zone { margin-bottom: 1.5rem; }
  .zone-title {
    font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.08em;
    color: #484f58; margin-bottom: 0.5rem; padding-left: 2px;
  }
  .zone-title .count { color: #6e7681; }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 0.6rem; }

  .card {
    background: #161b22; border: 1px solid #21262d; border-radius: 6px;
    padding: 0.75rem 1rem; cursor: pointer; transition: border-color 0.15s, background 0.15s;
  }
  .card:hover { border-color: #30363d; background: #1c2128; }
  .card.active { border-color: #58a6ff; }
  .card-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.4rem; }
  .card-id { font-size: 0.85rem; font-weight: 600; color: #c9d1d9; white-space: nowrap;
    overflow: hidden; text-overflow: ellipsis; max-width: 70%; }
  .badge {
    font-size: 0.65rem; font-weight: 600; padding: 2px 8px; border-radius: 10px;
    text-transform: uppercase; letter-spacing: 0.04em; white-space: nowrap;
  }
  .card-details { display: flex; gap: 1rem; font-size: 0.7rem; color: #8b949e; flex-wrap: wrap; }
  .card-details .label { color: #484f58; }

  .empty-state { text-align: center; color: #484f58; padding: 4rem 1rem; font-size: 0.85rem; }

  .activity-log { border-top: 1px solid #21262d; background: #161b22; }
  .activity-toggle {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.6rem 1rem; cursor: pointer; font-size: 0.75rem; color: #8b949e;
    user-select: none;
  }
  .activity-toggle:hover { color: #c9d1d9; }
  .activity-toggle .arrow { transition: transform 0.15s; display: inline-block; margin-right: 6px; }
  .activity-toggle.open .arrow { transform: rotate(90deg); }
  .activity-feed {
    max-height: 0; overflow: hidden; transition: max-height 0.25s ease;
  }
  .activity-feed.open { max-height: 300px; overflow-y: auto; }
  .evt-row {
    display: flex; gap: 0.75rem; padding: 0.3rem 1rem; font-size: 0.7rem;
    border-top: 1px solid #21262d; color: #8b949e;
  }
  .evt-ts { color: #484f58; white-space: nowrap; }
  .evt-ws { font-weight: 600; color: #58a6ff; }
  .evt-kind { font-weight: 600; }

  @media (max-width: 768px) {
    .layout { flex-direction: column; }
    .terminal-panel.open { width: 100%; min-width: 0; height: 50%; }
    .sidebar { flex: none; height: 50%; }
    .cards { grid-template-columns: 1fr; }
    header { padding: 0.75rem 1rem; }
  }
</style>
</head>
<body>
<header>
  <h1>towr</h1>
  <span class="header-meta"><span class="dot"></span>live &middot; refreshing every 5s</span>
</header>
<div class="layout">
  <div class="sidebar" id="sidebar"></div>
  <div class="terminal-panel" id="termPanel">
    <div class="terminal-header">
      <span id="termTitle">terminal</span>
      <button class="terminal-close" id="termClose">close</button>
    </div>
    <div class="terminal-body" id="termBody"></div>
  </div>
</div>
<div class="activity-log">
  <div class="activity-toggle" id="actToggle"><span><span class="arrow">&#9654;</span>Activity Log <span id="actCount"></span></span></div>
  <div class="activity-feed" id="actFeed"></div>
</div>
<script>
(function() {
  "use strict";
  var STATUS_COLORS = {
    RUNNING: "#58a6ff", SPAWNED: "#58a6ff",
    READY: "#3fb950", MERGED: "#3fb950", LANDED: "#3fb950",
    BLOCKED: "#f85149", FAILED: "#f85149", ERROR: "#f85149",
    STALE: "#8b949e", ORPHANED: "#8b949e", IDLE: "#8b949e"
  };
  var DEFAULT_COLOR = "#8b949e";

  function statusColor(s) { return STATUS_COLORS[(s||"").toUpperCase()] || DEFAULT_COLOR; }

  function zone(status) {
    var s = (status||"").toUpperCase();
    if (s === "RUNNING" || s === "SPAWNED") return "working";
    if (s === "BLOCKED" || s === "FAILED" || s === "ERROR" || s === "STALE" || s === "ORPHANED") return "attention";
    if (s === "READY" || s === "MERGED" || s === "LANDED") return "completed";
    return "working";
  }

  function badgeBg(color) { return color + "22"; }

  var activeId = null;
  var evtSource = null;

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  function render(data) {
    var groups = { working: [], attention: [], completed: [] };
    (data || []).forEach(function(ws) { groups[zone(ws.status)].push(ws); });

    var zoneConfig = [
      { key: "working", title: "Working", color: "#58a6ff" },
      { key: "attention", title: "Needs Attention", color: "#f85149" },
      { key: "completed", title: "Completed", color: "#3fb950" }
    ];

    var html = "";
    var hasAny = false;
    zoneConfig.forEach(function(z) {
      var items = groups[z.key];
      if (items.length === 0) return;
      hasAny = true;
      html += '<div class="zone">';
      html += '<div class="zone-title" style="color:' + esc(z.color) + '">' + esc(z.title) +
              ' <span class="count">(' + items.length + ')</span></div>';
      html += '<div class="cards">';
      items.forEach(function(ws) {
        var c = statusColor(ws.status);
        var isActive = ws.id === activeId;
        html += '<div class="card' + (isActive ? ' active' : '') + '" data-id="' + esc(ws.id) + '">';
        html += '<div class="card-top">';
        html += '<span class="card-id">' + esc(ws.id) + '</span>';
        html += '<span class="badge" style="color:' + esc(c) + ';background:' + badgeBg(c) + '">' + esc(ws.status) + '</span>';
        html += '</div>';
        html += '<div class="card-details">';
        html += '<span><span class="label">task</span> ' + esc(ws.task) + '</span>';
        html += '<span><span class="label">diff</span> ' + esc(ws.diff) + '</span>';
        html += '<span><span class="label">age</span> ' + esc(ws.age) + '</span>';
        html += '</div></div>';
      });
      html += '</div></div>';
    });
    if (!hasAny) {
      html = '<div class="empty-state">No workspaces found.</div>';
    }
    document.getElementById("sidebar").innerHTML = html;

    document.querySelectorAll(".card").forEach(function(el) {
      el.addEventListener("click", function() { openTerminal(el.getAttribute("data-id")); });
    });
  }

  function openTerminal(id) {
    activeId = id;
    var panel = document.getElementById("termPanel");
    var body = document.getElementById("termBody");
    var title = document.getElementById("termTitle");
    panel.classList.add("open");
    title.textContent = id;
    body.textContent = "connecting...";

    if (evtSource) { evtSource.close(); evtSource = null; }
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    evtSource.onmessage = function(e) {
      body.textContent = "";
      var lines = e.data.split("\n");
      body.textContent = lines.join("\n");
      body.scrollTop = body.scrollHeight;
    };
    evtSource.onerror = function() {
      body.textContent += "\n[stream disconnected]";
    };

    document.querySelectorAll(".card").forEach(function(el) {
      el.classList.toggle("active", el.getAttribute("data-id") === id);
    });
  }

  document.getElementById("termClose").addEventListener("click", function() {
    document.getElementById("termPanel").classList.remove("open");
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    document.querySelectorAll(".card.active").forEach(function(el) { el.classList.remove("active"); });
  });

  var EVENT_COLORS = {
    "task.completed": "#3fb950", "task.failed": "#f85149",
    "task.dispatched": "#58a6ff", "task.blocked": "#d29922"
  };

  document.getElementById("actToggle").addEventListener("click", function() {
    this.classList.toggle("open");
    document.getElementById("actFeed").classList.toggle("open");
  });

  function renderEvents(events) {
    var feed = document.getElementById("actFeed");
    var countEl = document.getElementById("actCount");
    countEl.textContent = "(" + (events||[]).length + " events)";
    var html = "";
    (events||[]).forEach(function(ev) {
      var ts = new Date(ev.ts).toLocaleTimeString();
      var c = EVENT_COLORS[ev.kind] || "#8b949e";
      var summary = "";
      if (ev.data && ev.data.summary) summary = ev.data.summary;
      else if (ev.data && ev.data.message) summary = ev.data.message;
      html += '<div class="evt-row">';
      html += '<span class="evt-ts">' + esc(ts) + '</span>';
      html += '<span class="evt-ws">' + esc(ev.workspace_id||"-") + '</span>';
      html += '<span class="evt-kind" style="color:' + c + '">' + esc(ev.kind) + '</span>';
      html += '<span>' + esc(summary) + '</span>';
      html += '</div>';
    });
    feed.innerHTML = html;
  }

  function poll() {
    fetch("/api/workspaces").then(function(r) { return r.json(); }).then(render).catch(function() {});
    fetch("/api/events").then(function(r) { return r.json(); }).then(renderEvents).catch(function() {});
    setTimeout(poll, 5000);
  }
  poll();
})();
</script>
</body>
</html>
`))
