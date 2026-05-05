# Claude Code Usage Restriction Analyser — Implementation Plan

## Project Overview

**Application name:** `claude-usage-analyser`  
**CLI command:** `cua`  
**Purpose:** Analyse local Claude Code session history to show whether usage has become more restricted over time.  
**Working directory:** `/Users/seamus_waldron/Documents/Dev/go/ccan`  
**Go module:** `github.com/seamus-waldron/ccan`

All processing is local. No session data leaves the machine.

---

## Real JSONL Schema (from live exploration)

Each line in a session `.jsonl` file has a `type` field. Key types:

| type | Role |
|---|---|
| `user` | User message (`message.role=user`) OR tool result (`toolUseResult` present) |
| `assistant` | Claude response; content is `[]ContentBlock`; may be split across lines sharing the same `message.id` |
| `system` | API errors (`subtype=api_error`, `error.status=429` for rate limits) |
| `attachment` | Hook output injected into context |
| `queue-operation` | Sub-agent task queue entries |
| `ai-title`, `custom-title`, `last-prompt`, `permission-mode`, `agent-name`, `pr-link` | Session metadata |
| `file-history-snapshot`, `progress` | File backup and hook progress records |

**Critical parser facts:**
- One API response is split across **multiple consecutive lines** sharing the same `message.id` — token usage must be deduplicated by `message.id`
- Tool calls are inside assistant `message.content[]` as `{type: "tool_use", name: ..., id: ...}` blocks
- Tool results are `type=user` entries with `toolUseResult` present (no `message` field)
- Timestamps are ISO 8601 UTC strings (`"2026-04-18T06:55:49.625Z"`)
- Limit events are **not** a distinct entry type — must be detected via pattern matching on text + system error codes

---

## Technology Stack

| Component | Choice | Reason |
|---|---|---|
| Language | Go 1.21+ | Fast filesystem scan, good stdlib |
| CLI | `github.com/spf13/cobra` | Standard Go CLI framework |
| Database | SQLite via `modernc.org/sqlite` | Pure Go, no CGO |
| Config | `gopkg.in/yaml.v3` | YAML config file |
| Charts | Chart.js via CDN | Simple, local HTML — no user data sent |
| Report | Static HTML (Go `embed`) | Works offline, no server required |

---

## Package Structure

```
ccan/
  cmd/cua/
    main.go                   # Cobra root + subcommands
  internal/
    config/
      config.go               # Config struct, loader, defaults
    db/
      db.go                   # Open, close, exec helpers
      migrations.go           # Schema CREATE TABLE, RunMigrations()
    parser/
      types.go                # RawEntry, ParseResult, ParseOptions structs
      jsonl.go                # ParseSessionFile() — line-by-line scanner
      limits.go               # LoadLimitPatterns(), DetectLimitEvent()
      tokens.go               # AccumulateTokens(), EstimateTokens()
      session.go              # BuildSessionSummary() from []ParsedEntry
    analysis/
      daily.go                # AggregateDailyUsage()
      compare.go              # ComparePeridods() for baseline vs current
      restriction.go          # RestrictionScore()
    report/
      html.go                 # GenerateHTMLReport()
      json.go                 # ExportSummaryJSON()
      csv.go                  # ExportCSV()
    server/
      server.go               # Local HTTP server for dashboard
    redact/
      redact.go               # RedactPath(), RedactText()
  web/
    template.html             # Go embed HTML template
    styles.css                # Report styles
    app.js                    # Chart.js chart initialisation
  config/
    limit_patterns.yml        # Limit phrase patterns + classifications
  testdata/
    session_normal.jsonl
    session_with_limit.jsonl
    session_rate_limit_system.jsonl
    malformed_lines.jsonl
  docs/
    implementation-plan.md    # This file
  go.mod
  go.sum
```

---

## Database Schema

### `schema_version`
```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT DEFAULT CURRENT_TIMESTAMP
);
```

