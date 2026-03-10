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

// TestExpandEnv_PlainString verifies that a string without ${…} is returned unchanged.
func TestExpandEnv_PlainString(t *testing.T) {
	got := expandEnv("plainvalue")
	if got != "plainvalue" {
		t.Errorf("expandEnv(plain): got %q want plainvalue", got)
	}
}

// TestExpandEnv_EnvVarExpanded verifies that ${VAR} is replaced by the env value.
func TestExpandEnv_EnvVarExpanded(t *testing.T) {
	t.Setenv("TEST_PROJECT_TOKEN", "secret-value")
	got := expandEnv("${TEST_PROJECT_TOKEN}")
	if got != "secret-value" {
		t.Errorf("expandEnv(${TEST_PROJECT_TOKEN}): got %q want secret-value", got)
	}
}

// TestExpandEnv_UnsetVar verifies that ${UNSET_VAR} becomes empty string.
func TestExpandEnv_UnsetVar(t *testing.T) {
	os.Unsetenv("TEST_UNSET_PROJECT_VAR")
	got := expandEnv("${TEST_UNSET_PROJECT_VAR}")
	if got != "" {
		t.Errorf("expandEnv(unset): got %q want empty string", got)
	}
}

// TestExpandEnv_PartialSyntax verifies strings that look similar but aren't
// ${VAR} — they should not be expanded.
func TestExpandEnv_PartialSyntax(t *testing.T) {
	cases := []string{
		"$TOKEN",       // no braces
		"{TOKEN}",      // no dollar
		"${INCOMPLETE", // no closing brace
		"COMPLETE}",    // no opening
	}
	for _, s := range cases {
		got := expandEnv(s)
		if got != s {
			t.Errorf("expandEnv(%q): expected no expansion, got %q", s, got)
		}
	}
}

// TestLoadProjectConfig_ExpandsEnvVars verifies that token env-var references
// in a TOML file are expanded when loaded.
func TestLoadProjectConfig_ExpandsEnvVars(t *testing.T) {
	t.Setenv("TEST_PROJ_JIRA_TOKEN", "expanded-jira-token")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	content := `
[tracker]
provider = "jira"

[tracker.jira]
api_token = "${TEST_PROJ_JIRA_TOKEN}"
project_key = "PROJ"
base_url = "https://company.atlassian.net"
email = "bot@company.com"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProjectConfig(configPath)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if cfg.Tracker.Jira.APIToken != "expanded-jira-token" {
		t.Errorf("tracker.jira.api_token env expansion: got %q want expanded-jira-token",
			cfg.Tracker.Jira.APIToken)
	}
}
