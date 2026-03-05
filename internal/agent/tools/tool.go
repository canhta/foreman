package tools

import (
	"context"
	"encoding/json"
)

// Tool is implemented by every built-in tool in the registry.
// All Execute calls must be goroutine-safe — the registry runs them in parallel.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // hand-written JSON Schema
	Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error)
}
