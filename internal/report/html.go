package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

//go:embed embed_template.html
var rawTemplate string

//go:embed embed_styles.css
var rawCSS string

//go:embed embed_app.js
var rawAppJS string

type templateData struct {
	ProjectName string
	GeneratedAt string
	CSS         template.CSS
	AppJS       template.JS
	DataJSON    template.JS
	Summary     templateSummary
	Conclusion  string
}

type templateSummary struct {
	SessionCount          int
	ActiveDays            int
	UserMessages          int
	AssistantMessages     int
	ToolCalls             int
	KnownTokensFmt        string
	EstimatedTokensFmt    string
	LimitEvents           int
}

// GenerateHTMLReportWithConclusion writes the report with an optional evidence pack conclusion.
func GenerateHTMLReportWithConclusion(outDir string, rd *ReportData, conclusion string) error {
	return generateHTML(outDir, rd, conclusion)
}

func GenerateHTMLReport(outDir string, rd *ReportData) error {
	return generateHTML(outDir, rd, "")
}

func generateHTML(outDir string, rd *ReportData, conclusion string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// Prepare JSON data blob for the report
	type reportPayload struct {
		Daily       any `json:"daily"`
		Sessions    any `json:"sessions"`
		LimitEvents any `json:"limit_events"`
	}

	// Map daily rows to lowercase-key JSON
	type dailyJSON struct {
		Date                  string `json:"date"`
		SessionCount          int    `json:"session_count"`
		UserMessageCount      int    `json:"user_message_count"`
		AssistantMessageCount int    `json:"assistant_message_count"`
		ToolCallCount         int    `json:"tool_call_count"`
		KnownTokens           int64  `json:"known_tokens"`
		EstimatedTokens       int64  `json:"estimated_tokens"`
		ActiveSeconds         int64  `json:"active_seconds"`
		LimitEventCount       int    `json:"limit_event_count"`
	}
	type sessionJSON struct {
		StartedAt             string `json:"started_at"`
		EndedAt               string `json:"ended_at"`
		DurationSeconds       int64  `json:"duration_seconds"`
		UserMessageCount      int    `json:"user_message_count"`
		AssistantMessageCount int    `json:"assistant_message_count"`
		ToolCallCount         int    `json:"tool_call_count"`
		KnownTotalTokens      int64  `json:"known_total_tokens"`
		EstimatedTotalTokens  int64  `json:"estimated_total_tokens"`
		LimitEventCount       int    `json:"limit_event_count"`
		FirstLimitEventAt     string `json:"first_limit_event_at"`
		EndedAfterLimitEvent  bool   `json:"ended_after_limit_event"`
	}
	type limitJSON struct {
		Timestamp       string  `json:"timestamp"`
		Classification  string  `json:"classification"`
		MatchedPattern  string  `json:"matched_pattern"`
		Confidence      float64 `json:"confidence"`
		RedactedExcerpt string  `json:"redacted_excerpt"`
		SessionID       string  `json:"session_id"`
	}

	daily := make([]dailyJSON, len(rd.Daily))
	for i, r := range rd.Daily {
		daily[i] = dailyJSON{
			Date:                  r.Date,
			SessionCount:          r.SessionCount,
			UserMessageCount:      r.UserMessageCount,
			AssistantMessageCount: r.AssistantMessageCount,
			ToolCallCount:         r.ToolCallCount,
			KnownTokens:           r.KnownTokens,
			EstimatedTokens:       r.EstimatedTokens,
			ActiveSeconds:         r.ActiveSeconds,
			LimitEventCount:       r.LimitEventCount,
		}
	}
	sessions := make([]sessionJSON, len(rd.Sessions))
	for i, s := range rd.Sessions {
		sessions[i] = sessionJSON{
			StartedAt:             s.StartedAt,
			EndedAt:               s.EndedAt,
			DurationSeconds:       s.DurationSeconds,
			UserMessageCount:      s.UserMessageCount,
			AssistantMessageCount: s.AssistantMessageCount,
			ToolCallCount:         s.ToolCallCount,
			KnownTotalTokens:      s.KnownTotalTokens,
			EstimatedTotalTokens:  s.EstimatedTotalTokens,
			LimitEventCount:       s.LimitEventCount,
			FirstLimitEventAt:     s.FirstLimitEventAt,
			EndedAfterLimitEvent:  s.EndedAfterLimitEvent,
		}
	}
	limits := make([]limitJSON, len(rd.LimitEvents))
	for i, l := range rd.LimitEvents {
		limits[i] = limitJSON{
			Timestamp:       l.Timestamp,
			Classification:  l.Classification,
			MatchedPattern:  l.MatchedPattern,
			Confidence:      l.Confidence,
			RedactedExcerpt: l.RedactedExcerpt,
			SessionID:       l.SessionID,
		}
	}

	payload := reportPayload{Daily: daily, Sessions: sessions, LimitEvents: limits}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal report data: %w", err)
	}

	tmpl, err := template.New("report").Parse(rawTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	td := templateData{
		ProjectName: rd.ProjectName,
		GeneratedAt: rd.GeneratedAt,
		CSS:         template.CSS(rawCSS),
		AppJS:       template.JS(rawAppJS),
		DataJSON:    template.JS(jsonBytes),
		Conclusion:  conclusion,
		Summary: templateSummary{
			SessionCount:       rd.Summary.SessionCount,
			ActiveDays:         rd.Summary.ActiveDays,
			UserMessages:       rd.Summary.UserMessages,
			AssistantMessages:  rd.Summary.AssistantMessages,
			ToolCalls:          rd.Summary.ToolCalls,
			KnownTokensFmt:     fmtNum(rd.Summary.KnownTotalTokens),
			EstimatedTokensFmt: fmtNum(rd.Summary.EstimatedTotalTokens),
			LimitEvents:        rd.Summary.LimitEvents,
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	outPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("Report written to %s\n", outPath)
	return nil
}

func fmtNum(n int64) string {
	if n == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	// insert commas
	result := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
