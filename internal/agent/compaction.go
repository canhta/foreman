package agent

import (
	"context"
	"encoding/json"
	"strings"

	fmtctx "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// CompactMessages reduces the token footprint of a message history to fit within budgetTokens.
//
// Strategy:
//  1. If the full history fits within budget, return it unchanged.
//  2. Always preserve the first message (original user task) and the last 3 turns intact.
//  3. For older tool results in the middle, replace large outputs with a truncation notice.
//  4. If still over budget, drop middle assistant+tool pairs entirely, keeping the outer constraint.
//
// A "turn" is an assistant message with tool calls followed by a user message with tool results.
func CompactMessages(messages []models.Message, budgetTokens int) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	// Fast path: already within budget.
	if countAllTokens(messages) <= budgetTokens {
		return messages
	}

	// Identify turn boundaries: pairs of (assistant+toolcall, user+toolresult).
	// Everything else (pure user/assistant text) is preserved verbatim.
	type turnPair struct {
		assistantIdx int
		resultIdx    int
	}
	var turns []turnPair
	for i := 0; i < len(messages)-1; i++ {
		m := messages[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			next := messages[i+1]
			if next.Role == "user" && len(next.ToolResults) > 0 {
				turns = append(turns, turnPair{assistantIdx: i, resultIdx: i + 1})
			}
		}
	}

	// Preserve the last 3 turns intact; compact older ones.
	preserveFromTurn := len(turns) - 3
	if preserveFromTurn < 0 {
		preserveFromTurn = 0
	}

	// Build a set of indices that should be compacted (truncate tool result).
	compactSet := make(map[int]bool)
	for i := 0; i < preserveFromTurn; i++ {
		compactSet[turns[i].resultIdx] = true
	}

	result := make([]models.Message, len(messages))
	copy(result, messages)

	// Phase 1: truncate tool result content in compactable turns.
	const truncationNotice = "[output truncated for context window management]"
	for idx := range compactSet {
		msg := result[idx]
		newResults := make([]models.ToolResult, len(msg.ToolResults))
		for j, tr := range msg.ToolResults {
			if len(tr.Content) > 200 {
				newResults[j] = models.ToolResult{
					ToolCallID: tr.ToolCallID,
					Content:    truncationNotice,
					IsError:    tr.IsError,
				}
			} else {
				newResults[j] = tr
			}
		}
		result[idx] = models.Message{
			Role:        msg.Role,
			ToolResults: newResults,
		}
	}

	// Check again after truncation.
	if countAllTokens(result) <= budgetTokens {
		return result
	}

	// Phase 2: if still over budget, drop entire turn pairs from the oldest non-preserved turns.
	// Build a drop set of indices.
	dropSet := make(map[int]bool)
	for i := 0; i < preserveFromTurn; i++ {
		dropSet[turns[i].assistantIdx] = true
		dropSet[turns[i].resultIdx] = true
	}

	var filtered []models.Message
	for i, m := range result {
		if dropSet[i] {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// splitForSummarization splits messages into two groups:
//   - old: everything except the last 3 turn-pairs (and any trailing non-turn messages).
//     The original task message (messages[0]) is included here so the summarizer sees the goal.
//   - recent: the last 3 turn-pairs plus any trailing non-turn messages.
//
// If there are fewer than 3 turn-pairs total, recent contains all turn-pairs and old is empty.
func splitForSummarization(messages []models.Message) (old, recent []models.Message) {
	if len(messages) == 0 {
		return nil, nil
	}

	// Identify turn boundaries: (assistant+toolcall, user+toolresult) pairs.
	type turnPair struct {
		assistantIdx int
		resultIdx    int
	}
	var turns []turnPair
	for i := 0; i < len(messages)-1; i++ {
		m := messages[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			next := messages[i+1]
			if next.Role == "user" && len(next.ToolResults) > 0 {
				turns = append(turns, turnPair{assistantIdx: i, resultIdx: i + 1})
			}
		}
	}

	// If 3 or fewer turns, everything stays recent; nothing to summarize.
	if len(turns) <= 3 {
		return nil, messages
	}

	// The last 3 turns go to recent; everything older goes to old.
	keepFromTurn := len(turns) - 3

	// Build a set of indices belonging to recent turns.
	recentTurnIdx := make(map[int]bool)
	for i := keepFromTurn; i < len(turns); i++ {
		recentTurnIdx[turns[i].assistantIdx] = true
		recentTurnIdx[turns[i].resultIdx] = true
	}

	// Build old and recent slices.
	// messages[0] (original task) always goes into old so the summarizer sees the goal.
	for i, m := range messages {
		if recentTurnIdx[i] {
			recent = append(recent, m)
		} else {
			old = append(old, m)
		}
	}
	return old, recent
}

// SummarizeHistory calls the LLM to produce a structured summary of completed work.
// It renders messagesToSummarize as plain text and asks the model to produce output in:
//
//	## Goal
//	## Accomplished
//	## Remaining
//	## Relevant Files
//
// The returned message has Role "user" and is prefixed with "[context summary]".
// If the LLM call fails for any reason, a plain-text fallback is returned instead.
func SummarizeHistory(ctx context.Context, provider llm.LlmProvider, model string, messagesToSummarize []models.Message) models.Message {
	// Render messages as plain text for the summarization prompt.
	var sb strings.Builder
	sb.WriteString("<conversation history>\n")
	for _, m := range messagesToSummarize {
		switch {
		case m.Content != "":
			sb.WriteString("[")
			sb.WriteString(m.Role)
			sb.WriteString("]: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		case len(m.ToolCalls) > 0:
			sb.WriteString("[")
			sb.WriteString(m.Role)
			sb.WriteString(" tool calls]: ")
			for _, tc := range m.ToolCalls {
				sb.WriteString(tc.Name)
				raw, _ := json.Marshal(tc.Input)
				sb.WriteString("(")
				sb.Write(raw)
				sb.WriteString(") ")
			}
			sb.WriteString("\n")
		case len(m.ToolResults) > 0:
			sb.WriteString("[tool results]: ")
			for _, tr := range m.ToolResults {
				if len(tr.Content) > 300 {
					sb.WriteString(tr.Content[:300])
					sb.WriteString("...[truncated]")
				} else {
					sb.WriteString(tr.Content)
				}
				sb.WriteString(" ")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</conversation history>\n\n")
	sb.WriteString("Summarize the above conversation following this template:\n")
	sb.WriteString("## Goal\n## Accomplished\n## Remaining\n## Relevant Files (comma-separated, or \"none\" if not applicable)\n")

	req := models.LlmRequest{
		Model:             model,
		SystemPrompt:      "You are a precise technical summarizer. Return only the filled-in template. Do not add prose outside the section headers.",
		UserPrompt:        sb.String(),
		MaxTokens:         500,
		Temperature:       0.1,
		CacheSystemPrompt: true,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil || resp == nil || resp.Content == "" {
		// Fallback: plain text notice.
		return models.Message{
			Role:    "user",
			Content: "[context summary: LLM summarization failed — older turns dropped]\n" + compactionSummary(len(messagesToSummarize)/2),
		}
	}
	return models.Message{
		Role:    "user",
		Content: "[context summary]\n" + resp.Content,
	}
}

// countAllTokens sums token counts across all messages.
func countAllTokens(messages []models.Message) int {
	total := 0
	for _, m := range messages {
		total += countMessageTokens(m)
	}
	return total
}

// countMessageTokens estimates the token count of a single message.
func countMessageTokens(m models.Message) int {
	total := fmtctx.CountTokens(m.Content)
	for _, tc := range m.ToolCalls {
		total += fmtctx.CountTokens(tc.Name)
		raw, _ := json.Marshal(tc.Input)
		total += fmtctx.CountTokens(string(raw))
	}
	for _, tr := range m.ToolResults {
		total += fmtctx.CountTokens(tr.Content)
	}
	return total
}

// compactionSummary generates a brief summary to inject when context is compacted.
// Used for the agent progress notes injected by the builtin runner.
func compactionSummary(droppedTurns int) string {
	return strings.Join([]string{
		"[Context compacted: " + itoa(droppedTurns) + " older turn(s) dropped to fit context window.]",
		"[Earlier tool outputs have been truncated. Continue working toward the original goal.]",
	}, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i := len(digits) - 1; i >= 0; i-- {
		buf = append(buf, digits[i])
	}
	return string(buf)
}
