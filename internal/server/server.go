package server

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/seamus-waldron/ccan/internal/db"
)

//go:embed dashboard.html
var dashboardHTMLBytes []byte

//go:embed project.html
var projectHTMLBytes []byte

type Server struct {
	db   *db.DB
	host string
	port int
}

func New(database *db.DB, host string, port int) *Server {
	return &Server{db: database, host: host, port: port}
}

func (s *Server) Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/daily", s.handleDaily)
	mux.HandleFunc("/api/projects", s.handleProjects)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/limit-events", s.handleLimitEvents)
	mux.HandleFunc("/api/project/", s.handleProjectDetail)
	mux.HandleFunc("/project", s.handleProjectPage)
	mux.HandleFunc("/", s.handleDashboard)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	fmt.Printf("Dashboard: http://%s\n", addr)

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
		},
	}

	listener, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			fmt.Printf("Port %s already in use, terminating existing process...\n", addr)
			killProcessOnPort(s.port)
			// Retry with backoff after killing the process
			for i := 0; i < 5; i++ {
				time.Sleep(time.Duration((i+1)*200) * time.Millisecond)
				listener, err = lc.Listen(context.Background(), "tcp", addr)
				if err == nil {
					fmt.Fprintf(os.Stderr, "✓ Port freed, server starting...\n")
					break
				}
			}
			if err != nil {
				return fmt.Errorf("failed to bind after terminating old process: %w", err)
			}
		} else {
			return err
		}
	}
	return http.Serve(listener, mux)
}

func killProcessOnPort(port int) {
	// Try to kill the process using lsof (macOS/Linux)
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return
	}
	pids := strings.Fields(strings.TrimSpace(string(output)))
	if len(pids) == 0 {
		return
	}
	myPID := os.Getpid()
	for _, pidStr := range pids {
		if pidStr == fmt.Sprintf("%d", myPID) {
			// Don't kill ourselves
			continue
		}
		if err := exec.Command("kill", "-9", pidStr).Run(); err == nil {
			fmt.Fprintf(os.Stderr, "Killed process %s on port %d\n", pidStr, port)
		}
	}
}

// ── summary ───────────────────────────────────────────────────────────────────

