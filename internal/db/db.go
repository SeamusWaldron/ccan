package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		sqldb.Close()
		return nil, err
	}
	if err := RunMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, err
	}
	return &DB{sqldb}, nil
}

// ResetDailyUsage truncates the daily_usage table so it can be rebuilt from scratch.
func (d *DB) ResetDailyUsage() error {
	_, err := d.Exec(`DELETE FROM daily_usage`)
	return err
}

// SessionExistsByFile returns the session DB id if the source file is already
// in the database, or 0 if not found.
func (d *DB) SessionExistsByFile(sourceFile string) (int64, error) {
	var id int64
	row := d.QueryRow(`SELECT id FROM sessions WHERE source_file = ?`, sourceFile)
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// DeleteSessionChildren removes events and limit_events for a given session DB id.
func (d *DB) DeleteSessionChildren(sessionDBID int64) error {
	if _, err := d.Exec(`DELETE FROM events WHERE session_db_id = ?`, sessionDBID); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM limit_events WHERE session_db_id = ?`, sessionDBID)
	return err
}

func (d *DB) UpsertProject(encodedPath, decodedGuess string) (int64, error) {
	_, err := d.Exec(`
		INSERT INTO projects (encoded_path, decoded_path_guess, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(encoded_path) DO UPDATE SET
			decoded_path_guess = excluded.decoded_path_guess,
			updated_at = CURRENT_TIMESTAMP`,
		encodedPath, decodedGuess)
	if err != nil {
		return 0, err
	}
	var id int64
	row := d.QueryRow(`SELECT id FROM projects WHERE encoded_path = ?`, encodedPath)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) UpsertSession(s *SessionRow) (int64, error) {
	res, err := d.Exec(`
		INSERT INTO sessions (
			project_id, session_id, source_file,
			started_at, ended_at, duration_seconds,
			user_message_count, assistant_message_count, system_message_count,
			tool_call_count, tool_result_count,
			known_input_tokens, known_output_tokens, known_total_tokens,
			estimated_input_tokens, estimated_output_tokens, estimated_total_tokens,
			cache_creation_tokens, cache_read_tokens,
			limit_event_count, first_limit_event_at, ended_after_limit_event,
			parse_error_count, created_at, updated_at
		) VALUES (
			?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
		)
		ON CONFLICT(source_file) DO UPDATE SET
			session_id               = excluded.session_id,
			started_at               = excluded.started_at,
			ended_at                 = excluded.ended_at,
			duration_seconds         = excluded.duration_seconds,
			user_message_count       = excluded.user_message_count,
			assistant_message_count  = excluded.assistant_message_count,
			system_message_count     = excluded.system_message_count,
			tool_call_count          = excluded.tool_call_count,
			tool_result_count        = excluded.tool_result_count,
			known_input_tokens       = excluded.known_input_tokens,
			known_output_tokens      = excluded.known_output_tokens,
			known_total_tokens       = excluded.known_total_tokens,
			estimated_input_tokens   = excluded.estimated_input_tokens,
			estimated_output_tokens  = excluded.estimated_output_tokens,
			estimated_total_tokens   = excluded.estimated_total_tokens,
			cache_creation_tokens    = excluded.cache_creation_tokens,
			cache_read_tokens        = excluded.cache_read_tokens,
			limit_event_count        = excluded.limit_event_count,
			first_limit_event_at     = excluded.first_limit_event_at,
			ended_after_limit_event  = excluded.ended_after_limit_event,
			parse_error_count        = excluded.parse_error_count,
			updated_at               = CURRENT_TIMESTAMP`,
		s.ProjectID, s.SessionID, s.SourceFile,
		s.StartedAt, s.EndedAt, s.DurationSeconds,
		s.UserMessageCount, s.AssistantMessageCount, s.SystemMessageCount,
		s.ToolCallCount, s.ToolResultCount,
		s.KnownInputTokens, s.KnownOutputTokens, s.KnownTotalTokens,
		s.EstimatedInputTokens, s.EstimatedOutputTokens, s.EstimatedTotalTokens,
		s.CacheCreationTokens, s.CacheReadTokens,
		s.LimitEventCount, s.FirstLimitEventAt, s.EndedAfterLimitEvent,
		s.ParseErrorCount,
	)
	if err != nil {
		return 0, err
	}
	// Always SELECT after upsert: ON CONFLICT DO UPDATE returns the existing rowid
	// via LastInsertId in some SQLite drivers but not all; SELECT is always correct.
	_ = res
	var id int64
	row := d.QueryRow(`SELECT id FROM sessions WHERE source_file = ?`, s.SourceFile)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) InsertEvent(e *EventRow) error {
	_, err := d.Exec(`
		INSERT INTO events (
			project_id, session_db_id, session_id, source_file, line_number,
			timestamp, event_type, role, message_type, tool_name, tool_input_summary,
			char_count, estimated_tokens, known_input_tokens, known_output_tokens, known_total_tokens,
			cache_creation_tokens, cache_read_tokens, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		e.ProjectID, e.SessionDBID, e.SessionID, e.SourceFile, e.LineNumber,
		e.Timestamp, e.EventType, e.Role, e.MessageType, e.ToolName, e.ToolInputSummary,
		e.CharCount, e.EstimatedTokens, e.KnownInputTokens, e.KnownOutputTokens, e.KnownTotalTokens,
		e.CacheCreationTokens, e.CacheReadTokens,
	)
	return err
}

func (d *DB) InsertLimitEvent(le *LimitEventRow) error {
	_, err := d.Exec(`
		INSERT INTO limit_events (
			project_id, session_db_id, session_id, source_file, line_number,
			timestamp, classification, matched_pattern, confidence, redacted_excerpt,
			created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		le.ProjectID, le.SessionDBID, le.SessionID, le.SourceFile, le.LineNumber,
		le.Timestamp, le.Classification, le.MatchedPattern, le.Confidence, le.RedactedExcerpt,
	)
	return err
}

func (d *DB) UpsertDailyUsage(r *DailyUsageRow) error {
	_, err := d.Exec(`
		INSERT INTO daily_usage (
			date, session_count, project_count,
			user_message_count, assistant_message_count, tool_call_count,
			known_tokens, estimated_tokens, active_seconds,
			limit_event_count, first_activity_at, last_activity_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(date) DO UPDATE SET
			session_count           = session_count + excluded.session_count,
			project_count           = MAX(project_count, excluded.project_count),
			user_message_count      = user_message_count + excluded.user_message_count,
			assistant_message_count = assistant_message_count + excluded.assistant_message_count,
			tool_call_count         = tool_call_count + excluded.tool_call_count,
			known_tokens            = known_tokens + excluded.known_tokens,
			estimated_tokens        = estimated_tokens + excluded.estimated_tokens,
			active_seconds          = active_seconds + excluded.active_seconds,
			limit_event_count       = limit_event_count + excluded.limit_event_count,
			first_activity_at       = MIN(first_activity_at, excluded.first_activity_at),
			last_activity_at        = MAX(last_activity_at, excluded.last_activity_at)`,
		r.Date, r.SessionCount, r.ProjectCount,
		r.UserMessageCount, r.AssistantMessageCount, r.ToolCallCount,
		r.KnownTokens, r.EstimatedTokens, r.ActiveSeconds,
		r.LimitEventCount, r.FirstActivityAt, r.LastActivityAt,
	)
	return err
}

// ---- row types ----

type SessionRow struct {
	ProjectID             int64
	SessionID             string
	SourceFile            string
	StartedAt             string
	EndedAt               string
	DurationSeconds       int64
	UserMessageCount      int
	AssistantMessageCount int
	SystemMessageCount    int
	ToolCallCount         int
	ToolResultCount       int
	KnownInputTokens      int64
	KnownOutputTokens     int64
	KnownTotalTokens      int64
	EstimatedInputTokens  int64
	EstimatedOutputTokens int64
	EstimatedTotalTokens  int64
	CacheCreationTokens   int64
	CacheReadTokens       int64
	LimitEventCount       int
	FirstLimitEventAt     string
	EndedAfterLimitEvent  bool
	ParseErrorCount       int
}

type EventRow struct {
	ProjectID           int64
	SessionDBID         int64
	SessionID           string
	SourceFile          string
	LineNumber          int
	Timestamp           string
	EventType           string
	Role                string
	MessageType         string
	ToolName            string
	ToolInputSummary    string
	CharCount           int
	EstimatedTokens     int
	KnownInputTokens    int64
	KnownOutputTokens   int64
	KnownTotalTokens    int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type LimitEventRow struct {
	ProjectID       int64
	SessionDBID     int64
	SessionID       string
	SourceFile      string
	LineNumber      int
	Timestamp       string
	Classification  string
	MatchedPattern  string
	Confidence      float64
	RedactedExcerpt string
}

type DailyUsageRow struct {
	Date                  string
	SessionCount          int
	ProjectCount          int
	UserMessageCount      int
	AssistantMessageCount int
	ToolCallCount         int
	KnownTokens           int64
	EstimatedTokens       int64
	ActiveSeconds         int64
	LimitEventCount       int
	FirstActivityAt       string
	LastActivityAt        string
}
