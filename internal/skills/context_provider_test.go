package skills_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
)

type mockProgressDB struct {
	patterns []models.ProgressPattern
}

func (m *mockProgressDB) GetProgressPatterns(_ context.Context, _ string, _ []string) ([]models.ProgressPattern, error) {
	return m.patterns, nil
}

func TestSkillsContextProvider_InjectsPatterns(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "import-style", PatternValue: "use named imports", CreatedAt: time.Now()},
	}}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")
	result, err := cp.OnFilesAccessed(context.Background(), []string{"src/auth/handler.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "import-style") {
		t.Errorf("expected pattern in context, got %q", result)
	}
}

func TestSkillsContextProvider_Deduplication(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "style", PatternValue: "use tabs", CreatedAt: time.Now()},
	}}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")

	result1, _ := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if result1 == "" {
		t.Fatal("expected content on first call")
	}

	result2, _ := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if result2 != "" {
		t.Errorf("expected empty on second call (dedup), got %q", result2)
	}
}

func TestSkillsContextProvider_EmptyWhenNoPatterns(t *testing.T) {
	db := &mockProgressDB{patterns: nil}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")
	result, err := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSkillsContextProvider_TokenBudget(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "style", PatternValue: strings.Repeat("x", 1000), CreatedAt: time.Now()},
	}}
	// tiny budget — 1 token
	cp := skills.NewSkillsContextProvider(db, "ticket-1").WithTokenBudget(1)
	// First call injects and consumes budget
	cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	// Second call — budget exhausted, returns empty even for new pattern
	db.patterns = append(db.patterns, models.ProgressPattern{
		PatternKey: "other", PatternValue: "something", CreatedAt: time.Now(),
	})
	result, _ := cp.OnFilesAccessed(context.Background(), []string{"other.go"})
	if result != "" {
		t.Errorf("expected empty after budget consumed, got %q", result)
	}
}