type summaryResponse struct {
	ProjectCount          int    `json:"project_count"`
	SessionCount          int    `json:"session_count"`
	UserMessages          int64  `json:"user_messages"`
	AssistantMessages     int64  `json:"assistant_messages"`
	ToolCalls             int64  `json:"tool_calls"`
	KnownTotalTokens      int64  `json:"known_total_tokens"`
	EstimatedTotalTokens  int64  `json:"estimated_total_tokens"`
	LimitEvents           int64  `json:"limit_events"`
	ActiveDays            int    `json:"active_days"`
	FirstSessionAt        string `json:"first_session_at"`
	LastSessionAt         string `json:"last_session_at"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	since, until := dateFilters(r)

	row := s.db.QueryRow(`
		SELECT
			COUNT(DISTINCT project_id),
			COUNT(*),
			COALESCE(SUM(user_message_count),0),
			COALESCE(SUM(assistant_message_count),0),
			COALESCE(SUM(tool_call_count),0),
			COALESCE(SUM(known_total_tokens),0),
			COALESCE(SUM(estimated_total_tokens),0),
			COALESCE(SUM(limit_event_count),0),
			MIN(started_at),
			MAX(ended_at)
		FROM sessions
		WHERE (? = '' OR started_at >= ?) AND (? = '' OR started_at <= ?)`,
		since, since, until, until)

	var resp summaryResponse
	var firstAt, lastAt sql.NullString
	if err := row.Scan(
		&resp.ProjectCount, &resp.SessionCount,
		&resp.UserMessages, &resp.AssistantMessages, &resp.ToolCalls,
		&resp.KnownTotalTokens, &resp.EstimatedTotalTokens,
		&resp.LimitEvents, &firstAt, &lastAt,
	); err != nil {
		jsonErr(w, err)
		return
	}
	resp.FirstSessionAt = firstAt.String
	resp.LastSessionAt = lastAt.String

	// count active days
	dayRow := s.db.QueryRow(`
		SELECT COUNT(*) FROM daily_usage
		WHERE (? = '' OR date >= ?) AND (? = '' OR date <= ?)`,
		since, since, until, until)
	_ = dayRow.Scan(&resp.ActiveDays)

	jsonOK(w, resp)
}

// ── daily ─────────────────────────────────────────────────────────────────────

type dailyRow struct {
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

func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	since, until := dateFilters(r)
	rows, err := s.db.Query(`
		SELECT date, session_count, user_message_count, assistant_message_count,
		       tool_call_count, known_tokens, estimated_tokens, active_seconds, limit_event_count
		FROM daily_usage
		WHERE (? = '' OR date >= ?) AND (? = '' OR date <= ?)
		ORDER BY date ASC`,
		since, since, until, until)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer rows.Close()
	var result []dailyRow
	for rows.Next() {
		var r dailyRow
		if err := rows.Scan(
			&r.Date, &r.SessionCount, &r.UserMessageCount, &r.AssistantMessageCount,
			&r.ToolCallCount, &r.KnownTokens, &r.EstimatedTokens, &r.ActiveSeconds, &r.LimitEventCount,
		); err != nil {
			jsonErr(w, err)
			return
		}
		result = append(result, r)
	}
	jsonOK(w, result)
}

// ── projects ──────────────────────────────────────────────────────────────────

type projectRow struct {
	ID              int64  `json:"id"`
	EncodedPath     string `json:"encoded_path"`
	DecodedPath     string `json:"decoded_path"`
	SessionCount    int    `json:"session_count"`
	FirstSeenAt     string `json:"first_seen_at"`
	LastSeenAt      string `json:"last_seen_at"`
	TotalMessages   int64  `json:"total_messages"`
	TotalToolCalls  int64  `json:"total_tool_calls"`
	TotalKnownTok   int64  `json:"total_known_tokens"`
	TotalLimitEvts  int64  `json:"total_limit_events"`
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	since, until := dateFilters(r)
	rows, err := s.db.Query(`
		SELECT p.id, p.encoded_path, COALESCE(p.decoded_path_guess,''),
		       COUNT(s.id),
		       MIN(s.started_at), MAX(s.ended_at),
		       COALESCE(SUM(s.user_message_count+s.assistant_message_count),0),
		       COALESCE(SUM(s.tool_call_count),0),
		       COALESCE(SUM(s.known_total_tokens),0),
		       COALESCE(SUM(s.limit_event_count),0)
		FROM projects p
		LEFT JOIN sessions s ON s.project_id = p.id
		WHERE (? = '' OR s.started_at >= ?) AND (? = '' OR s.started_at <= ?)
		GROUP BY p.id
		ORDER BY MAX(s.ended_at) DESC`,
		since, since, until, until)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer rows.Close()
	var result []projectRow
	for rows.Next() {
		var pr projectRow
		var first, last sql.NullString
		if err := rows.Scan(
			&pr.ID, &pr.EncodedPath, &pr.DecodedPath,
			&pr.SessionCount, &first, &last,
			&pr.TotalMessages, &pr.TotalToolCalls,
			&pr.TotalKnownTok, &pr.TotalLimitEvts,
		); err != nil {
			jsonErr(w, err)
			return
		}
		pr.FirstSeenAt = first.String
		pr.LastSeenAt = last.String
		result = append(result, pr)
	}
	jsonOK(w, result)
}

// ── sessions ──────────────────────────────────────────────────────────────────

type sessionRow struct {
	ID                    int64  `json:"id"`
	SessionID             string `json:"session_id"`
	ProjectID             int64  `json:"project_id"`
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

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	since, until := dateFilters(r)
	limitStr := r.URL.Query().Get("limit")
	lim := 500
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			lim = n
		}
	}

	rows, err := s.db.Query(`
		SELECT id, COALESCE(session_id,''), project_id,
		       COALESCE(started_at,''), COALESCE(ended_at,''), COALESCE(duration_seconds,0),
		       user_message_count, assistant_message_count, tool_call_count,
		       known_total_tokens, estimated_total_tokens,
		       limit_event_count, COALESCE(first_limit_event_at,''), ended_after_limit_event
		FROM sessions
		WHERE (? = '' OR started_at >= ?) AND (? = '' OR started_at <= ?)
		ORDER BY started_at DESC LIMIT ?`,
		since, since, until, until, lim)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer rows.Close()
	var result []sessionRow
	for rows.Next() {
		var sr sessionRow
		if err := rows.Scan(
			&sr.ID, &sr.SessionID, &sr.ProjectID,
			&sr.StartedAt, &sr.EndedAt, &sr.DurationSeconds,
			&sr.UserMessageCount, &sr.AssistantMessageCount, &sr.ToolCallCount,
			&sr.KnownTotalTokens, &sr.EstimatedTotalTokens,
			&sr.LimitEventCount, &sr.FirstLimitEventAt, &sr.EndedAfterLimitEvent,
		); err != nil {
			jsonErr(w, err)
			return
		}
		result = append(result, sr)
	}
	jsonOK(w, result)
}

// ── limit-events ──────────────────────────────────────────────────────────────

type limitEventRow struct {
	Timestamp       string  `json:"timestamp"`
	Classification  string  `json:"classification"`
	MatchedPattern  string  `json:"matched_pattern"`
	Confidence      float64 `json:"confidence"`
	RedactedExcerpt string  `json:"redacted_excerpt"`
	SessionID       string  `json:"session_id"`
}

func (s *Server) handleLimitEvents(w http.ResponseWriter, r *http.Request) {
	since, until := dateFilters(r)
	rows, err := s.db.Query(`
		SELECT COALESCE(timestamp,''), classification, matched_pattern, confidence,
		       COALESCE(redacted_excerpt,''), COALESCE(session_id,'')
		FROM limit_events
		WHERE (? = '' OR timestamp >= ?) AND (? = '' OR timestamp <= ?)
		ORDER BY timestamp DESC`,
		since, since, until, until)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer rows.Close()
	var result []limitEventRow
	for rows.Next() {
		var le limitEventRow
		if err := rows.Scan(
			&le.Timestamp, &le.Classification, &le.MatchedPattern, &le.Confidence,
			&le.RedactedExcerpt, &le.SessionID,
		); err != nil {
			jsonErr(w, err)
			return
		}
		result = append(result, le)
	}
	jsonOK(w, result)
}

// ── project detail ────────────────────────────────────────────────────────────

type projectDetailResponse struct {
	Project      projectInfoRow      `json:"project"`
	Summary      summaryResponse     `json:"summary"`
	Daily        []dailyRow          `json:"daily"`
	Sessions     []sessionRow        `json:"sessions"`
	LimitEvents  []limitEventRow     `json:"limit_events"`
}

type projectInfoRow struct {
	ID          int64  `json:"id"`
	EncodedPath string `json:"encoded_path"`
	DecodedPath string `json:"decoded_path"`
	FirstSeenAt string `json:"first_seen_at"`
	LastSeenAt  string `json:"last_seen_at"`
}

func (s *Server) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/api/project/"):]
	if idStr == "" {
		jsonErr(w, fmt.Errorf("missing project id"))
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonErr(w, fmt.Errorf("invalid project id: %w", err))
		return
	}

	// Fetch project info
	var proj projectInfoRow
	row := s.db.QueryRow(`
		SELECT p.id, p.encoded_path, COALESCE(p.decoded_path_guess,''),
		       COALESCE(MIN(s.started_at),''), COALESCE(MAX(s.ended_at),'')
		FROM projects p
		LEFT JOIN sessions s ON s.project_id = p.id
		WHERE p.id = ?
		GROUP BY p.id`, id)
	if err := row.Scan(&proj.ID, &proj.EncodedPath, &proj.DecodedPath, &proj.FirstSeenAt, &proj.LastSeenAt); err != nil {
		jsonErr(w, err)
		return
	}

	// Fetch summary (aggregated from sessions for this project)
	var summ summaryResponse
	sumRow := s.db.QueryRow(`
		SELECT 1,
		       COUNT(*),
		       COALESCE(SUM(user_message_count),0),
		       COALESCE(SUM(assistant_message_count),0),
		       COALESCE(SUM(tool_call_count),0),
		       COALESCE(SUM(known_total_tokens),0),
		       COALESCE(SUM(estimated_total_tokens),0),
		       COALESCE(SUM(limit_event_count),0),
		       MIN(started_at), MAX(ended_at)
		FROM sessions WHERE project_id = ?`, id)
	var firstAt, lastAt sql.NullString
	if err := sumRow.Scan(
		&summ.ProjectCount, &summ.SessionCount,
		&summ.UserMessages, &summ.AssistantMessages, &summ.ToolCalls,
		&summ.KnownTotalTokens, &summ.EstimatedTotalTokens,
		&summ.LimitEvents, &firstAt, &lastAt,
	); err == nil {
		summ.FirstSessionAt = firstAt.String
		summ.LastSessionAt = lastAt.String
	}

	// Fetch daily usage (reconstructed from sessions per day)
	dailyRows, err := s.db.Query(`
		SELECT COALESCE(DATE(s.started_at),'') as date,
		       COUNT(*) as session_count,
		       COALESCE(SUM(s.user_message_count),0) as user_messages,
		       COALESCE(SUM(s.assistant_message_count),0) as asst_messages,
		       COALESCE(SUM(s.tool_call_count),0) as tool_calls,
		       COALESCE(SUM(s.known_total_tokens),0) as known_tokens,
		       COALESCE(SUM(s.estimated_total_tokens),0) as est_tokens,
		       COALESCE(SUM(s.duration_seconds),0) as active_seconds,
		       COALESCE(SUM(s.limit_event_count),0) as limit_events
		FROM sessions s
		WHERE s.project_id = ?
		GROUP BY DATE(s.started_at)
		ORDER BY date ASC`, id)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer dailyRows.Close()
	var daily []dailyRow
	for dailyRows.Next() {
		var r dailyRow
		if err := dailyRows.Scan(
			&r.Date, &r.SessionCount, &r.UserMessageCount, &r.AssistantMessageCount,
			&r.ToolCallCount, &r.KnownTokens, &r.EstimatedTokens, &r.ActiveSeconds, &r.LimitEventCount,
		); err != nil {
			jsonErr(w, err)
			return
		}
		daily = append(daily, r)
	}

	// Fetch sessions
	sessRows, err := s.db.Query(`
		SELECT id, COALESCE(session_id,''), project_id,
		       COALESCE(started_at,''), COALESCE(ended_at,''), COALESCE(duration_seconds,0),
		       user_message_count, assistant_message_count, tool_call_count,
		       known_total_tokens, estimated_total_tokens,
		       limit_event_count, COALESCE(first_limit_event_at,''), ended_after_limit_event
		FROM sessions
		WHERE project_id = ?
		ORDER BY started_at DESC
		LIMIT 1000`, id)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer sessRows.Close()
	var sessions []sessionRow
	for sessRows.Next() {
		var r sessionRow
		if err := sessRows.Scan(
			&r.ID, &r.SessionID, &r.ProjectID,
			&r.StartedAt, &r.EndedAt, &r.DurationSeconds,
			&r.UserMessageCount, &r.AssistantMessageCount, &r.ToolCallCount,
			&r.KnownTotalTokens, &r.EstimatedTotalTokens,
			&r.LimitEventCount, &r.FirstLimitEventAt, &r.EndedAfterLimitEvent,
		); err != nil {
			jsonErr(w, err)
			return
		}
		sessions = append(sessions, r)
	}

	// Fetch limit events
	limRows, err := s.db.Query(`
		SELECT COALESCE(timestamp,''), classification, matched_pattern, confidence,
		       COALESCE(redacted_excerpt,''), COALESCE(session_id,'')
		FROM limit_events
		WHERE project_id = ?
		ORDER BY timestamp DESC
		LIMIT 1000`, id)
	if err != nil {
		jsonErr(w, err)
		return
	}
	defer limRows.Close()
	var limits []limitEventRow
	for limRows.Next() {
		var r limitEventRow
		if err := limRows.Scan(
			&r.Timestamp, &r.Classification, &r.MatchedPattern, &r.Confidence,
			&r.RedactedExcerpt, &r.SessionID,
		); err != nil {
			jsonErr(w, err)
			return
		}
		limits = append(limits, r)
	}

	resp := projectDetailResponse{
		Project:     proj,
		Summary:     summ,
		Daily:       daily,
		Sessions:    sessions,
		LimitEvents: limits,
	}
	jsonOK(w, resp)
}

func (s *Server) handleProjectPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(projectHTMLBytes)
}

// ── dashboard ─────────────────────────────────────────────────────────────────


func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTMLBytes)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func dateFilters(r *http.Request) (since, until string) {
	since = r.URL.Query().Get("since")
	until = r.URL.Query().Get("until")
	if since != "" {
		if _, err := time.Parse("2006-01-02", since); err != nil {
			since = ""
		}
	}
	if until != "" {
		if _, err := time.Parse("2006-01-02", until); err != nil {
			until = ""
		}
	}
	return
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
