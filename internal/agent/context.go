package agent

import "context"

// ContextProvider is implemented by the skills layer to inject reactive context
// into the builtin runner mid-session. It is nil-safe — the runner always checks
// for nil before calling.
type ContextProvider interface {
	// OnFilesAccessed is called after all tools in a single turn complete.
	// paths is the batch of file paths touched in that turn.
	// Returns new context text to inject as a user message, or "" if nothing new.
	OnFilesAccessed(ctx context.Context, paths []string) (string, error)
}
