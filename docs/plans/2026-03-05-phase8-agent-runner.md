# Phase 8: AgentRunner Interface — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a pluggable `AgentRunner` interface that allows Foreman's YAML skill engine to delegate tasks to external AI coding agents. Three external implementations (`claudecode`, `copilot`, `builtin`) plus a multi-turn tool-use loop that makes `builtin` a genuine agent — not just a single `LlmProvider.Complete()` wrapper.

**Architecture:** Mirrors `internal/llm/` exactly — interface + factory + multiple implementations. The `SkillEngine` gets a new `agentsdk` step type that dispatches to whichever runner is configured. Agent runner costs are tracked separately from the core pipeline budget.

**Key Design Decisions:**

1. **Claude Code** — No Go SDK exists. Use CLI subprocess with `claude -p --output-format json` and parse the full `SDKResultMessage` (includes `total_cost_usd`, `num_turns`, `usage`, `structured_output`).

2. **Copilot** — Native Go SDK (`github.com/github/copilot-sdk/go`) uses session-based JSON-RPC: `NewClient()` → `CreateSession()` → `SendAndWait()` → `Destroy()`.

3. **Builtin** — The critical insight: a single `LlmProvider.Complete()` call is identical to the existing `llm_call` step type. To be genuinely useful, `builtin` must run a multi-turn tool-use loop: call LLM → detect tool_use → execute tools → feed results back → repeat. This requires:
   - Adding `CompleteWithTools()` to the `LlmProvider` interface
   - Implementing tool_use handling in `anthropic.go` (primary provider)
   - Built-in tool implementations (Read, Glob, Grep) in pure Go
   - The loop itself in `builtin.go` (~100 LOC)

4. **No new SDK dependency** — The official `anthropic-sdk-go` v1.26.0 has a `BetaToolRunner` that implements the exact loop. However, Foreman's `LlmProvider` is provider-agnostic. We **study** the SDK's patterns (schema conversion in `schemautil.go`, message assembly in `betamessageutil.go`) but **write our own loop** against `LlmProvider.CompleteWithTools()`. This keeps the builtin runner working across all 4 providers.

5. **Separate cost cap** — `max_cost_per_ticket_usd` for agent calls, independent of core pipeline budget.

**Tech Stack:** Go 1.23+, `github.com/github/copilot-sdk/go` (Copilot), Claude Code CLI (`claude` binary), existing `internal/llm`, `internal/runner`, `internal/skills` packages

---

### Task 1: AgentRunner Interface and Types

**Files:**
- Create: `internal/agent/runner.go`
- Create: `internal/agent/runner_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/runner_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAgentRequest_Defaults(t *testing.T) {
	req := AgentRequest{
		Prompt:  "test prompt",
		WorkDir: "/tmp/test",
	}

	if req.Prompt != "test prompt" {
		t.Fatalf("expected prompt 'test prompt', got %q", req.Prompt)
	}
	if req.MaxTurns != 0 {
		t.Fatalf("expected default MaxTurns 0, got %d", req.MaxTurns)
	}
	if req.TimeoutSecs != 0 {
		t.Fatalf("expected default TimeoutSecs 0, got %d", req.TimeoutSecs)
	}
	if req.OutputSchema != nil {
		t.Fatal("expected nil OutputSchema")
	}
}

func TestAgentResult_WithUsage(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	result := AgentResult{
		Output: `{"severity":"low"}`,
		Usage: AgentUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			CostUSD:      0.02,
			NumTurns:     3,
			DurationMs:   5000,
		},
	}

	if result.Output != `{"severity":"low"}` {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.CostUSD != 0.02 {
		t.Fatalf("expected cost 0.02, got %f", result.Usage.CostUSD)
	}
	if result.Usage.NumTurns != 3 {
		t.Fatalf("expected 3 turns, got %d", result.Usage.NumTurns)
	}
	_ = schema
}
```

**Step 2: Write the implementation**

```go
// internal/agent/runner.go
package agent

import (
	"context"
	"encoding/json"
)

// AgentRunner abstracts any external agent SDK or CLI that can execute
// a bounded, scoped task and return a result. Used exclusively by the
// Skills engine at hook points — never inside the core pipeline.
type AgentRunner interface {
	// Run executes a single agent task and returns structured output.
	Run(ctx context.Context, req AgentRequest) (AgentResult, error)
	// HealthCheck verifies the runner is installed and configured.
	HealthCheck(ctx context.Context) error
	// RunnerName returns the identifier for logging/config.
	RunnerName() string
	// Close cleans up resources (e.g. stops Copilot CLI subprocess).
	Close() error
}

// AgentRequest defines the input for a single agent task.
type AgentRequest struct {
	Prompt       string          // What the agent should do
	SystemPrompt string          // Appended to the agent's system prompt
	WorkDir      string          // Working directory for file operations
	AllowedTools []string        // Enforced per-runner; empty = runner default
	MaxTurns     int             // 0 = runner default
	TimeoutSecs  int             // 0 = runner default
	OutputSchema json.RawMessage // Optional: JSON Schema for structured output
}

// AgentResult holds the output of an agent task.
type AgentResult struct {
	Output     string      // Final text or JSON string output
	Structured interface{} // Populated if OutputSchema was provided and runner supports it
	Usage      AgentUsage
}

// AgentUsage tracks resource consumption for an agent task.
type AgentUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64 // Estimated; 0 if runner doesn't expose it
	NumTurns     int     // Number of agentic turns used
	DurationMs   int     // Total execution time in milliseconds
}
```

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestAgent -v
```

---

### Task 2: LLM Provider — Add `CompleteWithTools()` and Tool Types

**Files:**
- Modify: `internal/models/pipeline.go` — add tool-use types to LlmRequest/LlmResponse
- Modify: `internal/llm/provider.go` — extend LlmProvider interface

**Why:** The builtin runner needs a multi-turn tool-use loop. This requires the LLM provider to accept tool definitions, return tool_use stop reasons, and handle tool result messages.

**Step 1: Add types to `internal/models/pipeline.go`**

```go
// Add to existing StopReason constants
const StopReasonToolUse StopReason = "tool_use"

