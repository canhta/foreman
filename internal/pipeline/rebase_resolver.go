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
	Success  bool
	Resolved string
}

// AttemptConflictResolution asks the LLM to resolve a git merge conflict.
// Returns the resolved content if successful, or Success=false if the LLM
// cannot produce a valid resolution.
func AttemptConflictResolution(ctx context.Context, provider llm.LlmProvider, conflictDiff, model string) (*ConflictResolution, error) {
	resp, err := provider.Complete(ctx, models.LlmRequest{
		Model: model,
		SystemPrompt: `You are resolving a git merge conflict. Analyze both sides and produce the correct merged result.

Output format:
<<<< RESOLVED
<the correct merged code>
>>>> END

If you cannot confidently resolve the conflict, say "CANNOT_RESOLVE" and explain why.`,
		UserPrompt: fmt.Sprintf("## Conflict\n```\n%s\n```\n\nResolve this conflict:", conflictDiff),
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
