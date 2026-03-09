# OpenCode Patterns Adoption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Adopt proven patterns from OpenCode into Foreman to improve robustness, undo/rollback, structured output, command system, and agent loop safety.

**Architecture:** Each adoption is a self-contained module in `internal/` with clear interfaces. Snapshot uses a separate git repo per worktree. Structured output wraps the existing LLM provider. Command registry sits alongside the existing skills engine. All changes are additive — no existing functionality is removed.

**Tech Stack:** Go 1.25+, go-git (for snapshot), zerolog, existing interfaces

**Depends on:** This plan can be executed independently from the Unified Prompt Registry plan. They are complementary but not sequential.

---

### Task 1: Snapshot system — core tracking and diff

**Files:**
- Create: `internal/snapshot/snapshot.go`
- Create: `internal/snapshot/snapshot_test.go`

**Step 1: Write the failing test**

```go
// internal/snapshot/snapshot_test.go
package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackAndPatch(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()

	s := New(workDir, dataDir)

	// Create initial file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main"), 0o644))

	// Track initial state
	hash, err := s.Track()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Modify file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n\nfunc main() {}"), 0o644))

	// Add new file
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "util.go"), []byte("package main"), 0o644))

	// Get patch — should show both files changed
	patch, err := s.Patch(hash)
	require.NoError(t, err)
	assert.Contains(t, patch.Files, filepath.Join(workDir, "main.go"))
	assert.Contains(t, patch.Files, filepath.Join(workDir, "util.go"))
}

func TestDiff(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()
	s := New(workDir, dataDir)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.go"), []byte("hello"), 0o644))
	hash, err := s.Track()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.go"), []byte("world"), 0o644))

	diff, err := s.Diff(hash)
	require.NoError(t, err)
	assert.Contains(t, diff, "hello")
	assert.Contains(t, diff, "world")
}

func TestRestore(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()
	s := New(workDir, dataDir)

	filePath := filepath.Join(workDir, "main.go")
	require.NoError(t, os.WriteFile(filePath, []byte("original"), 0o644))

	hash, err := s.Track()
	require.NoError(t, err)

	// Modify
	require.NoError(t, os.WriteFile(filePath, []byte("modified"), 0o644))

	// Restore
	err = s.Restore(hash)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/snapshot/ -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/snapshot/snapshot.go
package snapshot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Patch describes the files changed since a tracked state.
type Patch struct {
	Hash  string
	Files []string
}

// Snapshot manages file-state tracking using a separate git repo.
// This enables undo/rollback within pipeline stages without polluting
// the user's git history.
type Snapshot struct {
	workDir string
	gitDir  string
}

// New creates a Snapshot tracker. dataDir is where the separate git repo lives.
func New(workDir, dataDir string) *Snapshot {
	gitDir := filepath.Join(dataDir, "snapshot")
	return &Snapshot{workDir: workDir, gitDir: gitDir}
}

// Track snapshots the current working tree state and returns a tree hash.
func (s *Snapshot) Track() (string, error) {
	if err := s.ensureInit(); err != nil {
		return "", err
	}
	if err := s.add(); err != nil {
		return "", err
	}
	out, err := s.git("write-tree")
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Patch returns the list of files changed since the given hash.
func (s *Snapshot) Patch(hash string) (*Patch, error) {
	if err := s.add(); err != nil {
		return nil, err
	}
	out, err := s.git("diff", "--name-only", hash, "--", ".")
	if err != nil {
		return &Patch{Hash: hash}, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, filepath.Join(s.workDir, line))
		}
	}
	return &Patch{Hash: hash, Files: files}, nil
}

// Diff returns the unified diff between current state and the given hash.
func (s *Snapshot) Diff(hash string) (string, error) {
	if err := s.add(); err != nil {
		return "", err
	}
	out, err := s.git("diff", "--no-ext-diff", hash, "--", ".")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Restore reverts the working tree to the given snapshot hash.
func (s *Snapshot) Restore(hash string) error {
	if _, err := s.git("read-tree", hash); err != nil {
		return fmt.Errorf("read-tree: %w", err)
	}
	if _, err := s.git("checkout-index", "-a", "-f"); err != nil {
		return fmt.Errorf("checkout-index: %w", err)
	}
	return nil
}

func (s *Snapshot) ensureInit() error {
	cmd := exec.Command("git", "init", "--bare", s.gitDir)
	cmd.Dir = s.workDir
	return cmd.Run()
}

func (s *Snapshot) add() error {
	_, err := s.git("add", ".")
	return err
}

func (s *Snapshot) git(args ...string) (string, error) {
	fullArgs := append([]string{
		"--git-dir", s.gitDir,
		"--work-tree", s.workDir,
	}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, string(out))
	}
	return string(out), nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/snapshot/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/snapshot/
git commit -m "feat(snapshot): add file-state tracking system for undo/rollback"
```

