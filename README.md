# Claude Code Usage Analyser (cua)

Monitor your Claude Code usage across all projects and identify message/token burn patterns over time.

**cua** is a local-first CLI tool that analyses your Claude Code session history to reveal:

- **Usage trends** — messages, tool calls, and token consumption by day, project, and session
- **Limit events** — detect when you hit rate limits, usage limits, capacity limits, or quota restrictions
- **Restriction patterns** — compare periods to see if Claude Code has become more restrictive
- **Per-project breakdown** — drill down into individual projects to investigate high-usage periods

All processing is local. No data leaves your machine.

## Features

### 🔍 Local Session Analysis
- Parses JSONL session files from `~/.claude/projects/**/*.jsonl`
- Extracts token counts, messages, tool calls, and session metadata
- Detects limit events via configurable text patterns and HTTP error codes
- Deduplicates token counting across split API responses
- Auto-ingests recently modified session files every 10 minutes when running `cua serve`

### 📊 Interactive Dashboard (`cua serve`)

A multi-page local web dashboard served at `http://127.0.0.1:1974`:

#### Main Dashboard (`/`)
- Summary cards: projects, sessions, messages, tokens, limit events
- Charts: usage over time, limit events per day, sessions per day — click any bar to drill into that day
- Projects table with clickable rows to the project detail view
- Recent sessions list with clickable rows to session detail
- Limit events log with clickable rows to the session where the limit occurred
- Date range filters with quick-select pills: Today, Yesterday, Last 2/5 days, Last week/2 weeks, Last/This month

#### Project Detail (`/project`)
- Same date filters scoped to a single project
- Per-day usage charts and session list
- Limit events log for that project

#### Day View (`/day`)
- Hour-by-hour activity breakdown (user messages, assistant messages, tool calls, tokens, limit events)
- Click any hour bar or row to drill into 5-minute chunks for that hour
- Sessions list with project dropdown filter, session ID text filter, and sortable columns
- Limit events for that day
- Auto-refresh every 10 minutes with manual "Refresh now" button that triggers a live ingest
- Quick-nav pills: Today, Yesterday, 2 days ago

#### Session Detail (`/session`)
- Summary cards and metadata grid for the session
- Per-minute activity timeline chart (stacked bar: user / assistant / tool)
- Full event table: timestamp, role/type, tool name, character count, tokens
- Limit events table with classification, pattern, confidence, and excerpt
- Red banner and auto-scroll when limit events are present

#### Sessions Browser (`/sessions`)
- All sessions across all projects with server-side pagination
- Sort by any column (started, duration, user msgs, tools, tokens, limits)
- Text search filter (project path or session ID)
- Date range filters
- View modes: List (paginated) | 15-min chunks | 1-hour chunks

### 📈 Navigation & Drill-Throughs
- Every chart bar navigates to the corresponding day/hour view
- Session rows link to the session detail page
- Limit event rows link to the session detail, scrolled to the limit
- All navigation preserves date filters and project context via URL parameters
- Back links return you to the exact page you came from

### 📤 Multi-Format Reports
- **Static HTML Report** — self-contained with embedded CSS, JS, and JSON data
- **CSV Export** — daily usage, sessions, limit events for analysis in spreadsheets
- **JSON Export** — structured data for programmatic access

### 🎯 Period Comparison
Compare two date ranges to reveal restriction patterns:
- Message/day growth (+215% in 30 days observed)
- Tool call trends (+120% in 30 days)
- Limit event frequency (+34.4% per day)
- Median time-to-first-limit degradation (7,133 min → 0.5 min)

### 🚀 All-Projects Scanner
Automatically walk and analyse all Claude Code projects at once.

## Installation

### Requirements
- Go 1.21 or later
- macOS, Linux, or WSL

### Build from Source

```bash
git clone https://github.com/seamus-waldron/ccan.git
cd ccan
go build -o cua ./cmd/cua
```

Then use `./cua` (or `sudo mv cua /usr/local/bin/cua` to install system-wide).

## Quick Start

