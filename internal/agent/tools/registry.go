package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// RunFn is injected via SetRunFn for SubagentTool two-phase init.
// Using a function reference breaks the circular Registry ↔ BuiltinRunner dependency.
// remainingBudget is the parent's remaining turn budget (0 = unconstrained).
// agentDepth is the current nesting depth of the calling agent.
type RunFn func(ctx context.Context, task, workDir string, toolNames []string, maxTurns, remainingBudget, agentDepth int) (string, error)

// ToolHooks are optional callbacks fired around every tool execution.
type ToolHooks struct {
	// PreToolUse is called before execution — return non-nil to block the call.
	PreToolUse func(ctx context.Context, name string, input json.RawMessage) error
	// PostToolUse is called after execution for logging/auditing.
	PostToolUse func(ctx context.Context, name, output string, err error)
}

// Registry maps tool names to implementations and fires hooks around execution.
// It is safe for concurrent reads (Execute may be called from multiple goroutines).
type Registry struct {
	hooks           ToolHooks
	tools           map[string]Tool
	mcpMgr          *mcp.Manager
	runFn           RunFn
	allowedCommands []string
	todoStore       *TodoStore
	// parentBudget and parentDepth are set by the builtin runner before each run
	// so the subagent tool can enforce budget and depth constraints.
	parentBudget int
	parentDepth  int
}

// NewRegistry creates a Registry. gitProvider and cmdRunner may be nil — those
// tool groups return informative errors if invoked when their dependency is absent.
// This is a convenience wrapper around NewRegistryWithMCP(gitProvider, cmdRunner, hooks, nil).
func NewRegistry(gitProvider git.GitProvider, cmdRunner runner.CommandRunner, hooks ToolHooks) *Registry {
	return NewRegistryWithMCP(gitProvider, cmdRunner, hooks, nil)
}

// NewRegistryWithMCP creates a Registry with an optional MCP Manager wired into
// the ListMCPTools tool and direct MCP tool execution.
// All other parameters behave the same as NewRegistry.
func NewRegistryWithMCP(gitProvider git.GitProvider, cmdRunner runner.CommandRunner, hooks ToolHooks, mcpMgr *mcp.Manager) *Registry {
	todoStore := NewTodoStore()
	r := &Registry{
		tools:     make(map[string]Tool),
		hooks:     hooks,
		mcpMgr:    mcpMgr,
		todoStore: todoStore,
	}
	registerFS(r)
	registerGit(r, gitProvider)
	registerCode(r, cmdRunner)
	registerExec(r, cmdRunner, mcpMgr)
	r.Register(&batchTool{reg: r})
	registerLSP(r)
	r.Register(&todoWriteTool{store: todoStore})
	r.Register(&todoReadTool{store: todoStore})
	return r
}

// Register adds a tool to the registry. Panics on duplicate name (programming error).
func (r *Registry) Register(t Tool) {
	if _, exists := r.tools[t.Name()]; exists {
		panic(fmt.Sprintf("tools.Registry: duplicate tool name %q", t.Name()))
	}
	r.tools[t.Name()] = t
}

// Execute runs the named tool, firing pre/post hooks.
// If the tool is not registered locally but is an MCP tool (mcp_ prefix),
// it is routed to the MCP manager.
func (r *Registry) Execute(ctx context.Context, workDir, name string, input json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		if r.mcpMgr != nil && r.mcpMgr.IsMCPTool(name) {
			if r.hooks.PreToolUse != nil {
				if err := r.hooks.PreToolUse(ctx, name, input); err != nil {
					return "", err
				}
			}
			out, err := r.mcpMgr.CallTool(ctx, name, input)
			if r.hooks.PostToolUse != nil {
				r.hooks.PostToolUse(ctx, name, out, err)
			}
			return out, err
		}
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if err := validateRequiredFields(t.Schema(), input); err != nil {
		return "", err
	}
	if r.hooks.PreToolUse != nil {
		if err := r.hooks.PreToolUse(ctx, name, input); err != nil {
			return "", err
		}
	}
	out, err := t.Execute(ctx, workDir, input)
	if r.hooks.PostToolUse != nil {
		r.hooks.PostToolUse(ctx, name, out, err)
	}
	return out, err
}

// Defs returns ToolDef slices for the named tools, in request order. Unknown names are skipped.
// When an MCP manager is configured, all cached MCP tool defs are appended automatically
// so the LLM always sees MCP tool schemas without the caller needing to enumerate them.
func (r *Registry) Defs(names []string) []models.ToolDef {
	var defs []models.ToolDef
	for _, name := range names {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		defs = append(defs, models.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	if r.mcpMgr != nil {
		defs = append(defs, r.mcpMgr.CachedToolDefs()...)
	}
	return defs
}

// validateRequiredFields checks that all fields listed in schema's "required"
// array are present in input. It is a lightweight pre-execution guard — it does
// not perform type checking or enum validation.
func validateRequiredFields(schema, input json.RawMessage) error {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Required) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return fmt.Errorf("invalid input JSON: %w", err)
	}
	for _, field := range s.Required {
		if _, ok := obj[field]; !ok {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return nil
}

// Has reports whether the named tool is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// SetAllowedCommands configures the Bash/RunTest command whitelist.
func (r *Registry) SetAllowedCommands(cmds []string) { r.allowedCommands = cmds }

// AllowedCommands returns the current command whitelist.
func (r *Registry) AllowedCommands() []string { return r.allowedCommands }

// SetRunFn injects the agent runner function for SubagentTool (two-phase init).
// Call this after both Registry and BuiltinRunner are constructed.
func (r *Registry) SetRunFn(fn RunFn) { r.runFn = fn }

// RunFn returns the injected run function (may be nil before SetRunFn is called).
func (r *Registry) GetRunFn() RunFn { return r.runFn }

// SetParentBudgetAndDepth records the calling agent's remaining budget and depth.
// Called by BuiltinRunner at the start of each Run() so the Subagent tool can
// enforce budget inheritance and max-depth constraints.
func (r *Registry) SetParentBudgetAndDepth(remaining, depth int) {
	r.parentBudget = remaining
	r.parentDepth = depth
}

// GetParentBudgetAndDepth returns the stored budget and depth.
func (r *Registry) GetParentBudgetAndDepth() (remaining, depth int) {
	return r.parentBudget, r.parentDepth
}

// All register* functions implemented in their respective files:
// registerFS   → fs.go
// registerGit  → git.go
// registerCode → code.go
// registerExec → exec.go

// WithSemanticSearch registers the SemanticSearchTool if embedder is non-nil.
// Returns the registry for chaining.
func (r *Registry) WithSemanticSearch(embedder llm.Embedder, database db.Database) *Registry {
	if embedder != nil {
		r.Register(&SemanticSearchTool{db: database, embedder: embedder})
	}
	return r
}
