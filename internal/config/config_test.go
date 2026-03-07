package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	if cfg.Daemon.PollIntervalSecs != 60 {
		t.Errorf("expected default poll_interval_secs=60, got %d", cfg.Daemon.PollIntervalSecs)
	}
	if cfg.Daemon.MaxParallelTickets != 3 {
		t.Errorf("expected default max_parallel_tickets=3, got %d", cfg.Daemon.MaxParallelTickets)
	}
	if cfg.Cost.MaxLlmCallsPerTask != 8 {
		t.Errorf("expected default max_llm_calls_per_task=8, got %d", cfg.Cost.MaxLlmCallsPerTask)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected default driver=sqlite, got %q", cfg.Database.Driver)
	}
	if cfg.Daemon.MaxParallelTasks != 3 {
		t.Errorf("expected default max_parallel_tasks=3, got %d", cfg.Daemon.MaxParallelTasks)
	}
	if cfg.Daemon.TaskTimeoutMinutes != 15 {
		t.Errorf("expected default task_timeout_minutes=15, got %d", cfg.Daemon.TaskTimeoutMinutes)
	}
}

func TestValidateConfig_MaxParallelTasksZero(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.Daemon.MaxParallelTasks = 0

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if e != nil && e.Error() != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for max_parallel_tasks < 1")
	}
}

func TestValidateConfig_TaskTimeoutMinutesZero(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.Daemon.TaskTimeoutMinutes = 0

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if e != nil && e.Error() != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for task_timeout_minutes < 1")
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[daemon]
poll_interval_secs = 120
log_level = "debug"

[cost]
max_cost_per_ticket_usd = 25.0
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Daemon.PollIntervalSecs != 120 {
		t.Errorf("expected poll_interval_secs=120, got %d", cfg.Daemon.PollIntervalSecs)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("expected log_level=debug, got %q", cfg.Daemon.LogLevel)
	}
	if cfg.Cost.MaxCostPerTicketUSD != 25.0 {
		t.Errorf("expected max_cost_per_ticket_usd=25.0, got %f", cfg.Cost.MaxCostPerTicketUSD)
	}
}

func TestValidateConfig_MissingAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantMsg  string
	}{
		{"anthropic", "anthropic", "llm.anthropic.api_key is required"},
		{"openai", "openai", "llm.openai.api_key is required"},
		{"openrouter", "openrouter", "llm.openrouter.api_key is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := LoadDefaults()
			cfg.LLM.DefaultProvider = tt.provider
			// Ensure no API key is set
			cfg.LLM.Anthropic.APIKey = ""
			cfg.LLM.OpenAI.APIKey = ""
			cfg.LLM.OpenRouter.APIKey = ""

			errs := Validate(cfg)
			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), tt.wantMsg) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected validation error containing %q, got %v", tt.wantMsg, errs)
			}
		})
	}
}

func TestValidateConfig_APIKeyPresent(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.DefaultProvider = "anthropic"
	cfg.LLM.Anthropic.APIKey = "sk-test-key"

	errs := Validate(cfg)
	for _, e := range errs {
		if strings.Contains(e.Error(), "api_key") {
			t.Errorf("unexpected API key error: %v", e)
		}
	}
}

func TestValidateConfig_InvalidDashboardPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too_high", 70000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := LoadDefaults()
			cfg.LLM.Anthropic.APIKey = "sk-test"
			cfg.Dashboard.Enabled = true
			cfg.Dashboard.AuthToken = "test-token"
			cfg.Dashboard.Port = tt.port

			errs := Validate(cfg)
			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), "dashboard.port") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected dashboard.port validation error for port=%d, got %v", tt.port, errs)
			}
		})
	}
}

func TestValidateConfig_DashboardDisabledSkipsPortCheck(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.Anthropic.APIKey = "sk-test"
	cfg.Dashboard.Enabled = false
	cfg.Dashboard.Port = 0

	errs := Validate(cfg)
	for _, e := range errs {
		if strings.Contains(e.Error(), "dashboard.port") {
			t.Errorf("should not validate port when dashboard is disabled: %v", e)
		}
	}
}

func TestValidateConfig_ZeroCostBudget(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.Anthropic.APIKey = "sk-test"
	cfg.Cost.MaxCostPerTicketUSD = 0
	cfg.Cost.MaxCostPerDayUSD = 0
	cfg.Cost.MaxCostPerMonthUSD = 0

	errs := Validate(cfg)
	costErrors := 0
	for _, e := range errs {
		if strings.Contains(e.Error(), "cost.") && strings.Contains(e.Error(), "must be positive") {
			costErrors++
		}
	}
	if costErrors != 3 {
		t.Errorf("expected 3 cost validation errors, got %d: %v", costErrors, errs)
	}
}

func TestValidateConfig_FullyValid(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.Anthropic.APIKey = "sk-test-key"
	cfg.Dashboard.AuthToken = "test-token"

	errs := Validate(cfg)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for valid config, got %v", errs)
	}
}

func TestValidateConfig_DashboardEnabledWithoutAuthToken(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.Anthropic.APIKey = "sk-test-key"
	cfg.Dashboard.Enabled = true
	cfg.Dashboard.AuthToken = ""

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "dashboard.auth_token is required when dashboard is enabled") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected validation error for missing dashboard.auth_token, got %v", errs)
	}
}

func TestBuiltinRunnerConfig_ModelField(t *testing.T) {
	t.Run("model set via TOML", func(t *testing.T) {
		dir := t.TempDir()
		configFile := filepath.Join(dir, "foreman.toml")
		err := os.WriteFile(configFile, []byte(`
[skills.agent_runner]
type = "builtin"

[skills.agent_runner.builtin]
model = "claude-3-5-sonnet-20241022"
`), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadFromFile(configFile)
		if err != nil {
			t.Fatalf("LoadFromFile: %v", err)
		}

		if cfg.Skills.AgentRunner.Builtin.Model != "claude-3-5-sonnet-20241022" {
			t.Errorf("expected model=claude-3-5-sonnet-20241022, got %q",
				cfg.Skills.AgentRunner.Builtin.Model)
		}
	})

	t.Run("model not set defaults to empty string", func(t *testing.T) {
		cfg, err := LoadDefaults()
		if err != nil {
			t.Fatalf("LoadDefaults: %v", err)
		}

		if cfg.Skills.AgentRunner.Builtin.Model != "" {
			t.Errorf("expected empty model by default, got %q",
				cfg.Skills.AgentRunner.Builtin.Model)
		}
	})
}

func TestValidateConfig_SQLiteMaxParallel(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.Database.Driver = "sqlite"
	cfg.Daemon.MaxParallelTickets = 10

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if e.Error() != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for SQLite with max_parallel_tickets > 3")
	}
}