### 1. Initialize
```bash
cua init
```
Creates `~/.claude-usage-analyser/` with SQLite database and config.

### 2. Analyse All Projects
```bash
cua scan-all
```
Scans `~/.claude/projects/` and builds the database.

### 3. View Dashboard
```bash
cua serve
```
Opens interactive dashboard at `http://127.0.0.1:1974`. The server auto-ingests new session data every 10 minutes and on every manual refresh — no need to re-run `scan-all`.

### 4. Generate Report
```bash
cua export --format html --output ./my-report
```
Creates a standalone HTML report with charts and tables.

## Commands

### `cua init`
Initialize the database and config directory.

```bash
cua init
```

### `cua analyse-project <dir>`
Parse a single Claude Code project and update the database.

```bash
cua analyse-project ~/.claude/projects/my-project \
  --since 2026-03-01 \
  --until 2026-05-05 \
  --force-reparse
```

**Flags:**
- `--db` — SQLite database path (default: `~/.claude-usage-analyser/usage.sqlite`)
- `--since` — filter sessions from date (YYYY-MM-DD)
- `--until` — filter sessions until date (YYYY-MM-DD)
- `--force-reparse` — ignore existing DB records, re-parse all files
- `--redact` — replace home directory with `~` in reports

### `cua scan-all`
Scan and analyse all projects in `~/.claude/projects/`.

```bash
cua scan-all [--force-reparse]
```

### `cua serve`
Start an interactive dashboard server.

```bash
cua serve [--port 1974]
```

Automatically kills any existing `cua serve` process on the same port and rebinds.

Access at: `http://127.0.0.1:1974`

**Dashboard pages:**
- `/` — Main dashboard with date filters and charts
- `/project?id=<n>` — Project detail view
- `/day?date=YYYY-MM-DD` — Day view with hourly breakdown
- `/day?date=YYYY-MM-DD&hour=H` — Hour drill-through with 5-min chunks
- `/session?id=<n>` — Session detail with full event timeline
- `/sessions` — Full sessions browser with pagination

### `cua compare`
Compare usage between two date ranges to detect restrictions.

```bash
cua compare \
  --baseline 2026-03-10:2026-04-06 \
  --current 2026-04-15:2026-05-04
```

Outputs:
- Session and message growth
- Tool call trends
- Token consumption changes
- Median time-to-first-limit (key indicator)
- Restriction score (heuristic combining multiple factors)

### `cua export`
Export data in HTML, JSON, or CSV format.

```bash
cua export --format html --output ./evidence-pack
cua export --format csv --output ./data
```

**Formats:**
- `html` — Standalone report with embedded charts
- `csv` — Three files: `daily_usage.csv`, `sessions.csv`, `limit_events.csv`
- `json` — Structured JSON blobs for programmatic access

## Database Schema

SQLite database at `~/.claude-usage-analyser/usage.sqlite`:

### projects
```
id (PK), encoded_path, decoded_path_guess, created_at, updated_at
```

### sessions
```
id (PK), project_id (FK), session_id, source_file,
started_at, ended_at, duration_seconds,
user_message_count, assistant_message_count, system_message_count,
tool_call_count, tool_result_count,
known_total_tokens, estimated_total_tokens,
limit_event_count, first_limit_event_at, ended_after_limit_event,
parse_error_count, created_at, updated_at
```

### events
```
id (PK), project_id (FK), session_db_id (FK), session_id, source_file, line_number,
timestamp, event_type, role, message_type, tool_name,
char_count, estimated_tokens, known_input_tokens, known_output_tokens, known_total_tokens,
created_at
```

### limit_events
```
id (PK), project_id (FK), session_db_id (FK), session_id, source_file, line_number,
timestamp, classification, matched_pattern, confidence, redacted_excerpt,
created_at
```
Classifications: `rate_limit`, `hard_limit`, `quota_limit`, `temporary_capacity`