// ToolDef describes a tool the LLM can call.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"` // JSON Schema
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult holds the output of executing a tool.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// Message represents a single message in a multi-turn conversation.
type Message struct {
	Role        string       `json:"role"`         // "user", "assistant"
	Content     string       `json:"content"`      // text content
	ToolCalls   []ToolCall   `json:"tool_calls"`   // assistant's tool invocations
	ToolResults []ToolResult `json:"tool_results"` // user's tool results
}

// Add new fields to LlmRequest:
// Messages []Message       — replaces UserPrompt for multi-turn
// Tools    []ToolDef        — tool definitions for the LLM

// Add new field to LlmResponse:
// ToolCalls []ToolCall      — populated when StopReason == StopReasonToolUse
```

Extend `LlmRequest`:
```go
type LlmRequest struct {
	Model         string          `json:"model"`
	SystemPrompt  string          `json:"system_prompt"`
	UserPrompt    string          `json:"user_prompt"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   float64         `json:"temperature"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Messages      []Message       `json:"messages,omitempty"`      // multi-turn (overrides UserPrompt)
	Tools         []ToolDef       `json:"tools,omitempty"`         // tool definitions
}
```

Extend `LlmResponse`:
```go
type LlmResponse struct {
	Content      string     `json:"content"`
	TokensInput  int        `json:"tokens_input"`
	TokensOutput int        `json:"tokens_output"`
	Model        string     `json:"model"`
	DurationMs   int64      `json:"duration_ms"`
	StopReason   StopReason `json:"stop_reason"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"` // populated on StopReasonToolUse
}
```

**Step 2: The LlmProvider interface stays as `Complete()`**

No new method needed — `Complete()` already accepts `LlmRequest` and returns `LlmResponse`. We just extend the request/response types to support tools. When `req.Tools` is non-empty and `req.Messages` is non-empty, the provider uses multi-turn with tool definitions. When both are empty, it falls back to the existing single-shot behavior.

**Step 3: Verify**
```bash
go test ./internal/llm/ -v
go test ./internal/pipeline/ -v  # ensure existing callers still work
```

---

### Task 3: Anthropic Provider — Tool-Use Support

**Files:**
- Modify: `internal/llm/anthropic.go` — handle tool definitions, tool_use content blocks, multi-turn messages
- Modify: `internal/llm/anthropic_test.go` — test tool-use round trips

**Why:** The Anthropic API natively supports tool_use. Study the official `anthropic-sdk-go` v1.26.0's `betatoolrunner.go` (tool execution and message assembly patterns) and `schemautil.go` (schema conversion) — then implement the minimal subset needed in Foreman's existing raw HTTP client.

**Step 1: Write the failing test**

```go
// internal/llm/anthropic_test.go — add

func TestAnthropicProvider_Complete_WithTools(t *testing.T) {
	// Mock HTTP server that returns a tool_use response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify tools were sent
		if len(req.Tools) == 0 {
			t.Error("expected tools in request")
		}

		// Return a tool_use response
		resp := anthropicResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)},
			},
			StopReason: "tool_use",
			Usage:      anthropicUsage{InputTokens: 100, OutputTokens: 50},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []models.Message{{Role: "user", Content: "Read main.go"}},
		Tools: []models.ToolDef{
			{Name: "Read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != models.StopReasonToolUse {
		t.Fatalf("expected tool_use stop reason, got %s", result.StopReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "Read" {
		t.Fatalf("expected tool name 'Read', got %q", result.ToolCalls[0].Name)
	}
}

func TestAnthropicProvider_Complete_WithToolResults(t *testing.T) {
	// Mock server that receives tool results and returns final text
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify multi-turn messages with tool results
		if len(req.Messages) < 2 {
			t.Errorf("expected at least 2 messages, got %d", len(req.Messages))
		}

		resp := anthropicResponse{
			ID:   "msg_456",
			Type: "message",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "The file contains a Go program."},
			},
			StopReason: "end_turn",
			Usage:      anthropicUsage{InputTokens: 200, OutputTokens: 30},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	result, err := provider.Complete(context.Background(), models.LlmRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []models.Message{
			{Role: "user", Content: "Read main.go"},
			{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)}}},
			{Role: "user", ToolResults: []models.ToolResult{{ToolCallID: "call_1", Content: "package main\n\nfunc main() {}"}}},
		},
		Tools:     []models.ToolDef{{Name: "Read", Description: "Read a file", InputSchema: json.RawMessage(`{}`)}},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != models.StopReasonEndTurn {
		t.Fatalf("expected end_turn, got %s", result.StopReason)
	}
	if result.Content != "The file contains a Go program." {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}
```

**Step 2: Update anthropic.go**

Update the request/response types to handle the Anthropic API's tool_use format:

- `anthropicRequest` gains a `Tools []anthropicTool` field
- `anthropicMessage` becomes polymorphic — its `Content` can be a string (simple) or an array of content blocks (tool_use, tool_result)
- `anthropicResponse` content blocks parsed for both `text` and `tool_use` types
- Messages with `ToolCalls` serialize as assistant messages with `tool_use` content blocks
- Messages with `ToolResults` serialize as user messages with `tool_result` content blocks

Key patterns to adapt from `anthropic-sdk-go`:
- **Message assembly** (from `betamessageutil.go`): constructing `tool_result` content blocks with `tool_use_id`, `content`, and `is_error` fields
- **Tool definition** format: `{name, description, input_schema}` in the request body

**Step 3: Verify**
```bash
go test ./internal/llm/ -run TestAnthropicProvider_Complete_With -v
go test ./internal/llm/ -v  # all existing tests still pass
```

---

### Task 4: Built-In Tool Implementations

**Files:**
- Create: `internal/agent/tools.go`
- Create: `internal/agent/tools_test.go`

**Why:** The builtin runner's multi-turn loop needs actual tool implementations. These are pure Go, no subprocess, scoped to the working directory for security.

**Step 1: Write the failing test**

```go
// internal/agent/tools_test.go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "test.go"})
	result, err := builtinTools["Read"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "package main" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestReadTool_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	input, _ := json.Marshal(map[string]string{"path": "../../../etc/passwd"})
	_, err := builtinTools["Read"](dir, input)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestGlobTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "*.go"})
	result, err := builtinTools["Glob"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected glob results")
	}
}

func TestGrepTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\t// TODO: implement\n}\n"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "TODO", "path": "."})
	result, err := builtinTools["Grep"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected grep results")
	}
}

