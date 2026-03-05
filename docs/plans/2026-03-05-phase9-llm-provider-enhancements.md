# Phase 9: LLM Provider Enhancements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enhance the `LlmProvider` interface and its implementations with production-grade features that benefit both the core pipeline and the builtin agent runner: native structured output, extended thinking, prompt caching, cross-provider fallback, and `CompleteWithTools()` for OpenAI/OpenRouter/local providers.

**Context:** Phase 8 added `CompleteWithTools()` support to `anthropic.go` and built a multi-turn tool-use loop in `builtin.go`. Phase 9 extends this to all providers and adds the quality-of-life features that make the builtin runner competitive with Claude Code and Copilot.

**Architecture:** All changes go through the existing `LlmProvider` interface. No new SDK dependencies. Study the official `anthropic-sdk-go` v1.26.0 for patterns (structured output in `schemautil.go`, message assembly in `betamessageutil.go`) but implement against Foreman's raw HTTP client.

**Tech Stack:** Go 1.23+, existing `internal/llm` and `internal/agent` packages

**Reference:**
- `anthropic-sdk-go` v1.26.0: `schemautil.go` (schema conversion), `betamessageutil.go` (tool result assembly), `betatoolrunner.go` (loop patterns)
- Anthropic Messages API: `tool_use`, `thinking`, `cache_control` features
- OpenAI Chat Completions API: `function_calling`, `response_format.json_schema`

---

### Task 1: Native Structured Output for Anthropic

**Files:**
- Modify: `internal/models/pipeline.go` — add `OutputSchema` to `LlmRequest`
- Modify: `internal/llm/anthropic.go` — implement schema enforcement via `tool_choice`
- Create: `internal/llm/schema_test.go` — test schema conversion

**Why:** The Anthropic API supports structured output natively via the `tool_choice: {type: "tool"}` pattern — forcing a tool use with a schema-typed tool. This gives the builtin runner stronger schema enforcement than Claude Code's `--json-schema` because it goes through the API's native mechanism. Study `schemautil.go` from the official SDK for the conversion logic.

**Step 1: Add to LlmRequest**

```go
// internal/models/pipeline.go — add to LlmRequest
OutputSchema *json.RawMessage `json:"output_schema,omitempty"` // JSON Schema for structured output
```

**Step 2: Implement in anthropic.go**

When `OutputSchema` is set:
1. Create a synthetic tool definition with `name: "structured_output"` and `input_schema` set to the provided schema
2. Set `tool_choice: {"type": "tool", "name": "structured_output"}` to force the model to use this tool
3. Extract the tool input from the response as the structured output
4. Set `resp.Content` to the JSON string of the tool input

This is ~40 lines of schema conversion logic adapted from `schemautil.go`. The key insight is that Anthropic's structured output works by forcing tool use with a schema — the model's output is validated against the schema at the API level.

**Step 3: Wire to builtin runner**

When `AgentRequest.OutputSchema` is set, pass it through to `LlmRequest.OutputSchema`. The builtin runner's final turn result includes the structured output.

**Step 4: Wire to skill YAML**

Add `output_schema` field to `SkillStep`:
```yaml
- id: analyze
  type: agentsdk
  prompt: "Analyze this code for issues"
  output_schema:
    type: object
    properties:
      severity: { type: string, enum: [low, medium, high, critical] }
      findings: { type: array, items: { type: object } }
    required: [severity, findings]
```

**Step 5: Verify**
```bash
go test ./internal/llm/ -run TestAnthropicProvider_StructuredOutput -v
```

---

### Task 2: Native Structured Output for OpenAI

**Files:**
- Create: `internal/llm/openai.go` — (if not already fully implemented) add `response_format.json_schema` support
- Create: `internal/llm/openai_test.go`

**Why:** OpenAI supports structured output via `response_format: {type: "json_schema", json_schema: {name: "...", schema: {...}}}`. This is simpler than Anthropic's approach — no tool_choice trick needed.

**Step 1: Implement in openai.go**

When `OutputSchema` is set:
1. Add `response_format` to the request body with `type: "json_schema"` and the schema
2. Parse the response content as JSON
3. Set `resp.Content` to the JSON string

**Step 2: Verify**
```bash
go test ./internal/llm/ -run TestOpenAIProvider_StructuredOutput -v
```

---

### Task 3: Tool-Use Support for OpenAI Provider

**Files:**
- Modify: `internal/llm/openai.go` — handle function_calling (tools), multi-turn messages
- Modify: `internal/llm/openai_test.go`

**Why:** Phase 8 added tool-use to `anthropic.go`. For the builtin runner to work across all providers, `openai.go` needs the same capability. OpenAI uses `function_calling` with `tools` and `tool_calls` in responses.

**Step 1: Update openai.go request types**

- Add `Tools []openaiTool` to request (maps from `models.ToolDef`)
- Handle `Messages []models.Message` — serialize tool_calls and function results in OpenAI's format
- Parse `tool_calls` from response `choices[0].message`
- Return `StopReasonToolUse` when `finish_reason == "tool_calls"`

