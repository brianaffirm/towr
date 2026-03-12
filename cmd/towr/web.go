package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
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

//go:embed web/*
var webFS embed.FS

func newWebCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start a local HTTP dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, appErr := initApp()

			mux := http.NewServeMux()

			// Serve static assets (css, js) from embedded filesystem.
			staticSub, _ := fs.Sub(webFS, "web")
			mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				data, err := webFS.ReadFile("web/index.html")
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Write(data)
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

			mux.HandleFunc("/api/workspaces/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "method not allowed", 405)
					return
				}
				// Expect paths like /api/workspaces/{id}/approve or /api/workspaces/{id}/send
				// Parse action from the rightmost segment so IDs containing "/" (e.g. "feature/foo") work.
				rest := strings.TrimPrefix(r.URL.Path, "/api/workspaces/")
				lastSlash := strings.LastIndex(rest, "/")
				if lastSlash <= 0 || lastSlash == len(rest)-1 {
					http.Error(w, "invalid path", 400)
					return
				}
				id, action := rest[:lastSlash], rest[lastSlash+1:]

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

				switch action {
				case "approve":
					if err := term.SendKeys(id, "Enter"); err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case "send":
					var body struct {
						Message string `json:"message"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
						http.Error(w, "missing message", 400)
						return
					}
					if err := term.PasteBuffer(id, body.Message); err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					http.Error(w, "unknown action", 404)
				}
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

