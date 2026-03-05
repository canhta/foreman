// internal/context/progress_test.go
package context

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type mockProgressDB struct {
	patterns []models.ProgressPattern
}

func (m *mockProgressDB) GetProgressPatterns(_ context.Context, ticketID string, dirs []string) ([]models.ProgressPattern, error) {
	return m.patterns, nil
}

func TestGetPrunedPatterns(t *testing.T) {
	now := time.Now()
	db := &mockProgressDB{
		patterns: []models.ProgressPattern{
			// import_style has two entries; the newer one should be kept.
			{PatternKey: "import_style", PatternValue: "ESM imports", CreatedAt: now},
			{PatternKey: "import_style", PatternValue: "CommonJS", CreatedAt: now.Add(-time.Hour)},
			{PatternKey: "error_handling", PatternValue: "try/catch with custom errors", CreatedAt: now.Add(-2 * time.Hour)},
		},
	}

	result, err := GetPrunedPatterns(context.Background(), db, "ticket-1", []string{"src/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should deduplicate by key, keeping most recent (first in list)
	if len(result) != 2 {
		t.Errorf("expected 2 patterns after pruning, got %d", len(result))
	}
	// After sorting DESC, import_style (newest) comes first; verify it's the most recent value.
	var importStyle *models.ProgressPattern
	for i := range result {
		if result[i].PatternKey == "import_style" {
			importStyle = &result[i]
			break
		}
	}
	if importStyle == nil {
		t.Fatal("expected import_style pattern in result")
	}
	if importStyle.PatternValue != "ESM imports" {
		t.Errorf("expected 'ESM imports' (most recent), got %q", importStyle.PatternValue)
	}
}

func TestFormatPatternsForPrompt(t *testing.T) {
	patterns := []models.ProgressPattern{
		{PatternKey: "import_style", PatternValue: "ESM imports, no semicolons"},
		{PatternKey: "error_handling", PatternValue: "Wrap with fmt.Errorf"},
	}

	result := FormatPatternsForPrompt(patterns)
	if result == "" {
		t.Error("expected non-empty formatted patterns")
	}
	if !containsSubstring(result, "import_style") {
		t.Errorf("expected 'import_style' in formatted output, got: %s", result)
	}
	if !containsSubstring(result, "error_handling") {
		t.Errorf("expected 'error_handling' in formatted output, got: %s", result)
	}
}

func TestFormatPatternsForPrompt_Empty(t *testing.T) {
	result := FormatPatternsForPrompt(nil)
	if result != "" {
		t.Errorf("expected empty string for nil patterns, got: %q", result)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && strings.Contains(s, sub)
}
