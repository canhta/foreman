package config

import (
	"os"
	"path/filepath"
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
