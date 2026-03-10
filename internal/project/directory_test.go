package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateProjectDir(t *testing.T) {
	baseDir := t.TempDir()
	projectID := "test-project-id"

	dir, err := CreateProjectDir(baseDir, projectID)
	if err != nil {
		t.Fatalf("CreateProjectDir: %v", err)
	}

	expected := filepath.Join(baseDir, projectID)
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}

	// Check subdirectories exist
	for _, sub := range []string{"work", "ssh"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("subdir %q not created: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("%q is not a directory", sub)
		}
	}
}

// TestWriteAndLoadProjectConfig_RoundTrip verifies that all multi-word config
// fields survive a full write→read cycle through TOML. This catches toml tag
// mismatches (e.g. go-toml/v2 writes "CloneURL" but Viper+mapstructure reads
// "clone_url") — the class of bug fixed by adding toml:"<key>" tags.
func TestWriteAndLoadProjectConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	projectDir, err := CreateProjectDir(dir, "round-trip")
	if err != nil {
		t.Fatalf("CreateProjectDir: %v", err)
	}

	want := &ProjectConfig{}
	want.Project.Name = "My Project"
	want.Project.Description = "A round-trip test"
	want.Git.CloneURL = "git@github.com:org/repo.git"
	want.Git.DefaultBranch = "main"
	want.Git.Provider = "github"
	want.Git.GitHub.Token = "ghp_roundtrip"
	want.Tracker.Provider = "jira"
	want.Tracker.PickupLabel = "foreman-ready"
	want.Tracker.Jira.BaseURL = "https://company.atlassian.net"
	want.Tracker.Jira.Email = "bot@company.com"
	want.Tracker.Jira.APIToken = "jira-tok"
	want.Tracker.Jira.ProjectKey = "PROJ"
	want.Limits.MaxParallelTickets = 5
	want.Limits.MaxTasksPerTicket = 10
	want.Cost.MaxCostPerTicketUSD = 7.5
	want.AgentRunner.Provider = "builtin"
	want.Models.Planner = "anthropic:claude-sonnet-4-6"
	want.Models.Implementer = "anthropic:claude-sonnet-4-6"

	if err := WriteProjectConfig(projectDir, want); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}

	got, err := LoadProjectConfig(ProjectConfigPath(projectDir))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// Project fields
	if got.Project.Name != want.Project.Name {
		t.Errorf("project.name: got %q want %q", got.Project.Name, want.Project.Name)
	}
	if got.Project.Description != want.Project.Description {
		t.Errorf("project.description: got %q want %q", got.Project.Description, want.Project.Description)
	}

	// Git fields (multi-word keys — most likely to regress)
	if got.Git.CloneURL != want.Git.CloneURL {
		t.Errorf("git.clone_url: got %q want %q", got.Git.CloneURL, want.Git.CloneURL)
	}
	if got.Git.DefaultBranch != want.Git.DefaultBranch {
		t.Errorf("git.default_branch: got %q want %q", got.Git.DefaultBranch, want.Git.DefaultBranch)
	}
	if got.Git.Provider != want.Git.Provider {
		t.Errorf("git.provider: got %q want %q", got.Git.Provider, want.Git.Provider)
	}
	if got.Git.GitHub.Token != want.Git.GitHub.Token {
		t.Errorf("git.github.token: got %q want %q", got.Git.GitHub.Token, want.Git.GitHub.Token)
	}

	// Tracker sub-config fields
	if got.Tracker.Provider != want.Tracker.Provider {
		t.Errorf("tracker.provider: got %q want %q", got.Tracker.Provider, want.Tracker.Provider)
	}
	if got.Tracker.PickupLabel != want.Tracker.PickupLabel {
		t.Errorf("tracker.pickup_label: got %q want %q", got.Tracker.PickupLabel, want.Tracker.PickupLabel)
	}
	if got.Tracker.Jira.BaseURL != want.Tracker.Jira.BaseURL {
		t.Errorf("tracker.jira.base_url: got %q want %q", got.Tracker.Jira.BaseURL, want.Tracker.Jira.BaseURL)
	}
	if got.Tracker.Jira.Email != want.Tracker.Jira.Email {
		t.Errorf("tracker.jira.email: got %q want %q", got.Tracker.Jira.Email, want.Tracker.Jira.Email)
	}
	if got.Tracker.Jira.APIToken != want.Tracker.Jira.APIToken {
		t.Errorf("tracker.jira.api_token: got %q want %q", got.Tracker.Jira.APIToken, want.Tracker.Jira.APIToken)
	}
	if got.Tracker.Jira.ProjectKey != want.Tracker.Jira.ProjectKey {
		t.Errorf("tracker.jira.project_key: got %q want %q", got.Tracker.Jira.ProjectKey, want.Tracker.Jira.ProjectKey)
	}

	// Limits
	if got.Limits.MaxParallelTickets != want.Limits.MaxParallelTickets {
		t.Errorf("limits.max_parallel_tickets: got %d want %d", got.Limits.MaxParallelTickets, want.Limits.MaxParallelTickets)
	}
	if got.Limits.MaxTasksPerTicket != want.Limits.MaxTasksPerTicket {
		t.Errorf("limits.max_tasks_per_ticket: got %d want %d", got.Limits.MaxTasksPerTicket, want.Limits.MaxTasksPerTicket)
	}

	// Cost
	if got.Cost.MaxCostPerTicketUSD != want.Cost.MaxCostPerTicketUSD {
		t.Errorf("cost.max_cost_per_ticket_usd: got %f want %f", got.Cost.MaxCostPerTicketUSD, want.Cost.MaxCostPerTicketUSD)
	}

	// AgentRunner
	if got.AgentRunner.Provider != want.AgentRunner.Provider {
		t.Errorf("agent_runner.provider: got %q want %q", got.AgentRunner.Provider, want.AgentRunner.Provider)
	}

	// Models
	if got.Models.Planner != want.Models.Planner {
		t.Errorf("models.planner: got %q want %q", got.Models.Planner, want.Models.Planner)
	}
	if got.Models.Implementer != want.Models.Implementer {
		t.Errorf("models.implementer: got %q want %q", got.Models.Implementer, want.Models.Implementer)
	}
}

