package parser

import (
	"encoding/json"
	"time"
)

// RawEntry is the flexible envelope for any JSONL line.
type RawEntry struct {
	Type           string          `json:"type"`
	UUID           string          `json:"uuid"`
	ParentUUID     string          `json:"parentUuid"`
	SessionID      string          `json:"sessionId"`
	Timestamp      string          `json:"timestamp"`
	IsSidechain    bool            `json:"isSidechain"`
	Entrypoint     string          `json:"entrypoint"`
	CWD            string          `json:"cwd"`
	Version        string          `json:"version"`
	GitBranch      string          `json:"gitBranch"`
	Message        *RawMessage     `json:"message"`
	ToolUseResult  json.RawMessage `json:"toolUseResult"`
	Subtype        string          `json:"subtype"`
	Level          string          `json:"level"`
	Error          json.RawMessage `json:"error"`
	// metadata-only fields
	AITitle        string          `json:"aiTitle"`
	CustomTitle    string          `json:"customTitle"`
	LastPrompt     string          `json:"lastPrompt"`
	PermissionMode string          `json:"permissionMode"`
	AgentName      string          `json:"agentName"`
}

type RawMessage struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	Content    json.RawMessage `json:"content"` // string or []ContentBlock
	StopReason string          `json:"stop_reason"`
	Usage      *TokenUsage     `json:"usage"`
}

type TokenUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
	// tool_use fields
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// APIError captures the error object in system entries.
type APIError struct {
	Status    int    `json:"status"`
	RequestID string `json:"requestID"`
	Error     struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ParsedEntry is the normalised form of one JSONL line.
type ParsedEntry struct {
	LineNumber    int
	Timestamp     time.Time
	HasTimestamp  bool
	SessionID     string
	Type          string   // original type
	Role          string   // user / assistant
	MessageID     string   // assistant message.id (for dedup)
	IsSidechain   bool
	Entrypoint    string
	// counts
	IsUserMessage      bool
	IsToolResult       bool
	IsAssistant        bool
	IsSystem           bool
	ToolCalls          []string // tool names
	ToolInputSummaries []string // parallel to ToolCalls — file path, command, URL, etc.
	CharCount          int
	// tokens (only set if message.usage was present)
	HasTokenUsage  bool
	InputTokens    int
	OutputTokens   int
	CacheCreate    int
	CacheRead      int
	// text content (for limit detection; not persisted by default)
	TextContent    string
	// system error
	APIErrorStatus int
	APIErrorType   string
	APIErrorMsg    string
	// limit detection result
	LimitDetected  bool
	LimitClassification string
	LimitPattern   string
	LimitConfidence float64
}

// ParseOptions controls parser behaviour.
type ParseOptions struct {
	StoreContent bool
	Redact       bool
	Since        time.Time
	Until        time.Time
	HasSince     bool
	HasUntil     bool
	LimitPatterns *LimitPatternFile
}

// ParseResult aggregates everything from one session file.
type ParseResult struct {
	SourceFile  string
	SessionID   string
	Entries     []ParsedEntry
	ParseErrors []ParseError
}

type ParseError struct {
	Line int
	Err  error
}
