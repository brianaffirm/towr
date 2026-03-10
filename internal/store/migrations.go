package store

import (
	"database/sql"
	"fmt"
	"strings"
)

const currentSchemaVersion = 3

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
    last_activity TEXT,
    env_vars JSON,
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
		// Fresh database: schema_v1 already includes all columns through v2.
		if err := setSchemaVersion(db, currentSchemaVersion); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		return nil
	}

	if ver > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", ver, currentSchemaVersion)
	}

	if ver < 2 {
		// Add last_activity column. Ignore "duplicate column" for fresh v2 DBs
		// where schema_v1 already includes the column.
		if _, err := db.Exec(`ALTER TABLE workspaces ADD COLUMN last_activity TEXT`); err != nil {
			if !isDuplicateColumnErr(err) {
				return fmt.Errorf("apply schema v2 (add last_activity): %w", err)
			}
		}
		// Backfill last_activity from updated_at.
		if _, err := db.Exec(`UPDATE workspaces SET last_activity = updated_at WHERE last_activity IS NULL`); err != nil {
			return fmt.Errorf("apply schema v2 (backfill last_activity): %w", err)
		}
		if err := setSchemaVersion(db, 2); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}

	if ver < 3 {
		if _, err := db.Exec(`ALTER TABLE workspaces ADD COLUMN env_vars JSON`); err != nil {
			if !isDuplicateColumnErr(err) {
				return fmt.Errorf("apply schema v3 (add env_vars): %w", err)
			}
		}
		if err := setSchemaVersion(db, 3); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}

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

// isDuplicateColumnErr returns true if the error is a SQLite "duplicate column" error.
func isDuplicateColumnErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}
