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
				rows, err := collectWorkspaces(app, appErr)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if err := dashboardTmpl.Execute(w, rows); err != nil {
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

			fmt.Printf("towr web dashboard: http://localhost%s\n", addr)
			return http.ListenAndServe(addr, mux)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8090", "listen address")
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

func statusColor(status string) string {
	s := strings.ToUpper(status)
	switch {
	case s == "READY" || s == "MERGED" || s == "LANDED":
		return "#22c55e"
	case s == "RUNNING" || s == "SPAWNED":
		return "#eab308"
	case s == "BLOCKED" || s == "FAILED" || s == "ERROR":
		return "#ef4444"
	case s == "STALE" || s == "ORPHANED":
		return "#9ca3af"
	default:
		return "#d1d5db"
	}
}

var dashboardTmpl = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"statusColor": statusColor,
}).Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>towr — workspace dashboard</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: ui-monospace, "SF Mono", Menlo, monospace; background: #0f172a; color: #e2e8f0; padding: 2rem; }
  h1 { font-size: 1.25rem; margin-bottom: 1rem; color: #94a3b8; }
  table { border-collapse: collapse; width: 100%; }
  th { text-align: left; padding: 0.5rem 1rem; color: #64748b; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; border-bottom: 1px solid #1e293b; }
  td { padding: 0.5rem 1rem; border-bottom: 1px solid #1e293b; font-size: 0.875rem; }
  tr:hover { background: #1e293b; }
  .status { font-weight: 600; }
  .empty { color: #475569; text-align: center; padding: 2rem; }
  .refresh-note { color: #475569; font-size: 0.75rem; margin-top: 1rem; }
  a { color: #38bdf8; text-decoration: none; }
  a:hover { text-decoration: underline; }
</style>
</head>
<body>
<h1>towr workspaces</h1>
{{if .}}
<table>
<thead>
  <tr><th>ID</th><th>Status</th><th>Task</th><th>Diff</th><th>Age</th><th>Stream</th></tr>
</thead>
<tbody>
{{range .}}
  <tr>
    <td>{{.ID}}</td>
    <td class="status" style="color: {{statusColor .Status}}">{{.Status}}</td>
    <td>{{.Task}}</td>
    <td>{{.Diff}}</td>
    <td>{{.Age}}</td>
    <td><a href="/stream/{{.ID}}" target="_blank">stream</a></td>
  </tr>
{{end}}
</tbody>
</table>
{{else}}
<p class="empty">No workspaces found.</p>
{{end}}
<p class="refresh-note">Auto-refreshes every 5s</p>
<script>
setTimeout(function refresh() {
  fetch("/api/workspaces").then(r => r.json()).then(data => {
    const tbody = document.querySelector("tbody");
    if (!tbody) { location.reload(); return; }
    tbody.innerHTML = "";
    data.forEach(ws => {
      const tr = document.createElement("tr");
      const colors = {"READY":"#22c55e","MERGED":"#22c55e","LANDED":"#22c55e","RUNNING":"#eab308","SPAWNED":"#eab308","BLOCKED":"#ef4444","FAILED":"#ef4444","ERROR":"#ef4444","STALE":"#9ca3af","ORPHANED":"#9ca3af"};
      const c = colors[ws.status.toUpperCase()] || "#d1d5db";
      tr.innerHTML = '<td>'+ws.id+'</td><td class="status" style="color:'+c+'">'+ws.status+'</td><td>'+ws.task+'</td><td>'+ws.diff+'</td><td>'+ws.age+'</td><td><a href="/stream/'+ws.id+'" target="_blank">stream</a></td>';
      tbody.appendChild(tr);
    });
    setTimeout(refresh, 5000);
  }).catch(() => setTimeout(refresh, 5000));
}, 5000);
</script>
</body>
</html>
`))