func TestGrepTool_NoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "NOTFOUND", "path": "."})
	result, err := builtinTools["Grep"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got: %s", result)
	}
}
```

**Step 2: Write the implementation**

```go
// internal/agent/tools.go
package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ToolExecutor executes a tool within a working directory and returns the result.
type ToolExecutor func(workDir string, input json.RawMessage) (string, error)

// builtinTools maps tool names to their Go implementations.
// The builtin runner is read-only by default — no Edit/Write tools.
// This is safer for hook-point tasks (security scans, changelog generation).
var builtinTools = map[string]ToolExecutor{
	"Read": toolRead,
	"Glob": toolGlob,
	"Grep": toolGrep,
}

func toolRead(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Read: invalid input: %w", err)
	}
	abs := filepath.Join(workDir, filepath.Clean(args.Path))
	if !strings.HasPrefix(abs, filepath.Clean(workDir)) {
		return "", fmt.Errorf("Read: path %q outside working directory", args.Path)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("Read: %w", err)
	}
	return string(content), nil
}

func toolGlob(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Glob: invalid input: %w", err)
	}
	matches, err := filepath.Glob(filepath.Join(workDir, args.Pattern))
	if err != nil {
		return "", fmt.Errorf("Glob: %w", err)
	}
	var rel []string
	for _, m := range matches {
		r, _ := filepath.Rel(workDir, m)
		rel = append(rel, r)
	}
	return strings.Join(rel, "\n"), nil
}

func toolGrep(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Grep: invalid input: %w", err)
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("Grep: invalid pattern: %w", err)
	}

	searchDir := filepath.Join(workDir, filepath.Clean(args.Path))
	if !strings.HasPrefix(searchDir, filepath.Clean(workDir)) {
		return "", fmt.Errorf("Grep: path %q outside working directory", args.Path)
	}

	var results []string
	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Skip binary files and large files
		if info.Size() > 1<<20 { // 1MB
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		relPath, _ := filepath.Rel(workDir, path)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
		return nil
	})

	return strings.Join(results, "\n"), nil
}

// ToolDefs returns the JSON Schema tool definitions for the builtin tools.
func ToolDefs(toolNames []string) []json.RawMessage {
	schemas := map[string]json.RawMessage{
		"Read": json.RawMessage(`{"name":"Read","description":"Read a file's contents","input_schema":{"type":"object","properties":{"path":{"type":"string","description":"Relative path to the file"}},"required":["path"]}}`),
		"Glob": json.RawMessage(`{"name":"Glob","description":"Find files matching a glob pattern","input_schema":{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (e.g. **/*.go)"}},"required":["pattern"]}}`),
		"Grep": json.RawMessage(`{"name":"Grep","description":"Search file contents with a regex pattern","input_schema":{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"},"path":{"type":"string","description":"Relative directory or file path to search in"}},"required":["pattern","path"]}}`),
	}

	var defs []json.RawMessage
	for _, name := range toolNames {
		if schema, ok := schemas[name]; ok {
			defs = append(defs, schema)
		}
	}
	return defs
}
```

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestRead -v
go test ./internal/agent/ -run TestGlob -v
go test ./internal/agent/ -run TestGrep -v
```

---

### Task 5: Builtin Runner with Multi-Turn Tool-Use Loop

**Files:**
- Create: `internal/agent/builtin.go`
- Create: `internal/agent/builtin_test.go`

**Depends on:** Tasks 2, 3, 4

**Step 1: Write the failing test**

```go
// internal/agent/builtin_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

// mockToolLLM simulates a multi-turn tool-use conversation.
// First call returns tool_use, second call returns end_turn.
type mockToolLLM struct {
	calls    int
	maxCalls int
}

func (m *mockToolLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.calls++
	if m.calls == 1 && len(req.Tools) > 0 {
		// First call: request a tool
		return &models.LlmResponse{
			StopReason:   models.StopReasonToolUse,
			TokensInput:  500,
			TokensOutput: 100,
			Model:        req.Model,
			DurationMs:   500,
			ToolCalls: []models.ToolCall{
				{ID: "call_1", Name: "Read", Input: json.RawMessage(`{"path":"main.go"}`)},
			},
		}, nil
	}
	// Second call: return final answer
	return &models.LlmResponse{
		Content:      "The file contains a Go program with a main function.",
		StopReason:   models.StopReasonEndTurn,
		TokensInput:  800,
		TokensOutput: 50,
		Model:        req.Model,
		DurationMs:   300,
	}, nil
}

func (m *mockToolLLM) ProviderName() string                { return "mock" }
func (m *mockToolLLM) HealthCheck(_ context.Context) error { return nil }

func TestBuiltinRunner_MultiTurnToolUse(t *testing.T) {
	// Create a temp dir with a file for the Read tool
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644)

	mockLLM := &mockToolLLM{}
	runner := NewBuiltinRunner(mockLLM, "test-model", BuiltinConfig{
		MaxTurnsDefault:     10,
		DefaultAllowedTools: []string{"Read", "Glob", "Grep"},
	})

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "What is in main.go?",
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "The file contains a Go program with a main function." {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if mockLLM.calls != 2 {
		t.Fatalf("expected 2 LLM calls (tool_use + end_turn), got %d", mockLLM.calls)
	}
	if result.Usage.NumTurns != 2 {
		t.Fatalf("expected 2 turns, got %d", result.Usage.NumTurns)
	}
	// Usage should be accumulated across turns
	if result.Usage.InputTokens != 1300 {
		t.Fatalf("expected 1300 accumulated input tokens, got %d", result.Usage.InputTokens)
	}
}

func TestBuiltinRunner_SingleShot(t *testing.T) {
	mockLLM := &mockToolLLM{maxCalls: 1}
	// Override to return end_turn immediately
	mockLLM.calls = 1 // skip tool_use branch

	simpleMock := &mockLLM{response: "simple answer"}
	runner := NewBuiltinRunner(simpleMock, "test-model", BuiltinConfig{
		MaxTurnsDefault: 10,
	})

	result, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "What is 2+2?",
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "simple answer" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.NumTurns != 1 {
		t.Fatalf("expected 1 turn, got %d", result.Usage.NumTurns)
	}
}

func TestBuiltinRunner_MaxTurnsExceeded(t *testing.T) {
	// LLM always requests tools, never returns end_turn
	alwaysToolUse := &alwaysToolUseLLM{}
	runner := NewBuiltinRunner(alwaysToolUse, "test-model", BuiltinConfig{
		MaxTurnsDefault:     3,
		DefaultAllowedTools: []string{"Read"},
	})

	_, err := runner.Run(context.Background(), AgentRequest{
		Prompt:  "Read everything",
		WorkDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected max turns error")
	}
}

// type mockLLM for simple single-shot (already defined in tests above)
type simpleMockLLM struct{ response string }

func (m *simpleMockLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{Content: m.response, StopReason: models.StopReasonEndTurn, TokensInput: 100, TokensOutput: 50, Model: req.Model, DurationMs: 200}, nil
}
func (m *simpleMockLLM) ProviderName() string                { return "mock" }
func (m *simpleMockLLM) HealthCheck(_ context.Context) error { return nil }

type alwaysToolUseLLM struct{}

func (m *alwaysToolUseLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	return &models.LlmResponse{
		StopReason: models.StopReasonToolUse, TokensInput: 100, TokensOutput: 50,
		ToolCalls: []models.ToolCall{{ID: "c1", Name: "Read", Input: json.RawMessage(`{"path":"x.go"}`)}},
	}, nil
}
func (m *alwaysToolUseLLM) ProviderName() string                { return "mock" }
func (m *alwaysToolUseLLM) HealthCheck(_ context.Context) error { return nil }
```