---

### Task 2: Snapshot integration — wire into pipeline task runner

**Files:**
- Modify: `internal/pipeline/task_runner.go` — snapshot before/after implementation
- Modify: `internal/daemon/orchestrator.go` — create Snapshot per worktree
- Test: `internal/pipeline/task_runner_test.go`

**Step 1: Write the failing test**

```go
func TestTaskRunnerSnapshotRollback(t *testing.T) {
	// Test that when implementation fails, we can roll back to pre-implementation state
	// ... setup mock task runner with snapshot ...
	// Verify: after rollback, files match pre-implementation snapshot
}
```

**Step 2: Add Snapshot to TaskRunner**

In `task_runner.go`, add `*snapshot.Snapshot` field. Before calling implementer:
```go
preImplHash, _ := tr.snapshot.Track()
```

After implementation failure (max retries exceeded):
```go
if tr.snapshot != nil {
    _ = tr.snapshot.Restore(preImplHash)
}
```

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestTaskRunner -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/pipeline/task_runner.go internal/pipeline/task_runner_test.go internal/daemon/orchestrator.go
git commit -m "feat(pipeline): integrate snapshot for pre-implementation rollback"
```

---

### Task 3: Structured output — add schema-validated output to builtin runner

**Files:**
- Create: `internal/agent/structured.go`
- Create: `internal/agent/structured_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/structured_test.go
package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStructuredOutputTool(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["APPROVED", "REJECTED"]},
			"issues": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["status"]
	}`)

	tool := BuildStructuredOutputTool(schema)
	assert.Equal(t, "structured_output", tool.Name)
	assert.NotNil(t, tool.InputSchema)
}

func TestValidateStructuredOutput(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string"}
		},
		"required": ["status"]
	}`)

	// Valid output
	err := ValidateStructuredOutput(schema, `{"status": "APPROVED"}`)
	require.NoError(t, err)

	// Invalid output — not JSON
	err = ValidateStructuredOutput(schema, "not json")
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestStructured -v`
Expected: FAIL — functions undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/structured.go
package agent

import (
	"encoding/json"
	"fmt"
)

// StructuredOutputTool is a tool that forces the LLM to produce
// JSON output matching a given schema.
type StructuredOutputTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// BuildStructuredOutputTool creates a tool definition that instructs the LLM
// to output structured JSON matching the given schema.
func BuildStructuredOutputTool(schema json.RawMessage) StructuredOutputTool {
	return StructuredOutputTool{
		Name:        "structured_output",
		Description: "You MUST use this tool to provide your response. Output your analysis as structured JSON matching the schema.",
		InputSchema: schema,
	}
}

// ValidateStructuredOutput checks if output is valid JSON.
// Schema validation is best-effort — we verify JSON validity.
func ValidateStructuredOutput(schema json.RawMessage, output string) error {
	if !json.Valid([]byte(output)) {
		return fmt.Errorf("output is not valid JSON")
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestStructured -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/structured.go internal/agent/structured_test.go
git commit -m "feat(agent): add structured output tool for schema-validated LLM responses"
```

---

