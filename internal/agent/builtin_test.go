package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
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
	lastModel string
	calls     int
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

// --- REQ-LOOP-005: Per-Model Router tests ---

// mockCapturingLLM records every model used in LLM requests.
type mockCapturingLLM struct {
	capturedModels []string
}

func (m *mockCapturingLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.capturedModels = append(m.capturedModels, req.Model)
	return &models.LlmResponse{
		Content:      "done",
		StopReason:   models.StopReasonEndTurn,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        req.Model,
	}, nil
}
func (m *mockCapturingLLM) ProviderName() string                { return "mock" }
func (m *mockCapturingLLM) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_UsesConfiguredModel(t *testing.T) {
	t.Run("uses model from BuiltinConfig when set", func(t *testing.T) {
		mockLLM := &mockCapturingLLM{}
		runner := NewBuiltinRunner(mockLLM, "fallback-model", BuiltinConfig{
			MaxTurnsDefault: 5,
			Model:           "configured-model",
		}, nil, nil)

		_, err := runner.Run(context.Background(), AgentRequest{
			Prompt:  "Do something",
			WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mockLLM.capturedModels) == 0 {
			t.Fatal("expected at least one LLM call")
		}
		for i, m := range mockLLM.capturedModels {
			if m != "configured-model" {
				t.Errorf("call %d: expected model %q, got %q", i+1, "configured-model", m)
			}
		}
	})

	t.Run("falls back to constructor model when BuiltinConfig.Model is empty", func(t *testing.T) {
		mockLLM := &mockCapturingLLM{}
		runner := NewBuiltinRunner(mockLLM, "default-model", BuiltinConfig{
			MaxTurnsDefault: 5,
			Model:           "", // not set
		}, nil, nil)

		_, err := runner.Run(context.Background(), AgentRequest{
			Prompt:  "Do something",
			WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mockLLM.capturedModels) == 0 {
			t.Fatal("expected at least one LLM call")
		}
		for i, m := range mockLLM.capturedModels {
			if m != "default-model" {
				t.Errorf("call %d: expected model %q, got %q", i+1, "default-model", m)
			}
		}
	})
}

func TestNewAgentRunner_BuiltinConfigModelOverride(t *testing.T) {
	// When BuiltinRunnerConfig.Model is set, factory should use it instead of agentModel arg.
	mockLLM := &mockCapturingLLM{}
	cfg := models.AgentRunnerConfig{
		Provider: "builtin",
		Builtin: models.BuiltinRunnerConfig{
			Model: "per-model-router-model",
		},
	}

	agentRunner, err := NewAgentRunner(cfg, nil, mockLLM, "default-agent-model", nil, models.LLMConfig{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = agentRunner.Run(context.Background(), AgentRequest{
		Prompt:  "test",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockLLM.capturedModels) == 0 {
		t.Fatal("expected at least one LLM call")
	}
	for i, m := range mockLLM.capturedModels {
		if m != "per-model-router-model" {
			t.Errorf("call %d: expected model %q, got %q", i+1, "per-model-router-model", m)
		}
	}
}

func TestNewAgentRunner_BuiltinFallsBackToAgentModel(t *testing.T) {
	// When BuiltinRunnerConfig.Model is empty, factory uses agentModel arg.
	mockLLM := &mockCapturingLLM{}
	cfg := models.AgentRunnerConfig{
		Provider: "builtin",
		Builtin:  models.BuiltinRunnerConfig{
			// Model not set
		},
	}

	agentRunner, err := NewAgentRunner(cfg, nil, mockLLM, "default-agent-model", nil, models.LLMConfig{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = agentRunner.Run(context.Background(), AgentRequest{
		Prompt:  "test",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockLLM.capturedModels) == 0 {
		t.Fatal("expected at least one LLM call")
	}
	for i, m := range mockLLM.capturedModels {
		if m != "default-agent-model" {
			t.Errorf("call %d: expected model %q, got %q", i+1, "default-agent-model", m)
		}
	}
}

// mockSubagentCaptureLLM records what MaxTurns the subagent runner receives.
// First call: returns tool_use requesting Subagent. Second call: returns end_turn.
type mockSubagentCaptureLLM struct {
	calls int
}

func (m *mockSubagentCaptureLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.calls == 1 {
		return &models.LlmResponse{
			StopReason: models.StopReasonToolUse,
			ToolCalls: []models.ToolCall{
				{ID: "c1", Name: "Subagent", Input: json.RawMessage(`{"task":"do something","max_turns":20}`)},
			},
			TokensInput: 100, TokensOutput: 50, Model: req.Model,
		}, nil
	}
	return &models.LlmResponse{
		Content: "done", StopReason: models.StopReasonEndTurn,
		TokensInput: 50, TokensOutput: 20, Model: req.Model,
	}, nil
}
func (m *mockSubagentCaptureLLM) ProviderName() string                { return "mock" }
func (m *mockSubagentCaptureLLM) HealthCheck(_ context.Context) error { return nil }

func TestSubagent_InheritsBudget(t *testing.T) {
	// Parent: MaxTurns=10, currently on turn 7 (RemainingBudget=3)
	// Subagent requests max_turns=20 but should be capped to min(20, 3) = 3
	var capturedMaxTurns int
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.SetRunFn(func(_ context.Context, _ string, _ string, _ string, _ []string, maxTurns, remainingBudget, agentDepth int) (string, error) {
		capturedMaxTurns = maxTurns
		return "subagent done", nil
	})

	mockLLM := &mockSubagentCaptureLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{MaxTurnsDefault: 10}, reg, nil)

	_, err := runner.Run(context.Background(), AgentRequest{
		Prompt:          "do a task",
		WorkDir:         t.TempDir(),
		RemainingBudget: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The subagent should receive at most 3 turns (inherited from parent remaining budget)
	if capturedMaxTurns > 3 {
		t.Errorf("expected subagent max turns <= 3 (inherited budget), got %d", capturedMaxTurns)
	}
}

func TestSubagent_FailsWhenBudgetExhausted(t *testing.T) {
	// RemainingBudget < 0 means the parent budget is exhausted.
	// The subagent tool should return an error (fed back as tool error, not fatal).
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.SetRunFn(func(_ context.Context, _ string, _ string, _ string, _ []string, _, _, _ int) (string, error) {
		t.Error("subagent RunFn should not be called when budget is exhausted")
		return "", nil
	})

	mockLLM := &mockSubagentCaptureLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{MaxTurnsDefault: 10}, reg, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:          "do a task",
		WorkDir:         t.TempDir(),
		RemainingBudget: -1, // negative = exhausted
	})
	// The run itself should complete (error is fed back as tool error, not a fatal error)
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}
	_ = result
}

func TestSubagent_EnforcesMaxDepth(t *testing.T) {
	// AgentDepth at max: subagent call should fail with depth error
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.SetRunFn(func(_ context.Context, _ string, _ string, _ string, _ []string, _, _, _ int) (string, error) {
		t.Error("subagent RunFn should not be called when max depth is reached")
		return "", nil
	})

	mockLLM := &mockSubagentCaptureLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{MaxTurnsDefault: 10}, reg, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:     "do a task",
		WorkDir:    t.TempDir(),
		AgentDepth: MaxAgentDepth, // at max depth
	})
	// Should complete (depth error becomes tool error result, not fatal)
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}
	_ = result
}

// --- Task 1.5: Tool Call Deduplication Detector tests ---

// mockDuplicateToolLLM calls Read("main.go") twice (identical args), then ends.
type mockDuplicateToolLLM struct {
	receivedPrompt []string
	calls          int
}

func (m *mockDuplicateToolLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	// Capture all user messages to check for dedup warning.
	for _, msg := range req.Messages {
		if msg.Role == "user" && msg.Content != "" {
			m.receivedPrompt = append(m.receivedPrompt, msg.Content)
		}
	}
	switch m.calls {
	case 1:
		// First call: request Read("main.go")
		return &models.LlmResponse{
			StopReason:  models.StopReasonToolUse,
			ToolCalls:   []models.ToolCall{{ID: "c1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)}},
			TokensInput: 100, TokensOutput: 20, Model: req.Model,
		}, nil
	case 2:
		// Second call: request Read("main.go") AGAIN (identical args)
		return &models.LlmResponse{
			StopReason:  models.StopReasonToolUse,
			ToolCalls:   []models.ToolCall{{ID: "c2", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)}},
			TokensInput: 120, TokensOutput: 20, Model: req.Model,
		}, nil
	default:
		// Third call: done
		return &models.LlmResponse{
			Content: "all done", StopReason: models.StopReasonEndTurn,
			TokensInput: 50, TokensOutput: 10, Model: req.Model,
		}, nil
	}
}
func (m *mockDuplicateToolLLM) ProviderName() string                { return "mock" }
func (m *mockDuplicateToolLLM) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_DetectsDuplicateToolCalls(t *testing.T) {
	dir := t.TempDir()
	// Create the file so the Read tool doesn't error
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	mockLLM := &mockDuplicateToolLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{
		MaxTurnsDefault:     10,
		DefaultAllowedTools: []string{"Read"},
	}, nil, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "Read main.go and analyze it",
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "all done" {
		t.Fatalf("unexpected output: %q", result.Output)
	}

	// Assert that a deduplication warning was injected into the conversation.
	// The warning should appear in the messages sent to the LLM on the 3rd call.
	foundWarning := false
	for _, prompt := range mockLLM.receivedPrompt {
		if strings.Contains(strings.ToLower(prompt), "duplicate") ||
			strings.Contains(strings.ToLower(prompt), "repeated") ||
			strings.Contains(strings.ToLower(prompt), "already called") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected a deduplication warning to be injected into the conversation, but none found.\nReceived prompts: %v", mockLLM.receivedPrompt)
	}
}

// --- Task 3+4: Structured output integration tests ---

// mockStructuredOutputLLM simulates an LLM that calls the structured_output tool.
// On the first call it checks whether structured_output is in the tools list, then
// returns a tool_use for structured_output. On the second call it returns end_turn.
type mockStructuredOutputLLM struct {
	capturedTools []models.ToolDef
	calls         int
}

func (m *mockStructuredOutputLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.calls == 1 {
		m.capturedTools = req.Tools
		return &models.LlmResponse{
			StopReason: models.StopReasonToolUse,
			ToolCalls: []models.ToolCall{
				{
					ID:    "so_1",
					Name:  "structured_output",
					Input: json.RawMessage(`{"status":"APPROVED","issues":[]}`),
				},
			},
			TokensInput: 100, TokensOutput: 50, Model: req.Model,
		}, nil
	}
	return &models.LlmResponse{
		Content:     "done",
		StopReason:  models.StopReasonEndTurn,
		TokensInput: 50, TokensOutput: 10, Model: req.Model,
	}, nil
}
func (m *mockStructuredOutputLLM) ProviderName() string                { return "mock" }
func (m *mockStructuredOutputLLM) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_StructuredOutputToolInjected(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string"},
			"issues": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["status"]
	}`)

	mockLLM := &mockStructuredOutputLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{MaxTurnsDefault: 10}, nil, nil)

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:       "Analyze this code.",
		WorkDir:      t.TempDir(),
		OutputSchema: schema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify structured_output tool was injected
	found := false
	for _, tool := range mockLLM.capturedTools {
		if tool.Name == "structured_output" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected structured_output tool in tools sent to LLM, got: %v", mockLLM.capturedTools)
	}

	// Verify structured output was captured in AgentResult
	if len(result.Structured) == 0 {
		t.Fatal("expected Structured to be non-nil")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(result.Structured, &parsed); err != nil {
		t.Fatalf("failed to unmarshal structured output: %v", err)
	}
	if parsed["status"] != "APPROVED" {
		t.Errorf("expected status APPROVED, got %v", parsed["status"])
	}
}
