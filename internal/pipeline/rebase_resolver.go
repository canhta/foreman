// internal/pipeline/rebase_resolver.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// ConflictResolution holds the result of an LLM-assisted conflict resolution attempt.
type ConflictResolution struct {
	Resolved string
	Success  bool
}

// conflictResolutionTokenBudget is the default token budget for conflict resolution context.
const conflictResolutionTokenBudget = 40_000

// AttemptConflictResolution asks the LLM to resolve a git merge conflict.
// baseContent and headContent are the full file contents from the base and
// head revisions (REQ-PIPE-003). Both are optional (empty = not provided).
// tokenBudget limits total context size; 0 uses the default (40,000).
// Returns the resolved content if successful, or Success=false if the LLM
// cannot produce a valid resolution.
func AttemptConflictResolution(ctx context.Context, provider llm.LlmProvider, conflictDiff, baseContent, headContent, model string, tokenBudget int) (*ConflictResolution, error) {
	if tokenBudget <= 0 {
		tokenBudget = conflictResolutionTokenBudget
	}

	// Build user prompt with full file context, truncated to budget.
	var sb strings.Builder
	sb.WriteString("## Conflict Markers\n```\n")
	sb.WriteString(conflictDiff)
	sb.WriteString("\n```\n\n")

	// Estimate tokens used so far.
	used := len(sb.String()) / 4 // rough 4-char-per-token estimate
	remaining := tokenBudget - used

	if baseContent != "" && remaining > 200 {
		baseTokens := len(baseContent) / 4
		if baseTokens > remaining/2 {
			// Truncate to half of remaining budget.
			maxChars := (remaining / 2) * 4
			if maxChars < len(baseContent) {
				baseContent = baseContent[:maxChars] + "\n... (truncated)"
			}
		}
		fmt.Fprintf(&sb, "## Base Version (before your changes)\n```\n%s\n```\n\n", baseContent)
		remaining -= len(baseContent) / 4
	}

	if headContent != "" && remaining > 200 {
		headTokens := len(headContent) / 4
		if headTokens > remaining {
			maxChars := remaining * 4
			if maxChars < len(headContent) {
				headContent = headContent[:maxChars] + "\n... (truncated)"
			}
		}
		fmt.Fprintf(&sb, "## Head Version (incoming changes)\n```\n%s\n```\n\n", headContent)
	}

	sb.WriteString("Resolve this conflict:")

	resp, err := provider.Complete(ctx, models.LlmRequest{
		Model: model,
		SystemPrompt: `You are resolving a git merge conflict. You are provided with the conflict markers and, where available, the full base and head file contents for additional context.

Output format:
<<<< RESOLVED
<the correct merged code>
>>>> END

If you cannot confidently resolve the conflict, say "CANNOT_RESOLVE" and explain why.`,
		UserPrompt: sb.String(),
		MaxTokens:  4096,
	})
	if err != nil {
		return nil, fmt.Errorf("conflict resolution LLM call: %w", err)
	}

	// Parse the RESOLVED block
	content := resp.Content
	startMarker := "<<<< RESOLVED"
	endMarker := ">>>> END"

	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return &ConflictResolution{Success: false}, nil
	}

	resolved := strings.TrimSpace(content[startIdx+len(startMarker) : endIdx])
	return &ConflictResolution{
		Success:  true,
		Resolved: resolved,
	}, nil
}
