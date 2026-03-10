package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
[project]
name = "TestProject"
description = "A test project"

[tracker]
provider = "github"
pickup_label = "foreman-ready"

[tracker.github]
token = "test-token"
owner = "myorg"
repo = "myrepo"

[git]
provider = "github"
clone_url = "git@github.com:myorg/myrepo.git"
default_branch = "main"

[git.github]
token = "test-token"

[models]
planner = "anthropic:claude-sonnet-4-6"

[cost]
max_cost_per_ticket_usd = 10.0

[limits]
max_parallel_tickets = 2
max_parallel_tasks = 3
max_tasks_per_ticket = 15

[agent_runner]
provider = "builtin"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProjectConfig(configPath)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if cfg.Project.Name != "TestProject" {
		t.Errorf("name = %q, want TestProject", cfg.Project.Name)
	}
	if cfg.Tracker.Provider != "github" {
		t.Errorf("tracker.provider = %q, want github", cfg.Tracker.Provider)
	}
	if cfg.Limits.MaxParallelTickets != 2 {
		t.Errorf("limits.max_parallel_tickets = %d, want 2", cfg.Limits.MaxParallelTickets)
	}
	if cfg.Cost.MaxCostPerTicketUSD != 10.0 {
		t.Errorf("cost.max_cost_per_ticket_usd = %f, want 10.0", cfg.Cost.MaxCostPerTicketUSD)
	}
}
