// internal/context/progress.go
package context

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// ProgressStore is the subset of db.Database needed for progress patterns.
// Implementations should return patterns for the given ticketID scoped to the
// provided directories (best-effort — implementations may return all patterns
// for the ticket if directory filtering is not yet supported).
type ProgressStore interface {
	GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error)
}

// GetPrunedPatterns retrieves patterns and deduplicates by key, keeping the
// most recent occurrence per key (sorted by CreatedAt descending before
// deduplication, so result is deterministic regardless of DB return order).
func GetPrunedPatterns(ctx context.Context, db ProgressStore, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	patterns, err := db.GetProgressPatterns(ctx, ticketID, directories)
	if err != nil {
		return nil, fmt.Errorf("get progress patterns: %w", err)
	}

	// Sort descending by CreatedAt so the first occurrence of each key is
	// the most recent, regardless of what order the DB returned them in.
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].CreatedAt.After(patterns[j].CreatedAt)
	})

	// Deduplicate by key — keep first occurrence (most recent) per key.
	seen := make(map[string]bool)
	var pruned []models.ProgressPattern
	for _, p := range patterns {
		if !seen[p.PatternKey] {
			seen[p.PatternKey] = true
			pruned = append(pruned, p)
		}
	}
	return pruned, nil
}

// FormatPatternsForPrompt converts progress patterns into a human-readable
// string for inclusion in LLM prompts.
func FormatPatternsForPrompt(patterns []models.ProgressPattern) string {
	if len(patterns) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Discovered Patterns\n\n")
	for _, p := range patterns {
		fmt.Fprintf(&sb, "- **%s**: %s\n", p.PatternKey, p.PatternValue)
	}
	return sb.String()
}
