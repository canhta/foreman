package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// mockLlmProvider is a test double for llm.LlmProvider.
type mockLlmProvider struct {
	response *models.LlmResponse
	err      error
}

func (m *mockLlmProvider) Complete(_ context.Context, _ models.LlmRequest) (*models.LlmResponse, error) {
	return m.response, m.err
}
func (m *mockLlmProvider) ProviderName() string                { return "mock" }
func (m *mockLlmProvider) HealthCheck(_ context.Context) error { return nil }

var _ llm.LlmProvider = (*mockLlmProvider)(nil)

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

// TestSummarizeHistory_Success verifies that a successful LLM response is returned
// as a user message containing the summary content.
func TestSummarizeHistory_Success(t *testing.T) {
	summaryText := "## Goal\nDo something\n## Accomplished\n- step 1\n## Remaining\n- nothing\n## Relevant Files\nfoo.go"
	provider := &mockLlmProvider{
		response: &models.LlmResponse{
			Content:    summaryText,
			StopReason: models.StopReasonEndTurn,
		},
	}

	msgs := generateLargeConversation(5)
	msg := SummarizeHistory(context.Background(), provider, "test-model", msgs)

	if msg.Role != "user" {
		t.Errorf("expected Role 'user', got %q", msg.Role)
	}
	if !strings.Contains(msg.Content, summaryText) {
		t.Errorf("expected content to contain summary, got: %q", msg.Content)
	}
}

// TestSummarizeHistory_LLMFailure verifies that when the LLM errors, a fallback
// message is returned (not an error propagated to the caller).
func TestSummarizeHistory_LLMFailure(t *testing.T) {
	provider := &mockLlmProvider{
		err: fmt.Errorf("LLM unavailable"),
	}

	msgs := generateLargeConversation(5)
	msg := SummarizeHistory(context.Background(), provider, "test-model", msgs)

	if msg.Role != "user" {
		t.Errorf("expected Role 'user', got %q", msg.Role)
	}
	if msg.Content == "" {
		t.Error("expected non-empty fallback content")
	}
	// Fallback should mention summarization failure
	if !strings.Contains(msg.Content, "summarization failed") {
		t.Errorf("expected fallback message to mention 'summarization failed', got: %q", msg.Content)
	}
}

// TestSplitForSummarization_KeepsLastThreeTurns verifies that the split preserves
// exactly the last 3 turn-pairs in the recent slice.
func TestSplitForSummarization_KeepsLastThreeTurns(t *testing.T) {
	msgs := generateLargeConversation(7) // 7 turns → 1 initial + 7*(assistant+result) = 15 messages

	old, recent := splitForSummarization(msgs)

	// Count turn-pairs in recent (assistant with ToolCalls paired with a user ToolResults)
	recentTurns := 0
	for _, m := range recent {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			recentTurns++
		}
	}
	if recentTurns != 3 {
		t.Errorf("expected 3 turn-pairs in recent, got %d", recentTurns)
	}

	// The last turn in recent should be turn 6 (index 6, 0-based)
	var lastAssistant *models.Message
	for i := range recent {
		if recent[i].Role == "assistant" && len(recent[i].ToolCalls) > 0 {
			lastAssistant = &recent[i]
		}
	}
	if lastAssistant == nil || !strings.Contains(lastAssistant.Content, "Turn 6") {
		t.Errorf("expected last recent turn to be 'Turn 6', got: %v", lastAssistant)
	}

	// old should have the remaining turns (turns 0..3) plus the initial message
	// total old messages = 1 (initial) + 4 * 2 (turns 0-3) = 9
	if len(old) == 0 {
		t.Error("expected non-empty old messages")
	}
	// The initial task message should appear in old, not in recent
	for _, m := range recent {
		if m.Role == "user" && m.Content == "Initial task: do something" {
			t.Error("initial task message should be in old, not recent")
		}
	}
	_ = old
}
