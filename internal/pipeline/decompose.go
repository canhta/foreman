package pipeline

import (
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// scopeKeywords are words that suggest a ticket covers multiple features.
var scopeKeywords = []string{"and", "also", "plus", "additionally"}

// NeedsDecomposition returns true if the ticket is too large for a single PR.
func NeedsDecomposition(ticket *models.Ticket, cfg *models.DecomposeConfig) bool {
	if !cfg.Enabled || ticket.DecomposeDepth > 0 {
		return false
	}
	wordCount := len(strings.Fields(ticket.Description))
	if wordCount > cfg.MaxTicketWords {
		return true
	}
	if countScopeKeywords(ticket.Description) > cfg.MaxScopeKeywords {
		return true
	}
	vagueAndLong := ticket.AcceptanceCriteria == "" && wordCount > 100
	return vagueAndLong
}

// countScopeKeywords counts occurrences of scope-expanding words.
func countScopeKeywords(text string) int {
	lower := strings.ToLower(text)
	count := 0
	for _, kw := range scopeKeywords {
		words := strings.Fields(lower)
		for _, w := range words {
			if w == kw {
				count++
			}
		}
	}
	return count
}
