# OpenCode Analysis 04: Tool Usage, Capability Routing & MCP Integration

**Date:** 2026-03-11  
**Domain:** Tool definition, registration, input validation, permission system, output truncation, MCP integration  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`

---

## How OpenCode Solves This

### Tool Definition and Registration

`packages/opencode/src/tool/tool.ts:48–88` — every tool is wrapped by `Tool.define()`:

1. **Full Zod schema validation** at call time — type, enum, constraint checking
2. **`formatValidationError` hook** — tool-specific LLM-actionable error formatting
3. **Automatic output truncation** after every call unless tool sets `result.metadata.truncated` itself
4. **Structured output**: `{ title, metadata, output, attachments }`
5. **Tool.Context** first-class object: `sessionID`, `messageID`, `agent`, `callID`, `abort: AbortSignal`, `metadata()` (streaming UI updates), `ask()` (interactive permission from within tool)

**Registry** (`packages/opencode/src/tool/registry.ts`):
- Lazy-initialized once per project instance
- **Filesystem plugin loading**: scans `{tool,tools}/*.{js,ts}` in config directories, dynamically imports them
- **Feature-flag gated tools**: `BatchTool` behind `experimental.batch_tool`, `LspTool` behind env var, `PlanExitTool` behind plan mode flag
- **Model-specific tool filtering**: `codesearch`/`websearch` only for certain providers; `apply_patch` for GPT-4+ (non-OSS) swapping out `edit`/`write`
- **Plugin hooks on tool definitions**: `Plugin.trigger("tool.definition", ...)` — plugins can modify tool `description` and `parameters` before they reach the LLM

### Permission System

`packages/opencode/src/permission/next.ts` — three-action system: `allow`, `deny`, `ask`.

- **Default is `ask`** — interactive prompt via UI when no rule matches
- Uses `Wildcard.match()` — supports `**` glob patterns
- `ask()` publishes `permission.asked` bus event; execution **blocks** until `reply()` is called
- `Reply` has three values: `once`, `always` (persist for session), `reject` (block + user correction message)
- `CorrectedError` carries user's feedback back to the LLM as context
- `reply("always")` retroactively resolves all pending permission requests for the same rule
- Disabled tools are filtered from the LLM's tool list entirely (`disabled()` function)

### Output Truncation

`packages/opencode/src/tool/truncation.ts` — rich truncation with lifecycle:
- `head`/`tail` direction support
- Per-line byte counting
- Saves full output to disk with ascending ID filename
- **Retention cleanup**: runs every hour, deletes files older than 7 days
- **Context-aware hint**: if agent has `task` tool → "delegate to explore agent"; otherwise → "use Grep/Read with offset/limit"
- `TruncateHint` per-tool `metadata.truncated` flag — tools that handle their own truncation skip the wrapper

### MCP Integration

`packages/opencode/src/mcp/index.ts` — full-featured:
- **Transport fallback**: tries `StreamableHTTP` first, falls back to `SSE`
- **`ToolListChangedNotification` handler**: refreshes registry when server hot-reloads tools
- **OAuth2 PKCE flow**: full `McpOAuthProvider` with CSRF protection, callback server, browser-open-failed event
- **Descendant process cleanup**: uses `pgrep -P` recursively on shutdown
- **Per-server configurable timeout**

---

## Issues in Our Current Approach

### G1 — Input Validation Only Checks Field Presence (High)

`internal/agent/tools/registry.go:165–182` — `validateRequiredFields()` only checks that required field names are present in the JSON object. No type checking, no enum validation, no constraint checking. Error message: `"missing required field 'X'"` — no LLM guidance on how to correct.

### G2 — Truncation `savedPath` Always Empty (High)

`registry.go` lines 100, 127 — `TruncateHint("")` always passes an empty string, producing the useless message: `"Full output saved to: "`. `SaveTruncatedOutput()` exists but is never called from the registry. The hint does not help the LLM recover from truncated output.

### G3 — Bash Tool Uses `strings.Fields` for Argument Splitting (High)

`tools/exec.go` — `strings.Fields(cmd)` breaks on quoted arguments like `bash -c "echo 'hello world'"` (splits into `["bash", "-c", "echo", "'hello", "world'"]`). Silently misparses commands with quoted strings.

### G4 — Default Permission Is Deny with No Escalation Path (Medium)

`internal/agent/permission.go:33–47` — default action is `ActionDeny`. Any unconfigured tool call is blocked silently with no guidance. Only `filepath.Match` — no `**` recursive glob. No `ActionAsk` for interactive escalation. No correction message mechanism — LLM gets no feedback on why an action was denied.

### G5 — No `CorrectedError`/`DeniedError` with Context (Medium)

When the permission system blocks a call, the LLM receives `"permission denied"` with no suggestion for what to do next. No correction feedback path.

### G6 — No `metadata()` Streaming Callback for Long-Running Tools (Medium)

Long-running tools (`RunTest`, `Bash`) provide no intermediate feedback to the UI. The dashboard shows nothing until the tool completes. OpenCode's `metadata()` callback streams partial progress.

### G7 — No Truncation Cleanup Scheduler (Low)

No equivalent to OpenCode's 7-day retention + hourly cleanup. Disk fills up indefinitely with truncated tool output files.

### G8 — No Tool List Change Notification for MCP (Medium)

`internal/mcp/` — if an MCP server hot-reloads its tools, Foreman doesn't know. The cached tool summary becomes stale until restart.

### G9 — MCP Server Name Normalization Collisions (Low)

Two servers with names that normalize identically (e.g., `my-server` and `my_server`) shadow each other silently. No collision detection.

---

## Specific Improvements to Adopt

### I1: Full Input Validation (Type + Enum + Constraints)
Replace `validateRequiredFields()` with a `ValidateInput()` function that performs type checking, enum membership validation, and min/max constraint checking. Return LLM-actionable error messages.

**Effort:** Medium.

### I2: Wire `savedPath` Into Registry Truncation
Pass `dataDir` to `Registry` at construction. After `TruncateOutput()`, call `SaveTruncatedOutput()` and pass the returned path to `TruncateHint()`.

**Effort:** Low.

### I3: Fix Bash Argument Splitting
Replace `strings.Fields(cmd)` with `shlex.Split(cmd)` (`github.com/google/shlex` package). Handles POSIX shell quoting correctly.

**Effort:** Very low.

### I4: Add `ActionAsk` with Permission Asker Interface
Add `ActionAsk` as a third action type. Create a `PermissionAsker` interface. Change the default from `deny` to `ask`. Wire the dashboard WebSocket as a concrete asker for interactive mode; use a policy (allow/deny) for headless mode.

**Effort:** Medium.

### I5: Add `DeniedError` and `CorrectedError` Types
Structured error types that carry context back to the LLM: why the action was denied, and what the user's correction feedback says.

**Effort:** Low.

### I6: Tool List Change Handler for MCP
Handle `notifications/tools/list_changed` in the stdio client's read loop. Fire a callback that triggers `CacheToolSummaries()` in the manager.

**Effort:** Low.

### I7: Truncation Cleanup Scheduler
Add `CleanupTruncatedOutputs(dataDir)` and schedule it from the daemon's main loop (e.g., every hour on a ticker).

**Effort:** Low.

---

## Concrete Implementation Suggestions

### I1 — Full Input Validation

```go
// internal/agent/tools/validator.go
package tools

import (
    "encoding/json"
    "fmt"
    "strings"
)

type schemaField struct {
    Type    string        `json:"type"`
    Enum    []interface{} `json:"enum"`
    Minimum *float64      `json:"minimum"`
    Maximum *float64      `json:"maximum"`
}

type schemaDoc struct {
    Properties map[string]schemaField `json:"properties"`
    Required   []string               `json:"required"`
}

// ValidateInput checks required fields, types, enums, and constraints.
// Returns an LLM-actionable error message on failure.
func ValidateInput(toolID string, schemaDef, input json.RawMessage) error {
    var s schemaDoc
    if err := json.Unmarshal(schemaDef, &s); err != nil {
        return nil // permissive on schema parse failure
    }
    var obj map[string]json.RawMessage
    if err := json.Unmarshal(input, &obj); err != nil {
        return fmt.Errorf("the %s tool received invalid JSON input: %w.\nPlease rewrite the input as a valid JSON object.", toolID, err)
    }

    var errs []string
    for _, field := range s.Required {
        if _, ok := obj[field]; !ok {
            errs = append(errs, fmt.Sprintf("  - missing required field %q", field))
        }
    }
    for field, raw := range obj {
        def, ok := s.Properties[field]
        if !ok { continue }
        if len(def.Enum) > 0 && !enumContains(def.Enum, raw) {
            vals := make([]string, len(def.Enum))
            for i, v := range def.Enum { vals[i] = fmt.Sprintf("%v", v) }
            errs = append(errs, fmt.Sprintf("  - field %q must be one of: %s", field, strings.Join(vals, ", ")))
        }
        if (def.Type == "number" || def.Type == "integer") {
            var n float64
            if err := json.Unmarshal(raw, &n); err != nil {
                errs = append(errs, fmt.Sprintf("  - field %q must be a number", field))
                continue
            }
            if def.Minimum != nil && n < *def.Minimum {
                errs = append(errs, fmt.Sprintf("  - field %q must be >= %v", field, *def.Minimum))
            }
            if def.Maximum != nil && n > *def.Maximum {
                errs = append(errs, fmt.Sprintf("  - field %q must be <= %v", field, *def.Maximum))
            }
        }
    }
    if len(errs) == 0 { return nil }
    return fmt.Errorf(
        "the %s tool was called with invalid arguments:\n%s\nPlease rewrite the input so it satisfies the expected schema.",
        toolID, strings.Join(errs, "\n"),
    )
}
```

Replace in `registry.go`:
```go
if err := ValidateInput(name, t.Schema(), input); err != nil {
    return "", err
}
```

### I3 — Fix Bash Argument Splitting

```go
import "github.com/google/shlex"

// Replace:
// args := strings.Fields(cmd)
// With:
args, err := shlex.Split(cmd)
if err != nil {
    return "", fmt.Errorf("bash: failed to parse command %q: %w", cmd, err)
}
```

### I4 — Permission Asker Interface

```go
// internal/agent/permission.go — extend existing Action type
type Action string
const (
    ActionAllow Action = "allow"
    ActionDeny  Action = "deny"
    ActionAsk   Action = "ask"  // new
)

// Default is now ask (not deny)
func Evaluate(permission, pattern string, rules Ruleset) Action {
    result := ActionAsk  // changed from ActionDeny
    for _, rule := range rules {
        if matchPermission(permission, rule.Permission) && matchPattern(pattern, rule.Pattern) {
            result = rule.Action
        }
    }
    return result
}

// PermissionAsker is implemented by the dashboard WebSocket handler for interactive sessions.
type PermissionAsker interface {
    Ask(ctx context.Context, req PermissionRequest) (PermissionReply, error)
}

type PermissionRequest struct {
    ToolName   string
    Permission string
    Pattern    string
    Input      json.RawMessage
}

type PermissionReply struct {
    Reply    string // "once", "always", "reject"
    Feedback string
}

func EvaluateWithAsk(ctx context.Context, perm, pattern string, rules Ruleset, asker PermissionAsker) (Action, string, error) {
    action := Evaluate(perm, pattern, rules)
    if action != ActionAsk || asker == nil {
        return action, "", nil
    }
    reply, err := asker.Ask(ctx, PermissionRequest{Permission: perm, Pattern: pattern})
    if err != nil { return ActionDeny, "", err }
    switch reply.Reply {
    case "always", "once":
        return ActionAllow, "", nil
    default:
        return ActionDeny, reply.Feedback, nil
    }
}
```

### I5 — Structured Error Types

```go
// internal/agent/tools/errors.go
type DeniedError struct {
    ToolName   string
    Permission string
    Pattern    string
}
func (e *DeniedError) Error() string {
    return fmt.Sprintf(
        "the user has a permission rule that prevents the %s tool from accessing %q (permission: %s). "+
            "Consider a different approach or a narrower target path.",
        e.ToolName, e.Pattern, e.Permission,
    )
}

type CorrectedError struct{ Feedback string }
func (e *CorrectedError) Error() string {
    return "the user rejected this tool call with the following feedback: " + e.Feedback
}
```

### I6 — MCP Tool List Change Handler

```go
// internal/mcp/stdio_client.go — in readLoop() notification handling
var notification struct {
    Method string `json:"method"`
    ID     *int64 `json:"id"`
}
if err := json.Unmarshal(msg, &notification); err == nil && notification.ID == nil {
    if notification.Method == "notifications/tools/list_changed" && c.onToolsChanged != nil {
        c.onToolsChanged(c.serverName)
    }
    continue
}

// internal/mcp/manager.go — wire in RegisterClient()
if sc, ok := client.(*StdioClient); ok {
    sc.SetToolsChangedHandler(func(serverName string) {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if err := m.CacheToolSummaries(ctx); err != nil {
            log.Warn().Err(err).Str("server", serverName).Msg("mcp: failed to refresh tool cache after list change")
        }
    })
}
```

### I7 — Truncation Cleanup

```go
// internal/agent/tools/truncation.go — add:
func CleanupTruncatedOutputs(dataDir string, retentionDays int) error {
    dir := filepath.Join(dataDir, "tool-output")
    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) { return nil }
        return err
    }
    cutoff := time.Now().AddDate(0, 0, -retentionDays)
    for _, entry := range entries {
        info, err := entry.Info()
        if err != nil { continue }
        if info.ModTime().Before(cutoff) {
            _ = os.Remove(filepath.Join(dir, entry.Name()))
        }
    }
    return nil
}

// internal/daemon/daemon.go — add to daemon start:
go func() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    for range ticker.C {
        _ = tools.CleanupTruncatedOutputs(d.config.DataDir, 7)
    }
}()
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Priority |
|---|-------------|--------|----------|
| 1 | Fix Bash argument splitting — `shlex.Split` (I3) | Very low | **Critical** — silent command misparsing |
| 2 | Wire `savedPath` into truncation hint (I2) | Low | High — broken hint is misleading |
| 3 | Full input validation — type/enum/constraints (I1) | Medium | High — better LLM error recovery |
| 4 | MCP tool list change handler (I6) | Low | Medium |
| 5 | Structured `DeniedError`/`CorrectedError` types (I5) | Low | Medium |
| 6 | Truncation cleanup scheduler (I7) | Low | Medium |
| 7 | `ActionAsk` permission escalation (I4) | Medium | Low-Medium |
