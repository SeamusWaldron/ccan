package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const scannerBufSize = 10 * 1024 * 1024 // 10 MB

func ParseSessionFile(path string, opts ParseOptions) (*ParseResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := &ParseResult{SourceFile: path}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	// track seen message.id to deduplicate token usage
	seenMsgIDs := make(map[string]bool)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}

		var entry RawEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			result.ParseErrors = append(result.ParseErrors, ParseError{Line: lineNum, Err: err})
			continue
		}

		parsed := ParsedEntry{
			LineNumber:  lineNum,
			SessionID:   entry.SessionID,
			Type:        entry.Type,
			IsSidechain: entry.IsSidechain,
			Entrypoint:  entry.Entrypoint,
		}

		// parse timestamp
		if entry.Timestamp != "" {
			t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil {
				t, err = time.Parse(time.RFC3339, entry.Timestamp)
			}
			if err == nil {
				parsed.Timestamp = t
				parsed.HasTimestamp = true
			}
		}

		// session ID from first entry that has one
		if result.SessionID == "" && entry.SessionID != "" {
			result.SessionID = entry.SessionID
		}

		// apply date filters
		if parsed.HasTimestamp {
			if opts.HasSince && parsed.Timestamp.Before(opts.Since) {
				continue
			}
			if opts.HasUntil && parsed.Timestamp.After(opts.Until) {
				continue
			}
		}

		switch entry.Type {
		case "user":
			if err := parseUserEntry(&entry, &parsed, raw); err != nil {
				result.ParseErrors = append(result.ParseErrors, ParseError{Line: lineNum, Err: err})
			}
		case "assistant":
			parseAssistantEntry(&entry, &parsed, raw, seenMsgIDs)
		case "system":
			parseSystemEntry(&entry, &parsed, raw)
		}

		// limit detection
		if opts.LimitPatterns != nil {
			detectLimits(&parsed, opts.LimitPatterns, 0.4)
		}

		result.Entries = append(result.Entries, parsed)
	}

	if err := scanner.Err(); err != nil {
		result.ParseErrors = append(result.ParseErrors, ParseError{Line: lineNum, Err: fmt.Errorf("scanner: %w", err)})
	}

	return result, nil
}

func parseUserEntry(entry *RawEntry, parsed *ParsedEntry, raw []byte) error {
	if entry.Message != nil {
		parsed.IsUserMessage = true
		parsed.Role = "user"
		// measure content size without storing
		if len(entry.Message.Content) > 0 {
			var s string
			if err := json.Unmarshal(entry.Message.Content, &s); err == nil {
				parsed.CharCount = len(s)
				parsed.TextContent = s
			} else {
				parsed.CharCount = len(entry.Message.Content)
			}
		}
	} else if len(entry.ToolUseResult) > 0 {
		parsed.IsToolResult = true
		parsed.Role = "tool_result"
		parsed.CharCount = len(entry.ToolUseResult)
	}
	return nil
}

func parseAssistantEntry(entry *RawEntry, parsed *ParsedEntry, raw []byte, seenMsgIDs map[string]bool) {
	if entry.Message == nil {
		return
	}
	parsed.IsAssistant = true
	parsed.Role = "assistant"
	parsed.MessageID = entry.Message.ID

	// parse content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
		// content might be a plain string
		var s string
		if err2 := json.Unmarshal(entry.Message.Content, &s); err2 == nil {
			parsed.CharCount = len(s)
			parsed.TextContent = s
		}
	} else {
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parsed.CharCount += len(b.Text)
				parsed.TextContent += b.Text
			case "thinking":
				parsed.CharCount += len(b.Thinking)
			case "tool_use":
				parsed.ToolCalls = append(parsed.ToolCalls, b.Name)
				parsed.ToolInputSummaries = append(parsed.ToolInputSummaries, extractToolInputSummary(b.Name, b.Input))
				parsed.CharCount += len(b.Input)
			}
		}
	}

	// deduplicate token usage by message.id
	if entry.Message.Usage != nil && entry.Message.ID != "" && !seenMsgIDs[entry.Message.ID] {
		seenMsgIDs[entry.Message.ID] = true
		parsed.HasTokenUsage = true
		parsed.InputTokens = entry.Message.Usage.InputTokens
		parsed.OutputTokens = entry.Message.Usage.OutputTokens
		parsed.CacheCreate = entry.Message.Usage.CacheCreationInputTokens
		parsed.CacheRead = entry.Message.Usage.CacheReadInputTokens
	}
}

func parseSystemEntry(entry *RawEntry, parsed *ParsedEntry, raw []byte) {
	parsed.IsSystem = true
	if entry.Subtype != "api_error" || len(entry.Error) == 0 {
		return
	}
	var apiErr APIError
	if err := json.Unmarshal(entry.Error, &apiErr); err != nil {
		return
	}
	parsed.APIErrorStatus = apiErr.Status
	parsed.APIErrorType = apiErr.Error.Type
	parsed.APIErrorMsg = apiErr.Error.Message
	parsed.TextContent = strings.Join([]string{apiErr.Error.Type, apiErr.Error.Message}, " ")
}

// extractToolInputSummary returns a short human-readable summary of a tool's primary input.
func extractToolInputSummary(toolName string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	switch toolName {
	case "Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "Glob", "LS":
		var v struct {
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
			Notebook string `json:"notebook_path"`
		}
		if json.Unmarshal(input, &v) == nil {
			if v.FilePath != "" {
				return v.FilePath
			}
			if v.Notebook != "" {
				return v.Notebook
			}
			if v.Path != "" {
				return v.Path
			}
		}
	case "Bash":
		var v struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &v) == nil && v.Command != "" {
			if len(v.Command) > 120 {
				return v.Command[:120] + "…"
			}
			return v.Command
		}
	case "WebFetch":
		var v struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(input, &v) == nil {
			return v.URL
		}
	case "WebSearch":
		var v struct {
			Query string `json:"query"`
		}
		if json.Unmarshal(input, &v) == nil {
			return v.Query
		}
	case "Task", "dispatch_agent":
		var v struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(input, &v) == nil && v.Description != "" {
			if len(v.Description) > 100 {
				return v.Description[:100] + "…"
			}
			return v.Description
		}
	}
	return ""
}

func detectLimits(parsed *ParsedEntry, patterns *LimitPatternFile, threshold float64) {
	// text-based detection on assistant and system entries
	if (parsed.IsAssistant || parsed.IsSystem) && parsed.TextContent != "" {
		cls, pat, conf, ok := patterns.DetectInText(parsed.TextContent, threshold)
		if ok {
			parsed.LimitDetected = true
			parsed.LimitClassification = cls
			parsed.LimitPattern = pat
			parsed.LimitConfidence = conf
			return
		}
	}
	// system error code detection
	if parsed.IsSystem && (parsed.APIErrorStatus > 0 || parsed.APIErrorType != "") {
		cls, pat, conf, ok := patterns.DetectInSystemError(parsed.APIErrorStatus, parsed.APIErrorType, threshold)
		if ok {
			parsed.LimitDetected = true
			parsed.LimitClassification = cls
			parsed.LimitPattern = pat
			parsed.LimitConfidence = conf
		}
	}
}
