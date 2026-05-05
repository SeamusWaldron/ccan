package parser

import (
	"time"

	"github.com/seamus-waldron/ccan/internal/db"
)

// BuildSessionRow converts a ParseResult into a DB session row.
func BuildSessionRow(r *ParseResult, projectID int64, charsPerTok int) *db.SessionRow {
	row := &db.SessionRow{
		ProjectID:      projectID,
		SessionID:      r.SessionID,
		SourceFile:     r.SourceFile,
		ParseErrorCount: len(r.ParseErrors),
	}

	var firstTs, lastTs time.Time
	var firstLimitTs time.Time
	var hasFirst, hasLast bool
	var messagesBeforeLimit, messagesAfterLimit int
	var seenMsgIDs = make(map[string]bool)

	for _, e := range r.Entries {
		if e.HasTimestamp {
			if !hasFirst || e.Timestamp.Before(firstTs) {
				firstTs = e.Timestamp
				hasFirst = true
			}
			if !hasLast || e.Timestamp.After(lastTs) {
				lastTs = e.Timestamp
				hasLast = true
			}
		}

		if e.IsUserMessage {
			row.UserMessageCount++
		}
		if e.IsToolResult {
			row.ToolResultCount++
		}
		if e.IsSystem {
			row.SystemMessageCount++
		}
		if e.IsAssistant {
			// count unique assistant turns by message.id
			if e.MessageID != "" {
				if !seenMsgIDs[e.MessageID] {
					seenMsgIDs[e.MessageID] = true
					row.AssistantMessageCount++
				}
			} else {
				row.AssistantMessageCount++
			}
			row.ToolCallCount += len(e.ToolCalls)
		}

		// token accumulation (deduplicated in jsonl.go via HasTokenUsage)
		if e.HasTokenUsage {
			row.KnownInputTokens += int64(e.InputTokens)
			row.KnownOutputTokens += int64(e.OutputTokens)
		} else if e.CharCount > 0 {
			row.EstimatedInputTokens += int64(EstimateTokens(e.CharCount, charsPerTok))
		}

		// limit events
		if e.LimitDetected {
			if row.LimitEventCount == 0 && e.HasTimestamp {
				firstLimitTs = e.Timestamp
				row.FirstLimitEventAt = e.Timestamp.UTC().Format(time.RFC3339)
			}
			row.LimitEventCount++
		}

		// track messages before/after first limit
		if row.LimitEventCount == 0 {
			if e.IsUserMessage || e.IsAssistant {
				messagesBeforeLimit++
			}
		} else {
			if e.IsUserMessage || e.IsAssistant {
				messagesAfterLimit++
			}
		}
	}

	row.KnownTotalTokens = row.KnownInputTokens + row.KnownOutputTokens
	row.EstimatedTotalTokens = row.EstimatedInputTokens + row.EstimatedOutputTokens

	if hasFirst {
		row.StartedAt = firstTs.UTC().Format(time.RFC3339)
	}
	if hasLast {
		row.EndedAt = lastTs.UTC().Format(time.RFC3339)
	}
	if hasFirst && hasLast {
		row.DurationSeconds = int64(lastTs.Sub(firstTs).Seconds())
	}

	// ended_after_limit: first limit within 30 min of session end AND few messages after
	if row.LimitEventCount > 0 && hasLast && !firstLimitTs.IsZero() {
		sinceLimit := lastTs.Sub(firstLimitTs)
		postPct := float64(0)
		total := messagesBeforeLimit + messagesAfterLimit
		if total > 0 {
			postPct = float64(messagesAfterLimit) / float64(total)
		}
		if sinceLimit <= 30*time.Minute && postPct < 0.2 {
			row.EndedAfterLimitEvent = true
		}
	}

	return row
}

// BuildEventRows converts ParsedEntries into DB event rows.
func BuildEventRows(r *ParseResult, projectID, sessionDBID int64, charsPerTok int) []*db.EventRow {
	var rows []*db.EventRow
	seenMsgIDs := make(map[string]bool)

	for _, e := range r.Entries {
		ts := ""
		if e.HasTimestamp {
			ts = e.Timestamp.UTC().Format(time.RFC3339)
		}

		msgType := ""
		if e.IsUserMessage {
			msgType = "user_message"
		} else if e.IsToolResult {
			msgType = "tool_result"
		} else if e.IsAssistant {
			msgType = "assistant_message"
		} else if e.IsSystem {
			msgType = "system"
		} else {
			msgType = e.Type
		}

		toolName := ""
		if len(e.ToolCalls) > 0 {
			toolName = e.ToolCalls[0]
		}

		var knownIn, knownOut, knownTotal int64
		if e.HasTokenUsage && !seenMsgIDs[e.MessageID] {
			if e.MessageID != "" {
				seenMsgIDs[e.MessageID] = true
			}
			knownIn = int64(e.InputTokens)
			knownOut = int64(e.OutputTokens)
			knownTotal = knownIn + knownOut
		}

		est := 0
		if !e.HasTokenUsage && e.CharCount > 0 {
			est = EstimateTokens(e.CharCount, charsPerTok)
		}

		rows = append(rows, &db.EventRow{
			ProjectID:         projectID,
			SessionDBID:       sessionDBID,
			SessionID:         e.SessionID,
			SourceFile:        r.SourceFile,
			LineNumber:        e.LineNumber,
			Timestamp:         ts,
			EventType:         e.Type,
			Role:              e.Role,
			MessageType:       msgType,
			ToolName:          toolName,
			CharCount:         e.CharCount,
			EstimatedTokens:   est,
			KnownInputTokens:  knownIn,
			KnownOutputTokens: knownOut,
			KnownTotalTokens:  knownTotal,
		})

		// one row per tool call for multi-tool entries
		for i := 1; i < len(e.ToolCalls); i++ {
			extra := *rows[len(rows)-1]
			extra.ToolName = e.ToolCalls[i]
			rows = append(rows, &extra)
		}
	}
	return rows
}

// BuildLimitEventRows extracts limit events from ParsedEntries.
func BuildLimitEventRows(r *ParseResult, projectID, sessionDBID int64) []*db.LimitEventRow {
	var rows []*db.LimitEventRow
	for _, e := range r.Entries {
		if !e.LimitDetected {
			continue
		}
		ts := ""
		if e.HasTimestamp {
			ts = e.Timestamp.UTC().Format(time.RFC3339)
		}
		excerpt := ""
		if len(e.TextContent) > 200 {
			excerpt = e.TextContent[:200] + "…"
		} else {
			excerpt = e.TextContent
		}
		rows = append(rows, &db.LimitEventRow{
			ProjectID:       projectID,
			SessionDBID:     sessionDBID,
			SessionID:       e.SessionID,
			SourceFile:      r.SourceFile,
			LineNumber:      e.LineNumber,
			Timestamp:       ts,
			Classification:  e.LimitClassification,
			MatchedPattern:  e.LimitPattern,
			Confidence:      e.LimitConfidence,
			RedactedExcerpt: excerpt,
		})
	}
	return rows
}
