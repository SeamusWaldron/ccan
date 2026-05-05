package db

import (
	"database/sql"
	"strings"
)

const currentVersion = 2

// schemaMigrations create tables from scratch — safe to run repeatedly (IF NOT EXISTS).
// New columns are included here so fresh installs get them without ALTER TABLE.
var schemaMigrations = []string{
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		encoded_path TEXT NOT NULL UNIQUE,
		decoded_path_guess TEXT,
		first_seen_at TEXT,
		last_seen_at TEXT,
		session_count INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL,
		session_id TEXT,
		source_file TEXT NOT NULL UNIQUE,
		started_at TEXT,
		ended_at TEXT,
		duration_seconds INTEGER,
		user_message_count INTEGER DEFAULT 0,
		assistant_message_count INTEGER DEFAULT 0,
		system_message_count INTEGER DEFAULT 0,
		tool_call_count INTEGER DEFAULT 0,
		tool_result_count INTEGER DEFAULT 0,
		known_input_tokens INTEGER DEFAULT 0,
		known_output_tokens INTEGER DEFAULT 0,
		known_total_tokens INTEGER DEFAULT 0,
		estimated_input_tokens INTEGER DEFAULT 0,
		estimated_output_tokens INTEGER DEFAULT 0,
		estimated_total_tokens INTEGER DEFAULT 0,
		cache_creation_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		limit_event_count INTEGER DEFAULT 0,
		first_limit_event_at TEXT,
		ended_after_limit_event BOOLEAN DEFAULT FALSE,
		parse_error_count INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(project_id) REFERENCES projects(id)
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL,
		session_db_id INTEGER NOT NULL,
		session_id TEXT,
		source_file TEXT NOT NULL,
		line_number INTEGER NOT NULL,
		timestamp TEXT,
		event_type TEXT NOT NULL,
		role TEXT,
		message_type TEXT,
		tool_name TEXT,
		tool_input_summary TEXT DEFAULT '',
		char_count INTEGER DEFAULT 0,
		estimated_tokens INTEGER DEFAULT 0,
		known_input_tokens INTEGER DEFAULT 0,
		known_output_tokens INTEGER DEFAULT 0,
		known_total_tokens INTEGER DEFAULT 0,
		cache_creation_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(project_id) REFERENCES projects(id),
		FOREIGN KEY(session_db_id) REFERENCES sessions(id)
	)`,
	`CREATE TABLE IF NOT EXISTS limit_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL,
		session_db_id INTEGER NOT NULL,
		session_id TEXT,
		source_file TEXT NOT NULL,
		line_number INTEGER NOT NULL,
		timestamp TEXT,
		classification TEXT NOT NULL,
		matched_pattern TEXT NOT NULL,
		confidence REAL NOT NULL,
		redacted_excerpt TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(project_id) REFERENCES projects(id),
		FOREIGN KEY(session_db_id) REFERENCES sessions(id)
	)`,
	`CREATE TABLE IF NOT EXISTS daily_usage (
		date TEXT PRIMARY KEY,
		session_count INTEGER DEFAULT 0,
		project_count INTEGER DEFAULT 0,
		user_message_count INTEGER DEFAULT 0,
		assistant_message_count INTEGER DEFAULT 0,
		tool_call_count INTEGER DEFAULT 0,
		known_tokens INTEGER DEFAULT 0,
		estimated_tokens INTEGER DEFAULT 0,
		active_seconds INTEGER DEFAULT 0,
		limit_event_count INTEGER DEFAULT 0,
		first_activity_at TEXT,
		last_activity_at TEXT
	)`,
}

// v2Alters add new columns to existing databases.
// Fresh databases already have these columns from schemaMigrations above.
// "duplicate column name" errors are intentionally ignored.
var v2Alters = []string{
	`ALTER TABLE sessions ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN cache_read_tokens INTEGER DEFAULT 0`,
	`ALTER TABLE events ADD COLUMN tool_input_summary TEXT DEFAULT ''`,
	`ALTER TABLE events ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0`,
	`ALTER TABLE events ADD COLUMN cache_read_tokens INTEGER DEFAULT 0`,
}

func RunMigrations(db *sql.DB) error {
	for _, stmt := range schemaMigrations {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	var ver int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&ver)

	if ver < 2 {
		for _, stmt := range v2Alters {
			if _, err := db.Exec(stmt); err != nil {
				if !strings.Contains(err.Error(), "duplicate column name") {
					return err
				}
			}
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO schema_version(version) VALUES (2)`); err != nil {
			return err
		}
	}
	return nil
}