**Step 2: Write the implementation**

```go
// internal/agent/builtin.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// BuiltinConfig holds configuration for the builtin runner.
type BuiltinConfig struct {
	MaxTurnsDefault     int
	DefaultAllowedTools []string // e.g. ["Read", "Glob", "Grep"]
}

// BuiltinRunner runs a multi-turn tool-use loop against the LlmProvider.
// Unlike Claude Code or Copilot, this uses Foreman's own provider interface,
// making it work across all 4 LLM providers (Anthropic, OpenAI, OpenRouter, local).
type BuiltinRunner struct {
	provider llm.LlmProvider
	model    string
	config   BuiltinConfig
}

// NewBuiltinRunner creates a builtin runner with multi-turn tool-use capability.
func NewBuiltinRunner(provider llm.LlmProvider, model string, config BuiltinConfig) *BuiltinRunner {
	return &BuiltinRunner{provider: provider, model: model, config: config}
}

func (r *BuiltinRunner) RunnerName() string { return "builtin" }

func (r *BuiltinRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	systemPrompt := "You are a focused task executor. Complete the task and return only the result."
	if req.SystemPrompt != "" {
		systemPrompt = systemPrompt + "\n\n" + req.SystemPrompt
	}

	// Determine which tools to offer
	toolNames := req.AllowedTools
	if len(toolNames) == 0 {
		toolNames = r.config.DefaultAllowedTools
	}

	// Build tool definitions for the LLM
	var toolDefs []models.ToolDef
	for _, raw := range ToolDefs(toolNames) {
		var td models.ToolDef
		json.Unmarshal(raw, &td)
		toolDefs = append(toolDefs, td)
	}

	maxTurns := req.MaxTurns
	if maxTurns == 0 {
		maxTurns = r.config.MaxTurnsDefault
	}
	if maxTurns == 0 {
		maxTurns = 10
	}

	// Initialize conversation
	messages := []models.Message{
		{Role: "user", Content: req.Prompt},
	}

	var usage AgentUsage

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := r.provider.Complete(ctx, models.LlmRequest{
			Model:        r.model,
			SystemPrompt: systemPrompt,
			MaxTokens:    4096,
			Temperature:  0.2,
			Messages:     messages,
			Tools:        toolDefs,
		})
		if err != nil {
			return AgentResult{}, fmt.Errorf("builtin: turn %d: %w", turn+1, err)
		}

		usage.InputTokens += resp.TokensInput
		usage.OutputTokens += resp.TokensOutput
		usage.DurationMs += int(resp.DurationMs)
		usage.NumTurns++

		// Done — model returned a final text response
		if resp.StopReason == models.StopReasonEndTurn || resp.StopReason == models.StopReasonMaxTokens {
			return AgentResult{Output: resp.Content, Usage: usage}, nil
		}

		// Tool use — execute each tool call, append results
		if resp.StopReason == models.StopReasonToolUse && len(resp.ToolCalls) > 0 {
			// Append assistant message with tool calls
			messages = append(messages, models.Message{
				Role:      "assistant",
				ToolCalls: resp.ToolCalls,
			})

			// Execute tools and collect results
			var toolResults []models.ToolResult
			for _, tc := range resp.ToolCalls {
				executor, ok := builtinTools[tc.Name]
				if !ok {
					toolResults = append(toolResults, models.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("unknown tool: %s", tc.Name),
						IsError:    true,
					})
					continue
				}
				output, err := executor(req.WorkDir, tc.Input)
				if err != nil {
					toolResults = append(toolResults, models.ToolResult{
						ToolCallID: tc.ID,
						Content:    err.Error(),
						IsError:    true,
					})
					continue
				}
				toolResults = append(toolResults, models.ToolResult{
					ToolCallID: tc.ID,
					Content:    output,
				})
			}

			// Append user message with tool results
			messages = append(messages, models.Message{
				Role:        "user",
				ToolResults: toolResults,
			})
			continue
		}

		// Unexpected stop reason — return what we have
		return AgentResult{Output: resp.Content, Usage: usage}, nil
	}

	return AgentResult{}, fmt.Errorf("builtin: exceeded max turns %d without completion", maxTurns)
}

func (r *BuiltinRunner) HealthCheck(ctx context.Context) error {
	return r.provider.HealthCheck(ctx)
}

func (r *BuiltinRunner) Close() error { return nil }
```

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestBuiltin -v
```

---

### Task 6: Claude Code Runner (CLI Subprocess)

**Files:**
- Create: `internal/agent/claudecode.go`
- Create: `internal/agent/claudecode_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/claudecode_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/runner"
)

type mockCmdRunner struct {
	stdout   string
	stderr   string
	exitCode int
	timedOut bool
}

