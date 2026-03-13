package main

import (
	"encoding/csv"
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

				// Filter by ?type= query param.
				if typeFilter := r.URL.Query().Get("type"); typeFilter != "" {
					events = filterEventsByType(events, typeFilter)
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

			mux.HandleFunc("/api/cost", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if app == nil {
					json.NewEncoder(w).Encode(costSummary{Tasks: []costTaskItem{}})
					return
				}

				events, err := app.store.QueryEvents(store.EventQuery{Kind: store.EventTaskCost, RepoRoot: app.repoRoot})
				if err != nil {
					json.NewEncoder(w).Encode(costSummary{Tasks: []costTaskItem{}})
					return
				}

				var summary costSummary
				summary.Tasks = make([]costTaskItem, 0, len(events))

				for _, ev := range events {
					item := costTaskItem{
						ID: ev.WorkspaceID,
					}
					if v, ok := ev.Data["model"].(string); ok {
						item.Model = v
					}
					if v, ok := ev.Data["route_reason"].(string); ok {
						item.Reason = v
					}
					if v, ok := ev.Data["input_tokens"].(float64); ok {
						item.InputTokens = int(v)
					}
					if v, ok := ev.Data["output_tokens"].(float64); ok {
						item.OutputTokens = int(v)
					}
					if v, ok := ev.Data["estimated_cost"].(float64); ok {
						item.Cost = v
					}
					if v, ok := ev.Data["opus_baseline"].(float64); ok {
						item.OpusCost = v
					}
					if v, ok := ev.Data["token_source"].(string); ok {
						item.Source = v
					}
					summary.TotalSpent += item.Cost
					summary.TotalOpus += item.OpusCost
					summary.Tasks = append(summary.Tasks, item)
				}

				summary.TotalSaved = summary.TotalOpus - summary.TotalSpent
				if summary.TotalOpus > 0 {
					summary.SavingsPercent = summary.TotalSaved / summary.TotalOpus * 100
				}
				// Budget: always 0 in Phase 1 (not yet stored in events)

				json.NewEncoder(w).Encode(summary)
			})

			mux.HandleFunc("/api/workspace/", func(w http.ResponseWriter, r *http.Request) {
				// Expect /api/workspace/{id}/safety
				rest := strings.TrimPrefix(r.URL.Path, "/api/workspace/")
				if !strings.HasSuffix(rest, "/safety") {
					http.NotFound(w, r)
					return
				}
				id := strings.TrimSuffix(rest, "/safety")
				if id == "" {
					http.Error(w, "missing workspace id", 400)
					return
				}
				if app == nil {
					http.Error(w, "store not available", 500)
					return
				}

				events, err := app.store.QueryEvents(store.EventQuery{WorkspaceID: id, RepoRoot: app.repoRoot})
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}

				summary := buildSafetySummary(id, events)

				// Try to enrich with workspace metadata.
				if ws, err := app.store.GetWorkspace(app.repoRoot, id); err == nil && ws != nil {
					summary.Agent = ws.AgentRuntime
					summary.FilesChanged = countFilesChanged(ws)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(summary)
			})

			mux.HandleFunc("/api/audit/export", func(w http.ResponseWriter, r *http.Request) {
				if app == nil {
					http.Error(w, "store not available", 500)
					return
				}

				query := store.EventQuery{RepoRoot: app.repoRoot}

				if sinceParam := r.URL.Query().Get("since"); sinceParam != "" {
					t, err := parseSinceFlag(sinceParam)
					if err != nil {
						http.Error(w, fmt.Sprintf("invalid since param: %v", err), 400)
						return
					}
					query.Since = &t
				}

				events, err := app.store.QueryEvents(query)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}

				w.Header().Set("Content-Type", "text/csv")
				w.Header().Set("Content-Disposition", "attachment; filename=towr-audit.csv")
				writeCSVTo(w, events)
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
	Agent  string `json:"agent"`
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
			Agent:  ws.AgentRuntime,
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

// filterEventsByType filters events by a safety-related type category.
func filterEventsByType(events []Event, typ string) []Event {
	var out []Event
	for _, e := range events {
		lower := strings.ToLower(e.Kind)
		switch typ {
		case "approval":
			if strings.Contains(lower, "approved") || strings.Contains(lower, "blocked") || strings.Contains(lower, "bypass") {
				out = append(out, e)
			}
		case "bypass":
			if strings.Contains(lower, "forced") || strings.Contains(lower, "hooks_skipped") {
				out = append(out, e)
			}
		}
	}
	return out
}

type safetySummary struct {
	WorkspaceID  string `json:"workspace_id"`
	Agent        string `json:"agent"`
	Sandbox      string `json:"sandbox"`
	Approvals    int    `json:"approvals"`
	Blocks       int    `json:"blocks"`
	Bypasses     int    `json:"bypasses"`
	FilesChanged int    `json:"files_changed"`
	SafetyLevel  string `json:"safety_level"`
}

type costSummary struct {
	TotalSpent     float64        `json:"totalSpent"`
	TotalOpus      float64        `json:"totalOpus"`
	TotalSaved     float64        `json:"totalSaved"`
	SavingsPercent float64        `json:"savingsPercent"`
	Budget         float64        `json:"budget"`
	Tasks          []costTaskItem `json:"tasks"`
}

type costTaskItem struct {
	ID           string  `json:"id"`
	Model        string  `json:"model"`
	Reason       string  `json:"reason"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost"`
	OpusCost     float64 `json:"opusCost"`
	Source       string  `json:"source"`
}

// buildSafetySummary computes safety metrics from workspace events.
func buildSafetySummary(wsID string, events []Event) safetySummary {
	s := safetySummary{
		WorkspaceID: wsID,
		Sandbox:     "allowlist",
	}
	for _, e := range events {
		lower := strings.ToLower(e.Kind)
		switch {
		case strings.Contains(lower, "forced") || strings.Contains(lower, "hooks_skipped"):
			s.Bypasses++
		case strings.Contains(lower, "blocked"):
			s.Blocks++
		case strings.Contains(lower, "approved") || strings.Contains(lower, "resolved"):
			s.Approvals++
		}
	}

	switch {
	case s.Bypasses > 0:
		s.SafetyLevel = "red"
	case s.Approvals > 0:
		s.SafetyLevel = "yellow"
	default:
		s.SafetyLevel = "green"
	}
	return s
}

// countFilesChanged returns a rough file count from workspace diff stats.
func countFilesChanged(ws *store.Workspace) int {
	if ws.RepoRoot == "" || ws.BaseBranch == "" || ws.Branch == "" {
		return 0
	}
	added, removed := getDiffCounts(ws.RepoRoot, ws.BaseBranch, ws.Branch)
	return added + removed
}

// writeCSVTo writes audit events as CSV to an io.Writer.
func writeCSVTo(w http.ResponseWriter, events []Event) {
	cw := csv.NewWriter(w)
	cw.Write([]string{"timestamp", "workspace_id", "kind", "actor", "data"})
	for _, e := range events {
		cw.Write([]string{
			e.Timestamp.Format(time.RFC3339),
			e.WorkspaceID,
			formatKind(e.Kind),
			e.Actor,
			dataSummary(e.Data),
		})
	}
	cw.Flush()
}