// TestWriteAndLoadProjectConfig_GitHubTracker verifies GitHub tracker round-trip,
// specifically owner/repo split that lives in GitHubTrackerConfig.
func TestWriteAndLoadProjectConfig_GitHubTracker(t *testing.T) {
	dir := t.TempDir()
	projectDir, err := CreateProjectDir(dir, "github-tracker")
	if err != nil {
		t.Fatalf("CreateProjectDir: %v", err)
	}

	want := &ProjectConfig{}
	want.Tracker.Provider = "github"
	want.Tracker.GitHub.Token = "ghp_test"
	want.Tracker.GitHub.Owner = "myorg"
	want.Tracker.GitHub.Repo = "myrepo"
	want.Tracker.GitHub.BaseURL = "https://github.example.com"

	if err := WriteProjectConfig(projectDir, want); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	got, err := LoadProjectConfig(ProjectConfigPath(projectDir))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if got.Tracker.GitHub.Token != want.Tracker.GitHub.Token {
		t.Errorf("tracker.github.token: got %q want %q", got.Tracker.GitHub.Token, want.Tracker.GitHub.Token)
	}
	if got.Tracker.GitHub.Owner != want.Tracker.GitHub.Owner {
		t.Errorf("tracker.github.owner: got %q want %q", got.Tracker.GitHub.Owner, want.Tracker.GitHub.Owner)
	}
	if got.Tracker.GitHub.Repo != want.Tracker.GitHub.Repo {
		t.Errorf("tracker.github.repo: got %q want %q", got.Tracker.GitHub.Repo, want.Tracker.GitHub.Repo)
	}
	if got.Tracker.GitHub.BaseURL != want.Tracker.GitHub.BaseURL {
		t.Errorf("tracker.github.base_url: got %q want %q", got.Tracker.GitHub.BaseURL, want.Tracker.GitHub.BaseURL)
	}
}

// TestWriteAndLoadProjectConfig_LinearTracker verifies Linear tracker round-trip.
func TestWriteAndLoadProjectConfig_LinearTracker(t *testing.T) {
	dir := t.TempDir()
	projectDir, err := CreateProjectDir(dir, "linear-tracker")
	if err != nil {
		t.Fatalf("CreateProjectDir: %v", err)
	}

	want := &ProjectConfig{}
	want.Tracker.Provider = "linear"
	want.Tracker.Linear.APIKey = "lin_api_abc"
	want.Tracker.Linear.TeamID = "TEAM1"
	want.Tracker.Linear.BaseURL = "https://api.linear.app"

	if err := WriteProjectConfig(projectDir, want); err != nil {
		t.Fatalf("WriteProjectConfig: %v", err)
	}
	got, err := LoadProjectConfig(ProjectConfigPath(projectDir))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if got.Tracker.Linear.APIKey != want.Tracker.Linear.APIKey {
		t.Errorf("tracker.linear.api_key: got %q want %q", got.Tracker.Linear.APIKey, want.Tracker.Linear.APIKey)
	}
	if got.Tracker.Linear.TeamID != want.Tracker.Linear.TeamID {
		t.Errorf("tracker.linear.team_id: got %q want %q", got.Tracker.Linear.TeamID, want.Tracker.Linear.TeamID)
	}
	if got.Tracker.Linear.BaseURL != want.Tracker.Linear.BaseURL {
		t.Errorf("tracker.linear.base_url: got %q want %q", got.Tracker.Linear.BaseURL, want.Tracker.Linear.BaseURL)
	}
}

func TestDeleteProjectDir(t *testing.T) {
	baseDir := t.TempDir()
	projectID := "to-delete"
	dir, _ := CreateProjectDir(baseDir, projectID)

	// Write a file to verify recursive deletion
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("test"), 0644)

	if err := DeleteProjectDir(dir); err != nil {
		t.Fatalf("DeleteProjectDir: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("directory still exists after deletion")
	}
}
