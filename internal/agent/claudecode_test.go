package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/runner"
)

type mockCmdRunner struct {
	stdout   string
	stderr   string
	exitCode int
	timedOut bool
}

func (m *mockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{
		Stdout:   m.stdout,
		Stderr:   m.stderr,
		ExitCode: m.exitCode,
		Duration: 2 * time.Second,
		TimedOut: m.timedOut,
	}, nil
}
func (m *mockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestClaudeCodeRunner_Run_Success(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "success",
		"result":         "Fixed the bug by adding nil check",
		"model":          "claude-sonnet-4-6-20250514",
		"total_cost_usd": 0.035, "num_turns": 3, "duration_ms": 4500, "is_error": false,
		"usage": map[string]interface{}{"input_tokens": 2000, "output_tokens": 800},
	}
	resultJSON, _ := json.Marshal(sdkResult)

	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{
		DefaultAllowedTools: []string{"Read", "Edit", "Glob"},
		MaxTurnsDefault:     10,
		TimeoutSecsDefault:  120,
	})

	result, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix the bug", WorkDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Fixed the bug by adding nil check" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.CostUSD != 0.035 {
		t.Fatalf("expected cost 0.035, got %f", result.Usage.CostUSD)
	}
	if result.Usage.NumTurns != 3 {
		t.Fatalf("expected 3 turns, got %d", result.Usage.NumTurns)
	}
	if result.Usage.InputTokens != 2000 {
		t.Fatalf("expected 2000 input tokens, got %d", result.Usage.InputTokens)
	}
	if result.Usage.Model != "claude-sonnet-4-6-20250514" {
		t.Fatalf("expected model 'claude-sonnet-4-6-20250514', got %q", result.Usage.Model)
	}
}

func TestClaudeCodeRunner_Run_ModelFromSDK(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "success",
		"result":         "done",
		"model":          "claude-opus-4-6-20250610",
		"total_cost_usd": 0.10, "num_turns": 1, "duration_ms": 1000, "is_error": false,
		"usage": map[string]interface{}{"input_tokens": 500, "output_tokens": 200},
	}
	resultJSON, _ := json.Marshal(sdkResult)

	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{
		Model:          "claude-sonnet-4-6-20250514",
		TimeoutSecsDefault: 120,
	})

	result, err := r.Run(context.Background(), AgentRequest{Prompt: "Do it", WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use model from SDK output, not from config
	if result.Usage.Model != "claude-opus-4-6-20250610" {
		t.Fatalf("expected model from SDK output 'claude-opus-4-6-20250610', got %q", result.Usage.Model)
	}
}

func TestClaudeCodeRunner_Run_NoModelFallsBackToConfig(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "success",
		"result":         "done",
		"total_cost_usd": 0.01, "num_turns": 1, "duration_ms": 500, "is_error": false,
		"usage": map[string]interface{}{"input_tokens": 100, "output_tokens": 50},
	}
	resultJSON, _ := json.Marshal(sdkResult)

	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{
		Model:              "claude-sonnet-4-6-20250514",
		TimeoutSecsDefault: 120,
	})

	result, err := r.Run(context.Background(), AgentRequest{Prompt: "Do it", WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When SDK doesn't report model, fall back to configured model
	if result.Usage.Model != "claude-sonnet-4-6-20250514" {
		t.Fatalf("expected fallback model 'claude-sonnet-4-6-20250514', got %q", result.Usage.Model)
	}
}

func TestClaudeCodeRunner_Run_Timeout(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{timedOut: true}, ClaudeCodeConfig{TimeoutSecsDefault: 120})
	_, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix", WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClaudeCodeRunner_Run_NonZeroExit(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{exitCode: 1, stderr: "auth failed"}, ClaudeCodeConfig{TimeoutSecsDefault: 120})
	_, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix", WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestClaudeCodeRunner_Run_ErrorResult(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "error_max_turns", "is_error": true,
		"total_cost_usd": 0.05, "num_turns": 10, "duration_ms": 30000,
		"usage":  map[string]interface{}{"input_tokens": 5000, "output_tokens": 2000},
		"errors": []string{"max turns reached"},
	}
	resultJSON, _ := json.Marshal(sdkResult)
	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{TimeoutSecsDefault: 120})
	_, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix", WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("expected error for error result")
	}
}

func TestClaudeCodeRunner_Run_WithStructuredOutput(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "success", "is_error": false,
		"result": `{"severity":"high"}`, "total_cost_usd": 0.01,
		"num_turns": 1, "duration_ms": 1000,
		"usage":             map[string]interface{}{"input_tokens": 100, "output_tokens": 50},
		"structured_output": map[string]string{"severity": "high"},
	}
	resultJSON, _ := json.Marshal(sdkResult)
	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{TimeoutSecsDefault: 120})

	result, err := r.Run(context.Background(), AgentRequest{
		Prompt:       "Analyze",
		WorkDir:      "/tmp",
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Structured == nil {
		t.Fatal("expected structured output")
	}
}

func TestClaudeCodeRunner_HealthCheck(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: "1.0.0", exitCode: 0}, ClaudeCodeConfig{})
	if err := r.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClaudeCodeRunner_HealthCheck_NotFound(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{exitCode: 1, stderr: "not found"}, ClaudeCodeConfig{})
	if err := r.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestClaudeCodeRunner_RunnerName(t *testing.T) {
	r := NewClaudeCodeRunner(nil, ClaudeCodeConfig{})
	if r.RunnerName() != "claudecode" {
		t.Fatalf("expected 'claudecode', got %q", r.RunnerName())
	}
}

func TestParseSDKResultMessage_MultilineOutput(t *testing.T) {
	// Simulate stream-json output where result is last line
	output := `{"type":"system","subtype":"init"}
{"type":"assistant","message":"thinking..."}
{"type":"result","subtype":"success","result":"done","is_error":false,"total_cost_usd":0.01,"num_turns":1,"duration_ms":500,"usage":{"input_tokens":50,"output_tokens":20}}`

	result, err := parseSDKResultMessage(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}
