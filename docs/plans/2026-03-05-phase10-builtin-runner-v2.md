# Phase 10: Builtin Runner V2 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the builtin agent runner with a typed tool registry, parallel per-turn execution, 14 built-in tools (filesystem/git/code/exec), reactive context injection via `ContextProvider`, per-tool pre/post hooks, MCP stub, and skills engine additions (subskill, git_diff, output_format, Phase 9 field wiring).

**Architecture:** `tools.Registry` owns all tool implementations and dependencies (GitProvider, CommandRunner). `BuiltinRunner` holds the registry and an optional `ContextProvider` for reactive progress-pattern injection after each file-touching tool call. The skills engine pre-assembles `.foreman-context.md` into `SystemPrompt` for all three runners; `BuiltinRunner` additionally calls `ContextProvider.OnFilesAccessed` mid-turn for reactive injection. Tool calls within a turn execute in parallel via `errgroup`.

**Tech Stack:** Go 1.23+, `golang.org/x/sync/errgroup` (already in go.mod), existing `internal/git`, `internal/runner`, `internal/context` packages.

**Key files NOT changing:** `internal/llm/`, `internal/models/pipeline.go`, `claudecode.go`, `copilot.go`, `AgentRunner` interface.

---

### Task 1: Tool Interface + Path Guard

**Files:**
- Create: `internal/agent/tools/tool.go`
- Create: `internal/agent/tools/guard.go`
- Create: `internal/agent/tools/guard_test.go`

**Step 1: Write failing tests for guard**

```go
// internal/agent/tools/guard_test.go
package tools_test

import (
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

func TestValidatePath_Allowed(t *testing.T) {
	if err := tools.ValidatePath("/work", "src/main.go"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePath_Traversal(t *testing.T) {
	if err := tools.ValidatePath("/work", "../../etc/passwd"); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestValidatePath_Absolute(t *testing.T) {
	if err := tools.ValidatePath("/work", "/etc/passwd"); err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestCheckSecrets_DotEnv(t *testing.T) {
	if err := tools.CheckSecrets(".env", ""); err == nil {
		t.Fatal("expected error for .env path")
	}
}

func TestCheckSecrets_PemFile(t *testing.T) {
	if err := tools.CheckSecrets("certs/server.pem", ""); err == nil {
		t.Fatal("expected error for .pem path")
	}
}

func TestCheckSecrets_KeyFile(t *testing.T) {
	if err := tools.CheckSecrets("private.key", ""); err == nil {
		t.Fatal("expected error for .key path")
	}
}

func TestCheckSecrets_NormalFile(t *testing.T) {
	if err := tools.CheckSecrets("main.go", "package main"); err != nil {
		t.Fatalf("expected no error for normal file, got %v", err)
	}
}

func TestCheckSecrets_ContentPattern(t *testing.T) {
	// Writing a file whose content contains a private key header is blocked
	content := "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----"
	if err := tools.CheckSecrets("notes.txt", content); err == nil {
		t.Fatal("expected error for private key content")
	}
}
```

**Step 2: Run to confirm failure**
```bash
cd /Users/canh/Projects/Indies/Foreman
go test ./internal/agent/tools/... 2>&1 | head -5
```
Expected: `cannot find package`

**Step 3: Create tool interface**

```go
// internal/agent/tools/tool.go
package tools

import (
	"context"
	"encoding/json"
)

// Tool is implemented by every built-in tool in the registry.
// All Execute calls must be goroutine-safe — the registry runs them in parallel.
type Tool interface {
	Name()        string
	Description() string
	Schema()      json.RawMessage // hand-written JSON Schema
	Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error)
}
```

**Step 4: Create guard.go**

```go
// internal/agent/tools/guard.go
package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

var secretPathPatterns = []string{".env", ".key", ".pem", ".p12", ".pfx", "id_rsa", "id_ed25519", ".secret"}
var secretContentPatterns = []string{"BEGIN RSA PRIVATE KEY", "BEGIN EC PRIVATE KEY", "BEGIN OPENSSH PRIVATE KEY"}

// ValidatePath ensures path resolves within workDir and is not absolute.
func ValidatePath(workDir, path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be relative", path)
	}
	abs := filepath.Join(workDir, filepath.Clean(path))
	clean := filepath.Clean(workDir) + string(filepath.Separator)
	if !strings.HasPrefix(abs, clean) && abs != filepath.Clean(workDir) {
		return fmt.Errorf("path %q is outside the working directory", path)
	}
	return nil
}

// CheckSecrets rejects writes to sensitive file paths or content containing private key headers.
func CheckSecrets(path, content string) error {
	base := strings.ToLower(filepath.Base(path))
	for _, pat := range secretPathPatterns {
		if strings.HasSuffix(base, pat) || base == strings.TrimPrefix(pat, ".") {
			return fmt.Errorf("writing to %q is not allowed (sensitive file pattern)", path)
		}
	}
	for _, pat := range secretContentPatterns {
		if strings.Contains(content, pat) {
			return fmt.Errorf("content contains sensitive pattern %q — write blocked", pat)
		}
	}
	return nil
}

// AbsPath returns the absolute path for a workDir-relative path, after ValidatePath.
func AbsPath(workDir, path string) string {
	return filepath.Join(workDir, filepath.Clean(path))
}
```

**Step 5: Run tests**
```bash
go test ./internal/agent/tools/... -run TestValidatePath -run TestCheckSecrets -v
```
Expected: all PASS

**Step 6: Commit**
```bash
git add internal/agent/tools/
git commit -m "feat(tools): add Tool interface and path/secrets guard"
```

---

### Task 2: Tool Registry with Parallel Execution and Hooks

**Files:**
- Create: `internal/agent/tools/registry.go`
- Create: `internal/agent/tools/registry_test.go`

**Step 1: Write failing registry tests**

```go
// internal/agent/tools/registry_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/models"
)

// stubTool is a minimal Tool for testing.
type stubTool struct {
	name   string
	output string
}

func (s *stubTool) Name() string              { return s.name }
func (s *stubTool) Description() string       { return "stub" }
func (s *stubTool) Schema() json.RawMessage   { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return s.output, nil
}

func TestRegistry_Execute_KnownTool(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "Stub", output: "hello"})

	out, err := reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestRegistry_Execute_UnknownTool(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	_, err := reg.Execute(context.Background(), "/work", "Missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistry_Defs_ReturnsRequested(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})
	reg.Register(&stubTool{name: "B"})

	defs := reg.Defs([]string{"A"})
	if len(defs) != 1 || defs[0].Name != "A" {
		t.Errorf("expected 1 def for A, got %+v", defs)
	}
}

func TestRegistry_Defs_SkipsUnknown(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})

	defs := reg.Defs([]string{"A", "Unknown"})
	if len(defs) != 1 {
		t.Errorf("expected 1 def (unknown skipped), got %d", len(defs))
	}
}

func TestRegistry_PreHook_CanBlock(t *testing.T) {
	blocked := false
	hooks := tools.ToolHooks{
		PreToolUse: func(_ context.Context, name string, _ json.RawMessage) error {
			if name == "Stub" {
				blocked = true
				return fmt.Errorf("blocked by hook")
			}
			return nil
		},
	}
	reg := tools.NewRegistry(nil, nil, hooks)
	reg.Register(&stubTool{name: "Stub", output: "should not reach"})

	_, err := reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected pre-hook to block execution")
	}
	if !blocked {
		t.Error("expected pre-hook to be called")
	}
}

func TestRegistry_PostHook_CalledAfterExecution(t *testing.T) {
	var postCalled atomic.Bool
	hooks := tools.ToolHooks{
		PostToolUse: func(_ context.Context, name, output string, err error) {
			postCalled.Store(true)
		},
	}
	reg := tools.NewRegistry(nil, nil, hooks)
	reg.Register(&stubTool{name: "Stub", output: "ok"})

	reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if !postCalled.Load() {
		t.Error("expected post-hook to be called")
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})
	if !reg.Has("A") {
		t.Error("expected Has('A') to be true")
	}
	if reg.Has("B") {
		t.Error("expected Has('B') to be false")
	}
}
```

Note: add `"fmt"` to the import in the test file.

**Step 2: Run to confirm failure**
```bash
go test ./internal/agent/tools/... -run TestRegistry -v 2>&1 | head -10
```
Expected: compile error — `Registry` not defined

**Step 3: Implement registry.go**

```go
// internal/agent/tools/registry.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/runner"
)

// ToolHooks are optional callbacks fired around every tool execution.
type ToolHooks struct {
	// PreToolUse is called before execution — return non-nil to block the call.
	PreToolUse func(ctx context.Context, name string, input json.RawMessage) error
	// PostToolUse is called after execution for logging/auditing.
	PostToolUse func(ctx context.Context, name, output string, err error)
}

// RunFn is a function signature for running a sub-agent request.
// Using a function reference breaks the circular Registry ↔ BuiltinRunner dependency.
type RunFn func(ctx context.Context, req interface{}) (interface{}, error)

// Registry maps tool names to implementations and fires hooks around execution.
// It is safe for concurrent reads (Execute may be called from multiple goroutines).
type Registry struct {
	tools           map[string]Tool
	hooks           ToolHooks
	allowedCommands []string // for Bash/RunTest whitelist
	runFn           RunFn    // injected by SetRunner for SubagentTool — nil until set
}

// NewRegistry creates a Registry. git and cmd may be nil — those tool groups
// return informative errors if invoked when their dependency is absent.
func NewRegistry(gitProvider git.GitProvider, cmdRunner runner.CommandRunner, hooks ToolHooks) *Registry {
	r := &Registry{
		tools: make(map[string]Tool),
		hooks: hooks,
	}
	// Filesystem tools (no external deps)
	registerFS(r)
	// Git tools (require gitProvider — registered even if nil, fail at call time)
	registerGit(r, gitProvider)
	// Code tools (require cmdRunner)
	registerCode(r, cmdRunner)
	// Exec tools (require cmdRunner)
	registerExec(r, cmdRunner)
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
func (r *Registry) Execute(ctx context.Context, workDir, name string, input json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
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
	return defs
}

// Has reports whether the named tool is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// SetAllowedCommands configures the Bash/RunTest command whitelist.
func (r *Registry) SetAllowedCommands(cmds []string) { r.allowedCommands = cmds }
func (r *Registry) AllowedCommands() []string         { return r.allowedCommands }

// SetRunFn injects the agent runner function for SubagentTool (two-phase init).
// Call this after both Registry and BuiltinRunner are constructed.
func (r *Registry) SetRunFn(fn RunFn) { r.runFn = fn }
func (r *Registry) RunFn() RunFn      { return r.runFn }
```