### Task 4: Structured output — integrate into builtin runner

**Files:**
- Modify: `internal/agent/builtin.go` — inject structured_output tool when OutputSchema is set
- Test: `internal/agent/builtin_test.go`

**Step 1: Write the failing test**

```go
func TestBuiltinRunnerStructuredOutput(t *testing.T) {
	// Test that when AgentRequest.OutputSchema is set:
	// 1. structured_output tool is injected
	// 2. System prompt includes instruction to use it
	// 3. Output is extracted from tool call result
}
```

**Step 2: Modify Run() in builtin.go**

When `req.OutputSchema != nil`:
- Inject `structured_output` tool into available tools
- Append to system prompt: "You MUST use the structured_output tool to provide your final answer."
- When agent calls the tool, capture the input as the structured output
- Validate JSON and return as `AgentResult.Structured`

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestBuiltinRunner -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/builtin.go internal/agent/builtin_test.go
git commit -m "feat(agent): integrate structured output into builtin runner"
```

---

### Task 5: Structured output — use for plan parsing

**Files:**
- Modify: `internal/pipeline/planner.go` — use structured output for YAML plan parsing
- Modify: `internal/pipeline/yaml_parser.go` — add JSON-based parsing path
- Test: `internal/pipeline/planner_test.go`

**Step 1: Define plan output schema**

```go
var planOutputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tasks": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"title": {"type": "string"},
					"description": {"type": "string"},
					"acceptance_criteria": {"type": "array", "items": {"type": "string"}},
					"files_to_read": {"type": "array", "items": {"type": "string"}},
					"files_to_modify": {"type": "array", "items": {"type": "string"}},
					"test_assertions": {"type": "array", "items": {"type": "string"}},
					"estimated_complexity": {"type": "string", "enum": ["simple", "medium", "complex"]},
					"depends_on": {"type": "array", "items": {"type": "string"}}
				},
				"required": ["title", "description", "acceptance_criteria"]
			}
		}
	},
	"required": ["tasks"]
}`)
```

**Step 2: Modify planner to use structured output when using builtin runner**

When the agent runner supports structured output (builtin), set `OutputSchema` on the request. Fall back to YAML parsing for other runners.

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestPlanner -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/pipeline/planner.go internal/pipeline/yaml_parser.go internal/pipeline/planner_test.go
git commit -m "feat(pipeline): use structured output for reliable plan parsing"
```

---

### Task 6: Doom loop detection — improve agent loop safety

**Files:**
- Create: `internal/agent/doomloop.go`
- Create: `internal/agent/doomloop_test.go`
- Modify: `internal/agent/builtin.go` — integrate detection

**Step 1: Write the failing test**

```go
// internal/agent/doomloop_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoomLoopDetector(t *testing.T) {
	d := NewDoomLoopDetector(3)

	// First call — no loop
	assert.False(t, d.Check("Read", `{"path": "main.go"}`))

	// Same call again — no loop yet (need 3)
	assert.False(t, d.Check("Read", `{"path": "main.go"}`))

	// Third identical call — DOOM LOOP
	assert.True(t, d.Check("Read", `{"path": "main.go"}`))

	// Different call resets
	assert.False(t, d.Check("Write", `{"path": "main.go"}`))
}

