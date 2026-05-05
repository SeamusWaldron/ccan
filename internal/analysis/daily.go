package analysis

import (
	"database/sql"
	"time"

	"github.com/seamus-waldron/ccan/internal/db"
	"github.com/seamus-waldron/ccan/internal/parser"
)

// AggregateDailyUsage derives daily_usage rows from a parse result and upserts them.
func AggregateDailyUsage(database *db.DB, r *parser.ParseResult, sessionRow *db.SessionRow) error {
	// Group entries by date
	type dayKey = string
	type dayBucket struct {
		userMessages      int
		assistantMessages int
		toolCalls         int
		knownTokens       int64
		estimatedTokens   int64
		limitEvents       int
		firstAt           time.Time
		lastAt            time.Time
		hasFirst          bool
	}
	days := make(map[dayKey]*dayBucket)
	seenMsgIDs := make(map[string]bool)

	for _, e := range r.Entries {
		if !e.HasTimestamp {
			continue
		}
		dateStr := e.Timestamp.UTC().Format("2006-01-02")
		bucket, ok := days[dateStr]
		if !ok {
			bucket = &dayBucket{}
			days[dateStr] = bucket
		}
		if !bucket.hasFirst || e.Timestamp.Before(bucket.firstAt) {
			bucket.firstAt = e.Timestamp
			bucket.hasFirst = true
		}
		if e.Timestamp.After(bucket.lastAt) {
			bucket.lastAt = e.Timestamp
		}

		if e.IsUserMessage {
			bucket.userMessages++
		}
		if e.IsAssistant {
			if e.MessageID == "" || !seenMsgIDs[e.MessageID] {
				if e.MessageID != "" {
					seenMsgIDs[e.MessageID] = true
				}
				bucket.assistantMessages++
			}
			bucket.toolCalls += len(e.ToolCalls)
		}
		if e.HasTokenUsage {
			bucket.knownTokens += int64(e.InputTokens + e.OutputTokens)
		} else if e.CharCount > 0 {
			bucket.estimatedTokens += int64(e.CharCount / 4)
		}
		if e.LimitDetected {
			bucket.limitEvents++
		}
	}

	for dateStr, bucket := range days {
		var activeSec int64
		if !bucket.lastAt.IsZero() && !bucket.firstAt.IsZero() {
			activeSec = int64(bucket.lastAt.Sub(bucket.firstAt).Seconds())
		}
		row := &db.DailyUsageRow{
			Date:                  dateStr,
			SessionCount:          1,
			ProjectCount:          1,
			UserMessageCount:      bucket.userMessages,
			AssistantMessageCount: bucket.assistantMessages,
			ToolCallCount:         bucket.toolCalls,
			KnownTokens:           bucket.knownTokens,
			EstimatedTokens:       bucket.estimatedTokens,
			ActiveSeconds:         activeSec,
			LimitEventCount:       bucket.limitEvents,
		}
		if bucket.hasFirst {
			row.FirstActivityAt = bucket.firstAt.UTC().Format(time.RFC3339)
			row.LastActivityAt = bucket.lastAt.UTC().Format(time.RFC3339)
		}
		if err := database.UpsertDailyUsage(row); err != nil {
			return err
		}
	}
	return nil
}

// SessionSummary holds per-session data for reporting.
type SessionSummary struct {
	ID                    int64
	SessionID             string
	SourceFile            string
	StartedAt             string
	EndedAt               string
	DurationSeconds       int64
	UserMessageCount      int
	AssistantMessageCount int
	ToolCallCount         int
	KnownTotalTokens      int64
	EstimatedTotalTokens  int64
	LimitEventCount       int
	FirstLimitEventAt     string
	EndedAfterLimitEvent  bool
}

// DailyRow is the summary shape for a single day.
type DailyRow struct {
	Date                  string
	SessionCount          int
	UserMessageCount      int
	AssistantMessageCount int
	ToolCallCount         int
	KnownTokens           int64
	EstimatedTokens       int64
	ActiveSeconds         int64
	LimitEventCount       int
}

// LimitEventSummary is for report display.
type LimitEventSummary struct {
	Timestamp       string
	Classification  string
	MatchedPattern  string
	Confidence      float64
	RedactedExcerpt string
	SessionID       string
}

