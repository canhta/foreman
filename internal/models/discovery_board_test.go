package models_test

import (
	"sync"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

// TestDiscoveryBoard_AddPattern_ThreadSafe verifies concurrent AddPattern calls
// do not race or panic.
func TestDiscoveryBoard_AddPattern_ThreadSafe(t *testing.T) {
	b := models.NewDiscoveryBoard()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			key := "pattern"
			value := "value"
			b.AddPattern(key, value)
		}(i)
	}
	wg.Wait()

	patterns := b.GetPatterns()
	if _, ok := patterns["pattern"]; !ok {
		t.Errorf("expected pattern key to be present after concurrent writes")
	}
}

// TestDiscoveryBoard_AddFile_KeepsHigherScore verifies that AddFile replaces an
// existing entry only when the new score is strictly higher.
func TestDiscoveryBoard_AddFile_KeepsHigherScore(t *testing.T) {
	b := models.NewDiscoveryBoard()

	b.AddFile("internal/foo.go", 0.5)
	files := b.GetFiles()
	if got := files["internal/foo.go"]; got != 0.5 {
		t.Fatalf("expected 0.5, got %f", got)
	}

	// Lower score must NOT replace.
	b.AddFile("internal/foo.go", 0.3)
	files = b.GetFiles()
	if got := files["internal/foo.go"]; got != 0.5 {
		t.Errorf("lower score should not replace higher: expected 0.5, got %f", got)
	}

	// Higher score MUST replace.
	b.AddFile("internal/foo.go", 0.9)
	files = b.GetFiles()
	if got := files["internal/foo.go"]; got != 0.9 {
		t.Errorf("higher score should replace lower: expected 0.9, got %f", got)
	}
}

// TestDiscoveryBoard_GetPatterns_ReturnsCopy verifies that mutations to the
// returned map do not affect the board's internal state.
func TestDiscoveryBoard_GetPatterns_ReturnsCopy(t *testing.T) {
	b := models.NewDiscoveryBoard()
	b.AddPattern("error-handling", "use fmt.Errorf with %w")

	copy1 := b.GetPatterns()
	// Mutate the copy.
	copy1["injected"] = "should not appear in board"
	delete(copy1, "error-handling")

	// Board should be unaffected.
	copy2 := b.GetPatterns()
	if _, ok := copy2["error-handling"]; !ok {
		t.Error("board should still contain 'error-handling' after external mutation")
	}
	if _, ok := copy2["injected"]; ok {
		t.Error("board should not contain 'injected' key added to the copy")
	}
}

// TestDiscoveryBoard_Patterns_ConvertsToProgressPatterns verifies that Patterns()
// produces a []ProgressPattern with the correct TicketID, PatternKey, and PatternValue.
func TestDiscoveryBoard_Patterns_ConvertsToProgressPatterns(t *testing.T) {
	b := models.NewDiscoveryBoard()
	b.AddPattern("repo-interface", "all services implement Repository")
	b.AddPattern("error-wrap", "always wrap with fmt.Errorf %w")

	ticketID := "ticket-42"
	pp := b.Patterns(ticketID)
	if len(pp) != 2 {
		t.Fatalf("expected 2 ProgressPatterns, got %d", len(pp))
	}

	// Build a lookup to verify both entries without order dependency.
	byKey := make(map[string]ProgressPattern, len(pp))
	for _, p := range pp {
		byKey[p.PatternKey] = p
	}

	for _, want := range []struct{ key, value string }{
		{"repo-interface", "all services implement Repository"},
		{"error-wrap", "always wrap with fmt.Errorf %w"},
	} {
		p, ok := byKey[want.key]
		if !ok {
			t.Errorf("missing pattern key %q", want.key)
			continue
		}
		if p.TicketID != ticketID {
			t.Errorf("pattern %q: want TicketID=%q, got %q", want.key, ticketID, p.TicketID)
		}
		if p.PatternValue != want.value {
			t.Errorf("pattern %q: want value=%q, got %q", want.key, want.value, p.PatternValue)
		}
	}
}

// ProgressPattern is a local type alias for the test to reference from within
// the models package. Use the models package type directly.
type ProgressPattern = models.ProgressPattern