func (m *mockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{
		Stdout:   m.stdout,
		Stderr:   m.stderr,
		ExitCode: m.exitCode,
		Duration: 2 * time.Second,
		TimedOut: m.timedOut,
	}, nil
}
func (m *mockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestClaudeCodeRunner_Run_Success(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "success",
		"result": "Fixed the bug by adding nil check",
		"total_cost_usd": 0.035, "num_turns": 3, "duration_ms": 4500, "is_error": false,
		"usage": map[string]interface{}{"input_tokens": 2000, "output_tokens": 800},
	}
	resultJSON, _ := json.Marshal(sdkResult)

	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{
		DefaultAllowedTools: []string{"Read", "Edit", "Glob"},
		MaxTurnsDefault: 10, TimeoutSecsDefault: 120,
	})

	result, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix the bug", WorkDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Fixed the bug by adding nil check" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.Usage.CostUSD != 0.035 {
		t.Fatalf("expected cost 0.035, got %f", result.Usage.CostUSD)
	}
	if result.Usage.NumTurns != 3 {
		t.Fatalf("expected 3 turns, got %d", result.Usage.NumTurns)
	}
}

func TestClaudeCodeRunner_Run_Timeout(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{timedOut: true}, ClaudeCodeConfig{TimeoutSecsDefault: 120})
	_, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix the bug", WorkDir: "/tmp/test"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClaudeCodeRunner_Run_ErrorResult(t *testing.T) {
	sdkResult := map[string]interface{}{
		"type": "result", "subtype": "error_max_turns", "is_error": true,
		"total_cost_usd": 0.05, "num_turns": 10, "duration_ms": 30000,
		"usage": map[string]interface{}{"input_tokens": 5000, "output_tokens": 2000},
		"errors": []string{"max turns reached"},
	}
	resultJSON, _ := json.Marshal(sdkResult)
	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: string(resultJSON), exitCode: 0}, ClaudeCodeConfig{TimeoutSecsDefault: 120})
	_, err := r.Run(context.Background(), AgentRequest{Prompt: "Fix", WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("expected error for error result")
	}
}

func TestClaudeCodeRunner_HealthCheck(t *testing.T) {
	r := NewClaudeCodeRunner(&mockCmdRunner{stdout: "1.0.0", exitCode: 0}, ClaudeCodeConfig{})
	if err := r.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Write the implementation**

```go
// internal/agent/claudecode.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

// ClaudeCodeConfig holds configuration for the Claude Code CLI runner.
type ClaudeCodeConfig struct {
	Bin                 string   // Path to "claude" binary
	DefaultAllowedTools []string // e.g. ["Read", "Edit", "Glob", "Grep", "Bash"]
	MaxTurnsDefault     int
	TimeoutSecsDefault  int
	MaxBudgetUSD        float64  // Per-invocation cost cap
	Model               string   // e.g. "sonnet", "opus"
}

// ClaudeCodeRunner invokes the Claude Agent SDK via CLI subprocess.
type ClaudeCodeRunner struct {
	bin    string
	runner runner.CommandRunner
	config ClaudeCodeConfig
}

func NewClaudeCodeRunner(cmdRunner runner.CommandRunner, cfg ClaudeCodeConfig) *ClaudeCodeRunner {
	bin := cfg.Bin
	if bin == "" {
		bin = "claude"
	}
	return &ClaudeCodeRunner{bin: bin, runner: cmdRunner, config: cfg}
}

func (r *ClaudeCodeRunner) RunnerName() string { return "claudecode" }

func (r *ClaudeCodeRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	args := []string{
		"-p", req.Prompt,
		"--output-format", "json",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
	}

	if mt := resolveInt(req.MaxTurns, r.config.MaxTurnsDefault); mt > 0 {
		args = append(args, "--max-turns", strconv.Itoa(mt))
	}
	if r.config.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", r.config.MaxBudgetUSD))
	}
	if req.OutputSchema != nil {
		args = append(args, "--json-schema", string(req.OutputSchema))
	}
	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = r.config.DefaultAllowedTools
	}
	for _, tool := range tools {
		args = append(args, "--allowedTools", tool)
	}
	if r.config.Model != "" {
		args = append(args, "--model", r.config.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}

	timeout := resolveInt(req.TimeoutSecs, r.config.TimeoutSecsDefault)
	if timeout == 0 {
		timeout = 120
	}

	out, err := r.runner.Run(ctx, req.WorkDir, r.bin, args, timeout)
	if err != nil {
		return AgentResult{}, fmt.Errorf("claudecode: command error: %w", err)
	}
	if out.TimedOut {
		return AgentResult{}, fmt.Errorf("claudecode: timed out after %ds", timeout)
	}
	if out.ExitCode != 0 {
		return AgentResult{}, fmt.Errorf("claudecode: exit %d: %s", out.ExitCode, truncate(out.Stderr, 500))
	}

	return parseSDKResultMessage(out.Stdout)
}

type sdkResultMessage struct {
	Type         string   `json:"type"`
	Subtype      string   `json:"subtype"`
	Result       string   `json:"result"`
	IsError      bool     `json:"is_error"`
	TotalCostUSD float64  `json:"total_cost_usd"`
	NumTurns     int      `json:"num_turns"`
	DurationMs   int      `json:"duration_ms"`
	Errors       []string `json:"errors"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StructuredOutput interface{} `json:"structured_output"`
}

func parseSDKResultMessage(stdout string) (AgentResult, error) {
	var msg sdkResultMessage
	if err := json.Unmarshal([]byte(stdout), &msg); err != nil {
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if len(lines) == 0 {
			return AgentResult{}, fmt.Errorf("claudecode: empty output")
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &msg); err != nil {
			return AgentResult{}, fmt.Errorf("claudecode: parse error: %w", err)
		}
	}
	if msg.Type != "result" {
		return AgentResult{}, fmt.Errorf("claudecode: unexpected message type %q", msg.Type)
	}
	if msg.IsError {
		errMsg := msg.Subtype
		if len(msg.Errors) > 0 {
			errMsg = strings.Join(msg.Errors, "; ")
		}
		return AgentResult{}, fmt.Errorf("claudecode: agent error (%s): %s", msg.Subtype, errMsg)
	}
	return AgentResult{
		Output:     msg.Result,
		Structured: msg.StructuredOutput,
		Usage: AgentUsage{
			InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens,
			CostUSD: msg.TotalCostUSD, NumTurns: msg.NumTurns, DurationMs: msg.DurationMs,
		},
	}, nil
}