### daily_usage
```
id (PK), date (UNIQUE), session_count, project_count,
user_message_count, assistant_message_count, tool_call_count,
known_tokens, estimated_tokens, active_seconds,
limit_event_count, first_activity_at, last_activity_at
```

## Configuration

Config file at `~/.claude-usage-analyser/config.yml`:

```yaml
claude_dir: ~/.claude
projects_dir: ~/.claude/projects
database_path: ~/.claude-usage-analyser/usage.sqlite
reports_dir: ~/.claude-usage-analyser/reports

redact: false              # Replace home dir with ~ in reports
store_content: false       # Store message content (privacy-first default)
chars_per_token: 4         # Estimation multiplier for tokens

limits:
  patterns:
    - pattern: "rate limit"
      classification: rate_limit
      confidence: 1.0
    - pattern: "usage limit"
      classification: hard_limit
      confidence: 1.0
```

## How It Works

### JSONL Parsing
1. Opens Claude Code session JSONL files line-by-line with 10MB buffer
2. Extracts messages, tool calls, token usage, and metadata
3. Detects limit events via pattern matching and HTTP error codes
4. Deduplicates tokens (API responses split across multiple lines)
5. Force-reparses files modified in the last 2 hours to catch in-progress sessions

### Token Counting
- **Known tokens**: from `message.usage.input_tokens` + `output_tokens`
- **Estimated tokens**: when exact tokens unavailable, uses character count ÷ 4
- Both totals kept separate in reports

### Limit Detection
Patterns detected from:
- Assistant message text (case-insensitive regex)
- System error messages (status codes, error types)
- Matched confidence ranges 0.0–1.0

Default patterns (in `config/limit_patterns.yml`):
- `rate limit`, `too many requests`, `429` → `rate_limit`
- `usage limit` → `hard_limit`
- `capacity`, `overloaded` → `temporary_capacity`
- `quota exceeded` → `quota_limit`

## Real-World Example

After 50 days of Claude Code usage across 81 projects:

```
Total: 19,054 sessions, 128,492 messages, 47,594 tool calls
Known tokens: 26.8M | Estimated: 103.4M
Limit events: 256 (breakdown: 111 rate limit, 78 hard limit, 65 capacity, 2 quota)

Restriction Pattern (Mar 10 – May 4):
  Messages/day:           +215.2%  (1,339 → 4,219)
  Tool calls/day:         +120.8%  (610 → 1,346)
  Tokens/day:             +102.4%  (1.85M → 3.75M)
  Limit events/day:       +34.4%   (4.4 → 6.0)
  Median time-to-limit:   -99.9%   (7,133 min → 0.5 min)  ⚠️ KEY SIGNAL

Conclusion: Despite 2.5x more usage, limits are hit 14,000x faster,
indicating tighter per-session throttling.
```

## Privacy

- **Local-first**: All processing runs on your machine
- **No external calls**: No data sent to Anthropic, Anthropic servers, or any external service
- **Optional redaction**: `--redact` flag replaces home directory with `~`
- **Optional content storage**: `store_content: false` by default (content not retained)
- **Token counting caveats**: Estimates use char count ÷ 4; exact billing requires Anthropic API

## Contributing

Contributions welcome! Areas for enhancement:

- [ ] Webhook integration for real-time session updates
- [ ] SQLite → PostgreSQL migration path
- [ ] Time-series forecasting (predict when next limit will trigger)
- [ ] Email alerts on high limit event frequency
- [ ] Historical comparison (month-over-month trends)
- [ ] Windows support
- [ ] Model breakdown (usage per Claude model version)

## License

MIT

## Disclaimer

This tool is for local analytics of your Claude Code usage patterns only. Token counts marked as "estimated" are approximations (characters ÷ 4) and should not be treated as exact billing figures. Consult Anthropic's billing dashboard or API for authoritative token usage.

Limit event detection uses configurable text patterns and HTTP error codes and may include false positives or false negatives. Use this tool as a monitoring aid, not a substitute for official Anthropic quota and rate limit documentation.

## Author

Built by Seamus Waldron for monitoring Claude Code usage and identifying restriction patterns.
