package report

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func ExportCSV(outDir string, rd *ReportData) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if err := writeCSVFile(
		filepath.Join(outDir, "daily_usage.csv"),
		[]string{"date", "session_count", "user_message_count", "assistant_message_count",
			"tool_call_count", "known_tokens", "estimated_tokens", "active_seconds", "limit_event_count"},
		func(w *csv.Writer) error {
			for _, r := range rd.Daily {
				if err := w.Write([]string{
					r.Date,
					strconv.Itoa(r.SessionCount),
					strconv.Itoa(r.UserMessageCount),
					strconv.Itoa(r.AssistantMessageCount),
					strconv.Itoa(r.ToolCallCount),
					strconv.FormatInt(r.KnownTokens, 10),
					strconv.FormatInt(r.EstimatedTokens, 10),
					strconv.FormatInt(r.ActiveSeconds, 10),
					strconv.Itoa(r.LimitEventCount),
				}); err != nil {
					return err
				}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("daily_usage.csv: %w", err)
	}

	if err := writeCSVFile(
		filepath.Join(outDir, "sessions.csv"),
		[]string{"started_at", "ended_at", "duration_seconds", "user_messages",
			"assistant_messages", "tool_calls", "known_total_tokens", "estimated_total_tokens",
			"limit_event_count", "ended_after_limit_event"},
		func(w *csv.Writer) error {
			for _, s := range rd.Sessions {
				if err := w.Write([]string{
					s.StartedAt,
					s.EndedAt,
					strconv.FormatInt(s.DurationSeconds, 10),
					strconv.Itoa(s.UserMessageCount),
					strconv.Itoa(s.AssistantMessageCount),
					strconv.Itoa(s.ToolCallCount),
					strconv.FormatInt(s.KnownTotalTokens, 10),
					strconv.FormatInt(s.EstimatedTotalTokens, 10),
					strconv.Itoa(s.LimitEventCount),
					strconv.FormatBool(s.EndedAfterLimitEvent),
				}); err != nil {
					return err
				}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("sessions.csv: %w", err)
	}

	if err := writeCSVFile(
		filepath.Join(outDir, "limit_events.csv"),
		[]string{"timestamp", "classification", "matched_pattern", "confidence", "redacted_excerpt"},
		func(w *csv.Writer) error {
			for _, l := range rd.LimitEvents {
				if err := w.Write([]string{
					l.Timestamp,
					l.Classification,
					l.MatchedPattern,
					strconv.FormatFloat(l.Confidence, 'f', 2, 64),
					l.RedactedExcerpt,
				}); err != nil {
					return err
				}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("limit_events.csv: %w", err)
	}

	fmt.Printf("CSV exported to %s\n", outDir)
	return nil
}

func writeCSVFile(path string, headers []string, fn func(*csv.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(headers); err != nil {
		return err
	}
	if err := fn(w); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}
