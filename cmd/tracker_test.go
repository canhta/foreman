package cmd

import (
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

// ── Task 2: buildTracker wiring ───────────────────────────────────────────────

func TestBuildTracker_GitHubUsesConfigToken(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "github"
	cfg.Tracker.GitHub.Token = "ghp_from_config"
	cfg.Tracker.GitHub.Owner = "my-org"
	cfg.Tracker.GitHub.Repo = "my-repo"

	tr, err := buildTracker(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTracker_GitHubFallsBackToEnvToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_from_env")

	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "github"
	cfg.Tracker.GitHub.Token = "" // no config token
	cfg.Tracker.GitHub.Owner = "my-org"
	cfg.Tracker.GitHub.Repo = "my-repo"

	tr, err := buildTracker(cfg)
	if err != nil {
		t.Fatalf("expected no error with env fallback, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTracker_GitHubErrorsWhenNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "github"
	cfg.Tracker.GitHub.Token = ""
	cfg.Tracker.GitHub.Owner = "my-org"
	cfg.Tracker.GitHub.Repo = "my-repo"

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error when no github token, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error should mention 'token', got: %v", err)
	}
}

func TestBuildTracker_JiraErrorsWhenNoBaseURL(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "jira"
	cfg.Tracker.Jira.BaseURL = ""
	cfg.Tracker.Jira.APIToken = "tok"
	cfg.Tracker.Jira.ProjectKey = "PROJ"

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error when jira base_url missing, got nil")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("error should mention 'base_url', got: %v", err)
	}
}

func TestBuildTracker_JiraErrorsWhenNoAPIToken(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "jira"
	cfg.Tracker.Jira.BaseURL = "https://company.atlassian.net"
	cfg.Tracker.Jira.APIToken = ""
	cfg.Tracker.Jira.ProjectKey = "PROJ"

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error when jira api_token missing, got nil")
	}
	if !strings.Contains(err.Error(), "api_token") {
		t.Errorf("error should mention 'api_token', got: %v", err)
	}
}

func TestBuildTracker_JiraErrorsWhenNoProjectKey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "jira"
	cfg.Tracker.Jira.BaseURL = "https://company.atlassian.net"
	cfg.Tracker.Jira.APIToken = "tok"
	cfg.Tracker.Jira.ProjectKey = ""

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error when jira project_key missing, got nil")
	}
	if !strings.Contains(err.Error(), "project_key") {
		t.Errorf("error should mention 'project_key', got: %v", err)
	}
}

func TestBuildTracker_LinearErrorsWhenNoAPIKey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "linear"
	cfg.Tracker.Linear.APIKey = ""

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error when linear api_key missing, got nil")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error should mention 'api_key', got: %v", err)
	}
}

func TestBuildTracker_LinearSucceedsWithAPIKey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "linear"
	cfg.Tracker.Linear.APIKey = "lin_key_123"

	tr, err := buildTracker(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTracker_LocalFileSucceeds(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "local_file"
	cfg.Tracker.LocalFile.Path = t.TempDir()

	tr, err := buildTracker(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTracker_LocalFileUsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "local_file"
	cfg.Tracker.LocalFile.Path = dir

	tr, err := buildTracker(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTracker_UnknownProviderErrors(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "unknown-provider"

	_, err := buildTracker(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tracker provider") {
		t.Errorf("error should mention 'unknown tracker provider', got: %v", err)
	}
}

// ── buildTrackerForDoctor ─────────────────────────────────────────────────────

func TestBuildTrackerForDoctor_GitHubUsesConfigToken(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "github"
	cfg.Tracker.GitHub.Token = "ghp_from_config"
	cfg.Tracker.GitHub.Owner = "my-org"
	cfg.Tracker.GitHub.Repo = "my-repo"

	tr, err := buildTrackerForDoctor(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestBuildTrackerForDoctor_JiraRequiresAllFields(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "jira"
	// missing base_url, api_token, project_key

	_, err := buildTrackerForDoctor(cfg)
	if err == nil {
		t.Fatal("expected error for jira with missing fields, got nil")
	}
}

func TestBuildTrackerForDoctor_LinearRequiresAPIKey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "linear"
	cfg.Tracker.Linear.APIKey = ""

	_, err := buildTrackerForDoctor(cfg)
	if err == nil {
		t.Fatal("expected error for linear with missing api_key, got nil")
	}
}

func TestBuildTrackerForDoctor_LocalFileSucceeds(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Tracker.Provider = "local_file"
	cfg.Tracker.LocalFile.Path = t.TempDir()

	tr, err := buildTrackerForDoctor(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil tracker")
	}
}

// defaultTestConfig returns a minimal config for testing build* functions.
func defaultTestConfig() *models.Config {
	return &models.Config{
		Tracker: models.TrackerConfig{
			PickupLabel: "foreman-ready",
		},
	}
}
