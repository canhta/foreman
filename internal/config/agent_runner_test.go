package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Task 5: top-level [agent_runner] config + BuiltinRunnerConfig expansion ───

func TestLoadConfig_AgentRunnerDefaultProvider(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.AgentRunner.Provider != "builtin" {
		t.Errorf("expected agent_runner.provider='builtin', got %q", cfg.AgentRunner.Provider)
	}
}

func TestLoadConfig_AgentRunnerBuiltinDefaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.AgentRunner.Builtin.MaxTurns != 15 {
		t.Errorf("expected agent_runner.builtin.max_turns=15, got %d", cfg.AgentRunner.Builtin.MaxTurns)
	}
	if cfg.AgentRunner.Builtin.MaxContextTokens != 100000 {
		t.Errorf("expected agent_runner.builtin.max_context_tokens=100000, got %d", cfg.AgentRunner.Builtin.MaxContextTokens)
	}
	if cfg.AgentRunner.Builtin.ReflectionInterval != 5 {
		t.Errorf("expected agent_runner.builtin.reflection_interval=5, got %d", cfg.AgentRunner.Builtin.ReflectionInterval)
	}
}

func TestLoadConfig_AgentRunnerClaudeCodeDefaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.AgentRunner.ClaudeCode.Bin != "claude" {
		t.Errorf("expected agent_runner.claudecode.bin='claude', got %q", cfg.AgentRunner.ClaudeCode.Bin)
	}
	if cfg.AgentRunner.ClaudeCode.MaxTurnsDefault != 15 {
		t.Errorf("expected agent_runner.claudecode.max_turns_default=15, got %d", cfg.AgentRunner.ClaudeCode.MaxTurnsDefault)
	}
	if cfg.AgentRunner.ClaudeCode.TimeoutSecsDefault != 300 {
		t.Errorf("expected agent_runner.claudecode.timeout_secs_default=300, got %d", cfg.AgentRunner.ClaudeCode.TimeoutSecsDefault)
	}
}

func TestLoadConfig_AgentRunnerCopilotDefaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.AgentRunner.Copilot.CLIPath != "copilot" {
		t.Errorf("expected agent_runner.copilot.cli_path='copilot', got %q", cfg.AgentRunner.Copilot.CLIPath)
	}
	if cfg.AgentRunner.Copilot.Model != "gpt-4o" {
		t.Errorf("expected agent_runner.copilot.model='gpt-4o', got %q", cfg.AgentRunner.Copilot.Model)
	}
	if cfg.AgentRunner.Copilot.TimeoutSecsDefault != 300 {
		t.Errorf("expected agent_runner.copilot.timeout_secs_default=300, got %d", cfg.AgentRunner.Copilot.TimeoutSecsDefault)
	}
}

func TestLoadConfig_SkillsBuiltinExpandedDefaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Skills.AgentRunner.Builtin.MaxTurns != 10 {
		t.Errorf("expected skills.agent_runner.builtin.max_turns=10, got %d", cfg.Skills.AgentRunner.Builtin.MaxTurns)
	}
	if cfg.Skills.AgentRunner.Builtin.MaxContextTokens != 50000 {
		t.Errorf("expected skills.agent_runner.builtin.max_context_tokens=50000, got %d", cfg.Skills.AgentRunner.Builtin.MaxContextTokens)
	}
	if cfg.Skills.AgentRunner.Builtin.ReflectionInterval != 0 {
		t.Errorf("expected skills.agent_runner.builtin.reflection_interval=0, got %d", cfg.Skills.AgentRunner.Builtin.ReflectionInterval)
	}
}

func TestLoadConfig_AgentRunnerFromTOML(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[agent_runner]
provider = "claudecode"

[agent_runner.builtin]
max_turns            = 20
max_context_tokens   = 200000
reflection_interval  = 3
model                = "claude-opus-4-6"

[agent_runner.claudecode]
bin                  = "/usr/local/bin/claude"
max_turns_default    = 25
timeout_secs_default = 600
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.AgentRunner.Provider != "claudecode" {
		t.Errorf("agent_runner.provider: got %q", cfg.AgentRunner.Provider)
	}
	if cfg.AgentRunner.Builtin.MaxTurns != 20 {
		t.Errorf("agent_runner.builtin.max_turns: got %d", cfg.AgentRunner.Builtin.MaxTurns)
	}
	if cfg.AgentRunner.Builtin.MaxContextTokens != 200000 {
		t.Errorf("agent_runner.builtin.max_context_tokens: got %d", cfg.AgentRunner.Builtin.MaxContextTokens)
	}
	if cfg.AgentRunner.Builtin.ReflectionInterval != 3 {
		t.Errorf("agent_runner.builtin.reflection_interval: got %d", cfg.AgentRunner.Builtin.ReflectionInterval)
	}
	if cfg.AgentRunner.Builtin.Model != "claude-opus-4-6" {
		t.Errorf("agent_runner.builtin.model: got %q", cfg.AgentRunner.Builtin.Model)
	}
	if cfg.AgentRunner.ClaudeCode.Bin != "/usr/local/bin/claude" {
		t.Errorf("agent_runner.claudecode.bin: got %q", cfg.AgentRunner.ClaudeCode.Bin)
	}
	if cfg.AgentRunner.ClaudeCode.MaxTurnsDefault != 25 {
		t.Errorf("agent_runner.claudecode.max_turns_default: got %d", cfg.AgentRunner.ClaudeCode.MaxTurnsDefault)
	}
	if cfg.AgentRunner.ClaudeCode.TimeoutSecsDefault != 600 {
		t.Errorf("agent_runner.claudecode.timeout_secs_default: got %d", cfg.AgentRunner.ClaudeCode.TimeoutSecsDefault)
	}
}