### `projects`
```sql
CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    encoded_path TEXT NOT NULL UNIQUE,
    decoded_path_guess TEXT,
    first_seen_at TEXT,
    last_seen_at TEXT,
    session_count INTEGER DEFAULT 0,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);
```

### `sessions`
```sql
CREATE TABLE sessions (
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
    limit_event_count INTEGER DEFAULT 0,
    first_limit_event_at TEXT,
    ended_after_limit_event BOOLEAN DEFAULT FALSE,
    parse_error_count INTEGER DEFAULT 0,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id)
);
```

### `events`
```sql
CREATE TABLE events (
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
    char_count INTEGER DEFAULT 0,
    estimated_tokens INTEGER DEFAULT 0,
    known_input_tokens INTEGER DEFAULT 0,
    known_output_tokens INTEGER DEFAULT 0,
    known_total_tokens INTEGER DEFAULT 0,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id),
    FOREIGN KEY(session_db_id) REFERENCES sessions(id)
);
```

### `limit_events`
```sql
CREATE TABLE limit_events (
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
);
```

### `daily_usage`
```sql
CREATE TABLE daily_usage (
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
);
```

---

## Limit Pattern File (`config/limit_patterns.yml`)

```yaml
patterns:
  - pattern: "usage limit"
    classification: hard_limit
    confidence: 1.0
  - pattern: "rate limit"
    classification: rate_limit
    confidence: 1.0
  - pattern: "limit reached"
    classification: hard_limit
    confidence: 1.0
  - pattern: "message limit"
    classification: hard_limit
    confidence: 1.0
  - pattern: "quota exceeded"
    classification: quota_limit
    confidence: 1.0
  - pattern: "too many requests"
    classification: rate_limit
    confidence: 0.9
  - pattern: "try again later"
    classification: rate_limit
    confidence: 0.8
  - pattern: "try again in"
    classification: rate_limit
    confidence: 0.8
  - pattern: "come back later"
    classification: hard_limit
    confidence: 0.8
  - pattern: "you have reached your"
    classification: hard_limit
    confidence: 0.9
  - pattern: "maximum usage"
    classification: hard_limit
    confidence: 0.9
  - pattern: "overloaded"
    classification: temporary_capacity
    confidence: 0.6
  - pattern: "temporarily unavailable"
    classification: temporary_capacity
    confidence: 0.6
  - pattern: "capacity"
    classification: temporary_capacity
    confidence: 0.4

system_error_codes:
  - status: 429
    classification: rate_limit
    confidence: 1.0
  - error_type: "rate_limit_error"
    classification: rate_limit
    confidence: 1.0
  - error_type: "overloaded_error"
    classification: temporary_capacity
    confidence: 0.8
```

---

## CLI Commands

### Phase 1
```
cua init                         Create ~/.claude-usage-analyser/ with DB and config
cua analyse-project <dir>        Parse one project directory, write DB + HTML report
  --db        path to SQLite DB  (default: ~/.claude-usage-analyser/usage.sqlite)
  --output    report output dir  (default: ./reports/<project-name>)
  --since     YYYY-MM-DD         filter from date
  --until     YYYY-MM-DD         filter to date
  --redact                       redact paths and usernames
  --force-reparse                ignore existing DB records
```

### Phase 2
```
cua scan-all                     Walk ~/.claude/projects, parse all sessions
  --claude-dir   ~/.claude       base Claude dir
  --projects-dir ~/.claude/projects
  --db, --output, --since, --until, --redact, --force-reparse
```

### Phase 3
```
cua serve                        Start local dashboard at http://127.0.0.1:1974
  --port  1974
  --db    path
```

### Phase 4
```
cua compare                      Compare two date periods
  --baseline  YYYY-MM-DD:YYYY-MM-DD
  --current   YYYY-MM-DD:YYYY-MM-DD
  --db        path
```

### Phase 5
```
cua export                       Export evidence pack
  --format  html|json|csv
  --db      path
  --output  dir
  --redact
```

---

## Token Counting Rules