func (r *ClaudeCodeRunner) HealthCheck(ctx context.Context) error {
	out, err := r.runner.Run(ctx, ".", r.bin, []string{"--version"}, 10)
	if err != nil {
		return fmt.Errorf("claude binary error: %w", err)
	}
	if out.ExitCode != 0 {
		return fmt.Errorf("claude binary not found or not working at %q", r.bin)
	}
	return nil
}

func (r *ClaudeCodeRunner) Close() error { return nil }

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func resolveInt(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}
```

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestClaudeCode -v
```

---

### Task 7: Copilot Runner (Native Go SDK)

**Files:**
- Create: `internal/agent/copilot.go`
- Create: `internal/agent/copilot_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/copilot_test.go
package agent

import (
	"testing"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func TestCopilotRunner_RunnerName(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{}}
	if r.RunnerName() != "copilot" {
		t.Fatalf("expected 'copilot', got %q", r.RunnerName())
	}
}

func TestCopilotRunner_BuildSessionConfig(t *testing.T) {
	r := &CopilotRunner{
		config: CopilotConfig{
			Model: "gpt-4o", DefaultAllowedTools: []string{"Read", "Glob"}, TimeoutSecsDefault: 180,
		},
	}
	req := AgentRequest{Prompt: "Analyze this", WorkDir: "/tmp/test", AllowedTools: []string{"Read", "Edit", "Bash"}}
	cfg := r.buildSessionConfig(req)

	if cfg.Model != "gpt-4o" {
		t.Fatalf("expected model 'gpt-4o', got %q", cfg.Model)
	}
	if len(cfg.AvailableTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(cfg.AvailableTools))
	}
}

func TestCopilotRunner_BuildSessionConfig_DefaultTools(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{Model: "gpt-4o", DefaultAllowedTools: []string{"Read", "Glob"}}}
	cfg := r.buildSessionConfig(AgentRequest{Prompt: "Analyze", WorkDir: "/tmp"})
	if len(cfg.AvailableTools) != 2 {
		t.Fatalf("expected 2 default tools, got %d", len(cfg.AvailableTools))
	}
}

func TestCopilotRunner_ResolveTimeout(t *testing.T) {
	r := &CopilotRunner{config: CopilotConfig{TimeoutSecsDefault: 180}}
	if d := r.resolveTimeout(AgentRequest{TimeoutSecs: 60}); d != 60*time.Second {
		t.Fatalf("expected 60s, got %v", d)
	}
	if d := r.resolveTimeout(AgentRequest{}); d != 180*time.Second {
		t.Fatalf("expected 180s, got %v", d)
	}
	r.config.TimeoutSecsDefault = 0
	if d := r.resolveTimeout(AgentRequest{}); d != 120*time.Second {
		t.Fatalf("expected 120s default, got %v", d)
	}
}
```

**Step 2: Write the implementation**

