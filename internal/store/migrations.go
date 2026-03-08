package store

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 1

// schema_v1 creates the initial database schema.
const schema_v1 = `
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    kind TEXT NOT NULL,
    workspace_id TEXT,
    repo_root TEXT,
    runtime TEXT,
    actor TEXT,
    data JSON,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT NOT NULL,
    repo_root TEXT NOT NULL,
    base_branch TEXT,
    base_ref TEXT,
    branch TEXT,
    worktree_path TEXT,
    source_kind TEXT,
    source_value TEXT,
    status TEXT NOT NULL,
    agent_runtime TEXT,
    agent_id TEXT,
    agent_model_version TEXT,
    exit_code INTEGER,
    error TEXT,
    merge_commit TEXT,
    checkpoint JSON,
    terminal_target TEXT,
    created_at TEXT,
    updated_at TEXT,
    PRIMARY KEY (id, repo_root)
);

CREATE TABLE IF NOT EXISTS queue (
    id TEXT PRIMARY KEY,
    workspace_id TEXT,
    repo_root TEXT,
    type TEXT NOT NULL,
    priority TEXT NOT NULL,
    summary TEXT,
    context JSON,
    options JSON,
    resolution TEXT,
    resolved_by TEXT,
    resolved_at TEXT,
    timeout TEXT,
    timeout_action TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_events_workspace ON events(workspace_id);
CREATE INDEX IF NOT EXISTS idx_events_kind ON events(kind);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_workspaces_repo ON workspaces(repo_root);
CREATE INDEX IF NOT EXISTS idx_workspaces_status ON workspaces(status);
CREATE INDEX IF NOT EXISTS idx_queue_repo ON queue(repo_root);
`

// migrate runs all necessary migrations to bring the database up to date.
func migrate(db *sql.DB) error {
	ver, err := getSchemaVersion(db)
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	if ver == 0 {
		if _, err := db.Exec(schema_v1); err != nil {
			return fmt.Errorf("apply schema v1: %w", err)
		}
		if err := setSchemaVersion(db, 1); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		return nil
	}

	if ver > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", ver, currentSchemaVersion)
	}

	// Future migrations go here:
	// if ver < 2 { migrate_v1_to_v2(db); ver = 2 }

	return nil
}

func getSchemaVersion(db *sql.DB) (int, error) {
	var ver int
	err := db.QueryRow("PRAGMA user_version").Scan(&ver)
	return ver, err
}

func setSchemaVersion(db *sql.DB, ver int) error {
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", ver))
	return err
}