func TestDoomLoopDetector_DifferentInputs(t *testing.T) {
	d := NewDoomLoopDetector(3)

	assert.False(t, d.Check("Read", `{"path": "a.go"}`))
	assert.False(t, d.Check("Read", `{"path": "b.go"}`))
	assert.False(t, d.Check("Read", `{"path": "c.go"}`))
	// All different inputs — no loop
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestDoomLoop -v`
Expected: FAIL — undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/doomloop.go
package agent

import (
	"crypto/sha256"
	"fmt"
)

// DoomLoopDetector tracks repeated identical tool calls to prevent infinite loops.
type DoomLoopDetector struct {
	history   []string
	threshold int
}

// NewDoomLoopDetector creates a detector that triggers after `threshold` consecutive
// identical tool calls.
func NewDoomLoopDetector(threshold int) *DoomLoopDetector {
	return &DoomLoopDetector{threshold: threshold}
}

// Check returns true if the same tool+input has been called `threshold` times
// consecutively.
func (d *DoomLoopDetector) Check(toolName, input string) bool {
	key := hash(toolName, input)
	d.history = append(d.history, key)

	// Only check the last `threshold` entries
	if len(d.history) < d.threshold {
		return false
	}

	last := d.history[len(d.history)-d.threshold:]
	for _, h := range last {
		if h != key {
			return false
		}
	}
	return true
}

// Reset clears the history.
func (d *DoomLoopDetector) Reset() {
	d.history = nil
}

func hash(tool, input string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", tool, input)))
	return fmt.Sprintf("%x", h[:8])
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestDoomLoop -v`
Expected: PASS

**Step 5: Integrate into builtin runner**

In `builtin.go`, create `DoomLoopDetector` at start of `Run()`. Before executing each tool call, check. If doom loop detected, inject a user message: "You are repeating the same action. Stop and reconsider your approach."

**Step 6: Commit**

```bash
git add internal/agent/doomloop.go internal/agent/doomloop_test.go internal/agent/builtin.go
git commit -m "feat(agent): add doom loop detection to prevent infinite tool call repetition"
```

---

### Task 7: Command registry — create command abstraction

**Files:**
- Create: `internal/command/registry.go`
- Create: `internal/command/registry_test.go`

**Step 1: Write the failing test**

```go
// internal/command/registry_test.go
package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryAddAndGet(t *testing.T) {
	r := NewRegistry()

	r.Register(Command{
		Name:        "review",
		Description: "Review changes",
		Template:    "Review the following diff:\n$ARGUMENTS",
		Subtask:     true,
	})

	cmd, err := r.Get("review")
	require.NoError(t, err)
	assert.Equal(t, "review", cmd.Name)
	assert.True(t, cmd.Subtask)
}

func TestRegistryRender(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:     "review",
		Template: "Review changes for $1 in branch $2",
	})

	result, err := r.Render("review", "auth module", "feature/auth")
	require.NoError(t, err)
	assert.Contains(t, result, "auth module")
	assert.Contains(t, result, "feature/auth")
}

func TestRegistryRenderArguments(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:     "explain",
		Template: "Explain the following:\n$ARGUMENTS",
	})

	result, err := r.Render("explain", "how does the pipeline work?")
	require.NoError(t, err)
	assert.Contains(t, result, "how does the pipeline work?")
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "a", Description: "aaa", Template: "a"})
	r.Register(Command{Name: "b", Description: "bbb", Template: "b"})

	cmds := r.List()
	assert.Len(t, cmds, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/command/ -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/command/registry.go
package command

import (
	"fmt"
	"sort"
	"strings"
)

// Command is a user-invokable action.
type Command struct {
	Name        string
	Description string
	Template    string
	Agent       string // optional: run with specific agent
	Model       string // optional: override model
	Subtask     bool   // run as subtask (background)
	Source      string // "builtin", "config", "skill"
}

// Registry holds all available commands.
type Registry struct {
	commands map[string]Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds or replaces a command.
func (r *Registry) Register(cmd Command) {
	r.commands[cmd.Name] = cmd
}

// Get retrieves a command by name.
func (r *Registry) Get(name string) (Command, error) {
	cmd, ok := r.commands[name]
	if !ok {
		return Command{}, fmt.Errorf("command %q not found", name)
	}
	return cmd, nil
}

// List returns all commands sorted by name.
func (r *Registry) List() []Command {
	result := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		result = append(result, cmd)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Render substitutes arguments into a command template.
// $ARGUMENTS is replaced with all args joined by space.
// $1, $2, etc. are replaced with positional args.
func (r *Registry) Render(name string, args ...string) (string, error) {
	cmd, err := r.Get(name)
	if err != nil {
		return "", err
	}

	result := cmd.Template

	// Replace positional args
	for i, arg := range args {
		result = strings.ReplaceAll(result, fmt.Sprintf("$%d", i+1), arg)
	}

	// Replace $ARGUMENTS with all args
	result = strings.ReplaceAll(result, "$ARGUMENTS", strings.Join(args, " "))

	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/command/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/
git commit -m "feat(command): add command registry with template rendering"
```

---

### Task 8: Command registry — wire into dashboard API

**Files:**
- Modify: `internal/dashboard/api.go` — add GET/POST `/api/commands` endpoints
- Modify: `internal/dashboard/server.go` — inject command registry
- Test: `internal/dashboard/api_test.go`

**Step 1: Add API endpoints**

```go
// GET /api/commands — list all commands
// POST /api/commands/:name — execute a command with args
```

**Step 2: Load commands from config and skills**

At startup, populate registry from:
- `foreman.toml [commands]` section (new config)
- Prompt registry COMMAND.md files (from Task Plan 1)
- Skill IDs (as implicit commands)

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/dashboard/ internal/command/
git commit -m "feat(dashboard): expose commands via REST API"
```

---

### Task 9: Enhanced compaction — pruning-first approach

**Files:**
- Modify: `internal/agent/builtin.go` — add pruning phase before summarization
- Create: `internal/agent/compaction.go`
- Create: `internal/agent/compaction_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/compaction_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/canhta/foreman/internal/models"
)