```go
// internal/agent/copilot.go
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// CopilotConfig holds configuration for the Copilot SDK runner.
type CopilotConfig struct {
	CLIPath             string
	GitHubToken         string
	Model               string
	DefaultAllowedTools []string
	TimeoutSecsDefault  int
	Provider            *copilot.ProviderConfig // BYOK support
}

// CopilotRunner uses the GitHub Copilot Go SDK (native, no subprocess).
type CopilotRunner struct {
	client *copilot.Client
	config CopilotConfig
}

func NewCopilotRunner(cfg CopilotConfig) (*CopilotRunner, error) {
	opts := &copilot.ClientOptions{}
	if cfg.CLIPath != "" {
		opts.CLIPath = cfg.CLIPath
	}
	if cfg.GitHubToken != "" {
		opts.GitHubToken = cfg.GitHubToken
	}
	client := copilot.NewClient(opts)
	if err := client.Start(); err != nil {
		return nil, fmt.Errorf("copilot: failed to start client: %w", err)
	}
	return &CopilotRunner{client: client, config: cfg}, nil
}

func (r *CopilotRunner) RunnerName() string { return "copilot" }

func (r *CopilotRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	sessionCfg := r.buildSessionConfig(req)
	session, err := r.client.CreateSession(ctx, sessionCfg)
	if err != nil {
		return AgentResult{}, fmt.Errorf("copilot: create session: %w", err)
	}
	defer session.Destroy()

	var usage AgentUsage
	var mu sync.Mutex
	unsubscribe := session.On(func(event copilot.SessionEvent) {
		if event.Type == copilot.AssistantUsage {
			mu.Lock()
			defer mu.Unlock()
			if event.Data.InputTokens != nil {
				usage.InputTokens += int(*event.Data.InputTokens)
			}
			if event.Data.OutputTokens != nil {
				usage.OutputTokens += int(*event.Data.OutputTokens)
			}
		}
	})
	defer unsubscribe()

	taskCtx, cancel := context.WithTimeout(ctx, r.resolveTimeout(req))
	defer cancel()

	result, err := session.SendAndWait(taskCtx, copilot.MessageOptions{Prompt: req.Prompt})
	if err != nil {
		return AgentResult{}, fmt.Errorf("copilot: %w", err)
	}

	output := ""
	if result != nil && result.Data.Content != nil {
		output = *result.Data.Content
	}
	return AgentResult{Output: output, Usage: usage}, nil
}

func (r *CopilotRunner) HealthCheck(ctx context.Context) error {
	_, err := r.client.Ping(ctx, "foreman-health")
	return err
}

func (r *CopilotRunner) Close() error {
	if r.client != nil {
		return r.client.Stop()
	}
	return nil
}

func (r *CopilotRunner) buildSessionConfig(req AgentRequest) *copilot.SessionConfig {
	cfg := &copilot.SessionConfig{
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
		Model:               r.config.Model,
		WorkingDirectory:    req.WorkDir,
		InfiniteSessions:    &copilot.InfiniteSessionConfig{Enabled: copilot.Bool(false)},
	}
	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = r.config.DefaultAllowedTools
	}
	cfg.AvailableTools = tools
	if r.config.Provider != nil {
		cfg.Provider = r.config.Provider
	}
	if req.SystemPrompt != "" {
		cfg.SystemMessage = &copilot.SystemMessageConfig{Mode: "append", Content: req.SystemPrompt}
	}
	return cfg
}

func (r *CopilotRunner) resolveTimeout(req AgentRequest) time.Duration {
	if req.TimeoutSecs > 0 {
		return time.Duration(req.TimeoutSecs) * time.Second
	}
	if r.config.TimeoutSecsDefault > 0 {
		return time.Duration(r.config.TimeoutSecsDefault) * time.Second
	}
	return 120 * time.Second
}
```

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestCopilot -v
```

---

### Task 8: Factory and Config Types

**Files:**
- Create: `internal/agent/factory.go`
- Create: `internal/agent/factory_test.go`
- Modify: `internal/models/config.go` — add `Skills` config section
- Modify: `internal/config/config.go` — add defaults

**Step 1: Write the failing test**

```go
// internal/agent/factory_test.go
package agent

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestNewAgentRunner_Builtin(t *testing.T) {
	cfg := models.AgentRunnerConfig{Provider: "builtin"}
	runner, err := NewAgentRunner(cfg, nil, &simpleMockLLM{response: "ok"}, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "builtin" {
		t.Fatalf("expected 'builtin', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_EmptyDefaultsToBuiltin(t *testing.T) {
	cfg := models.AgentRunnerConfig{Provider: ""}
	runner, err := NewAgentRunner(cfg, nil, &simpleMockLLM{response: "ok"}, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "builtin" {
		t.Fatalf("expected 'builtin', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_ClaudeCode(t *testing.T) {
	cfg := models.AgentRunnerConfig{
		Provider: "claudecode",
		ClaudeCode: models.ClaudeCodeRunnerConfig{
			Bin: "/usr/local/bin/claude", DefaultAllowedTools: []string{"Read", "Edit"}, TimeoutSecsDefault: 180,
		},
	}
	runner, err := NewAgentRunner(cfg, &mockCmdRunner{exitCode: 0}, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.RunnerName() != "claudecode" {
		t.Fatalf("expected 'claudecode', got %q", runner.RunnerName())
	}
}

func TestNewAgentRunner_Unknown(t *testing.T) {
	_, err := NewAgentRunner(models.AgentRunnerConfig{Provider: "unknown"}, nil, nil, "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
```

**Step 2: Write the factory**

```go
// internal/agent/factory.go
package agent

import (
	"fmt"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

func NewAgentRunner(
	cfg models.AgentRunnerConfig,
	cmdRunner runner.CommandRunner,
	llmProvider llm.LlmProvider,
	agentModel string,
) (AgentRunner, error) {
	switch cfg.Provider {
	case "builtin", "":
		return NewBuiltinRunner(llmProvider, agentModel, BuiltinConfig{
			MaxTurnsDefault:     cfg.MaxTurnsDefault,
			DefaultAllowedTools: cfg.Builtin.DefaultAllowedTools,
		}), nil

	case "claudecode":
		c := cfg.ClaudeCode
		return NewClaudeCodeRunner(cmdRunner, ClaudeCodeConfig{
			Bin: c.Bin, DefaultAllowedTools: c.DefaultAllowedTools,
			MaxTurnsDefault: c.MaxTurnsDefault, TimeoutSecsDefault: c.TimeoutSecsDefault,
			MaxBudgetUSD: c.MaxBudgetUSD, Model: c.Model,
		}), nil

	case "copilot":
		c := cfg.Copilot
		return NewCopilotRunner(CopilotConfig{
			CLIPath: c.CLIPath, GitHubToken: c.GitHubToken, Model: c.Model,
			DefaultAllowedTools: c.DefaultAllowedTools, TimeoutSecsDefault: c.TimeoutSecsDefault,
		})

	default:
		return nil, fmt.Errorf("unknown agent runner provider %q — valid: builtin, claudecode, copilot", cfg.Provider)
	}
}
```

**Step 3: Add config types to `models/config.go`**

```go
// Add to Config struct:
Skills SkillsConfig `mapstructure:"skills"`

// New types:
type SkillsConfig struct {
	AgentRunner AgentRunnerConfig `mapstructure:"agent_runner"`
}

type AgentRunnerConfig struct {
	Provider            string                   `mapstructure:"provider"`
	MaxCostPerTicketUSD float64                  `mapstructure:"max_cost_per_ticket_usd"`
	MaxTurnsDefault     int                      `mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int                      `mapstructure:"timeout_secs_default"`
	Builtin             BuiltinRunnerConfig      `mapstructure:"builtin"`
	ClaudeCode          ClaudeCodeRunnerConfig   `mapstructure:"claudecode"`
	Copilot             CopilotRunnerConfig      `mapstructure:"copilot"`
}

type BuiltinRunnerConfig struct {
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
}

type ClaudeCodeRunnerConfig struct {
	Bin                 string   `mapstructure:"bin"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	MaxTurnsDefault     int      `mapstructure:"max_turns_default"`
	TimeoutSecsDefault  int      `mapstructure:"timeout_secs_default"`
	MaxBudgetUSD        float64  `mapstructure:"max_budget_usd"`
	Model               string   `mapstructure:"model"`
}

type CopilotRunnerConfig struct {
	CLIPath             string   `mapstructure:"cli_path"`
	GitHubToken         string   `mapstructure:"github_token"`
	Model               string   `mapstructure:"model"`
	DefaultAllowedTools []string `mapstructure:"default_allowed_tools"`
	TimeoutSecsDefault  int      `mapstructure:"timeout_secs_default"`
}
```

**Step 4: Add defaults to `config/config.go`**

```go
v.SetDefault("skills.agent_runner.provider", "builtin")
v.SetDefault("skills.agent_runner.max_cost_per_ticket_usd", 2.0)
v.SetDefault("skills.agent_runner.max_turns_default", 10)
v.SetDefault("skills.agent_runner.timeout_secs_default", 120)
v.SetDefault("skills.agent_runner.builtin.default_allowed_tools", []string{"Read", "Glob", "Grep"})
v.SetDefault("skills.agent_runner.claudecode.bin", "claude")
v.SetDefault("skills.agent_runner.claudecode.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
v.SetDefault("skills.agent_runner.claudecode.max_turns_default", 10)
v.SetDefault("skills.agent_runner.claudecode.timeout_secs_default", 180)
v.SetDefault("skills.agent_runner.copilot.cli_path", "copilot")
v.SetDefault("skills.agent_runner.copilot.model", "gpt-4o")
v.SetDefault("skills.agent_runner.copilot.default_allowed_tools", []string{"Read", "Edit", "Glob", "Grep", "Bash"})
v.SetDefault("skills.agent_runner.copilot.timeout_secs_default", 180)
```

**Step 5: Add env var expansion**

In `expandEnvVars()`:
```go
cfg.Skills.AgentRunner.Copilot.GitHubToken = expandEnv(cfg.Skills.AgentRunner.Copilot.GitHubToken)
```

**Step 6: Verify**
```bash
go test ./internal/agent/ -run TestNewAgentRunner -v
go test ./internal/config/ -v
```

---

### Task 9: `agentsdk` Step Type in Skills Engine

**Files:**
- Modify: `internal/skills/loader.go` — add `agentsdk` to valid step types, add new fields to `SkillStep`
- Modify: `internal/skills/engine.go` — add `agentRunner` field and `executeAgentSDK` method
- Modify: `internal/skills/engine_test.go` — test the new step type

**Step 1: Update loader.go**

Add `"agentsdk": true` to `validStepTypes`. Add fields to `SkillStep`:
```go
AllowedTools []string `yaml:"allowed_tools,omitempty"`
MaxTurns     int      `yaml:"max_turns,omitempty"`
OutputKey    string   `yaml:"output_key,omitempty"`
TimeoutSecs  int      `yaml:"timeout_secs,omitempty"`
```

**Step 2: Update engine.go**

Add `agentRunner AgentRunner` field to `Engine`. Add `SetAgentRunner()` method. Add `case "agentsdk"` to `executeStep`. Implement `executeAgentSDK`:

```go
func (e *Engine) executeAgentSDK(ctx context.Context, step SkillStep, _ *SkillContext) (*StepResult, error) {
	if e.agentRunner == nil {
		return nil, fmt.Errorf("agentsdk step '%s': no agent runner configured", step.ID)
	}
	result, err := e.agentRunner.Run(ctx, agent.AgentRequest{
		Prompt: step.Content, WorkDir: e.workDir,
		AllowedTools: step.AllowedTools, MaxTurns: step.MaxTurns, TimeoutSecs: step.TimeoutSecs,
	})
	if err != nil {
		return nil, fmt.Errorf("agentsdk step '%s': %w", step.ID, err)
	}
	return &StepResult{Output: result.Output}, nil
}
```

**Step 3: Verify**
```bash
go test ./internal/skills/ -v
```

---

### Task 10: Mock Runner, Doctor Check, Example TOML, Example Skill

**Files:**
- Create: `internal/agent/mock.go`
- Modify: CLI doctor command — add agent runner health check
- Modify: `foreman.example.toml` — add skills.agent_runner section
- Create: `skills/community/security-scan.yml`

**Mock runner:**
```go
// internal/agent/mock.go
package agent

import "context"

type MockRunner struct {
	RunFunc         func(ctx context.Context, req AgentRequest) (AgentResult, error)
	HealthCheckFunc func(ctx context.Context) error
	Name            string
}

func (m *MockRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	if m.RunFunc != nil { return m.RunFunc(ctx, req) }
	return AgentResult{Output: "mock output"}, nil
}
func (m *MockRunner) HealthCheck(ctx context.Context) error {
	if m.HealthCheckFunc != nil { return m.HealthCheckFunc(ctx) }
	return nil
}
func (m *MockRunner) RunnerName() string {
	if m.Name != "" { return m.Name }
	return "mock"
}
func (m *MockRunner) Close() error { return nil }
```

**Example TOML section:**
```toml
[skills.agent_runner]
provider = "builtin"
max_cost_per_ticket_usd = 2.00
max_turns_default = 10
timeout_secs_default = 120

[skills.agent_runner.builtin]
default_allowed_tools = ["Read", "Glob", "Grep"]

[skills.agent_runner.claudecode]
bin = "claude"
default_allowed_tools = ["Read", "Edit", "Glob", "Grep", "Bash"]
max_turns_default = 10
timeout_secs_default = 180
max_budget_usd = 2.00

[skills.agent_runner.copilot]
cli_path = "copilot"
github_token = "${GITHUB_TOKEN}"
model = "gpt-4o"
default_allowed_tools = ["Read", "Edit", "Glob", "Grep", "Bash"]
timeout_secs_default = 180
```

**Example skill:**
```yaml
# skills/community/security-scan.yml
id: security-scan
description: Review code changes for security vulnerabilities
trigger: post_lint
steps:
  - id: audit
    type: agentsdk
    content: |
      Review the code in the current working directory for security issues.
      Focus on: injection vulnerabilities, hardcoded secrets, insecure defaults.
      Return JSON: {"severity": "low|medium|high|critical", "findings": [...]}
    allowed_tools: [Read, Glob, Grep]
    max_turns: 6
    output_key: result
  - id: write-report
    type: file_write
    path: .foreman/security-report.json
    content: "{{ .Steps.audit.result }}"
    mode: overwrite
```

**Verify:**
```bash
go vet ./internal/agent/
go build ./...
```

---

## File Structure Summary

```
internal/
  agent/
    runner.go          # AgentRunner interface + types
    tools.go           # Built-in Read/Glob/Grep tool implementations
    builtin.go         # Multi-turn tool-use loop against LlmProvider
    claudecode.go      # CLI subprocess: claude -p --output-format json
    copilot.go         # Native Go SDK: github.com/github/copilot-sdk/go
    factory.go         # NewAgentRunner() dispatch
    mock.go            # MockRunner for unit tests
    *_test.go          # Tests for each
  llm/
    provider.go        # Extended: ToolDef, ToolCall, ToolResult, Message types
    anthropic.go       # Extended: tool_use handling, multi-turn messages
  models/
    pipeline.go        # Extended: StopReasonToolUse, tool-use types
    config.go          # Extended: SkillsConfig, AgentRunnerConfig
  skills/
    loader.go          # Extended: agentsdk step type
    engine.go          # Extended: agentRunner field, executeAgentSDK method
```

## Verification Checklist

- [ ] `go test ./internal/agent/ -v` — all agent tests pass
- [ ] `go test ./internal/llm/ -v` — tool-use in anthropic provider works
- [ ] `go test ./internal/skills/ -v` — agentsdk step type works
- [ ] `go test ./internal/config/ -v` — config loads agent runner defaults
- [ ] `go vet ./...` — no issues
- [ ] `go build ./...` — compiles clean
- [ ] `foreman doctor` — shows agent runner status
