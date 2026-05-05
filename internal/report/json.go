package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seamus-waldron/ccan/internal/analysis"
)

type ReportData struct {
	GeneratedAt string                         `json:"generated_at"`
	ProjectName string                         `json:"project_name"`
	Summary     SummaryJSON                    `json:"summary"`
	Daily       []analysis.DailyRow            `json:"daily"`
	Sessions    []analysis.SessionSummary      `json:"sessions"`
	LimitEvents []analysis.LimitEventSummary   `json:"limit_events"`
}

type SummaryJSON struct {
	SessionCount          int    `json:"session_count"`
	ActiveDays            int    `json:"active_days"`
	UserMessages          int    `json:"user_messages"`
	AssistantMessages     int    `json:"assistant_messages"`
	ToolCalls             int    `json:"tool_calls"`
	KnownTotalTokens      int64  `json:"known_total_tokens"`
	EstimatedTotalTokens  int64  `json:"estimated_total_tokens"`
	LimitEvents           int    `json:"limit_events"`
	FirstSessionAt        string `json:"first_session_at"`
	LastSessionAt         string `json:"last_session_at"`
}

func BuildReportData(
	projectName string,
	sessions []analysis.SessionSummary,
	daily []analysis.DailyRow,
	limitEvents []analysis.LimitEventSummary,
) *ReportData {
	rd := &ReportData{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ProjectName: projectName,
		Daily:       daily,
		Sessions:    sessions,
		LimitEvents: limitEvents,
	}

	for _, s := range sessions {
		rd.Summary.SessionCount++
		rd.Summary.UserMessages += s.UserMessageCount
		rd.Summary.AssistantMessages += s.AssistantMessageCount
		rd.Summary.ToolCalls += s.ToolCallCount
		rd.Summary.KnownTotalTokens += s.KnownTotalTokens
		rd.Summary.EstimatedTotalTokens += s.EstimatedTotalTokens
		rd.Summary.LimitEvents += s.LimitEventCount
		if rd.Summary.FirstSessionAt == "" || s.StartedAt < rd.Summary.FirstSessionAt {
			rd.Summary.FirstSessionAt = s.StartedAt
		}
		if s.EndedAt > rd.Summary.LastSessionAt {
			rd.Summary.LastSessionAt = s.EndedAt
		}
	}
	rd.Summary.ActiveDays = len(daily)

	return rd
}

func WriteJSONData(outDir string, rd *ReportData) error {
	dataDir := filepath.Join(outDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	files := map[string]any{
		"summary.json":      rd.Summary,
		"daily_usage.json":  rd.Daily,
		"sessions.json":     rd.Sessions,
		"limit_events.json": rd.LimitEvents,
	}
	for name, v := range files {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
