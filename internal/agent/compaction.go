package agent

import (
	"encoding/json"
	"strings"

	fmtctx "github.com/canhta/foreman/internal/context"
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
