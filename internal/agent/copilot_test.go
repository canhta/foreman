package agent

import (
	"testing"
	"time"
)

func TestCopilotRunner_RunnerName(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{}}
	if r.RunnerName() != "copilot" {
		t.Fatalf("expected 'copilot', got %q", r.RunnerName())
	}
}

func TestCopilotRunner_BuildSessionConfig(t *testing.T) {
	r := &CopilotRunner{
		config: CopilotConfig{
			Model:               "gpt-4o",
			DefaultAllowedTools: []string{"Read", "Glob"},
			TimeoutSecsDefault:  180,
		},
	}

	req := AgentRequest{
		Prompt:       "Analyze this code",
		WorkDir:      "/tmp/test",
		AllowedTools: []string{"Read", "Edit", "Bash"},
	}

	cfg := r.buildSessionConfig(req)

	if cfg.Model != "gpt-4o" {
		t.Fatalf("expected model 'gpt-4o', got %q", cfg.Model)
	}
	if len(cfg.AvailableTools) != 3 {
		t.Fatalf("expected 3 available tools, got %d", len(cfg.AvailableTools))
	}
	if cfg.WorkingDirectory != "/tmp/test" {
		t.Fatalf("expected workdir '/tmp/test', got %q", cfg.WorkingDirectory)
	}
}

func TestCopilotRunner_BuildSessionConfig_DefaultTools(t *testing.T) {
	r := &CopilotRunner{
		config: CopilotConfig{
			Model:               "gpt-4o",
			DefaultAllowedTools: []string{"Read", "Glob"},
		},
	}

	cfg := r.buildSessionConfig(AgentRequest{Prompt: "Analyze", WorkDir: "/tmp"})
	if len(cfg.AvailableTools) != 2 {
		t.Fatalf("expected 2 default tools, got %d", len(cfg.AvailableTools))
	}
}

func TestCopilotRunner_BuildSessionConfig_SystemPrompt(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{Model: "gpt-4o"}}

	cfg := r.buildSessionConfig(AgentRequest{
		Prompt:       "Do something",
		WorkDir:      "/tmp",
		SystemPrompt: "You are a security expert.",
	})

	if cfg.SystemMessage == nil {
		t.Fatal("expected system message to be set")
	}
	if cfg.SystemMessage.Content != "You are a security expert." {
		t.Fatalf("unexpected system message: %s", cfg.SystemMessage.Content)
	}
	if cfg.SystemMessage.Mode != "append" {
		t.Fatalf("expected mode 'append', got %q", cfg.SystemMessage.Mode)
	}
}

func TestCopilotRunner_ResolveTimeout(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{TimeoutSecsDefault: 180}}

	// Request timeout takes precedence
	if d := r.resolveTimeout(AgentRequest{TimeoutSecs: 60}); d != 60*time.Second {
		t.Fatalf("expected 60s, got %v", d)
	}

	// Falls back to config default
	if d := r.resolveTimeout(AgentRequest{}); d != 180*time.Second {
		t.Fatalf("expected 180s, got %v", d)
	}

	// Falls back to hardcoded default
	r.config.TimeoutSecsDefault = 0
	if d := r.resolveTimeout(AgentRequest{}); d != 120*time.Second {
		t.Fatalf("expected 120s, got %v", d)
	}
}

func TestCopilotRunner_Close_NilClient(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{}}
	if err := r.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