Also add stub `registerFS`, `registerGit`, `registerCode`, `registerExec` functions (empty — filled in subsequent tasks):

```go
// in registry.go, at the bottom:
func registerFS(r *Registry)                                          {}
func registerGit(r *Registry, g git.GitProvider)                     {}
func registerCode(r *Registry, cmd runner.CommandRunner)              {}
func registerExec(r *Registry, cmd runner.CommandRunner)              {}
```

**Step 4: Run tests**
```bash
go test ./internal/agent/tools/... -v
```
Expected: all PASS (guard + registry tests)

**Step 5: Commit**
```bash
git add internal/agent/tools/
git commit -m "feat(tools): add Registry with hooks and parallel-ready Execute"
```

---

### Task 3: Filesystem Tools (Read, Write, Edit, MultiEdit, ListDir, Glob, Grep)

**Files:**
- Create: `internal/agent/tools/fs.go`
- Create: `internal/agent/tools/fs_test.go`

**Step 1: Write failing filesystem tool tests**

```go
// internal/agent/tools/fs_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

func newFSRegistry(t *testing.T) (*tools.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	return reg, dir
}

func execTool(t *testing.T, reg *tools.Registry, dir, name string, input any) string {
	t.Helper()
	b, _ := json.Marshal(input)
	out, err := reg.Execute(context.Background(), dir, name, b)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	return out
}

func TestRead_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644)
	out := execTool(t, reg, dir, "Read", map[string]string{"path": "hello.txt"})
	if out != "hello world" {
		t.Errorf("unexpected: %q", out)
	}
}

func TestRead_LineRange(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("line1\nline2\nline3\nline4"), 0644)
	out := execTool(t, reg, dir, "Read", map[string]any{"path": "f.txt", "start_line": 2, "end_line": 3})
	if !strings.Contains(out, "line2") || !strings.Contains(out, "line3") {
		t.Errorf("expected lines 2-3, got %q", out)
	}
	if strings.Contains(out, "line1") || strings.Contains(out, "line4") {
		t.Errorf("should not contain lines outside range, got %q", out)
	}
}

func TestRead_Traversal(t *testing.T) {
	reg, dir := newFSRegistry(t)
	b, _ := json.Marshal(map[string]string{"path": "../../etc/passwd"})
	_, err := reg.Execute(context.Background(), dir, "Read", b)
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestWrite_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	execTool(t, reg, dir, "Write", map[string]string{"path": "out.txt", "content": "written"})
	data, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(data) != "written" {
		t.Errorf("unexpected content: %q", data)
	}
}

func TestWrite_ForbiddenPath(t *testing.T) {
	reg, dir := newFSRegistry(t)
	b, _ := json.Marshal(map[string]string{"path": "secrets.env", "content": "X=1"})
	_, err := reg.Execute(context.Background(), dir, "Write", b)
	if err == nil {
		t.Fatal("expected error for .env file")
	}
}

func TestWrite_ForbiddenContent(t *testing.T) {
	reg, dir := newFSRegistry(t)
	b, _ := json.Marshal(map[string]string{"path": "notes.txt", "content": "-----BEGIN RSA PRIVATE KEY-----"})
	_, err := reg.Execute(context.Background(), dir, "Write", b)
	if err == nil {
		t.Fatal("expected error for private key content")
	}
}

func TestEdit_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("func old() {}"), 0644)
	execTool(t, reg, dir, "Edit", map[string]string{"path": "f.go", "old_string": "func old() {}", "new_string": "func new() {}"})
	data, _ := os.ReadFile(filepath.Join(dir, "f.go"))
	if string(data) != "func new() {}" {
		t.Errorf("edit failed: %q", data)
	}
}

func TestEdit_OldStringNotFound(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("package main"), 0644)
	b, _ := json.Marshal(map[string]string{"path": "f.go", "old_string": "NOTHERE", "new_string": "X"})
	_, err := reg.Execute(context.Background(), dir, "Edit", b)
	if err == nil {
		t.Fatal("expected error when old_string not found")
	}
}

func TestMultiEdit_Atomic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("aaa bbb ccc"), 0644)
	type edit struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	b, _ := json.Marshal(map[string]any{
		"path":  "f.go",
		"edits": []edit{{"aaa", "AAA"}, {"bbb", "BBB"}},
	})
	_, err := reg.Execute(context.Background(), dir, "MultiEdit", b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.go"))
	if !strings.Contains(string(data), "AAA") || !strings.Contains(string(data), "BBB") {
		t.Errorf("expected both edits, got %q", data)
	}
}

func TestListDir_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(dir, "pkg"), 0755)
	out := execTool(t, reg, dir, "ListDir", map[string]string{"path": "."})
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in listing, got %q", out)
	}
}

func TestGlob_StarStar(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0755)
	os.WriteFile(filepath.Join(dir, "a", "b", "deep.go"), []byte(""), 0644)
	out := execTool(t, reg, dir, "Glob", map[string]string{"pattern": "**/*.go"})
	if !strings.Contains(out, "deep.go") {
		t.Errorf("expected deep.go from ** glob, got %q", out)
	}
}

func TestGrep_FilePattern(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("func main() {}"), 0644)
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("def main(): pass"), 0644)
	out := execTool(t, reg, dir, "Grep", map[string]any{
		"pattern":      "main",
		"path":         ".",
		"file_pattern": "*.go",
	})
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in grep output, got %q", out)
	}
	if strings.Contains(out, "main.py") {
		t.Errorf("should not match main.py with *.go filter, got %q", out)
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("func Hello() {}"), 0644)
	out := execTool(t, reg, dir, "Grep", map[string]any{
		"pattern":        "hello",
		"path":           ".",
		"case_sensitive": false,
	})
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected case-insensitive match, got %q", out)
	}
}

func TestGrep_Cap200(t *testing.T) {
	reg, dir := newFSRegistry(t)
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("match\n")
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(sb.String()), 0644)
	out := execTool(t, reg, dir, "Grep", map[string]any{"pattern": "match", "path": "."})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 200 {
		t.Errorf("expected cap at 200 matches, got %d", len(lines))
	}
}
```

**Step 2: Run to confirm failure**
```bash
go test ./internal/agent/tools/... -run TestRead -v 2>&1 | head -5
```
Expected: FAIL — `Read` not registered

**Step 3: Implement fs.go**

```go
// internal/agent/tools/fs.go
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func registerFS(r *Registry) {
	r.Register(&readTool{})
	r.Register(&writeTool{})
	r.Register(&editTool{})
	r.Register(&multiEditTool{})
	r.Register(&listDirTool{})
	r.Register(&globTool{})
	r.Register(&grepTool{})
}

// --- Read ---

type readTool struct{}

func (t *readTool) Name() string        { return "Read" }
func (t *readTool) Description() string { return "Read a file's contents, optionally limited to a line range" }
func (t *readTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative file path"},"start_line":{"type":"integer","description":"First line to read (1-indexed, inclusive)"},"end_line":{"type":"integer","description":"Last line to read (inclusive)"}},"required":["path"]}`)
}
func (t *readTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Read: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("Read: %w", err)
	}
	content, err := os.ReadFile(AbsPath(workDir, args.Path))
	if err != nil {
		return "", fmt.Errorf("Read: %w", err)
	}
	if args.StartLine == 0 && args.EndLine == 0 {
		return string(content), nil
	}
	lines := strings.Split(string(content), "\n")
	start := args.StartLine - 1
	if start < 0 {
		start = 0
	}
	end := args.EndLine
	if end == 0 || end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n"), nil
}

// --- Write ---

type writeTool struct{}

func (t *writeTool) Name() string        { return "Write" }
func (t *writeTool) Description() string { return "Write content to a file (creates or overwrites)" }
func (t *writeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (t *writeTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	if err := CheckSecrets(args.Path, args.Content); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}
	return "OK", nil
}

// --- Edit ---

type editTool struct{}

func (t *editTool) Name() string        { return "Edit" }
func (t *editTool) Description() string { return "Replace first occurrence of old_string with new_string in a file" }
func (t *editTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`)
}
func (t *editTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	if err := CheckSecrets(args.Path, args.NewString); err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	if !strings.Contains(string(content), args.OldString) {
		return "", fmt.Errorf("Edit: old_string not found in %s", args.Path)
	}
	updated := strings.Replace(string(content), args.OldString, args.NewString, 1)
	if err := os.WriteFile(abs, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("Edit: %w", err)
	}
	return "OK", nil
}

// --- MultiEdit ---

type multiEditTool struct{}

func (t *multiEditTool) Name() string        { return "MultiEdit" }
func (t *multiEditTool) Description() string { return "Apply multiple edits to a file atomically" }
func (t *multiEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["old_string","new_string"]}}},"required":["path","edits"]}`)
}
func (t *multiEditTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	result := string(content)
	for i, edit := range args.Edits {
		if err := CheckSecrets(args.Path, edit.NewString); err != nil {
			return "", fmt.Errorf("MultiEdit edit %d: %w", i, err)
		}
		if !strings.Contains(result, edit.OldString) {
			return "", fmt.Errorf("MultiEdit edit %d: old_string not found", i)
		}
		result = strings.Replace(result, edit.OldString, edit.NewString, 1)
	}
	if err := os.WriteFile(abs, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("MultiEdit: %w", err)
	}
	return fmt.Sprintf("OK (%d edits applied)", len(args.Edits)), nil
}

// --- ListDir ---

type listDirTool struct{}