// LoadSessionsForProject fetches all sessions for a project from the DB.
func LoadSessionsForProject(database *db.DB, projectID int64) ([]SessionSummary, error) {
	rows, err := database.Query(`
		SELECT id, session_id, source_file,
		       started_at, ended_at, duration_seconds,
		       user_message_count, assistant_message_count, tool_call_count,
		       known_total_tokens, estimated_total_tokens,
		       limit_event_count, first_limit_event_at, ended_after_limit_event
		FROM sessions WHERE project_id = ? ORDER BY started_at ASC`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []SessionSummary
	for rows.Next() {
		var s SessionSummary
		var startedAt, endedAt, firstLimit sql.NullString
		var endedAfter sql.NullBool
		if err := rows.Scan(
			&s.ID, &s.SessionID, &s.SourceFile,
			&startedAt, &endedAt, &s.DurationSeconds,
			&s.UserMessageCount, &s.AssistantMessageCount, &s.ToolCallCount,
			&s.KnownTotalTokens, &s.EstimatedTotalTokens,
			&s.LimitEventCount, &firstLimit, &endedAfter,
		); err != nil {
			return nil, err
		}
		s.StartedAt = startedAt.String
		s.EndedAt = endedAt.String
		s.FirstLimitEventAt = firstLimit.String
		s.EndedAfterLimitEvent = endedAfter.Bool
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// LoadDailyUsage fetches all daily_usage rows ordered by date.
func LoadDailyUsage(database *db.DB) ([]DailyRow, error) {
	rows, err := database.Query(`
		SELECT date, session_count, user_message_count, assistant_message_count,
		       tool_call_count, known_tokens, estimated_tokens, active_seconds, limit_event_count
		FROM daily_usage ORDER BY date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DailyRow
	for rows.Next() {
		var r DailyRow
		if err := rows.Scan(
			&r.Date, &r.SessionCount, &r.UserMessageCount, &r.AssistantMessageCount,
			&r.ToolCallCount, &r.KnownTokens, &r.EstimatedTokens, &r.ActiveSeconds, &r.LimitEventCount,
		); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// LoadLimitEvents fetches all limit events for a project.
func LoadLimitEvents(database *db.DB, projectID int64) ([]LimitEventSummary, error) {
	rows, err := database.Query(`
		SELECT le.timestamp, le.classification, le.matched_pattern, le.confidence,
		       le.redacted_excerpt, le.session_id
		FROM limit_events le WHERE le.project_id = ? ORDER BY le.timestamp ASC`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LimitEventSummary
	for rows.Next() {
		var l LimitEventSummary
		var ts, cls, pat, excerpt, sessID sql.NullString
		var conf sql.NullFloat64
		if err := rows.Scan(&ts, &cls, &pat, &conf, &excerpt, &sessID); err != nil {
			return nil, err
		}
		l.Timestamp = ts.String
		l.Classification = cls.String
		l.MatchedPattern = pat.String
		l.Confidence = conf.Float64
		l.RedactedExcerpt = excerpt.String
		l.SessionID = sessID.String
		result = append(result, l)
	}
	return result, rows.Err()
}

// LoadAllSessions returns all sessions across all projects, ordered by start time.
func LoadAllSessions(database *db.DB) ([]SessionSummary, error) {
	rows, err := database.Query(`
		SELECT id, session_id, source_file,
		       started_at, ended_at, duration_seconds,
		       user_message_count, assistant_message_count, tool_call_count,
		       known_total_tokens, estimated_total_tokens,
		       limit_event_count, first_limit_event_at, ended_after_limit_event
		FROM sessions ORDER BY started_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []SessionSummary
	for rows.Next() {
		var s SessionSummary
		var startedAt, endedAt, firstLimit sql.NullString
		var endedAfter sql.NullBool
		if err := rows.Scan(
			&s.ID, &s.SessionID, &s.SourceFile,
			&startedAt, &endedAt, &s.DurationSeconds,
			&s.UserMessageCount, &s.AssistantMessageCount, &s.ToolCallCount,
			&s.KnownTotalTokens, &s.EstimatedTotalTokens,
			&s.LimitEventCount, &firstLimit, &endedAfter,
		); err != nil {
			return nil, err
		}
		s.StartedAt = startedAt.String
		s.EndedAt = endedAt.String
		s.FirstLimitEventAt = firstLimit.String
		s.EndedAfterLimitEvent = endedAfter.Bool
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// LoadAllLimitEvents returns all limit events across all projects.
func LoadAllLimitEvents(database *db.DB) ([]LimitEventSummary, error) {
	rows, err := database.Query(`
		SELECT timestamp, classification, matched_pattern, confidence,
		       redacted_excerpt, session_id
		FROM limit_events ORDER BY timestamp ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LimitEventSummary
	for rows.Next() {
		var l LimitEventSummary
		var ts, cls, pat, excerpt, sessID sql.NullString
		var conf sql.NullFloat64
		if err := rows.Scan(&ts, &cls, &pat, &conf, &excerpt, &sessID); err != nil {
			return nil, err
		}
		l.Timestamp = ts.String
		l.Classification = cls.String
		l.MatchedPattern = pat.String
		l.Confidence = conf.Float64
		l.RedactedExcerpt = excerpt.String
		l.SessionID = sessID.String
		result = append(result, l)
	}
	return result, rows.Err()
}
