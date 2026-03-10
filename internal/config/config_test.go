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

func TestValidateConfig_BranchPrefixMissingSeparator(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.LLM.Anthropic.APIKey = "sk-test"
	cfg.Dashboard.AuthToken = "test-token"
	cfg.Git.BranchPrefix = "foreman" // missing trailing / or -

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "git.branch_prefix") && strings.Contains(e.Error(), "separator") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected branch_prefix separator validation error, got %v", errs)
	}
}

func TestValidateConfig_BranchPrefixValidSeparators(t *testing.T) {
	for _, prefix := range []string{"foreman/", "foreman-", ""} {
		cfg, _ := LoadDefaults()
		cfg.LLM.Anthropic.APIKey = "sk-test"
		cfg.Dashboard.AuthToken = "test-token"
		cfg.Git.BranchPrefix = prefix

		errs := Validate(cfg)
		for _, e := range errs {
			if strings.Contains(e.Error(), "git.branch_prefix") {
				t.Errorf("prefix %q should be valid, got error: %v", prefix, e)
			}
		}
	}
}

// ── Task 1: tracker sub-config structs ────────────────────────────────────────

func TestLoadConfig_TrackerJiraDefaultStatuses(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Tracker.Jira.StatusInProgress != "In Progress" {
		t.Errorf("expected jira.status_in_progress='In Progress', got %q", cfg.Tracker.Jira.StatusInProgress)
	}
	if cfg.Tracker.Jira.StatusInReview != "In Review" {
		t.Errorf("expected jira.status_in_review='In Review', got %q", cfg.Tracker.Jira.StatusInReview)
	}
	if cfg.Tracker.Jira.StatusDone != "Done" {
		t.Errorf("expected jira.status_done='Done', got %q", cfg.Tracker.Jira.StatusDone)
	}
	if cfg.Tracker.Jira.StatusBlocked != "Blocked" {
		t.Errorf("expected jira.status_blocked='Blocked', got %q", cfg.Tracker.Jira.StatusBlocked)
	}
}

func TestLoadConfig_TrackerLocalFileDefaultPath(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Tracker.LocalFile.Path != "./tickets" {
		t.Errorf("expected tracker.local_file.path='./tickets', got %q", cfg.Tracker.LocalFile.Path)
	}
}

func TestLoadConfig_TrackerSubConfigsFromTOML(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[tracker]
provider = "jira"

[tracker.jira]
base_url    = "https://company.atlassian.net"
email       = "bot@company.com"
api_token   = "tok123"
project_key = "PROJ"

[tracker.github]
owner    = "my-org"
repo     = "my-repo"
token    = "ghp_test"
base_url = "https://api.github.com"

[tracker.linear]
api_key = "lin_api_key"
team_id = "TEAM1"

[tracker.local_file]
path = "/tmp/tickets"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Tracker.Jira.BaseURL != "https://company.atlassian.net" {
		t.Errorf("jira.base_url: got %q", cfg.Tracker.Jira.BaseURL)
	}
	if cfg.Tracker.Jira.Email != "bot@company.com" {
		t.Errorf("jira.email: got %q", cfg.Tracker.Jira.Email)
	}
	if cfg.Tracker.Jira.APIToken != "tok123" {
		t.Errorf("jira.api_token: got %q", cfg.Tracker.Jira.APIToken)
	}
	if cfg.Tracker.Jira.ProjectKey != "PROJ" {
		t.Errorf("jira.project_key: got %q", cfg.Tracker.Jira.ProjectKey)
	}
	if cfg.Tracker.GitHub.Owner != "my-org" {
		t.Errorf("tracker.github.owner: got %q", cfg.Tracker.GitHub.Owner)
	}
	if cfg.Tracker.GitHub.Repo != "my-repo" {
		t.Errorf("tracker.github.repo: got %q", cfg.Tracker.GitHub.Repo)
	}
	if cfg.Tracker.GitHub.Token != "ghp_test" {
		t.Errorf("tracker.github.token: got %q", cfg.Tracker.GitHub.Token)
	}
	if cfg.Tracker.Linear.APIKey != "lin_api_key" {
		t.Errorf("tracker.linear.api_key: got %q", cfg.Tracker.Linear.APIKey)
	}
	if cfg.Tracker.Linear.TeamID != "TEAM1" {
		t.Errorf("tracker.linear.team_id: got %q", cfg.Tracker.Linear.TeamID)
	}
	if cfg.Tracker.LocalFile.Path != "/tmp/tickets" {
		t.Errorf("tracker.local_file.path: got %q", cfg.Tracker.LocalFile.Path)
	}
}

