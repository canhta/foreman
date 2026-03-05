package agent

import (
	"encoding/json"
	"testing"
)

func TestAgentRequest_Defaults(t *testing.T) {
	req := AgentRequest{
		Prompt:  "test prompt",
		WorkDir: "/tmp/test",
	}

	if req.Prompt != "test prompt" {
		t.Fatalf("expected prompt 'test prompt', got %q", req.Prompt)
	}
	if req.MaxTurns != 0 {
		t.Fatalf("expected default MaxTurns 0, got %d", req.MaxTurns)
	}
	if req.TimeoutSecs != 0 {
		t.Fatalf("expected default TimeoutSecs 0, got %d", req.TimeoutSecs)
	}
	if req.OutputSchema != nil {
		t.Fatal("expected nil OutputSchema")
	}
}

func TestAgentResult_WithUsage(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	result := AgentResult{
		Output: `{"severity":"low"}`,
		Usage: AgentUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			CostUSD:      0.02,
			NumTurns:     3,
			DurationMs:   5000,
		},
	}

	if result.Output != `{"severity":"low"}` {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.CostUSD != 0.02 {
		t.Fatalf("expected cost 0.02, got %f", result.Usage.CostUSD)
	}
	if result.Usage.NumTurns != 3 {
		t.Fatalf("expected 3 turns, got %d", result.Usage.NumTurns)
	}
	_ = schema
}
