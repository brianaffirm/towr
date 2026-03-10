package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database and an
// append-only JSONL audit log.
type SQLiteStore struct {
	db    *sql.DB
	audit *AuditWriter
}

// NewSQLiteStore returns an uninitialised store. Call Init to open the database.
func NewSQLiteStore() *SQLiteStore {
	return &SQLiteStore{}
}

// Init opens (or creates) the SQLite database at dbPath, runs migrations,
// enables WAL mode, and opens the companion audit log.
func (s *SQLiteStore) Init(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	s.db = db

	if err := migrate(s.db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Open audit log next to the database.
	auditPath := filepath.Join(filepath.Dir(dbPath), "audit.jsonl")
	aw, err := NewAuditWriter(auditPath)
	if err != nil {
		return fmt.Errorf("audit writer: %w", err)
	}
	s.audit = aw

	return nil
}

// Close shuts down the database and audit log.
func (s *SQLiteStore) Close() error {
	var errs []string
	if s.audit != nil {
		if err := s.audit.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ---------- Events ----------

// EmitEvent writes an event to both the SQLite events table and the audit log.
func (s *SQLiteStore) EmitEvent(event Event) error {
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	ts := event.Timestamp.UTC().Format(time.RFC3339Nano)

	_, err = s.db.Exec(
		`INSERT INTO events (id, timestamp, kind, workspace_id, repo_root, runtime, actor, data)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, ts, event.Kind, event.WorkspaceID, event.RepoRoot, event.Runtime, event.Actor, string(dataJSON),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	if err := s.audit.WriteEvent(event); err != nil {
		return fmt.Errorf("audit event: %w", err)
	}

	// Auto-update last_activity on the workspace.
	if event.WorkspaceID != "" && event.RepoRoot != "" {
		_, _ = s.db.Exec(
			`UPDATE workspaces SET last_activity = ? WHERE id = ? AND repo_root = ?`,
			ts, event.WorkspaceID, event.RepoRoot,
		)
	}

	return nil
}

// QueryEvents returns events matching the given query filters.
func (s *SQLiteStore) QueryEvents(query EventQuery) ([]Event, error) {
	var clauses []string
	var args []interface{}

	if query.WorkspaceID != "" {
		clauses = append(clauses, "workspace_id = ?")
		args = append(args, query.WorkspaceID)
	}
	if query.RepoRoot != "" {
		clauses = append(clauses, "repo_root = ?")
		args = append(args, query.RepoRoot)
	}
	if query.Kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, query.Kind)
	}
	if query.Since != nil {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, query.Since.UTC().Format(time.RFC3339Nano))
	}
	if query.Until != nil {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, query.Until.UTC().Format(time.RFC3339Nano))
	}

	q := "SELECT id, timestamp, kind, workspace_id, repo_root, runtime, actor, data FROM events"
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY timestamp ASC"
	if query.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", query.Limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var ts string
		var dataStr sql.NullString
		var wsID, repoRoot, runtime, actor sql.NullString

		if err := rows.Scan(&e.ID, &ts, &e.Kind, &wsID, &repoRoot, &runtime, &actor, &dataStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		e.WorkspaceID = wsID.String
		e.RepoRoot = repoRoot.String
		e.Runtime = runtime.String
		e.Actor = actor.String

		if dataStr.Valid && dataStr.String != "" {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr.String), &data); err == nil {
				e.Data = data
			}
		}

		events = append(events, e)
	}
	return events, rows.Err()
}

// ---------- Workspaces ----------

// SaveWorkspace performs an upsert of workspace state.
func (s *SQLiteStore) SaveWorkspace(w *Workspace) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if w.CreatedAt == "" {
		w.CreatedAt = now
	}
	w.UpdatedAt = now

	var exitCode sql.NullInt64
	if w.ExitCode != nil {
		exitCode = sql.NullInt64{Int64: int64(*w.ExitCode), Valid: true}
	}

	checkpoint := sql.NullString{}
	if len(w.Checkpoint) > 0 {
		checkpoint = sql.NullString{String: string(w.Checkpoint), Valid: true}
	}

	if w.LastActivity == "" {
		w.LastActivity = w.UpdatedAt
	}

	envVars := sql.NullString{}
	if len(w.EnvVars) > 0 {
		envVars = sql.NullString{String: string(w.EnvVars), Valid: true}
	}

	_, err := s.db.Exec(
		`INSERT INTO workspaces (id, repo_root, base_branch, base_ref, branch, worktree_path,
		    source_kind, source_value, status, agent_runtime, agent_id, agent_model_version,
		    exit_code, error, merge_commit, checkpoint, terminal_target, created_at, updated_at, last_activity, env_vars)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id, repo_root) DO UPDATE SET
		    base_branch=excluded.base_branch, base_ref=excluded.base_ref,
		    branch=excluded.branch, worktree_path=excluded.worktree_path,
		    source_kind=excluded.source_kind, source_value=excluded.source_value,
		    status=excluded.status, agent_runtime=excluded.agent_runtime,
		    agent_id=excluded.agent_id, agent_model_version=excluded.agent_model_version,
		    exit_code=excluded.exit_code, error=excluded.error,
		    merge_commit=excluded.merge_commit, checkpoint=excluded.checkpoint,
		    terminal_target=excluded.terminal_target, updated_at=excluded.updated_at, last_activity=excluded.last_activity,
		    env_vars=excluded.env_vars`,
		w.ID, w.RepoRoot, w.BaseBranch, w.BaseRef, w.Branch, w.WorktreePath,
		w.SourceKind, w.SourceValue, w.Status, w.AgentRuntime, w.AgentID, w.AgentModelVersion,
		exitCode, w.Error, w.MergeCommit, checkpoint, w.TerminalTarget, w.CreatedAt, w.UpdatedAt, w.LastActivity, envVars,
	)
	if err != nil {
		return fmt.Errorf("save workspace: %w", err)
	}
	return nil
}

// GetWorkspace retrieves a single workspace by repo root and ID.
func (s *SQLiteStore) GetWorkspace(repoRoot, id string) (*Workspace, error) {
	w := &Workspace{}
	var exitCode sql.NullInt64
	var checkpoint, envVars sql.NullString

	err := s.db.QueryRow(
		`SELECT id, repo_root, base_branch, base_ref, branch, worktree_path,
		    source_kind, source_value, status, agent_runtime, agent_id, agent_model_version,
		    exit_code, error, merge_commit, checkpoint, terminal_target, created_at, updated_at,
		    COALESCE(last_activity, updated_at), env_vars
		 FROM workspaces WHERE id = ? AND repo_root = ?`, id, repoRoot,
	).Scan(
		&w.ID, &w.RepoRoot, &w.BaseBranch, &w.BaseRef, &w.Branch, &w.WorktreePath,
		&w.SourceKind, &w.SourceValue, &w.Status, &w.AgentRuntime, &w.AgentID, &w.AgentModelVersion,
		&exitCode, &w.Error, &w.MergeCommit, &checkpoint, &w.TerminalTarget, &w.CreatedAt, &w.UpdatedAt,
		&w.LastActivity, &envVars,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}

	if exitCode.Valid {
		v := int(exitCode.Int64)
		w.ExitCode = &v
	}
	if checkpoint.Valid {
		w.Checkpoint = json.RawMessage(checkpoint.String)
	}
	if envVars.Valid {
		w.EnvVars = json.RawMessage(envVars.String)
	}

	return w, nil
}

// ListWorkspaces returns workspaces for a repo, optionally filtered.
func (s *SQLiteStore) ListWorkspaces(repoRoot string, filter ListFilter) ([]*Workspace, error) {
	var clauses []string
	var args []interface{}

	if !filter.AllRepos {
		clauses = append(clauses, "repo_root = ?")
		args = append(args, repoRoot)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}

	q := `SELECT id, repo_root, base_branch, base_ref, branch, worktree_path,
	    source_kind, source_value, status, agent_runtime, agent_id, agent_model_version,
	    exit_code, error, merge_commit, checkpoint, terminal_target, created_at, updated_at,
	    COALESCE(last_activity, updated_at), env_vars
	 FROM workspaces`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at ASC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var result []*Workspace
	for rows.Next() {
		w := &Workspace{}
		var exitCode sql.NullInt64
		var checkpoint, envVars sql.NullString

		if err := rows.Scan(
			&w.ID, &w.RepoRoot, &w.BaseBranch, &w.BaseRef, &w.Branch, &w.WorktreePath,
			&w.SourceKind, &w.SourceValue, &w.Status, &w.AgentRuntime, &w.AgentID, &w.AgentModelVersion,
			&exitCode, &w.Error, &w.MergeCommit, &checkpoint, &w.TerminalTarget, &w.CreatedAt, &w.UpdatedAt,
			&w.LastActivity, &envVars,
		); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}

		if exitCode.Valid {
			v := int(exitCode.Int64)
			w.ExitCode = &v
		}
		if checkpoint.Valid {
			w.Checkpoint = json.RawMessage(checkpoint.String)
		}
		if envVars.Valid {
			w.EnvVars = json.RawMessage(envVars.String)
		}

		result = append(result, w)
	}
	return result, rows.Err()
}

// DeleteWorkspace removes a workspace from the materialized state.
func (s *SQLiteStore) DeleteWorkspace(repoRoot, id string) error {
	res, err := s.db.Exec("DELETE FROM workspaces WHERE id = ? AND repo_root = ?", id, repoRoot)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace %s not found in repo %s", id, repoRoot)
	}
	return nil
}

// parseWorkspaceRef splits a workspace reference into an optional repo basename
// prefix and the workspace ID. The format is "repobasename:id". If no colon is
// present (or the ref starts with ":"), the entire string is treated as a bare ID.
// Only the first colon is used as delimiter — IDs may contain colons.
func parseWorkspaceRef(ref string) (repoBasename, id string) {
	idx := strings.Index(ref, ":")
	if idx <= 0 {
		// No colon, or colon at position 0 — treat whole thing as bare ID.
		return "", ref
	}
	return ref[:idx], ref[idx+1:]
}

// FindWorkspaceByID searches all repo databases for a workspace matching the
// given ref. The ref can be a bare workspace ID or "repobasename:id" to
// disambiguate when the same ID exists in multiple repos.
// Returns the workspace and nil if found exactly once.
// Returns an error if the ID is ambiguous (found in multiple repos).
func FindWorkspaceByID(reposDir, ref string) (*Workspace, error) {
	repoPrefix, id := parseWorkspaceRef(ref)

	entries, err := filepath.Glob(filepath.Join(reposDir, "*", "state.db"))
	if err != nil {
		return nil, fmt.Errorf("scan repo databases: %w", err)
	}

	// Also include the global store for non-repo workspaces.
	globalDB := filepath.Join(filepath.Dir(reposDir), "global-state.db")
	entries = append(entries, globalDB)

	type match struct {
		ws       *Workspace
		repoName string // basename of the repo directory
	}

	var matches []match
	for _, dbPath := range entries {
		repoName := filepath.Base(filepath.Dir(dbPath))

		// If a repo prefix was given, skip repos that don't match.
		if repoPrefix != "" && repoName != repoPrefix {
			continue
		}

		s := NewSQLiteStore()
		if err := s.Init(dbPath); err != nil {
			continue
		}
		// List all workspaces in this DB and find by ID.
		wss, err := s.ListWorkspaces("", ListFilter{AllRepos: true})
		_ = s.Close()
		if err != nil {
			continue
		}
		for _, ws := range wss {
			if ws.ID == id {
				matches = append(matches, match{ws: ws, repoName: repoName})
			}
		}
	}

	switch len(matches) {
	case 0:
		if repoPrefix != "" {
			return nil, fmt.Errorf("workspace %q not found in repo %q", id, repoPrefix)
		}
		return nil, fmt.Errorf("workspace %q not found in any repo", id)
	case 1:
		return matches[0].ws, nil
	default:
		var lines []string
		for _, m := range matches {
			lines = append(lines, fmt.Sprintf("  %s:%s\t%s", m.repoName, id, m.ws.RepoRoot))
		}
		return nil, fmt.Errorf("workspace %q exists in multiple repos:\n%s\nuse \"towr <cmd> <repo>:%s\" to disambiguate",
			id, strings.Join(lines, "\n"), id)
	}
}

// ListAllWorkspaces scans all repo databases under reposDir (matching
// reposDir/*/state.db) and the global-state.db for non-repo workspaces,
// returning every workspace found.
func ListAllWorkspaces(reposDir string) ([]*Workspace, error) {
	entries, err := filepath.Glob(filepath.Join(reposDir, "*", "state.db"))
	if err != nil {
		return nil, fmt.Errorf("scan repo databases: %w", err)
	}

	// Also include the global store for non-repo workspaces.
	globalDB := filepath.Join(filepath.Dir(reposDir), "global-state.db")
	entries = append(entries, globalDB)

	var all []*Workspace
	for _, dbPath := range entries {
		s := NewSQLiteStore()
		if err := s.Init(dbPath); err != nil {
			continue
		}
		wss, err := s.ListWorkspaces("", ListFilter{AllRepos: true})
		_ = s.Close()
		if err != nil {
			continue
		}
		all = append(all, wss...)
	}
	return all, nil
}

// LastHookResult returns the result of the most recent hook for a workspace.
// Returns "pass", "fail", or "" (no hooks run).
func (s *SQLiteStore) LastHookResult(repoRoot, workspaceID string) string {
	var kind string
	err := s.db.QueryRow(`
		SELECT kind FROM events
		WHERE workspace_id = ? AND repo_root = ?
		AND kind IN (?, ?)
		ORDER BY timestamp DESC LIMIT 1`,
		workspaceID, repoRoot,
		EventWorkspaceHookCompleted, EventWorkspaceHookFailed,
	).Scan(&kind)
	if err != nil {
		return ""
	}
	if kind == EventWorkspaceHookCompleted {
		return "pass"
	}
	return "fail"
}

// ---------- Queue ----------

// EnqueueApproval adds an item to the approval queue.
func (s *SQLiteStore) EnqueueApproval(item QueueItem) error {
	ctx := sql.NullString{}
	if len(item.Context) > 0 {
		ctx = sql.NullString{String: string(item.Context), Valid: true}
	}
	opts := sql.NullString{}
	if len(item.Options) > 0 {
		opts = sql.NullString{String: string(item.Options), Valid: true}
	}

	_, err := s.db.Exec(
		`INSERT INTO queue (id, workspace_id, repo_root, type, priority, summary, context, options, timeout, timeout_action)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.WorkspaceID, item.RepoRoot, item.Type, item.Priority,
		item.Summary, ctx, opts, item.Timeout, item.TimeoutAction,
	)
	if err != nil {
		return fmt.Errorf("enqueue approval: %w", err)
	}
	return nil
}

// GetQueue returns unresolved queue items for a repo.
func (s *SQLiteStore) GetQueue(repoRoot string) ([]QueueItem, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, repo_root, type, priority, summary, context, options,
		    resolution, resolved_by, resolved_at, timeout, timeout_action, created_at
		 FROM queue WHERE repo_root = ? AND (resolution IS NULL OR resolution = '')
		 ORDER BY created_at ASC`, repoRoot,
	)
	if err != nil {
		return nil, fmt.Errorf("get queue: %w", err)
	}
	defer rows.Close()

	var items []QueueItem
	for rows.Next() {
		var item QueueItem
		var ctx, opts sql.NullString
		var resolution, resolvedBy, resolvedAt sql.NullString

		if err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.RepoRoot, &item.Type, &item.Priority,
			&item.Summary, &ctx, &opts, &resolution, &resolvedBy, &resolvedAt,
			&item.Timeout, &item.TimeoutAction, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan queue item: %w", err)
		}

		if ctx.Valid {
			item.Context = json.RawMessage(ctx.String)
		}
		if opts.Valid {
			item.Options = json.RawMessage(opts.String)
		}
		item.Resolution = resolution.String
		item.ResolvedBy = resolvedBy.String
		item.ResolvedAt = resolvedAt.String

		items = append(items, item)
	}
	return items, rows.Err()
}

// ResolveQueueItem marks a queue item as resolved.
func (s *SQLiteStore) ResolveQueueItem(id string, resolution Resolution) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE queue SET resolution = ?, resolved_by = ?, resolved_at = ? WHERE id = ?`,
		resolution.Action, resolution.ResolvedBy, now, id,
	)
	if err != nil {
		return fmt.Errorf("resolve queue item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("queue item %s not found", id)
	}
	return nil
}
