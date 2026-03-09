package cmd

import (
	"testing"
)

// ── Task 4: buildPRCreator / buildPRChecker ───────────────────────────────────

func TestBuildPRCreator_UsesGitConfigToken(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = "ghp_from_config"
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	pr := buildPRCreator(cfg)
	if pr == nil {
		t.Fatal("expected non-nil PRCreator when token and clone_url are set")
	}
}

func TestBuildPRCreator_FallsBackToEnvToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_from_env")

	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = "" // no config token
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	pr := buildPRCreator(cfg)
	if pr == nil {
		t.Fatal("expected non-nil PRCreator when GITHUB_TOKEN env is set")
	}
}

func TestBuildPRCreator_ReturnsNilWhenNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = ""
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	pr := buildPRCreator(cfg)
	if pr != nil {
		t.Fatal("expected nil PRCreator when no token available")
	}
}

func TestBuildPRCreator_ReturnsNilWhenNoCloneURL(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = "ghp_token"
	cfg.Git.CloneURL = "" // no clone URL → can't parse owner/repo

	pr := buildPRCreator(cfg)
	if pr != nil {
		t.Fatal("expected nil PRCreator when clone_url is empty")
	}
}

func TestBuildPRCreator_UsesGitHubBaseURL(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = "ghp_token"
	cfg.Git.GitHub.BaseURL = "https://github.example.com/api/v3"
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	// Non-nil result means the base URL was accepted (not validated at construction time)
	pr := buildPRCreator(cfg)
	if pr == nil {
		t.Fatal("expected non-nil PRCreator with custom base URL")
	}
}

func TestBuildPRChecker_UsesGitConfigToken(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = "ghp_from_config"
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	checker := buildPRChecker(cfg)
	if checker == nil {
		t.Fatal("expected non-nil PRChecker when token and clone_url are set")
	}
}

func TestBuildPRChecker_FallsBackToEnvToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_from_env")

	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = ""
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	checker := buildPRChecker(cfg)
	if checker == nil {
		t.Fatal("expected non-nil PRChecker when GITHUB_TOKEN env is set")
	}
}

func TestBuildPRChecker_ReturnsNilWhenNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	cfg := defaultTestConfig()
	cfg.Git.GitHub.Token = ""
	cfg.Git.CloneURL = "https://github.com/my-org/my-repo.git"

	checker := buildPRChecker(cfg)
	if checker != nil {
		t.Fatal("expected nil PRChecker when no token available")
	}
}