func TestPruneOldToolOutputs(t *testing.T) {
	messages := []models.Message{
		{Role: "user", Content: "implement auth"},
		{Role: "assistant", Content: "I'll read the file", ToolCalls: []models.ToolCall{{Name: "Read"}}},
		{Role: "tool", Content: strings.Repeat("x", 10000)}, // large tool output
		{Role: "assistant", Content: "Now I'll edit"},
		{Role: "user", Content: "looks good"},
		{Role: "assistant", Content: "I'll read another file", ToolCalls: []models.ToolCall{{Name: "Read"}}},
		{Role: "tool", Content: strings.Repeat("y", 5000)}, // recent tool output
	}

	pruned := PruneOldToolOutputs(messages, 8000)

	// Old tool output should be truncated, recent one kept
	assert.Less(t, len(pruned[2].Content), 10000)
	assert.Equal(t, len(pruned[6].Content), 5000) // recent kept
}
```

**Step 2: Implement pruning**

```go
// internal/agent/compaction.go
package agent

// PruneOldToolOutputs truncates old tool outputs to free tokens
// while preserving recent ones. Works backwards from the most recent
// messages, protecting the last `protectTokens` worth of content.
func PruneOldToolOutputs(messages []models.Message, protectTokens int) []models.Message {
	// ... implementation
}
```

**Step 3: Integrate into builtin runner's compaction logic**

In `builtin.go`, before the existing summarization phase, run pruning first. Only proceed to summarization if pruning didn't free enough tokens.

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/agent/ -run TestPrune -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/compaction.go internal/agent/compaction_test.go internal/agent/builtin.go
git commit -m "feat(agent): add pruning-first compaction to reduce token usage before summarization"
```

---

### Task 10: Worktree lifecycle — add reset and start commands

**Files:**
- Modify: `internal/git/git.go` — add `ResetWorktree()` and `CleanWorktree()` methods
- Modify: `internal/git/native.go` — implement for native git
- Test: `internal/git/native_test.go`

**Step 1: Add interface methods**

```go
// In git.GitProvider:
ResetWorktree(ctx context.Context, worktreePath, targetRef string) error
CleanWorktree(ctx context.Context, worktreePath string) error
```

**Step 2: Implement**

```go
func (n *NativeGit) ResetWorktree(ctx context.Context, path, ref string) error {
	_, err := n.runGit(ctx, path, "reset", "--hard", ref)
	if err != nil {
		return err
	}
	_, err = n.runGit(ctx, path, "clean", "-ffdx")
	return err
}
```

