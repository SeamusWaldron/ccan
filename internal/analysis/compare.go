package analysis

import (
	"fmt"
	"time"

	"github.com/seamus-waldron/ccan/internal/db"
)

// PeriodMetrics holds averaged daily metrics for a date range.
type PeriodMetrics struct {
	Start                    string
	End                      string
	ActiveDays               int
	TotalSessions            int
	TotalUserMessages        int64
	TotalAssistantMessages   int64
	TotalToolCalls           int64
	TotalKnownTokens         int64
	TotalEstimatedTokens     int64
	TotalActiveSecs          int64
	TotalLimitEvents         int64
	MessagesPerActiveDay     float64
	ToolCallsPerActiveDay    float64
	TokensPerActiveDay       float64
	ActiveMinsPerDay         float64
	LimitEventsPerActiveDay  float64
	SessionsPerActiveDay     float64
	MedianTimeToFirstLimitMin float64
}

// ComparisonResult holds baseline vs current comparison data.
type ComparisonResult struct {
	Baseline        PeriodMetrics
	Current         PeriodMetrics
	Changes         map[string]float64 // metric -> % change
	RestrictionScore float64
}

// ComparePeriods loads and compares two date ranges from the database.
func ComparePeriods(database *db.DB, baselineStart, baselineEnd, currentStart, currentEnd string) (*ComparisonResult, error) {
	baseline, err := loadPeriodMetrics(database, baselineStart, baselineEnd)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	baseline.Start = baselineStart
	baseline.End = baselineEnd

	current, err := loadPeriodMetrics(database, currentStart, currentEnd)
	if err != nil {
		return nil, fmt.Errorf("current: %w", err)
	}
	current.Start = currentStart
	current.End = currentEnd

	changes := make(map[string]float64)
	pct := func(base, curr float64) float64 {
		if base == 0 {
			return 0
		}
		return ((curr - base) / base) * 100
	}

	changes["messages_per_active_day"] = pct(baseline.MessagesPerActiveDay, current.MessagesPerActiveDay)
	changes["tool_calls_per_active_day"] = pct(baseline.ToolCallsPerActiveDay, current.ToolCallsPerActiveDay)
	changes["tokens_per_active_day"] = pct(baseline.TokensPerActiveDay, current.TokensPerActiveDay)
	changes["active_mins_per_day"] = pct(baseline.ActiveMinsPerDay, current.ActiveMinsPerDay)
	changes["limit_events_per_active_day"] = pct(baseline.LimitEventsPerActiveDay, current.LimitEventsPerActiveDay)
	changes["sessions_per_active_day"] = pct(baseline.SessionsPerActiveDay, current.SessionsPerActiveDay)

	score := computeRestrictionScore(changes)

	return &ComparisonResult{
		Baseline:         *baseline,
		Current:          *current,
		Changes:          changes,
		RestrictionScore: score,
	}, nil
}

func loadPeriodMetrics(database *db.DB, start, end string) (*PeriodMetrics, error) {
	m := &PeriodMetrics{}

	// daily_usage gives us per-day aggregates
	rows, err := database.Query(`
		SELECT
			session_count,
			user_message_count + assistant_message_count,
			tool_call_count,
			known_tokens + estimated_tokens,
			active_seconds,
			limit_event_count
		FROM daily_usage
		WHERE (? = '' OR date >= ?) AND (? = '' OR date <= ?)`,
		start, start, end, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sessions, msgs, tools, tokens, secs, limits int64
		if err := rows.Scan(&sessions, &msgs, &tools, &tokens, &secs, &limits); err != nil {
			return nil, err
		}
		m.ActiveDays++
		m.TotalSessions += int(sessions)
		m.TotalUserMessages += msgs
		m.TotalToolCalls += tools
		m.TotalKnownTokens += tokens
		m.TotalActiveSecs += secs
		m.TotalLimitEvents += limits
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if m.ActiveDays > 0 {
		ad := float64(m.ActiveDays)
		m.MessagesPerActiveDay = float64(m.TotalUserMessages) / ad
		m.ToolCallsPerActiveDay = float64(m.TotalToolCalls) / ad
		m.TokensPerActiveDay = float64(m.TotalKnownTokens) / ad
		m.ActiveMinsPerDay = float64(m.TotalActiveSecs) / 60 / ad
		m.LimitEventsPerActiveDay = float64(m.TotalLimitEvents) / ad
		m.SessionsPerActiveDay = float64(m.TotalSessions) / ad
	}

	// median time-to-first-limit in minutes
	m.MedianTimeToFirstLimitMin = medianTimeToLimit(database, start, end)

	return m, nil
}

// medianTimeToLimit computes the median duration from session start to first limit event.
func medianTimeToLimit(database *db.DB, start, end string) float64 {
	rows, err := database.Query(`
		SELECT started_at, first_limit_event_at
		FROM sessions
		WHERE first_limit_event_at IS NOT NULL AND first_limit_event_at != ''
		  AND (? = '' OR started_at >= ?) AND (? = '' OR started_at <= ?)
		ORDER BY started_at`,
		start, start, end, end)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var durations []float64
	for rows.Next() {
		var startedAt, firstLimit string
		if err := rows.Scan(&startedAt, &firstLimit); err != nil {
			continue
		}
		t0, e0 := time.Parse(time.RFC3339, startedAt)
		t1, e1 := time.Parse(time.RFC3339, firstLimit)
		if e0 != nil || e1 != nil {
			continue
		}
		dur := t1.Sub(t0).Minutes()
		if dur >= 0 {
			durations = append(durations, dur)
		}
	}

	if len(durations) == 0 {
		return 0
	}
	mid := len(durations) / 2
	return durations[mid]
}
