package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

// generateLargeConversation creates N turns of user+assistant+tool_use+tool_result messages.
func generateLargeConversation(turns int) []models.Message {
	msgs := []models.Message{
		{Role: "user", Content: "Initial task: do something"},
	}
	for i := 0; i < turns; i++ {
		// assistant calls a tool
		toolCallID := fmt.Sprintf("call_%d", i)
		msgs = append(msgs, models.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Turn %d: I will read a file.", i),
			ToolCalls: []models.ToolCall{
				{
					ID:    toolCallID,
					Name:  "Read",
					Input: json.RawMessage(fmt.Sprintf(`{"path":"file_%d.go"}`, i)),
				},
			},
		})
		// tool result with large output
		largeOutput := strings.Repeat(fmt.Sprintf("line %d content here\n", i), 100)
		msgs = append(msgs, models.Message{
			Role: "user",
			ToolResults: []models.ToolResult{
				{ToolCallID: toolCallID, Content: largeOutput},
			},
		})
	}
	return msgs
}

func countAllTokensInMessages(msgs []models.Message) int {
	total := 0
	for _, m := range msgs {
		total += countMessageTokens(m)
	}
	return total
}

func TestCompactMessages_TruncatesOldToolOutputs(t *testing.T) {
	msgs := generateLargeConversation(20) // 20 turns with large tool outputs

	// Pick a budget smaller than the full conversation but enough to hold a few turns
	budget := 10000
	compacted := CompactMessages(msgs, budget)

	totalTokens := countAllTokensInMessages(compacted)
	if totalTokens >= budget {
		t.Errorf("expected compacted messages to fit within budget %d, got %d tokens", budget, totalTokens)
	}
}

func TestCompactMessages_PreservesLastThreeTurns(t *testing.T) {
	msgs := generateLargeConversation(10)
	budget := 5000

	compacted := CompactMessages(msgs, budget)

	// Count turns (assistant + tool_result pairs) in compacted messages
	// The last 3 assistant messages with their tool results should be intact
	var assistantMsgs []models.Message
	for _, m := range compacted {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assistantMsgs = append(assistantMsgs, m)
		}
	}

	if len(assistantMsgs) < 3 {
		t.Errorf("expected at least 3 assistant turns preserved, got %d", len(assistantMsgs))
	}

	// Verify the last turn is the 10th turn (index 9)
	lastAssistant := assistantMsgs[len(assistantMsgs)-1]
	if !strings.Contains(lastAssistant.Content, "Turn 9") {
		t.Errorf("expected last preserved turn to be Turn 9, got: %q", lastAssistant.Content)
	}
}

func TestCompactMessages_DoesNotCompactIfUnderBudget(t *testing.T) {
	msgs := generateLargeConversation(2) // small conversation
	budget := 200000                     // very large budget

	compacted := CompactMessages(msgs, budget)

	// Should return messages unchanged
	if len(compacted) != len(msgs) {
		t.Errorf("expected %d messages (unchanged), got %d", len(msgs), len(compacted))
	}
}

func TestCompactMessages_PreservesInitialUserMessage(t *testing.T) {
	msgs := generateLargeConversation(15)
	budget := 5000

	compacted := CompactMessages(msgs, budget)

	if len(compacted) == 0 {
		t.Fatal("expected non-empty compacted messages")
	}
	// First message should always be the original user message
	if compacted[0].Role != "user" || compacted[0].Content != "Initial task: do something" {
		t.Errorf("expected first message to be original user message, got role=%q content=%q",
			compacted[0].Role, compacted[0].Content)
	}
}

func TestCompactMessages_EmptyInput(t *testing.T) {
	compacted := CompactMessages(nil, 50000)
	if len(compacted) != 0 {
		t.Errorf("expected empty result for nil input, got %d messages", len(compacted))
	}
}