**Step 2: Key differences from Anthropic**

| Aspect | Anthropic | OpenAI |
|--------|-----------|--------|
| Tool definition | `input_schema` (JSON Schema) | `parameters` (JSON Schema) |
| Tool call response | Content blocks with `type: "tool_use"` | `tool_calls` array on message |
| Tool result | User message with `tool_result` content blocks | Message with `role: "tool"` and `tool_call_id` |
| Stop reason | `"tool_use"` | `"tool_calls"` |

**Step 3: Verify**
```bash
go test ./internal/llm/ -run TestOpenAIProvider_Complete_WithTools -v
```

---

### Task 4: Tool-Use for OpenRouter and Local Providers

**Files:**
- Modify: `internal/llm/openrouter.go` — pass through (OpenRouter is OpenAI-compatible)
- Modify: `internal/llm/local.go` — tool use if model supports it, graceful no-op if not

**Why:** OpenRouter uses the same API format as OpenAI, so tool-use support comes almost for free. Local models (Ollama) may or may not support tools — fail gracefully.

**OpenRouter:** Identical to `openai.go` — same request/response format, different base URL.

**Local:** Try to use tools if the model supports them. If the model returns a non-tool response even though tools were provided, treat it as a single-turn text response (the model doesn't support tools). This allows the builtin runner to degrade gracefully with local models.

**Verify:**
```bash
go test ./internal/llm/ -run TestOpenRouter -v
go test ./internal/llm/ -run TestLocal -v
```

---

### Task 5: Extended Thinking Support

**Files:**
- Modify: `internal/models/pipeline.go` — add `Thinking` to `LlmRequest`
- Modify: `internal/llm/anthropic.go` — send `thinking` parameter, parse thinking content blocks
- Modify: `internal/skills/loader.go` — add `thinking` to SkillStep schema

**Why:** The Anthropic API exposes extended thinking as `thinking: {type: "enabled", budget_tokens: N}`. This enables deep reasoning for complex analysis tasks in skills. Claude Code exposes this via `--betas interleaved-thinking`. With direct API access, the builtin runner gets finer control.

**Step 1: Add types**

```go
// internal/models/pipeline.go
type ThinkingConfig struct {
	Enabled      bool `json:"enabled"`
	BudgetTokens int  `json:"budget_tokens"` // e.g. 10000
}

// Add to LlmRequest:
Thinking *ThinkingConfig `json:"thinking,omitempty"`
```

**Step 2: Implement in anthropic.go**

When `req.Thinking` is set and `Enabled == true`:
1. Add `thinking: {type: "enabled", budget_tokens: N}` to the API request
2. Parse `thinking` content blocks from response (before text blocks)
3. The thinking content is informational — don't include it in `resp.Content`, but log it

When the model doesn't support thinking, silently ignore the parameter.

**Step 3: Skill YAML support**

```yaml
- id: architecture-review
  type: agentsdk
  thinking:
    enabled: true
    budget_tokens: 8000
  prompt: "Analyze this diff for architectural issues: {{ .Diff }}"
```

**Step 4: Verify**
```bash
go test ./internal/llm/ -run TestAnthropicProvider_Thinking -v
```

---

### Task 6: Prompt Caching

**Files:**
- Modify: `internal/models/pipeline.go` — add `CacheSystemPrompt` to `LlmRequest`
- Modify: `internal/llm/anthropic.go` — mark system prompt with `cache_control: {type: "ephemeral"}`

**Why:** The repo context assembled by `ContextAssembler` is identical across retries for the same ticket. Caching it cuts input token costs on repeated runs. The Anthropic API supports `cache_control` on system prompt blocks. Claude Code does this automatically — doing it explicitly gives Foreman control over where the cache breakpoint sits.

**Step 1: Add to LlmRequest**

```go
CacheSystemPrompt bool `json:"cache_system_prompt,omitempty"` // Anthropic only
```

**Step 2: Implement in anthropic.go**

When `CacheSystemPrompt == true`:
- Change `system` from a string to an array of content blocks:
```json
"system": [
  {
    "type": "text",
    "text": "...",
    "cache_control": {"type": "ephemeral"}
  }
]
```

When `CacheSystemPrompt == false`, use the existing string format.

**Step 3: Track cache usage**

Add to `LlmResponse`:
```go
CacheReadTokens    int `json:"cache_read_tokens"`
CacheCreationTokens int `json:"cache_creation_tokens"`
```

Parse from Anthropic response `usage.cache_read_input_tokens` and `usage.cache_creation_input_tokens`.

**Step 4: Verify**
```bash
go test ./internal/llm/ -run TestAnthropicProvider_PromptCaching -v
```

---

### Task 7: Cross-Provider Fallback in Builtin Runner

**Files:**
- Modify: `internal/agent/builtin.go` — add fallback logic
- Modify: `internal/agent/runner.go` — add `FallbackModel` to `AgentRequest`
- Modify: `internal/skills/loader.go` — add `fallback_model` to SkillStep

**Why:** Unlike Claude Code's `--fallback-model` which is CLI-only and single-provider, the builtin runner can fall back across providers — e.g. primary `anthropic:claude-sonnet-4-5` → fallback `openrouter:claude-sonnet-4-5`. This requires access to the full LLM router.

**Step 1: Add to AgentRequest**

```go
FallbackModel string // e.g. "openrouter:claude-sonnet-4-5-20250929"
```

**Step 2: Implement in builtin.go Run()**

Wrap each `provider.Complete()` call:
```go
resp, err := r.provider.Complete(ctx, llmReq)
if err != nil && isOverloadedError(err) && fallbackModel != "" {
	llmReq.Model = fallbackModel
	fallbackModel = "" // prevent infinite fallback loop
	resp, err = r.provider.Complete(ctx, llmReq)
}
```

**Step 3: Skill YAML support**

```yaml
- id: review
  type: agentsdk
  prompt: "Review this code"
  fallback_model: "openrouter:claude-sonnet-4-5-20250929"
```

**Step 4: Verify**
```bash
go test ./internal/agent/ -run TestBuiltinRunner_Fallback -v
```

---

### Task 8: Builtin Runner — Edit Tool (Opt-In Write Access)

**Files:**
- Modify: `internal/agent/tools.go` — add `Edit` and `Write` tool implementations
- Modify: `internal/agent/tools_test.go`

**Why:** Phase 8's builtin runner is read-only by default (Read, Glob, Grep). For skills that need to write files (changelog generation, code formatting), the Edit/Write tools should be available as an opt-in. When a skill YAML specifies `allowed_tools: [Read, Edit, Write]`, the builtin runner enables write access — still scoped to the working directory.

**Step 1: Implement Edit tool**

Simple file write with path traversal protection:
```go
"Edit": func(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	// ... validate path, read file, replace old with new, write back
}
```

**Step 2: Implement Write tool**

```go
"Write": func(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	// ... validate path, write file
}
```

**Step 3: Security**

Both tools enforce:
- Path must be within `workDir` (no traversal)
- Path must not match secrets scanner patterns (`.env`, `*.key`, `*.pem`)
- Path must not match forbidden file patterns from config

**Step 4: Verify**
```bash
go test ./internal/agent/ -run TestEditTool -v
go test ./internal/agent/ -run TestWriteTool -v
```

---

### Task 9: Bash Tool (Opt-In Command Execution)

**Files:**
- Modify: `internal/agent/tools.go` — add `Bash` tool using existing `CommandRunner`
- Modify: `internal/agent/builtin.go` — inject `CommandRunner` dependency

**Why:** Claude Code and Copilot both offer Bash execution. The builtin runner should too, but using Foreman's existing `CommandRunner` interface which enforces allowed commands, forbidden paths, and timeout limits.

**Step 1: Implement**

The Bash tool wraps `CommandRunner.Run()`:
```go
"Bash": func(workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	// Parse command, validate against allowed commands list
	// Execute via CommandRunner with timeout
	// Return stdout + stderr
}
```

**Step 2: Inject CommandRunner**

`NewBuiltinRunner()` takes an optional `runner.CommandRunner`. When nil, the Bash tool returns an error if invoked.

**Step 3: Verify**
```bash
go test ./internal/agent/ -run TestBashTool -v
```

---

## Summary

| Task | What | Value |
|------|------|-------|
| 1 | Structured output — Anthropic | Native schema enforcement, stronger than CLI |
| 2 | Structured output — OpenAI | `response_format.json_schema` |
| 3 | Tool-use — OpenAI | Builtin runner works with OpenAI models |
| 4 | Tool-use — OpenRouter + Local | Full provider coverage |
| 5 | Extended thinking | Deep reasoning for complex analysis skills |
| 6 | Prompt caching | Cost reduction on repeated runs |
| 7 | Cross-provider fallback | Resilience — fall back across providers |
| 8 | Edit/Write tools | Opt-in write access for builtin runner |
| 9 | Bash tool | Command execution via existing CommandRunner |

## Priority Order

Ship in this order for maximum incremental value:

1. **Tasks 3-4** (tool-use for OpenAI/OpenRouter/local) — makes builtin runner provider-agnostic
2. **Tasks 1-2** (structured output) — enables validated JSON responses from skills
3. **Tasks 8-9** (Edit/Write/Bash tools) — makes builtin runner capable of code changes
4. **Task 5** (extended thinking) — high value for complex analysis skills
5. **Task 7** (cross-provider fallback) — resilience
6. **Task 6** (prompt caching) — cost reduction

## Verification Checklist

- [ ] `go test ./internal/llm/ -v` — all provider tests pass (tool-use, structured output, thinking, caching)
- [ ] `go test ./internal/agent/ -v` — builtin runner works with all providers and tools
- [ ] `go test ./internal/skills/ -v` — skill YAML supports new fields
- [ ] `go vet ./...` — no issues
- [ ] `go build ./...` — compiles clean
