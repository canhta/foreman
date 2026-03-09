package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Task 6: missing Limits and Context config fields ──────────────────────────

func TestLoadConfig_LimitsConflictResolutionTokenBudgetDefault(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Limits.ConflictResolutionTokenBudget != 40000 {
		t.Errorf("expected limits.conflict_resolution_token_budget=40000, got %d", cfg.Limits.ConflictResolutionTokenBudget)
	}
}

func TestLoadConfig_ContextGenerateMaxTokensDefault(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if cfg.Context.ContextGenerateMaxTokens != 32000 {
		t.Errorf("expected context.context_generate_max_tokens=32000, got %d", cfg.Context.ContextGenerateMaxTokens)
	}
}

func TestLoadConfig_LimitsFromTOML(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[limits]
conflict_resolution_token_budget = 60000
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Limits.ConflictResolutionTokenBudget != 60000 {
		t.Errorf("limits.conflict_resolution_token_budget: got %d", cfg.Limits.ConflictResolutionTokenBudget)
	}
}

func TestLoadConfig_ContextFromTOML(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[context]
context_generate_max_tokens = 16000
context_feedback_boost      = 2.0
`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Context.ContextGenerateMaxTokens != 16000 {
		t.Errorf("context.context_generate_max_tokens: got %d", cfg.Context.ContextGenerateMaxTokens)
	}
	if cfg.Context.ContextFeedbackBoost != 2.0 {
		t.Errorf("context.context_feedback_boost: got %f", cfg.Context.ContextFeedbackBoost)
	}
}
