package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// mockSingleShotLLM returns a text response immediately.
type mockSingleShotLLM struct {
	response string
}

func (m *mockSingleShotLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{
		Content:      m.response,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        req.Model,
		DurationMs:   200,
		StopReason:   models.StopReasonEndTurn,
	}, nil
}
func (m *mockSingleShotLLM) ProviderName() string                { return "mock" }
func (m *mockSingleShotLLM) HealthCheck(_ context.Context) error { return nil }

// mockToolUseLLM simulates a multi-turn tool-use conversation.
// First call returns tool_use, second call returns end_turn.
type mockToolUseLLM struct {
	calls int
}

func (m *mockToolUseLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.calls == 1 {
		return &models.LlmResponse{
			StopReason:   models.StopReasonToolUse,
			TokensInput:  500,
			TokensOutput: 100,
			Model:        req.Model,
			DurationMs:   500,
			ToolCalls: []models.ToolCall{
				{ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)},
			},
		}, nil
	}
	return &models.LlmResponse{
		Content:      "The file contains a Go program with a main function.",
		StopReason:   models.StopReasonEndTurn,
		TokensInput:  800,
		TokensOutput: 50,
		Model:        req.Model,
		DurationMs:   300,
	}, nil
}
func (m *mockToolUseLLM) ProviderName() string                { return "mock" }
func (m *mockToolUseLLM) HealthCheck(_ context.Context) error { return nil }

// alwaysToolUseLLM always requests tools, never returns end_turn.
type alwaysToolUseLLM struct{}

func (m *alwaysToolUseLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{
		StopReason:   models.StopReasonToolUse,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        req.Model,
		ToolCalls: []models.ToolCall{
			{ID: "c1", Name: "Read", Input: json.RawMessage(`{"path":"x.go"}`)},
		},
	}, nil
}
func (m *alwaysToolUseLLM) ProviderName() string                { return "mock" }
func (m *alwaysToolUseLLM) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_SingleShot(t *testing.T) {
	runner := NewBuiltinRunner(
		&mockSingleShotLLM{response: "simple answer"},
		"test-model",
		BuiltinConfig{MaxTurnsDefault: 10},
		nil, nil,
	)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "What is 2+2?",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "simple answer" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.NumTurns != 1 {
		t.Fatalf("expected 1 turn, got %d", result.Usage.NumTurns)
	}
}

func TestBuiltinRunner_MultiTurnToolUse(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644)

	mockLLM := &mockToolUseLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{
		MaxTurnsDefault:     10,
		DefaultAllowedTools: []string{"Read", "Glob", "Grep"},
	}, nil, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "What is in main.go?",
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "The file contains a Go program with a main function." {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if mockLLM.calls != 2 {
		t.Fatalf("expected 2 LLM calls (tool_use + end_turn), got %d", mockLLM.calls)
	}
	if result.Usage.NumTurns != 2 {
		t.Fatalf("expected 2 turns, got %d", result.Usage.NumTurns)
	}
	if result.Usage.InputTokens != 1300 {
		t.Fatalf("expected 1300 accumulated input tokens, got %d", result.Usage.InputTokens)
	}
}

func TestBuiltinRunner_MaxTurnsExceeded(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x"), 0644)

	runner := NewBuiltinRunner(&alwaysToolUseLLM{}, "test-model", BuiltinConfig{
		MaxTurnsDefault:     3,
		DefaultAllowedTools: []string{"Read"},
	}, nil, nil)

	_, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "Read everything",
		WorkDir: dir,
	})
	if err == nil {
		t.Fatal("expected max turns error")
	}
}

func TestBuiltinRunner_UnknownTool(t *testing.T) {
	// LLM requests a tool that doesn't exist
	unknownToolLLM := &mockToolUseLLMWithUnknown{}
	runner := NewBuiltinRunner(unknownToolLLM, "test-model", BuiltinConfig{
		MaxTurnsDefault:     10,
		DefaultAllowedTools: []string{"Read"},
	}, nil, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "Do something",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still complete after the error result is fed back
	if result.Output != "I see the tool failed." {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

type mockToolUseLLMWithUnknown struct {
	calls int
}

func (m *mockToolUseLLMWithUnknown) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.calls == 1 {
		return &models.LlmResponse{
			StopReason: models.StopReasonToolUse,
			ToolCalls: []models.ToolCall{
				{ID: "c1", Name: "UnknownTool", Input: json.RawMessage(`{}`)},
			},
			TokensInput: 100, TokensOutput: 50, Model: req.Model,
		}, nil
	}
	return &models.LlmResponse{
		Content: "I see the tool failed.", StopReason: models.StopReasonEndTurn,
		TokensInput: 100, TokensOutput: 50, Model: req.Model,
	}, nil
}
func (m *mockToolUseLLMWithUnknown) ProviderName() string                { return "mock" }
func (m *mockToolUseLLMWithUnknown) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_RunnerName(t *testing.T) {
	runner := NewBuiltinRunner(nil, "", BuiltinConfig{}, nil, nil)
	if runner.RunnerName() != "builtin" {
		t.Fatalf("expected 'builtin', got %q", runner.RunnerName())
	}
}

func TestBuiltinRunner_Close(t *testing.T) {
	runner := NewBuiltinRunner(nil, "", BuiltinConfig{}, nil, nil)
	if err := runner.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinRunner_HealthCheck(t *testing.T) {
	runner := NewBuiltinRunner(&mockSingleShotLLM{response: "ok"}, "test-model", BuiltinConfig{}, nil, nil)
	if err := runner.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinRunner_Fallback(t *testing.T) {
	fallbackLLM := &mockFallbackLLM{}
	runner := NewBuiltinRunner(fallbackLLM, "primary-model", BuiltinConfig{MaxTurnsDefault: 5}, nil, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:        "Do something",
		WorkDir:       t.TempDir(),
		FallbackModel: "fallback-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "fallback response" {
		t.Fatalf("expected fallback response, got %q", result.Output)
	}
	// Verify the fallback model was used for the second call
	if fallbackLLM.lastModel != "fallback-model" {
		t.Errorf("expected fallback model, got %q", fallbackLLM.lastModel)
	}
}

// mockFallbackLLM returns rate limit error on first call, success on second.
type mockFallbackLLM struct {
	calls     int
	lastModel string
}

func (m *mockFallbackLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	m.lastModel = req.Model
	if m.calls == 1 {
		// Simulate rate limit / overload on primary model
		return nil, &llm.RateLimitError{RetryAfterSecs: 30}
	}
	return &models.LlmResponse{
		Content:    "fallback response",
		StopReason: models.StopReasonEndTurn,
		Model:      req.Model,
	}, nil
}
func (m *mockFallbackLLM) ProviderName() string                { return "mock" }
func (m *mockFallbackLLM) HealthCheck(_ context.Context) error { return nil }
