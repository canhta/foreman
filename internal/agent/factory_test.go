package agent

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestNewAgentRunner_Builtin(t *testing.T) {
	cfg := models.AgentRunnerConfig{Provider: "builtin"}
	runner, err := NewAgentRunner(cfg, nil, &mockSingleShotLLM{response: "ok"}, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "builtin" {
		t.Fatalf("expected 'builtin', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_EmptyDefaultsToBuiltin(t *testing.T) {
	cfg := models.AgentRunnerConfig{Provider: ""}
	runner, err := NewAgentRunner(cfg, nil, &mockSingleShotLLM{response: "ok"}, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "builtin" {
		t.Fatalf("expected 'builtin', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_ClaudeCode(t *testing.T) {
	cfg := models.AgentRunnerConfig{
		Provider: "claudecode",
		ClaudeCode: models.ClaudeCodeRunnerConfig{
			Bin:                 "/usr/local/bin/claude",
			DefaultAllowedTools: []string{"Read", "Edit"},
			TimeoutSecsDefault:  180,
		},
	}

	runner, err := NewAgentRunner(cfg, &mockCmdRunner{exitCode: 0}, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "claudecode" {
		t.Fatalf("expected 'claudecode', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_Unknown(t *testing.T) {
	cfg := models.AgentRunnerConfig{Provider: "unknown"}
	_, err := NewAgentRunner(cfg, nil, nil, "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