- Known tokens: use `message.usage.input_tokens` + `message.usage.output_tokens` from unique `message.id` entries
- Cache tokens (`cache_creation_input_tokens`, `cache_read_input_tokens`) are stored separately — informational only
- Estimated tokens: `char_count / 4` — only used when `message.usage` is absent
- **Never conflate known and estimated totals**
- Label estimates clearly in all reports as "Estimated (chars/4)"

---

## Phases

### Phase 1 — Foundation + Single Project Analysis
**Goal:** Working `cua init` and `cua analyse-project` commands.

Deliverables:
- `go.mod`, `go.sum`
- `config/limit_patterns.yml`
- `internal/config/config.go`
- `internal/db/db.go`, `internal/db/migrations.go`
- `internal/parser/types.go`
- `internal/parser/jsonl.go`
- `internal/parser/limits.go`
- `internal/parser/tokens.go`
- `internal/parser/session.go`
- `internal/analysis/daily.go`
- `internal/report/html.go`, `internal/report/json.go`
- `web/template.html`, `web/styles.css`, `web/app.js`
- `internal/redact/redact.go`
- `cmd/cua/main.go`
- `testdata/*.jsonl`

Acceptance test:
```bash
go build ./cmd/cua
./cua init
./cua analyse-project ~/.claude/projects/<any-dir> --output ./reports/test
open ./reports/test/index.html
# → summary cards + 3 charts populated with real data
```

### Phase 2 — All Projects Scanner
**Goal:** `cua scan-all` walks `~/.claude/projects`, processes every session.

Deliverables:
- `internal/discovery/discovery.go`
- `scan-all` command in `cmd/cua/main.go`
- Skip unchanged files (compare mtime vs DB record)
- Aggregate daily_usage across all projects

Acceptance test:
```bash
./cua scan-all
./cua analyse-project --output ./reports/all
# → all projects shown in report
```

### Phase 3 — Local Dashboard
**Goal:** `cua serve` starts a local HTTP server with a rich interactive dashboard.

Deliverables:
- `internal/server/server.go`
- API endpoints: `/api/summary`, `/api/daily`, `/api/projects`, `/api/sessions`, `/api/limit-events`
- Dashboard with date range picker, project filter, all charts from spec

Acceptance test:
```bash
./cua serve --port 1974
# → open http://localhost:1974, see live charts
```

### Phase 4 — Historical Restriction Analysis
**Goal:** `cua compare` computes restriction score between two date ranges.

Deliverables:
- `internal/analysis/compare.go`
- `internal/analysis/restriction.go`
- `compare` command in `cmd/cua/main.go`
- Restriction score formula
- Terminal output + JSON output

Acceptance test:
```bash
./cua compare --baseline 2025-09-01:2025-12-31 --current 2026-04-01:2026-05-04
# → table showing % change per metric, restriction score
```

### Phase 5 — Export + Evidence Pack
**Goal:** `cua export` generates a privacy-safe shareable report.

Deliverables:
- `internal/report/csv.go`
- `export` command in `cmd/cua/main.go`
- Written conclusion paragraph in HTML report
- Caveats section (estimated tokens, pattern-based detection)
- `--redact` hashes/removes paths, usernames, project names

Acceptance test:
```bash
./cua export --format html --redact --output ./evidence-pack
open ./evidence-pack/index.html
# → complete report with conclusion, no personal paths visible
```

---

## Verification Protocol (applied after each phase)

1. Diff implementation against this plan — flag MISSING, PARTIAL, or STUB items
2. Fix every issue found
3. Run `go vet ./...` — must produce no output
4. Run `go build ./cmd/cua` — must succeed
5. Run `go test ./...` — must pass
6. Run the acceptance test for that phase against real data

---

## Privacy Requirements

- No session content stored by default (`--no-content` is the default)
- `--redact` replaces home directory prefix with `~` in all path fields in reports
- Chart.js loaded from CDN (JS library only — no user data transmitted)
- No external API calls of any kind from the analysis pipeline