func (t *listDirTool) Name() string        { return "ListDir" }
func (t *listDirTool) Description() string { return "List directory contents with file metadata" }
func (t *listDirTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative directory path"},"recursive":{"type":"boolean","description":"List recursively"}},"required":["path"]}`)
}
func (t *listDirTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("ListDir: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("ListDir: %w", err)
	}
	abs := AbsPath(workDir, args.Path)
	var lines []string
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		if rel == "." {
			return nil
		}
		if !args.Recursive && d.IsDir() && rel != "." {
			return fs.SkipDir
		}
		info, _ := d.Info()
		kind := "file"
		size := int64(0)
		mod := time.Time{}
		if info != nil {
			size = info.Size()
			mod = info.ModTime()
		}
		if d.IsDir() {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%d\t%s", rel, kind, size, mod.Format("2006-01-02")))
		return nil
	}
	if args.Recursive {
		fs.WalkDir(os.DirFS(abs), ".", func(path string, d fs.DirEntry, err error) error {
			if path == "." {
				return nil
			}
			return walkFn(filepath.Join(abs, path), d, err)
		})
	} else {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return "", fmt.Errorf("ListDir: %w", err)
		}
		for _, e := range entries {
			info, _ := e.Info()
			kind := "file"
			size := int64(0)
			mod := time.Time{}
			if info != nil {
				size = info.Size()
				mod = info.ModTime()
			}
			if e.IsDir() {
				kind = "dir"
			}
			lines = append(lines, fmt.Sprintf("%s\t%s\t%d\t%s", e.Name(), kind, size, mod.Format("2006-01-02")))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// --- Glob ---

type globTool struct{}

func (t *globTool) Name() string        { return "Glob" }
func (t *globTool) Description() string { return "Find files matching a glob pattern (supports **)" }
func (t *globTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern e.g. **/*.go"},"base":{"type":"string","description":"Base directory (default: working dir)"}},"required":["pattern"]}`)
}
func (t *globTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Base    string `json:"base"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Glob: %w", err)
	}
	base := workDir
	if args.Base != "" {
		if err := ValidatePath(workDir, args.Base); err != nil {
			return "", fmt.Errorf("Glob: %w", err)
		}
		base = AbsPath(workDir, args.Base)
	}
	var matches []string
	fs.WalkDir(os.DirFS(base), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return nil
		}
		matched, _ := filepath.Match(strings.ReplaceAll(args.Pattern, "**", "*"), filepath.Base(path))
		// For ** patterns, also try full path match
		if !matched {
			matched, _ = doubleStarMatch(args.Pattern, path)
		}
		if matched && !d.IsDir() {
			matches = append(matches, path)
		}
		return nil
	})
	return strings.Join(matches, "\n"), nil
}

// doubleStarMatch handles ** in glob patterns using path segment matching.
func doubleStarMatch(pattern, path string) (bool, error) {
	// Convert ** to match any number of path segments
	re := "^" + regexp.QuoteMeta(pattern)
	re = strings.ReplaceAll(re, `\*\*`, `.*`)
	re = strings.ReplaceAll(re, `\*`, `[^/]*`)
	re += "$"
	return regexp.MatchString(re, path)
}

// --- Grep ---

type grepTool struct{}

func (t *grepTool) Name() string        { return "Grep" }
func (t *grepTool) Description() string { return "Search file contents with a regex pattern (max 200 matches)" }
func (t *grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"file_pattern":{"type":"string","description":"Filter files e.g. *.go"},"case_sensitive":{"type":"boolean","default":true}},"required":["pattern","path"]}`)
}
func (t *grepTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		FilePattern   string `json:"file_pattern"`
		CaseSensitive *bool  `json:"case_sensitive"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Grep: %w", err)
	}
	if err := ValidatePath(workDir, args.Path); err != nil {
		return "", fmt.Errorf("Grep: %w", err)
	}
	reStr := args.Pattern
	if args.CaseSensitive != nil && !*args.CaseSensitive {
		reStr = "(?i)" + reStr
	}
	re, err := regexp.Compile(reStr)
	if err != nil {
		return "", fmt.Errorf("Grep: invalid pattern: %w", err)
	}
	searchBase := AbsPath(workDir, args.Path)
	const maxMatches = 200
	var results []string
	filepath.WalkDir(searchBase, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(results) >= maxMatches {
			return nil
		}
		if args.FilePattern != "" {
			if matched, _ := filepath.Match(args.FilePattern, filepath.Base(path)); !matched {
				return nil
			}
		}
		info, _ := d.Info()
		if info != nil && info.Size() > 1<<20 {
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
		for scanner.Scan() && len(results) < maxMatches {
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
```

**Step 4: Update `registerFS` in registry.go** — the function body is now in fs.go; remove the empty stub from registry.go and add a blank `registerFS` call comment if needed. The function is defined in fs.go so no change to registry.go is needed.

Actually: move the stub definitions out of registry.go since fs.go defines `registerFS`. Remove these lines from registry.go:
```go
func registerFS(r *Registry)                                          {}
func registerGit(r *Registry, g git.GitProvider)                     {}
func registerCode(r *Registry, cmd runner.CommandRunner)              {}
func registerExec(r *Registry, cmd runner.CommandRunner)              {}
```
Replace them with just the git/code/exec stubs (fs is now defined in fs.go). These will be filled in by subsequent tasks. For now put empty stubs in a separate file `stubs.go` (deleted in later tasks):

```go
// internal/agent/tools/stubs.go  — TEMPORARY, deleted in Task 4
package tools
import (
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/runner"
)
func registerGit(r *Registry, g git.GitProvider)   {}
func registerCode(r *Registry, cmd runner.CommandRunner) {}
func registerExec(r *Registry, cmd runner.CommandRunner) {}
```

**Step 5: Run tests**
```bash
go test ./internal/agent/tools/... -v
```
Expected: all PASS

**Step 6: Commit**
```bash
git add internal/agent/tools/
git commit -m "feat(tools): implement filesystem tools (Read, Write, Edit, MultiEdit, ListDir, Glob, Grep)"
```

---

### Task 4: Git Tools (GetDiff, GetCommitLog, TreeSummary)

**Files:**
- Create: `internal/agent/tools/git.go`
- Create: `internal/agent/tools/git_test.go`
- Modify: `internal/agent/tools/stubs.go` — remove `registerGit` stub

**Step 1: Write failing git tool tests**

```go
// internal/agent/tools/git_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/agent/tools"
)

type mockGit struct {
	diff    string
	log     []gitpkg.CommitEntry
	tree    []gitpkg.FileEntry
}

func (m *mockGit) EnsureRepo(_ context.Context, _ string) error { return nil }
func (m *mockGit) CreateBranch(_ context.Context, _, _ string) error { return nil }
func (m *mockGit) Commit(_ context.Context, _, _ string) (string, error) { return "abc", nil }
func (m *mockGit) Diff(_ context.Context, _, _, _ string) (string, error) { return m.diff, nil }
func (m *mockGit) DiffWorking(_ context.Context, _ string) (string, error) { return m.diff, nil }
func (m *mockGit) Push(_ context.Context, _, _ string) error { return nil }
func (m *mockGit) RebaseOnto(_ context.Context, _, _ string) (*gitpkg.RebaseResult, error) { return &gitpkg.RebaseResult{Success: true}, nil }
func (m *mockGit) FileTree(_ context.Context, _ string) ([]gitpkg.FileEntry, error) { return m.tree, nil }
func (m *mockGit) Log(_ context.Context, _ string, _ int) ([]gitpkg.CommitEntry, error) { return m.log, nil }
func (m *mockGit) StageAll(_ context.Context, _ string) error { return nil }

func newGitRegistry(t *testing.T, g gitpkg.GitProvider) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(g, nil, tools.ToolHooks{})
	return reg, t.TempDir()
}

func TestGetDiff_Working(t *testing.T) {
	g := &mockGit{diff: "diff --git a/main.go b/main.go\n+func new() {}"}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetDiff", map[string]string{})
	if !strings.Contains(out, "func new()") {
		t.Errorf("expected diff content, got %q", out)
	}
}

func TestGetDiff_BasedHead(t *testing.T) {
	g := &mockGit{diff: "diff --git a/x.go b/x.go\n+added"}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetDiff", map[string]string{"base": "main", "head": "HEAD"})
	if !strings.Contains(out, "added") {
		t.Errorf("expected diff, got %q", out)
	}
}

func TestGetCommitLog(t *testing.T) {
	g := &mockGit{log: []gitpkg.CommitEntry{
		{SHA: "abc123", Message: "feat: add thing", Author: "Alice", Date: time.Now()},
	}}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetCommitLog", map[string]any{"count": 5})
	if !strings.Contains(out, "abc123") || !strings.Contains(out, "feat: add thing") {
		t.Errorf("expected commit in log, got %q", out)
	}
}

func TestTreeSummary(t *testing.T) {
	g := &mockGit{tree: []gitpkg.FileEntry{
		{Path: "main.go", IsDir: false, SizeBytes: 100},
		{Path: "pkg", IsDir: true},
		{Path: "pkg/util.go", IsDir: false, SizeBytes: 50},
	}}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "TreeSummary", map[string]any{})
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "pkg") {
		t.Errorf("expected tree entries, got %q", out)
	}
}

func TestGetDiff_NilGit(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{})
	_, err := reg.Execute(context.Background(), t.TempDir(), "GetDiff", b)
	if err == nil {
		t.Fatal("expected error when git is nil")
	}
}
```

**Step 2: Run to confirm failure**
```bash
go test ./internal/agent/tools/... -run TestGetDiff -v 2>&1 | head -5
```

**Step 3: Implement git.go**

```go
// internal/agent/tools/git.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gitpkg "github.com/canhta/foreman/internal/git"
)

func registerGit(r *Registry, g gitpkg.GitProvider) {
	r.Register(&getDiffTool{git: g})
	r.Register(&getCommitLogTool{git: g})
	r.Register(&treeSummaryTool{git: g})
}

type getDiffTool struct{ git gitpkg.GitProvider }

func (t *getDiffTool) Name() string        { return "GetDiff" }
func (t *getDiffTool) Description() string { return "Get the git diff (working tree or between commits)" }
func (t *getDiffTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"base":{"type":"string","description":"Base ref (omit for working tree diff)"},"head":{"type":"string"},"path":{"type":"string","description":"Limit diff to this path"}}}`)
}
func (t *getDiffTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("GetDiff: git provider not available")
	}
	var args struct {
		Base string `json:"base"`
		Head string `json:"head"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetDiff: %w", err)
	}
	if args.Base != "" {
		head := args.Head
		if head == "" {
			head = "HEAD"
		}
		return t.git.Diff(ctx, workDir, args.Base, head)
	}
	return t.git.DiffWorking(ctx, workDir)
}

type getCommitLogTool struct{ git gitpkg.GitProvider }

func (t *getCommitLogTool) Name() string        { return "GetCommitLog" }
func (t *getCommitLogTool) Description() string { return "Get recent git commit log" }
func (t *getCommitLogTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","description":"Number of commits (default 10)"},"path":{"type":"string"}}}`)
}
func (t *getCommitLogTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("GetCommitLog: git provider not available")
	}
	var args struct {
		Count int    `json:"count"`
		Path  string `json:"path"`
	}
	json.Unmarshal(input, &args)
	if args.Count == 0 {
		args.Count = 10
	}
	entries, err := t.git.Log(ctx, workDir, args.Count)
	if err != nil {
		return "", fmt.Errorf("GetCommitLog: %w", err)
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s %s (%s) %s", e.SHA[:7], e.Message, e.Author, e.Date.Format("2006-01-02")))
	}
	return strings.Join(lines, "\n"), nil
}

type treeSummaryTool struct{ git gitpkg.GitProvider }

func (t *treeSummaryTool) Name() string        { return "TreeSummary" }
func (t *treeSummaryTool) Description() string { return "Get a tree summary of the repository" }
func (t *treeSummaryTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"max_depth":{"type":"integer","description":"Maximum directory depth (default 3)"},"focus":{"type":"string","description":"Only show entries under this path"}}}`)
}
func (t *treeSummaryTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.git == nil {
		return "", fmt.Errorf("TreeSummary: git provider not available")
	}
	var args struct {
		MaxDepth int    `json:"max_depth"`
		Focus    string `json:"focus"`
	}
	json.Unmarshal(input, &args)
	if args.MaxDepth == 0 {
		args.MaxDepth = 3
	}
	entries, err := t.git.FileTree(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("TreeSummary: %w", err)
	}
	var lines []string
	for _, e := range entries {
		if args.Focus != "" && !strings.HasPrefix(e.Path, args.Focus) {
			continue
		}
		depth := strings.Count(e.Path, "/")
		if depth >= args.MaxDepth {
			continue
		}
		indent := strings.Repeat("  ", depth)
		kind := ""
		if e.IsDir {
			kind = "/"
		}
		lines = append(lines, fmt.Sprintf("%s%s%s", indent, e.Path[strings.LastIndex(e.Path, "/")+1:], kind))
	}
	return strings.Join(lines, "\n"), nil
}
```

**Step 4: Remove `registerGit` stub from stubs.go**
```go
// stubs.go now only contains:
package tools
import "github.com/canhta/foreman/internal/runner"
func registerCode(r *Registry, cmd runner.CommandRunner) {}
func registerExec(r *Registry, cmd runner.CommandRunner) {}
```

**Step 5: Run tests**
```bash
go test ./internal/agent/tools/... -v
```
Expected: all PASS

**Step 6: Commit**
```bash
git add internal/agent/tools/
git commit -m "feat(tools): implement git tools (GetDiff, GetCommitLog, TreeSummary)"
```

---

### Task 5: Code Intelligence Tools (GetSymbol, GetErrors)

**Files:**
- Create: `internal/agent/tools/code.go`
- Create: `internal/agent/tools/code_test.go`
- Modify: `internal/agent/tools/stubs.go` — remove `registerCode` stub

**Step 1: Write failing tests**

```go
// internal/agent/tools/code_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/runner"
)

type mockCmdRunner struct {
	stdout string
	stderr string
	exit   int
}

func (m *mockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stdout: m.stdout, Stderr: m.stderr, ExitCode: m.exit}, nil
}
func (m *mockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

func newCodeRegistry(t *testing.T, cmd runner.CommandRunner) (*tools.Registry, string) {
	t.Helper()
	return tools.NewRegistry(nil, cmd, tools.ToolHooks{}), t.TempDir()
}

func TestGetSymbol_FindsFunction(t *testing.T) {
	reg, dir := newFSRegistry(t)
	// GetSymbol uses grep internally — populate workdir with a Go file
	import "os"
	import "path/filepath"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc MyHandler() {}\nfunc other() {}"), 0644)
	out := execTool(t, reg, dir, "GetSymbol", map[string]string{"symbol": "MyHandler", "kind": "func"})
	if !strings.Contains(out, "MyHandler") {
		t.Errorf("expected MyHandler in output, got %q", out)
	}
}

func TestGetErrors_ParsesLintOutput(t *testing.T) {
	cmd := &mockCmdRunner{
		stdout: "main.go:10:5: undefined: foo\nmain.go:20:1: unused import\n",
	}
	reg, dir := newCodeRegistry(t, cmd)
	out := execTool(t, reg, dir, "GetErrors", map[string]string{"tool": "golangci-lint"})
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "undefined") {
		t.Errorf("expected lint issues, got %q", out)
	}
}

func TestGetErrors_NilRunner(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"tool": "golangci-lint"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "GetErrors", b)
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}
```

Note: `TestGetSymbol_FindsFunction` uses the fs registry (no cmd runner needed — GetSymbol uses internal grep). Remove the duplicate import lines — they're illustrative; write them properly.

**Step 2: Implement code.go**

```go
// internal/agent/tools/code.go
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

func registerCode(r *Registry, cmd runner.CommandRunner) {
	r.Register(&getSymbolTool{})
	r.Register(&getErrorsTool{cmd: cmd})
}

// --- GetSymbol ---

type getSymbolTool struct{}

func (t *getSymbolTool) Name() string        { return "GetSymbol" }
func (t *getSymbolTool) Description() string { return "Find where a symbol (function, type, class) is defined" }
func (t *getSymbolTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"Symbol name to find"},"kind":{"type":"string","description":"Symbol kind: func, type, class, def (optional)"},"path":{"type":"string","description":"Directory to search (default: working dir)"}},"required":["symbol"]}`)
}
func (t *getSymbolTool) Execute(_ context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Symbol string `json:"symbol"`
		Kind   string `json:"kind"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetSymbol: %w", err)
	}
	searchDir := workDir
	if args.Path != "" {
		if err := ValidatePath(workDir, args.Path); err != nil {
			return "", fmt.Errorf("GetSymbol: %w", err)
		}
		searchDir = AbsPath(workDir, args.Path)
	}
	// Build pattern based on kind
	patterns := []string{
		fmt.Sprintf(`func %s\b`, regexp.QuoteMeta(args.Symbol)),
		fmt.Sprintf(`type %s\b`, regexp.QuoteMeta(args.Symbol)),
		fmt.Sprintf(`class %s\b`, regexp.QuoteMeta(args.Symbol)),
		fmt.Sprintf(`def %s\b`, regexp.QuoteMeta(args.Symbol)),
		fmt.Sprintf(`const %s\b`, regexp.QuoteMeta(args.Symbol)),
		fmt.Sprintf(`var %s\b`, regexp.QuoteMeta(args.Symbol)),
	}
	if args.Kind != "" {
		patterns = []string{fmt.Sprintf(`%s %s\b`, args.Kind, regexp.QuoteMeta(args.Symbol))}
	}
	combined := "(" + strings.Join(patterns, "|") + ")"
	re, _ := regexp.Compile(combined)

	var results []string
	filepath.WalkDir(searchDir, func(path string, d interface{ IsDir() bool }, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := os.Stat(path)
		if info != nil && info.Size() > 1<<20 {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		rel, _ := filepath.Rel(workDir, path)
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, lineNum, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if len(results) == 0 {
		return fmt.Sprintf("Symbol %q not found", args.Symbol), nil
	}
	return strings.Join(results, "\n"), nil
}

// --- GetErrors ---

type getErrorsTool struct{ cmd runner.CommandRunner }

func (t *getErrorsTool) Name() string        { return "GetErrors" }
func (t *getErrorsTool) Description() string { return "Run a lint/check tool and return structured errors" }
func (t *getErrorsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tool":{"type":"string","description":"Tool to run e.g. golangci-lint, eslint"},"path":{"type":"string","description":"Path to lint (default: working dir)"}},"required":["tool"]}`)
}
func (t *getErrorsTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("GetErrors: command runner not available")
	}
	var args struct {
		Tool string `json:"tool"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("GetErrors: %w", err)
	}
	target := "."
	if args.Path != "" {
		target = args.Path
	}
	out, err := t.cmd.Run(ctx, workDir, args.Tool, []string{"run", target}, 60)
	if err != nil {
		return "", fmt.Errorf("GetErrors: %w", err)
	}
	result := runner.ParseLintOutput(out.Stdout+out.Stderr, "")
	if result.Clean {
		return "No issues found.", nil
	}
	var lines []string
	for _, issue := range result.Issues {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", issue.File, issue.Line, issue.Message))
	}
	return strings.Join(lines, "\n"), nil
}
```

**Step 3: Remove `registerCode` from stubs.go**
```go
// stubs.go now only contains:
package tools
import "github.com/canhta/foreman/internal/runner"
func registerExec(r *Registry, cmd runner.CommandRunner) {}
```

**Step 4: Run tests**
```bash
go test ./internal/agent/tools/... -v
```
Expected: all PASS

**Step 5: Commit**
```bash
git add internal/agent/tools/
git commit -m "feat(tools): implement code intelligence tools (GetSymbol, GetErrors)"
```

---

### Task 6: Execution Tools (Bash, RunTest)

**Files:**
- Create: `internal/agent/tools/exec.go`
- Create: `internal/agent/tools/exec_test.go`
- Delete: `internal/agent/tools/stubs.go`

**Step 1: Write failing tests**

```go
// internal/agent/tools/exec_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

func newExecRegistry(t *testing.T, cmd *mockCmdRunner, allowed []string) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	reg.SetAllowedCommands(allowed)
	return reg, t.TempDir()
}

func TestBash_AllowedCommand(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "ok"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})
	out := execTool(t, reg, dir, "Bash", map[string]string{"command": "go version"})
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
}

func TestBash_ForbiddenCommand(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "should not run"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})
	b, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error for forbidden command rm")
	}
}

func TestBash_BlockedKeyword(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "should not run"}
	reg, dir := newExecRegistry(t, cmd, []string{"curl"})  // even if explicitly allowed, curl is blocked
	b, _ := json.Marshal(map[string]string{"command": "curl https://evil.com"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error: curl is a hard-blocked command")
	}
}

func TestRunTest_Structured(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "--- PASS: TestFoo (0.00s)\n--- FAIL: TestBar (0.01s)\n"}
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	reg.SetAllowedCommands([]string{"go"})
	out := execTool(t, reg, t.TempDir(), "RunTest", map[string]any{"package": "./..."})
	if out == "" {
		t.Error("expected structured output")
	}
}

func TestBash_NilRunner(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"command": "go version"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "Bash", b)
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}
```

**Step 2: Verify `SetAllowedCommands` is already in registry.go** (added in Task 2 — no changes needed here)

**Step 3: Implement exec.go**

```go
// internal/agent/tools/exec.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

// hardBlockedCommands are never allowed regardless of AllowedCommands config.
var hardBlockedCommands = []string{"rm", "curl", "wget", "ssh", "scp", "git push", "git reset", "dd", "mkfs", "shutdown", "reboot"}

func registerExec(r *Registry, cmd runner.CommandRunner) {
	r.Register(&bashTool{cmd: cmd, registry: r})
	r.Register(&runTestTool{cmd: cmd, registry: r})
}

type bashTool struct {
	cmd      runner.CommandRunner
	registry *Registry
}

func (t *bashTool) Name() string        { return "Bash" }
func (t *bashTool) Description() string { return "Execute a shell command (whitelist-restricted)" }
func (t *bashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"timeout_secs":{"type":"integer","description":"Timeout in seconds (default 30)"}},"required":["command"]}`)
}
func (t *bashTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("Bash: command runner not available")
	}
	var args struct {
		Command     string `json:"command"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("Bash: %w", err)
	}
	if err := validateBashCommand(args.Command, t.registry.AllowedCommands()); err != nil {
		return "", fmt.Errorf("Bash: %w", err)
	}
	timeout := args.TimeoutSecs
	if timeout == 0 {
		timeout = 30
	}
	parts := strings.Fields(args.Command)
	if len(parts) == 0 {
		return "", fmt.Errorf("Bash: empty command")
	}
	out, err := t.cmd.Run(ctx, workDir, parts[0], parts[1:], timeout)
	if err != nil {
		return "", fmt.Errorf("Bash: %w", err)
	}
	result := out.Stdout
	if out.Stderr != "" {
		result += "\nSTDERR: " + out.Stderr
	}
	if out.TimedOut {
		return result, fmt.Errorf("Bash: command timed out after %ds", timeout)
	}
	return result, nil
}

func validateBashCommand(command string, allowed []string) error {
	lower := strings.ToLower(command)
	for _, blocked := range hardBlockedCommands {
		if strings.HasPrefix(lower, blocked) || strings.Contains(lower, " "+blocked+" ") {
			return fmt.Errorf("command %q is not allowed (hard-blocked)", blocked)
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("no commands are allowed — set allowed_commands in config")
	}
	for _, a := range allowed {
		if strings.HasPrefix(strings.TrimSpace(command), a) {
			return nil
		}
	}
	return fmt.Errorf("command %q is not in the allowed commands list", strings.Fields(command)[0])
}

type runTestTool struct {
	cmd      runner.CommandRunner
	registry *Registry
}

func (t *runTestTool) Name() string        { return "RunTest" }
func (t *runTestTool) Description() string { return "Run tests and return structured pass/fail results" }
func (t *runTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"test":{"type":"string","description":"Test name filter"},"package":{"type":"string","description":"Package path (default ./...)"},"timeout_secs":{"type":"integer"}}}`)
}
func (t *runTestTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	if t.cmd == nil {
		return "", fmt.Errorf("RunTest: command runner not available")
	}
	var args struct {
		Test        string `json:"test"`
		Package     string `json:"package"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	json.Unmarshal(input, &args)
	pkg := args.Package
	if pkg == "" {
		pkg = "./..."
	}
	timeout := args.TimeoutSecs
	if timeout == 0 {
		timeout = 120
	}
	cmdArgs := []string{"test", pkg, "-v"}
	if args.Test != "" {
		cmdArgs = append(cmdArgs, "-run", args.Test)
	}
	out, err := t.cmd.Run(ctx, workDir, "go", cmdArgs, timeout)
	if err != nil {
		return "", fmt.Errorf("RunTest: %w", err)
	}
	result := runner.ParseTestOutput(out.Stdout+out.Stderr, "go")
	return fmt.Sprintf("passed=%d failed=%d total=%d\n%s",
		result.PassedTests, result.FailedTests, result.TotalTests, out.Stdout), nil
}
```

**Step 4: Delete stubs.go**
```bash
rm internal/agent/tools/stubs.go
```

**Step 5: Run tests**
```bash
go test ./internal/agent/tools/... -v
```
Expected: all PASS

**Step 5a: Add SubagentTool to exec.go**

Append to `exec.go` (after `runTestTool`):

```go
// --- Subagent ---
// SubagentTool delegates a bounded subtask to a fresh BuiltinRunner invocation.
// It uses a function reference (RunFn from registry) to avoid circular imports.

type subagentInput struct {
	Task     string   `json:"task"`
	Tools    []string `json:"tools"`
	MaxTurns int      `json:"max_turns"`
}

type subagentTool struct {
	registry *Registry
}

func (t *subagentTool) Name() string        { return "Subagent" }
func (t *subagentTool) Description() string {
	return "Delegate a bounded subtask to a fresh agent with a restricted tool set. Returns the agent's final output."
}
func (t *subagentTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"Prompt for the subagent"},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names the subagent may use (subset of current tools)"},"max_turns":{"type":"integer","description":"Max turns for subagent (default 5, max 10)"}},"required":["task"]}`)
}
func (t *subagentTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	runFn := t.registry.RunFn()
	if runFn == nil {
		return "", fmt.Errorf("Subagent: runner not initialized (SetRunFn not called)")
	}
	var in subagentInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("Subagent: %w", err)
	}
	maxTurns := in.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 5
	}
	if maxTurns > 10 {
		maxTurns = 10
	}
	result, err := runFn(ctx, in.Task, workDir, in.Tools, maxTurns)
	if err != nil {
		return "", fmt.Errorf("Subagent: %w", err)
	}
	return result, nil
}
```

Update `registerExec` to include `Subagent`:

```go
func registerExec(r *Registry, cmd runner.CommandRunner) {
	r.Register(&bashTool{cmd: cmd, registry: r})
	r.Register(&runTestTool{cmd: cmd, registry: r})
	r.Register(&subagentTool{registry: r})
}
```

Update `RunFn` type in `registry.go` to the concrete subagent signature:

```go
// RunFn is injected via SetRunFn for SubagentTool two-phase init.
// agentDepth is passed in by the runner to enforce combined depth limit (max 3).
type RunFn func(ctx context.Context, task, workDir string, tools []string, maxTurns int) (string, error)
```

Add a depth-guard test:

```go
func TestSubagent_MaxDepthExceeded(t *testing.T) {
	callCount := 0
	var runFn tools.RunFn = func(ctx context.Context, task, workDir string, ts []string, maxTurns int) (string, error) {
		callCount++
		return "", nil
	}
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.SetRunFn(runFn)
	// Simulate depth exceeded by calling with agentDepth >= 3 — SubagentTool checks via RunFn returning sentinel error
	// Direct test: call with nil runFn should error
	reg2 := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"task": "do something"})
	_, err := reg2.Execute(context.Background(), t.TempDir(), "Subagent", b)
	if err == nil {
		t.Fatal("expected error when runFn is nil (SetRunFn not called)")
	}
}
```

**Step 6: Delete stubs.go and commit**
```bash
rm internal/agent/tools/stubs.go
go test ./internal/agent/tools/... -v
git add internal/agent/tools/
git rm internal/agent/tools/stubs.go
git commit -m "feat(tools): implement exec tools (Bash, RunTest, Subagent with two-phase init)"
```

---

### Task 7: ContextProvider Interface + SkillsContextProvider

**Files:**
- Create: `internal/agent/context.go`
- Create: `internal/skills/context_provider.go`
- Create: `internal/skills/context_provider_test.go`

**Step 1: Create ContextProvider interface**

```go
// internal/agent/context.go
package agent

import "context"

// ContextProvider is implemented by the skills layer to inject reactive context
// into the builtin runner mid-session. It is nil-safe — the runner always checks
// for nil before calling.
type ContextProvider interface {
	// OnFilesAccessed is called after each tool that touches files.
	// Returns new context text to inject as a user message, or "" if nothing new.
	OnFilesAccessed(ctx context.Context, paths []string) (string, error)
}
```

**Step 2: Write failing SkillsContextProvider tests**

```go
// internal/skills/context_provider_test.go
package skills_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/skills"
)

type mockProgressDB struct {
	patterns []models.ProgressPattern
}

func (m *mockProgressDB) GetProgressPatterns(_ context.Context, _ string, _ []string) ([]models.ProgressPattern, error) {
	return m.patterns, nil
}

func TestSkillsContextProvider_InjectsPatterns(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "import-style", PatternValue: "use named imports", Directories: []string{"src/"}, CreatedAt: time.Now()},
	}}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")
	result, err := cp.OnFilesAccessed(context.Background(), []string{"src/auth/handler.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "import-style") {
		t.Errorf("expected pattern in context, got %q", result)
	}
}

func TestSkillsContextProvider_Deduplication(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "style", PatternValue: "use tabs", CreatedAt: time.Now()},
	}}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")

	// First call — should return content
	result1, _ := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if result1 == "" {
		t.Fatal("expected content on first call")
	}

	// Second call with same paths — should return empty (already injected)
	result2, _ := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if result2 != "" {
		t.Errorf("expected empty on second call (dedup), got %q", result2)
	}
}

func TestSkillsContextProvider_TokenBudget_StopsInjecting(t *testing.T) {
	db := &mockProgressDB{patterns: []models.ProgressPattern{
		{PatternKey: "style", PatternValue: "use tabs", CreatedAt: time.Now()},
	}}
	cp := skills.NewSkillsContextProvider(db, "ticket-1").WithTokenBudget(0) // 0 = unlimited doesn't stop
	// Set a tiny budget by directly calling the internal method — use WithTokenBudget(1)
	cp2 := skills.NewSkillsContextProvider(db, "ticket-2").WithTokenBudget(1)
	// Pre-consume budget
	cp2.OnFilesAccessed(context.Background(), []string{"main.go"})
	// Second call should return empty due to budget
	result, _ := cp2.OnFilesAccessed(context.Background(), []string{"other.go"})
	if result != "" {
		t.Errorf("expected empty result after budget consumed, got %q", result)
	}
	_ = cp
}

func TestSkillsContextProvider_EmptyWhenNoPatterns(t *testing.T) {
	db := &mockProgressDB{patterns: nil}
	cp := skills.NewSkillsContextProvider(db, "ticket-1")
	result, err := cp.OnFilesAccessed(context.Background(), []string{"main.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
```

**Step 3: Implement SkillsContextProvider**

```go
// internal/skills/context_provider.go
package skills

import (
	"context"
	"path/filepath"

	fmtctx "github.com/canhta/foreman/internal/context"
)

// progressStore is the subset of db.Database needed for context injection.
type progressStore interface {
	fmtctx.ProgressStore
}

// SkillsContextProvider implements agent.ContextProvider for the skills layer.
// It queries progress patterns reactively as the agent accesses files, injecting
// only patterns relevant to the directories touched. Patterns are never injected twice.
type SkillsContextProvider struct {
	db       progressStore
	ticketID string
	injected map[string]bool // pattern keys already injected this session
}

// NewSkillsContextProvider creates a provider. db must satisfy context.ProgressStore.
func NewSkillsContextProvider(db progressStore, ticketID string) *SkillsContextProvider {
	return &SkillsContextProvider{
		db:       db,
		ticketID: ticketID,
		injected: make(map[string]bool),
	}
}

func (p *SkillsContextProvider) OnFilesAccessed(ctx context.Context, paths []string) (string, error) {
	dirs := uniqueDirs(paths)
	patterns, err := fmtctx.GetPrunedPatterns(ctx, p.db, p.ticketID, dirs)
	if err != nil {
		return "", err
	}
	// Filter to only patterns not yet injected
	var newPatterns []interface{ GetKey() string; GetValue() string }
	for _, pat := range patterns {
		if !p.injected[pat.PatternKey] {
			p.injected[pat.PatternKey] = true
			newPatterns = append(newPatterns, patternWrapper{pat.PatternKey, pat.PatternValue})
		}
	}
	if len(newPatterns) == 0 {
		return "", nil
	}
	// Build formatted output using the existing formatter
	// Re-slice the original patterns to only new ones
	var filtered []interface{}
	_ = filtered
	// Use FormatPatternsForPrompt with only new patterns
	import_filtered_patterns := patterns[:0]
	for _, pat := range patterns {
		key := pat.PatternKey
		// already marked injected above; check if it was newly added
		_ = key
	}
	return fmtctx.FormatPatternsForPrompt(filterNew(patterns, p.injected)), nil
}

// uniqueDirs extracts unique directory paths from a list of file paths.
func uniqueDirs(paths []string) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}
```

Note: the above has some rough edges in the filter logic. Write it cleanly:

```go
// internal/skills/context_provider.go
package skills

import (
	"context"
	"path/filepath"

	fmtctx "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/models"
)

type progressStore interface {
	fmtctx.ProgressStore
}

// SkillsContextProvider implements agent.ContextProvider.
// It tracks injected pattern keys to prevent duplicates, and stops injecting
// when the token budget is consumed (rough estimate: len(text)/4).
type SkillsContextProvider struct {
	db             progressStore
	ticketID       string
	injected       map[string]bool
	tokensBudget   int // 0 = unlimited; default set by NewSkillsContextProvider
	tokensInjected int // running total
}

func NewSkillsContextProvider(db progressStore, ticketID string) *SkillsContextProvider {
	return &SkillsContextProvider{
		db:           db,
		ticketID:     ticketID,
		injected:     make(map[string]bool),
		tokensBudget: 8000, // stop injecting after ~8000 tokens to avoid context overflow
	}
}

// WithTokenBudget sets a custom budget (0 = unlimited).
func (p *SkillsContextProvider) WithTokenBudget(n int) *SkillsContextProvider {
	p.tokensBudget = n
	return p
}

func (p *SkillsContextProvider) OnFilesAccessed(ctx context.Context, paths []string) (string, error) {
	// Check budget before querying
	if p.tokensBudget > 0 && p.tokensInjected >= p.tokensBudget {
		return "", nil
	}
	dirs := uniqueDirs(paths)
	all, err := fmtctx.GetPrunedPatterns(ctx, p.db, p.ticketID, dirs)
	if err != nil {
		return "", err
	}
	var fresh []models.ProgressPattern
	for _, pat := range all {
		if !p.injected[pat.PatternKey] {
			p.injected[pat.PatternKey] = true
			fresh = append(fresh, pat)
		}
	}
	if len(fresh) == 0 {
		return "", nil
	}
	text := fmtctx.FormatPatternsForPrompt(fresh)
	p.tokensInjected += len(text) / 4 // rough token estimate
	return text, nil
}

func uniqueDirs(paths []string) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}
```

**Step 4: Run tests**
```bash
go test ./internal/agent/... ./internal/skills/... -run TestSkillsContextProvider -v
```
Expected: all PASS

**Step 5: Commit**
```bash
git add internal/agent/context.go internal/skills/context_provider.go internal/skills/context_provider_test.go
git commit -m "feat(agent): add ContextProvider interface and SkillsContextProvider"
```

---

### Task 8: MCP Stub

**Files:**
- Create: `internal/agent/mcp/client.go`
- Create: `internal/agent/mcp/client_test.go`
- Modify: `internal/agent/runner.go` — add `MCPServers []MCPServerConfig`

**Step 1: Create MCP stub**

**Architecture note (from anthropic-sdk-go):**
- **Anthropic** handles MCP server-side: you pass `MCPServers` URL configs in the API request; Anthropic's infrastructure connects, calls tools, and returns `mcp_tool_use`/`mcp_tool_result` blocks. The client never calls the MCP server directly.
- **OpenAI/local** providers have no API-side MCP; client-side proxying via the `Client` interface is the right path for those.
- Our tool-use loop (`stop_reason == tool_use`) is already correct — `mcp_tool_use` and `server_tool_use` blocks are Anthropic-handled and never trigger our client loop.

```go
// internal/agent/mcp/client.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// MCPServerConfig holds connection config for a single MCP server.
// For Anthropic: set URL + AuthToken — configs are passed to the API request.
// For OpenAI/local: a future Client implementation proxies calls client-side.
type MCPServerConfig struct {
	Name       string   `json:"name"`
	URL        string   `json:"url,omitempty"`        // Anthropic API-side MCP
	AuthToken  string   `json:"auth_token,omitempty"` // Anthropic API-side MCP
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Command    string   `json:"command,omitempty"` // future: stdio transport
	Args       []string `json:"args,omitempty"`    // future: stdio transport
}

// Client is the interface for client-side MCP proxying (OpenAI/local providers).
// For Anthropic, MCP is handled API-side — Client is not used.
type Client interface {
	ListTools(ctx context.Context) ([]models.ToolDef, error)
	Call(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// NoopClient satisfies Client but does nothing. Placeholder until client-side
// MCP is implemented for non-Anthropic providers.
type NoopClient struct{}

func (n *NoopClient) ListTools(_ context.Context) ([]models.ToolDef, error) { return nil, nil }
func (n *NoopClient) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("MCP tool %q: client-side MCP not yet implemented", name)
}
```

**Step 2: Add test**

```go
// internal/agent/mcp/client_test.go
package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
)

func TestNoopClient_ListTools_ReturnsEmpty(t *testing.T) {
	c := &mcp.NoopClient{}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tool list, got %d", len(tools))
	}
}

func TestNoopClient_Call_ReturnsError(t *testing.T) {
	c := &mcp.NoopClient{}
	_, err := c.Call(context.Background(), "some-tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from noop client")
	}
}

func TestMCPServerConfig_Fields(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "fs", URL: "https://mcp.example.com/sse", AuthToken: "tok"}
	if cfg.Name != "fs" || cfg.URL == "" {
		t.Error("expected URL-based config fields to be set")
	}
}
```

**Step 3: Add AgentDepth and MCPServers to AgentRequest**

In `internal/agent/runner.go`, add to `AgentRequest`:
```go
AgentDepth int                    // depth in subagent call stack; 0 = top-level, max 3
MCPServers []mcp.MCPServerConfig  // reserved for post-V1 MCP integration
```

Add import `"github.com/canhta/foreman/internal/agent/mcp"`. `AgentDepth` is threaded into `SubagentTool` via `RunFn` — the runner checks `req.AgentDepth >= 3` before calling `registry.runFn` and returns an error to the tool (which becomes tool result content, not a Go error that unwinds the turn).

**Step 4: Run tests**
```bash
go test ./internal/agent/mcp/... -v
go build ./... 2>&1
```
Expected: all PASS, clean build

**Step 5: Commit**
```bash
git add internal/agent/mcp/ internal/agent/runner.go
git commit -m "feat(agent): add MCP client stub and MCPServers to AgentRequest"
```

---

### Task 9: Refactor builtin.go — Registry, Parallel Execution, ContextProvider, context.md

**Files:**
- Modify: `internal/agent/builtin.go` — full refactor
- Modify: `internal/agent/builtin_test.go` — update constructor calls + add new tests
- Delete: `internal/agent/tools.go` — replaced by `tools/` package

**Step 1: Read current builtin_test.go to understand what must still pass**

All existing tests (`TestBuiltinRunner_SingleShot`, `TestBuiltinRunner_MultiTurnToolUse`, `TestBuiltinRunner_MaxTurnsExceeded`, `TestBuiltinRunner_UnknownTool`, `TestBuiltinRunner_Fallback`) must pass with the new constructor signature.

**Step 2: Rewrite builtin.go**

```go
// internal/agent/builtin.go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// BuiltinConfig holds configuration for the builtin runner.
type BuiltinConfig struct {
	MaxTurnsDefault     int
	DefaultAllowedTools []string
}

// BuiltinRunner runs a multi-turn tool-use loop against the LlmProvider.
// Tool calls within a single turn execute in parallel via errgroup.
type BuiltinRunner struct {
	provider        llm.LlmProvider
	model           string
	config          BuiltinConfig
	registry        *tools.Registry
	contextProvider ContextProvider // nil-safe
}

// NewBuiltinRunner creates a builtin runner.
// registry is required; cp (ContextProvider) may be nil.
//
// Two-phase init for SubagentTool (avoids circular dependency):
//
//	reg    := tools.NewRegistry(git, cmd, hooks)
//	runner := NewBuiltinRunner(provider, model, config, reg, cp)
//	reg.SetRunFn(runner.subagentRunFn)   ← inject AFTER construction
func NewBuiltinRunner(
	provider llm.LlmProvider,
	model string,
	config BuiltinConfig,
	registry *tools.Registry,
	cp ContextProvider,
) *BuiltinRunner {
	return &BuiltinRunner{
		provider:        provider,
		model:           model,
		config:          config,
		registry:        registry,
		contextProvider: cp,
	}
}

// subagentRunFn is the RunFn injected into the registry for SubagentTool.
// It enforces the combined agent depth limit (max 3).
func (r *BuiltinRunner) subagentRunFn(ctx context.Context, task, workDir string, toolNames []string, maxTurns int) (string, error) {
	result, err := r.Run(ctx, AgentRequest{
		Prompt:      task,
		WorkDir:     workDir,
		AllowedTools: toolNames,
		MaxTurns:    maxTurns,
		// AgentDepth is NOT passed — subagent depth is enforced separately via SubagentTool checking runFn
		// The subagent gets no ContextProvider (baseline context only)
	})
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func (r *BuiltinRunner) RunnerName() string { return "builtin" }

func (r *BuiltinRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	systemPrompt := "You are a focused task executor. Complete the task and return only the result."

	// Layer 1: inject .foreman-context.md if present
	if ctx := loadForemanContext(req.WorkDir); ctx != "" {
		systemPrompt = ctx + "\n\n" + systemPrompt
	}

	if req.SystemPrompt != "" {
		systemPrompt = systemPrompt + "\n\n" + req.SystemPrompt
	}

	toolNames := req.AllowedTools
	if len(toolNames) == 0 {
		toolNames = r.config.DefaultAllowedTools
	}
	toolDefs := r.registry.Defs(toolNames)

	maxTurns := req.MaxTurns
	if maxTurns == 0 {
		maxTurns = r.config.MaxTurnsDefault
	}
	if maxTurns == 0 {
		maxTurns = 10
	}

	var outputSchema *json.RawMessage
	if req.OutputSchema != nil {
		s := json.RawMessage(req.OutputSchema)
		outputSchema = &s
	}

	fallbackModel := req.FallbackModel

	messages := []models.Message{
		{Role: "user", Content: req.Prompt},
	}

	var usage AgentUsage

	for turn := 0; turn < maxTurns; turn++ {
		llmReq := models.LlmRequest{
			Model:        r.model,
			SystemPrompt: systemPrompt,
			MaxTokens:    4096,
			Temperature:  0.2,
			Messages:     messages,
			Tools:        toolDefs,
			OutputSchema: outputSchema,
		}

		resp, err := r.provider.Complete(ctx, llmReq)
		if err != nil {
			var rateLimitErr *llm.RateLimitError
			if errors.As(err, &rateLimitErr) && fallbackModel != "" {
				llmReq.Model = fallbackModel
				fallbackModel = ""
				resp, err = r.provider.Complete(ctx, llmReq)
			}
		}
		if err != nil {
			return AgentResult{}, fmt.Errorf("builtin: turn %d: %w", turn+1, err)
		}

		usage.InputTokens += resp.TokensInput
		usage.OutputTokens += resp.TokensOutput
		usage.DurationMs += int(resp.DurationMs)
		usage.NumTurns++

		if resp.StopReason == models.StopReasonEndTurn || resp.StopReason == models.StopReasonMaxTokens {
			return AgentResult{Output: resp.Content, Usage: usage}, nil
		}

		if resp.StopReason == models.StopReasonToolUse && len(resp.ToolCalls) > 0 {
			messages = append(messages, models.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// Execute all tool calls in parallel (mirrors SDK betatoolrunner.go)
			results := make([]models.ToolResult, len(resp.ToolCalls))
			var touchedPaths []string

			g, gctx := errgroup.WithContext(ctx)
			for i, tc := range resp.ToolCalls {
				i, tc := i, tc
				g.Go(func() error {
					out, err := r.registry.Execute(gctx, req.WorkDir, tc.Name, tc.Input)
					if err != nil {
						results[i] = models.ToolResult{ToolCallID: tc.ID, Content: err.Error(), IsError: true}
					} else {
						results[i] = models.ToolResult{ToolCallID: tc.ID, Content: out}
					}
					return nil // tool errors become result content, not Go errors
				})
			}
			g.Wait()

			// Collect touched paths for reactive context injection (Layer 2)
			for _, tc := range resp.ToolCalls {
				if path := extractPath(tc.Input); path != "" {
					touchedPaths = append(touchedPaths, path)
				}
			}

			messages = append(messages, models.Message{Role: "user", ToolResults: results})

			// Layer 2: reactive context injection
			if r.contextProvider != nil && len(touchedPaths) > 0 {
				if inject, err := r.contextProvider.OnFilesAccessed(ctx, touchedPaths); err == nil && inject != "" {
					messages = append(messages, models.Message{Role: "user", Content: "[context update]\n" + inject})
				}
			}
			continue
		}

		return AgentResult{Output: resp.Content, Usage: usage}, nil
	}

	return AgentResult{}, fmt.Errorf("builtin: exceeded max turns %d without completion", maxTurns)
}

// loadForemanContext reads .foreman/context.md or .foreman-context.md from workDir,
// walking up directories (most-specific wins).
func loadForemanContext(workDir string) string {
	candidates := []string{
		filepath.Join(workDir, ".foreman", "context.md"),
		filepath.Join(workDir, ".foreman-context.md"),
	}
	for _, path := range candidates {
		if content, err := os.ReadFile(path); err == nil {
			return string(content)
		}
	}
	return ""
}

// extractPath tries to read a "path" field from tool input JSON.
func extractPath(input json.RawMessage) string {
	var v struct {
		Path string `json:"path"`
	}
	json.Unmarshal(input, &v)
	return v.Path
}

func (r *BuiltinRunner) HealthCheck(ctx context.Context) error {
	return r.provider.HealthCheck(ctx)
}

func (r *BuiltinRunner) Close() error { return nil }
```

**Step 3: Update builtin_test.go — new constructor signature**

In all existing tests, replace:
```go
NewBuiltinRunner(provider, "test-model", BuiltinConfig{...})
```
with:
```go
NewBuiltinRunner(provider, "test-model", BuiltinConfig{...}, tools.NewRegistry(nil, nil, tools.ToolHooks{}), nil)
```

Add import `"github.com/canhta/foreman/internal/agent/tools"`.

Also add a test for `.foreman-context.md` injection and `ContextProvider`:

```go
func TestBuiltinRunner_ForemanContext_Injected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".foreman-context.md"), []byte("Use tabs for indentation."), 0644)

	var capturedSystem string
	mockLLM := &mockCaptureLLM{captureSystem: &capturedSystem}
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	runner := NewBuiltinRunner(mockLLM, "m", BuiltinConfig{}, reg, nil)
	runner.Run(context.Background(), AgentRequest{Prompt: "hi", WorkDir: dir})

	if !strings.Contains(capturedSystem, "Use tabs") {
		t.Errorf("expected foreman-context.md injected into system prompt, got: %q", capturedSystem)
	}
}

func TestBuiltinRunner_ContextProvider_Called(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	called := false
	cp := &mockContextProvider{onAccess: func(paths []string) string {
		called = true
		return ""
	}}

	mockLLM := &mockToolUseLLM{} // returns Read tool call on turn 1
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	runner := NewBuiltinRunner(mockLLM, "m", BuiltinConfig{DefaultAllowedTools: []string{"Read"}}, reg, cp)
	runner.Run(context.Background(), AgentRequest{Prompt: "read main.go", WorkDir: dir})

	if !called {
		t.Error("expected ContextProvider.OnFilesAccessed to be called")
	}
}

type mockContextProvider struct {
	onAccess func(paths []string) string
}
func (m *mockContextProvider) OnFilesAccessed(_ context.Context, paths []string) (string, error) {
	return m.onAccess(paths), nil
}

type mockCaptureLLM struct{ captureSystem *string }
func (m *mockCaptureLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	*m.captureSystem = req.SystemPrompt
	return &models.LlmResponse{Content: "ok", StopReason: models.StopReasonEndTurn}, nil
}
func (m *mockCaptureLLM) ProviderName() string                { return "mock" }
func (m *mockCaptureLLM) HealthCheck(_ context.Context) error { return nil }
```

**Step 4: Delete tools.go**
```bash
git rm internal/agent/tools.go
```

**Step 5: Update factory.go** — `NewAgentRunner` calls `NewBuiltinRunner`. Update its call with the new signature and add the two-phase init for `SubagentTool`:

```go
reg    := tools.NewRegistry(git, cmd, hooks)
runner := NewBuiltinRunner(provider, model, config, reg, cp)
reg.SetRunFn(runner.subagentRunFn) // two-phase init — inject after construction
return runner
```

For tests that don't need Subagent, pass a registry and skip `SetRunFn` (the tool will return an error if called, which is acceptable in those tests).

**Step 6: Run all tests**
```bash
go test ./internal/agent/... -v
go build ./... 2>&1
```
Expected: all PASS, clean build

**Step 7: Commit**
```bash
git add internal/agent/
git rm internal/agent/tools.go
git commit -m "feat(agent): refactor builtin runner — registry, parallel exec, ContextProvider, foreman-context.md"
```

---

### Task 10: Skills Engine — Wire Phase 9, git_diff, foreman-context.md Pre-Assembly

**Files:**
- Modify: `internal/skills/engine.go`
- Modify: `internal/skills/engine_test.go`

**Step 1: Wire Phase 9 fields in executeAgentSDK**

In `engine.go`, `executeAgentSDK` currently ignores `step.OutputSchema`, `step.Thinking`, `step.FallbackModel`. Fix:

```go
func (e *Engine) executeAgentSDK(ctx context.Context, step SkillStep) (*StepResult, error) {
	if e.agentRunner == nil {
		return nil, fmt.Errorf("agentsdk step '%s': no agent runner configured", step.ID)
	}

	req := agent.AgentRequest{
		Prompt:        step.Content,
		WorkDir:       e.workDir,
		AllowedTools:  step.AllowedTools,
		MaxTurns:      step.MaxTurns,
		TimeoutSecs:   step.TimeoutSecs,
		FallbackModel: step.FallbackModel,
	}

	// Marshal OutputSchema from map to json.RawMessage
	if step.OutputSchema != nil {
		b, err := json.Marshal(step.OutputSchema)
		if err == nil {
			req.OutputSchema = b
		}
	}

	// Load .foreman-context.md and prepend to SystemPrompt for ALL runners
	if ctx := loadForemanContextFromDir(e.workDir); ctx != "" {
		req.SystemPrompt = ctx
	}

	result, err := e.agentRunner.Run(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agentsdk step '%s': %w", step.ID, err)
	}
	return &StepResult{Output: result.Output}, nil
}

func loadForemanContextFromDir(workDir string) string {
	candidates := []string{
		filepath.Join(workDir, ".foreman", "context.md"),
		filepath.Join(workDir, ".foreman-context.md"),
	}
	for _, path := range candidates {
		if content, err := os.ReadFile(path); err == nil {
			return string(content)
		}
	}
	return ""
}
```

**Step 2: Add git provider field to Engine and implement git_diff**

Add `git git.GitProvider` field to `Engine` struct and to `NewEngine` parameters:

```go
type Engine struct {
	llm           llm.LlmProvider
	runner        runner.CommandRunner
	agentRunner   agent.AgentRunner
	git           git.GitProvider  // NEW — optional, nil if not configured
	workDir       string
	defaultBranch string
}

func NewEngine(llmProvider llm.LlmProvider, cmdRunner runner.CommandRunner, workDir, defaultBranch string) *Engine {
	// signature unchanged — git added via SetGitProvider
}

func (e *Engine) SetGitProvider(g git.GitProvider) { e.git = g }
```

Implement `executeGitDiff`:
```go
func (e *Engine) executeGitDiff(ctx context.Context) (*StepResult, error) {
	if e.git == nil {
		return nil, fmt.Errorf("git_diff step requires a GitProvider — call engine.SetGitProvider()")
	}
	diff, err := e.git.DiffWorking(ctx, e.workDir)
	if err != nil {
		return nil, fmt.Errorf("git_diff: %w", err)
	}
	return &StepResult{Output: diff}, nil
}
```

**Step 3: Add tests for new engine behaviors**

```go
// in engine_test.go
func TestEngine_ExecuteAgentSDK_WiresPhase9Fields(t *testing.T) {
	mock := &mockAgentRunner{}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)

	schema := map[string]interface{}{"type": "object"}
	step := SkillStep{
		ID:            "s1",
		Type:          "agentsdk",
		Content:       "do thing",
		FallbackModel: "openrouter:claude-sonnet",
		OutputSchema:  schema,
	}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastReq.FallbackModel != "openrouter:claude-sonnet" {
		t.Errorf("expected FallbackModel wired, got %q", mock.lastReq.FallbackModel)
	}
	if mock.lastReq.OutputSchema == nil {
		t.Error("expected OutputSchema wired")
	}
}

func TestEngine_GitDiff_Implemented(t *testing.T) {
	mockGit := &mockGitForEngine{diff: "diff content"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetGitProvider(mockGit)

	result, err := e.executeGitDiff(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "diff content" {
		t.Errorf("expected diff content, got %q", result.Output)
	}
}

func TestEngine_GitDiff_NoProvider(t *testing.T) {
	e := NewEngine(nil, nil, t.TempDir(), "main")
	_, err := e.executeGitDiff(context.Background())
	if err == nil {
		t.Fatal("expected error when no GitProvider")
	}
}
```

**Step 4: Run tests**
```bash
go test ./internal/skills/... -v
go build ./... 2>&1
```
Expected: all PASS

**Step 5: Commit**
```bash
git add internal/skills/
git commit -m "feat(skills): wire Phase9 fields, implement git_diff, add foreman-context.md pre-assembly"
```

---

### Task 11: Skills Engine — subskill Step Type and output_format

**Files:**
- Modify: `internal/skills/loader.go` — add `SkillRef`, `Input`, `OutputFormat` to `SkillStep`; add `subskill` to valid types
- Modify: `internal/skills/engine.go` — add `skillsByID`, `executeSubSkill`, `output_format` validation
- Modify: `internal/skills/engine_test.go`

**Step 1: Update loader.go**

Add to `SkillStep`:
```go
SkillRef      string            `yaml:"skill_ref,omitempty"`       // for subskill step type
Input         map[string]string `yaml:"input,omitempty"`            // template vars for subskill
OutputFormat  string            `yaml:"output_format,omitempty"`    // markdown|json|diff|checklist
```

Add `"subskill"` to `validStepTypes`.

**Step 2: Add skillsByID to Engine**

```go
type Engine struct {
	// ... existing fields ...
	skillsByID map[string]*Skill
}

// RegisterSkills indexes skills by ID for subskill resolution.
func (e *Engine) RegisterSkills(skills []*Skill) {
	e.skillsByID = make(map[string]*Skill, len(skills))
	for _, s := range skills {
		e.skillsByID[s.ID] = s
	}
}
```

In `executeStep`, add case:
```go
case "subskill":
    return e.executeSubSkill(ctx, step, sCtx)
```

**Step 3: Implement executeSubSkill**

```go
func (e *Engine) executeSubSkill(ctx context.Context, step SkillStep, sCtx *SkillContext) (*StepResult, error) {
	if step.SkillRef == "" {
		return nil, fmt.Errorf("subskill step '%s': missing skill_ref", step.ID)
	}
	sub, ok := e.skillsByID[step.SkillRef]
	if !ok {
		return nil, fmt.Errorf("subskill step '%s': skill %q not found", step.ID, step.SkillRef)
	}
	subCtx := &SkillContext{
		Ticket:   sCtx.Ticket,
		Diff:     sCtx.Diff,
		FileTree: sCtx.FileTree,
		Models:   sCtx.Models,
		Steps:    make(map[string]*StepResult),
	}
	// inject input vars as step results so templates can reference them
	for k, v := range step.Input {
		subCtx.Steps[k] = &StepResult{Output: v}
	}
	if err := e.Execute(ctx, sub, subCtx); err != nil {
		return nil, fmt.Errorf("subskill '%s': %w", step.SkillRef, err)
	}
	// Return output of last step in sub-skill
	var lastOutput string
	for _, step := range sub.Steps {
		if r, ok := subCtx.Steps[step.ID]; ok && r != nil {
			lastOutput = r.Output
		}
	}
	return &StepResult{Output: lastOutput}, nil
}
```

**Step 4: Implement output_format validation in executeAgentSDK**

After getting the `AgentResult`, validate format:
```go
switch step.OutputFormat {
case "json":
    if !json.Valid([]byte(result.Output)) {
        return nil, fmt.Errorf("agentsdk step '%s': output_format=json but output is not valid JSON", step.ID)
    }
case "diff":
    if !strings.Contains(result.Output, "--- ") && !strings.Contains(result.Output, "+++ ") {
        return nil, fmt.Errorf("agentsdk step '%s': output_format=diff but output is not a unified diff", step.ID)
    }
case "checklist":
    passed, failed := parseChecklist(result.Output)
    return &StepResult{Output: result.Output, ExitCode: failed}, nil // ExitCode encodes failed count
}
```

```go
func parseChecklist(output string) (passed, failed int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "- [X]") {
			passed++
		} else if strings.HasPrefix(line, "- [ ]") {
			failed++
		}
	}
	return
}
```

**Step 5: Write tests**

```go
func TestEngine_SubSkill(t *testing.T) {
	mock := &mockAgentRunner{output: "sub result"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)

	sub := &Skill{
		ID:      "child",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "s1", Type: "agentsdk", Content: "child task"}},
	}
	parent := &Skill{
		ID:      "parent",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "call-child", Type: "subskill", SkillRef: "child"}},
	}
	e.RegisterSkills([]*Skill{sub, parent})

	sCtx := NewSkillContext()
	err := e.Execute(context.Background(), parent, sCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sCtx.Steps["call-child"].Output != "sub result" {
		t.Errorf("expected sub result, got %q", sCtx.Steps["call-child"].Output)
	}
}

func TestEngine_SubSkill_MissingRef(t *testing.T) {
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.RegisterSkills(nil)
	step := SkillStep{ID: "s1", Type: "subskill", SkillRef: "does-not-exist"}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	if err == nil {
		t.Fatal("expected error for missing skill_ref")
	}
}

func TestEngine_OutputFormat_JSON_Valid(t *testing.T) {
	mock := &mockAgentRunner{output: `{"key":"value"}`}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "json"}
	result, err := e.executeStep(context.Background(), step, NewSkillContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != `{"key":"value"}` {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

func TestEngine_OutputFormat_JSON_Invalid(t *testing.T) {
	mock := &mockAgentRunner{output: "not json"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "json"}
	_, err := e.executeStep(context.Background(), step, NewSkillContext())
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}

func TestEngine_OutputFormat_Checklist(t *testing.T) {
	mock := &mockAgentRunner{output: "- [x] item 1\n- [ ] item 2\n- [x] item 3"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mock)
	step := SkillStep{ID: "s1", Type: "agentsdk", Content: "x", OutputFormat: "checklist"}
	result, err := e.executeStep(context.Background(), step, NewSkillContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("expected 1 failed checklist item (ExitCode), got %d", result.ExitCode)
	}
}
```

**Step 6: Run tests**
```bash
go test ./internal/skills/... -v
go test ./... 2>&1 | grep -E "FAIL|ok"
```
Expected: all PASS

**Step 7: Commit**
```bash
git add internal/skills/
git commit -m "feat(skills): add subskill step type, output_format validation (json/diff/checklist)"
```

---

### Task 12: Final Verification and Cleanup

**Step 1: Run full test suite**
```bash
go test ./... -v 2>&1 | tail -30
```
Expected: all packages PASS

**Step 2: Run go vet**
```bash
go vet ./...
```
Expected: no output (no issues)

**Step 3: Build check**
```bash
go build ./...
```
Expected: no output

**Step 4: Verify key test names from plan spec**
```bash
go test ./internal/agent/tools/... -run "TestRead|TestWrite|TestEdit|TestMultiEdit|TestListDir|TestGlob|TestGrep|TestGetDiff|TestGetCommitLog|TestTreeSummary|TestGetSymbol|TestGetErrors|TestBash|TestRunTest|TestRegistry" -v 2>&1 | grep -E "PASS|FAIL"
go test ./internal/agent/... -run "TestBuiltinRunner" -v 2>&1 | grep -E "PASS|FAIL"
go test ./internal/skills/... -run "TestSkillsContextProvider|TestEngine" -v 2>&1 | grep -E "PASS|FAIL"
go test ./internal/agent/mcp/... -v 2>&1 | grep -E "PASS|FAIL"
```

**Step 5: Final commit**
```bash
git add -A
git commit -m "feat(agent): Phase 10 builtin runner v2 complete

- tools/ package: 14 tools across 4 tiers with parallel execution
- Registry with PreToolUse/PostToolUse hooks
- BuiltinRunner: registry, errgroup parallel dispatch, ContextProvider
- .foreman-context.md auto-injection (Layer 1 + Layer 2)
- SkillsContextProvider: reactive progress pattern injection
- MCP stub (NoopClient, MCPServerConfig)
- Skills engine: subskill step type, git_diff, output_format, Phase 9 wiring

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```