**Step 3: Add start command support**

In `internal/daemon/orchestrator.go`, after creating worktree, run configured start command from `foreman.toml`:
```toml
[worktree]
start_command = "npm install && npm run build"
```

**Step 4: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/git/ -run TestResetWorktree -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/ internal/daemon/ internal/models/
git commit -m "feat(git): add worktree reset, clean, and start command support"
```

---

### Task 11: Typed event bus — generalize telemetry

**Files:**
- Create: `internal/bus/bus.go`
- Create: `internal/bus/bus_test.go`

**Step 1: Write the failing test**

```go
// internal/bus/bus_test.go
package bus

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBusPubSub(t *testing.T) {
	b := New()

	var received []string
	var mu sync.Mutex

	b.Subscribe("task.completed", func(data any) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, data.(string))
	})

	b.Publish("task.completed", "task-1")
	b.Publish("task.completed", "task-2")
	b.Publish("other.event", "ignored")

	// Non-blocking publish — give goroutines time
	b.Drain()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"task-1", "task-2"}, received)
}

func TestBusSubscribeAll(t *testing.T) {
	b := New()

	var events []string
	var mu sync.Mutex

	b.SubscribeAll(func(topic string, data any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, topic)
	})

	b.Publish("a", nil)
	b.Publish("b", nil)
	b.Drain()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"a", "b"}, events)
}
```

**Step 2: Implement**

```go
// internal/bus/bus.go
package bus

import "sync"

type Handler func(data any)
type GlobalHandler func(topic string, data any)

type Bus struct {
	mu       sync.RWMutex
	subs     map[string][]Handler
	global   []GlobalHandler
	wg       sync.WaitGroup
}

func New() *Bus {
	return &Bus{subs: make(map[string][]Handler)}
}

func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], h)
}

func (b *Bus) SubscribeAll(h GlobalHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.global = append(b.global, h)
}

func (b *Bus) Publish(topic string, data any) {
	b.mu.RLock()
	handlers := b.subs[topic]
	globals := b.global
	b.mu.RUnlock()

	for _, h := range handlers {
		h := h
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			h(data)
		}()
	}
	for _, g := range globals {
		g := g
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			g(topic, data)
		}()
	}
}

func (b *Bus) Drain() {
	b.wg.Wait()
}
```

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/bus/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/bus/
git commit -m "feat(bus): add typed event bus for pipeline observability"
```

---

### Task 12: Hierarchical instruction loading

**Files:**
- Modify: `internal/context/rules.go` — walk directory tree for context files
- Test: `internal/context/rules_test.go`

**Step 1: Enhance rules loading**

Currently `loadForemanContextFromDir()` only checks the workdir root. Adopt OpenCode's pattern: walk up from the current file's directory to workdir root, collecting `AGENTS.md`, `.foreman-rules.md`, `.foreman/context.md` at each level.

```go
// WalkContextFiles walks from startDir up to workDir, collecting context files.
func WalkContextFiles(startDir, workDir string) []string {
	var files []string
	dir := startDir
	for {
		for _, name := range []string{"AGENTS.md", ".foreman-rules.md", ".foreman/context.md"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				files = append(files, path)
			}
		}
		if dir == workDir || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
	}
	return files
}
```

**Step 2: Test with nested directories**

Create a test with context files at multiple levels, verify all are found.

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/context/ -run TestWalkContext -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/context/
git commit -m "feat(context): add hierarchical instruction loading from directory tree"
```

---

### Task 13: Final verification

**Step 1: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1`
Expected: All PASS

**Step 2: Build**

Run: `cd /Users/canh/Projects/Indies/Foreman && make build`
Expected: Clean build

**Step 3: Update AGENTS.md**

Document new packages: `snapshot`, `command`, `bus`, and the enhanced agent features (doom loop, compaction, structured output).

**Step 4: Commit**

```bash
git add -A
git commit -m "docs: update AGENTS.md with new subsystems from OpenCode patterns adoption"
```