func TestExpandEnvVars_TrackerTokens(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	t.Setenv("TEST_GH_TOKEN", "gh-secret")
	t.Setenv("TEST_LINEAR_KEY", "linear-secret")

	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[tracker.jira]
api_token = "${TEST_JIRA_TOKEN}"

[tracker.github]
token = "${TEST_GH_TOKEN}"

[tracker.linear]
api_key = "${TEST_LINEAR_KEY}"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Tracker.Jira.APIToken != "jira-secret" {
		t.Errorf("tracker.jira.api_token env expansion: got %q", cfg.Tracker.Jira.APIToken)
	}
	if cfg.Tracker.GitHub.Token != "gh-secret" {
		t.Errorf("tracker.github.token env expansion: got %q", cfg.Tracker.GitHub.Token)
	}
	if cfg.Tracker.Linear.APIKey != "linear-secret" {
		t.Errorf("tracker.linear.api_key env expansion: got %q", cfg.Tracker.Linear.APIKey)
	}
}

// ── Task 3: git sub-config structs ────────────────────────────────────────────

func TestLoadConfig_GitGitLabBaseURLDefault(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Git.GitLab.BaseURL != "https://gitlab.com" {
		t.Errorf("expected git.gitlab.base_url='https://gitlab.com', got %q", cfg.Git.GitLab.BaseURL)
	}
}

func TestLoadConfig_GitSubConfigsFromTOML(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[git.github]
token    = "ghp_config_token"
base_url = "https://github.example.com/api/v3"

[git.gitlab]
token    = "glpat_config_token"
base_url = "https://gitlab.example.com"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Git.GitHub.Token != "ghp_config_token" {
		t.Errorf("git.github.token: got %q", cfg.Git.GitHub.Token)
	}
	if cfg.Git.GitHub.BaseURL != "https://github.example.com/api/v3" {
		t.Errorf("git.github.base_url: got %q", cfg.Git.GitHub.BaseURL)
	}
	if cfg.Git.GitLab.Token != "glpat_config_token" {
		t.Errorf("git.gitlab.token: got %q", cfg.Git.GitLab.Token)
	}
	if cfg.Git.GitLab.BaseURL != "https://gitlab.example.com" {
		t.Errorf("git.gitlab.base_url: got %q", cfg.Git.GitLab.BaseURL)
	}
}

func TestExpandEnvVars_GitTokens(t *testing.T) {
	t.Setenv("TEST_GH_GIT_TOKEN", "gh-git-secret")
	t.Setenv("TEST_GL_GIT_TOKEN", "gl-git-secret")

	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[git.github]
token = "${TEST_GH_GIT_TOKEN}"

[git.gitlab]
token = "${TEST_GL_GIT_TOKEN}"
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Git.GitHub.Token != "gh-git-secret" {
		t.Errorf("git.github.token env expansion: got %q", cfg.Git.GitHub.Token)
	}
	if cfg.Git.GitLab.Token != "gl-git-secret" {
		t.Errorf("git.gitlab.token env expansion: got %q", cfg.Git.GitLab.Token)
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
model = "claude-sonnet-4-6"
`), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadFromFile(configFile)
		if err != nil {
			t.Fatalf("LoadFromFile: %v", err)
		}

		if cfg.Skills.AgentRunner.Builtin.Model != "claude-sonnet-4-6" {
			t.Errorf("expected model=claude-sonnet-4-6, got %q",
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

func TestLoadConfig_DaemonMergeCheckIntervalDefault(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Daemon.MergeCheckIntervalSecs != 60 {
		t.Errorf("expected default merge_check_interval_secs=60, got %d", cfg.Daemon.MergeCheckIntervalSecs)
	}
}
