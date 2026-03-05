# Agent Runner

The `AgentRunner` interface allows skills to delegate open-ended, multi-turn tasks to an AI coding agent. Three implementations are provided: a built-in runner that uses Foreman's own LLM provider, and two delegating runners for Claude Code and GitHub Copilot.

The agent runner is used exclusively by `agentsdk` skill steps — it is not used inside the core pipeline (which uses direct `LlmProvider.Complete` calls for cost and determinism reasons).

---

## Interface

```go
type AgentRunner interface {
    Run(ctx context.Context, req AgentRequest) (AgentResult, error)
    HealthCheck(ctx context.Context) error
    RunnerName() string
    Close() error
}

type AgentRequest struct {
    Prompt       string          // Task description
    SystemPrompt string          // Prepended to the agent's system prompt
    WorkDir      string          // Working directory for file operations
    AllowedTools []string        // Restrict tools (empty = runner default)
    MaxTurns     int             // 0 = runner default
    TimeoutSecs  int             // 0 = runner default
    OutputSchema json.RawMessage // JSON Schema for structured output (optional)
}

type AgentResult struct {
    Output     string      // Final text or JSON string output
    Structured interface{} // Populated if OutputSchema was provided
    Usage      AgentUsage
}

type AgentUsage struct {
    InputTokens  int
    OutputTokens int
    CostUSD      float64
    NumTurns     int
    DurationMs   int
}
```

---

## Configuration

```toml
[agent_runner]
type = "builtin"   # builtin | claudecode | copilot
```

---

## Builtin Runner

The builtin runner implements a multi-turn tool-use loop on top of Foreman's own `LlmProvider`. It is the default runner and requires no external tools.

### How It Works

1. The runner sends the initial prompt to the configured LLM provider with a set of tool definitions.
2. When the model returns a `tool_use` response, the runner executes all tool calls in parallel.
3. Tool results are fed back to the model in the next turn.
4. The loop continues until the model stops requesting tool calls or `max_turns` is reached.

### Parallel Tool Execution

All tool calls within a single turn execute in parallel using `errgroup`. This matches the behaviour of the Anthropic SDK's `BetaToolRunner` and is typically 3× faster than sequential execution on multi-tool turns.

### Two-Layer Context Injection

The builtin runner injects project context through two mechanisms:

**Layer 1 — Pre-assembly (all runners):** Before any runner is called, the skills engine pre-assembles:
- `AGENTS.md` from the repo root (or `.foreman/context.md` as fallback)
- Path-scoped rules from `.foreman-rules.md` files
- Ticket and task metadata

This is prepended to `AgentRequest.SystemPrompt` for all three runner implementations.

**Layer 2 — Reactive injection (builtin only):** After each file-touching tool call (Read, Edit, Write, GetDiff), the builtin runner queries the database for:
- Progress patterns relevant to the accessed directories (coding conventions discovered during earlier tasks)
- Directory-specific rules for the newly accessed paths

These are injected as a context message before the next LLM turn. The runner tracks what has already been injected to avoid duplication.

### Configuration

```toml
[agent_runner.builtin]
max_turns    = 20
timeout_secs = 300
```

The builtin runner uses the same LLM provider and model routing as the core pipeline. To use a different model for agent tasks, configure a dedicated model in `[models]` (this feature is pending — currently uses the implementer model as default).

---

## Built-in Tools

The builtin runner provides 14 typed tools via a `tools.Registry`. All tool schemas are hand-written JSON Schema — no reflection dependency.

### Filesystem

| Tool | Description |
|---|---|
| `Read` | Read a file's contents. Enforces path guard (no traversal, no absolute paths). |
| `Write` | Write a file. Checks secrets patterns on both the path and content before writing. |
| `Edit` | Apply a SEARCH/REPLACE block to a file. Fuzzy matching at the configured threshold. |
| `MultiEdit` | Apply multiple SEARCH/REPLACE blocks to a file in a single call. |
| `ListDir` | List directory contents with file sizes. |
| `Glob` | Find files matching a glob pattern relative to the working directory. |

### Code Intelligence

| Tool | Description |
|---|---|
| `Grep` | Search for a regex pattern across files (with optional file glob filter). |
| `GetSymbol` | Extract a named symbol (function, type, variable) from a file. |
| `GetErrors` | Run the project's type checker or compiler and return structured errors. |
| `TreeSummary` | Return a compact directory tree summary (depth-limited). |

### Git

| Tool | Description |
|---|---|
| `GetDiff` | Get the current working diff or diff between two refs. |
| `GetCommitLog` | Get recent commit messages and metadata. |

### Execution

| Tool | Description |
|---|---|
| `Bash` | Run an arbitrary shell command. Subject to the runner's allowed commands list. |
| `RunTest` | Run the project's test suite and return structured pass/fail output. |

### Agent Composition

| Tool | Description |
|---|---|
| `Subagent` | Spawn a sub-agent with a fresh prompt. Used for decomposing complex tasks inside a skill. |

### Path Guards

All filesystem tools enforce:
- **No path traversal**: paths like `../../etc/passwd` are rejected
- **Relative paths only**: absolute paths are rejected  
- **Secrets blocking**: writes to `.env`, `*.key`, `*.pem`, and files containing private key content patterns are rejected

---

## Claude Code Runner

The Claude Code runner delegates tasks to the `claude` CLI binary. It requires the Claude Code CLI to be installed.

```toml
[agent_runner]
type = "claudecode"

[agent_runner.claudecode]
binary_path  = "claude"   # Path to the claude binary (must be on $PATH or absolute)
max_turns    = 20
timeout_secs = 300
```

### How It Works

The runner invokes:

```bash
claude -p --output-format json "<prompt>"
```

The `--output-format json` flag produces a structured `SDKResultMessage` containing the final output, total cost, number of turns, and token usage. Foreman parses this and maps it to `AgentResult`.

### System Prompt Injection

Before invoking the CLI, Foreman prepends the pre-assembled `SystemPrompt` from `AgentRequest` to the prompt. This includes `AGENTS.md` content and ticket metadata assembled by the skills engine.

### Requirements

- `claude` CLI must be installed (see the Claude Code documentation).
- A valid Anthropic API key must be available to the `claude` binary (typically via `ANTHROPIC_API_KEY`).

---

## Copilot Runner

The Copilot runner delegates tasks to the GitHub Copilot CLI via session-based JSON-RPC.

```toml
[agent_runner]
type = "copilot"

[agent_runner.copilot]
timeout_secs = 300
```

### How It Works

The runner uses the Copilot SDK Go client:

1. `NewClient()` — establish a connection to the Copilot CLI subprocess
2. `CreateSession()` — start a new agent session
3. `SendAndWait()` — send the prompt and wait for a complete response
4. `Destroy()` — tear down the session

### System Prompt Injection

The pre-assembled `SystemPrompt` from `AgentRequest` is passed to `SendAndWait` as a system message prefix, consistent with the other runner implementations.

### Requirements

- GitHub Copilot CLI must be installed and authenticated.
- A GitHub account with a Copilot subscription.

---

## Structured Output

When an `agentsdk` skill step specifies `output_schema`, the runner attempts to return structured JSON matching the schema.

**Builtin runner:** Passes the schema as `OutputSchema` in `LlmRequest`. Anthropic enforces the schema via forced tool use; OpenAI uses `response_format.json_schema`. The final output is validated against the schema and returned as `AgentResult.Structured`.

**Claude Code runner:** Passes the schema via the `--json-schema` CLI flag (if available) or injects a schema description into the prompt. Output is parsed and validated.

**Copilot runner:** Schema is injected as a prompt instruction. Output validation is best-effort.

---

## Health Checks

```bash
./foreman doctor
```

The `doctor` command runs `AgentRunner.HealthCheck()` for the configured runner:

- **Builtin**: verifies the LLM provider is reachable.
- **Claude Code**: checks that the `claude` binary is on `$PATH` and can execute.
- **Copilot**: checks that the Copilot CLI is installed and authenticated.

---

## Choosing a Runner

| Consideration | Builtin | Claude Code | Copilot |
|---|---|---|---|
| External binary required | No | Yes (`claude`) | Yes (Copilot CLI) |
| Works with any LLM provider | Yes | No (Anthropic only) | No (GitHub Copilot) |
| Parallel tool execution | Yes | Handled by claude | Handled by Copilot |
| Reactive context injection | Yes | No | No |
| Structured output (schema) | Full support | Partial (flag) | Prompt-based |
| Extended thinking | Via `LlmRequest` | Via `claude` flags | No |
| Custom tool restrictions | Yes (per step) | Limited | Limited |
| Cost attribution | Full (tracked in DB) | Approximate (from JSON) | Not exposed |

**Recommendation:** Use `builtin` for most cases. Use `claudecode` if you specifically need Claude Code's file editing capabilities or its native tool implementations. Use `copilot` in environments where GitHub Copilot is already the standard AI tool.

---

## Adding a Custom Runner

Implement the `AgentRunner` interface:

```go
// internal/agent/runner.go
type AgentRunner interface {
    Run(ctx context.Context, req AgentRequest) (AgentResult, error)
    HealthCheck(ctx context.Context) error
    RunnerName() string
    Close() error
}
```

Register it in the factory:

```go
// internal/agent/factory.go
func New(cfg config.AgentRunnerConfig, ...) (AgentRunner, error) {
    switch cfg.Type {
    case "builtin":
        return newBuiltin(cfg, ...)
    case "claudecode":
        return newClaudeCode(cfg)
    case "copilot":
        return newCopilot(cfg)
    case "myrunner":
        return newMyRunner(cfg)  // Add your case here
    }
}
```

The runner name from `RunnerName()` appears in logs and the dashboard.

---

## MCP Support

Foreman supports MCP servers via two mechanisms:

### Anthropic API-Side MCP

Set `URL` and `AuthToken` in `MCPServerConfig` and pass it in the Anthropic API request. Anthropic's infrastructure connects to the server server-side — no local subprocess is needed.

### stdio Transport (Client-Side)

The builtin runner can connect to MCP servers as local subprocesses over stdin/stdout using JSON-RPC 2.0. The `StdioClient` (`internal/agent/mcp/stdio_client.go`) handles:

- Subprocess lifecycle management (spawn, restart on failure, shutdown)
- `initialize` handshake and `tools/list` discovery
- Concurrent `tools/call` multiplexing via an atomic request ID and per-request response channels

The `Manager` (`internal/agent/mcp/manager.go`) aggregates tools from all registered servers and routes `tools/call` requests by matching the `mcp_{server}_` name prefix.

Tool names are normalized by `MCPToolName(server, tool string) string` (`internal/agent/mcp/naming.go`): special characters (`-`, `.`, spaces) are replaced with `_`, the result is prefixed with `mcp_`, and names longer than 64 characters (OpenAI limit) are truncated with a 6-character hash suffix.

Configure stdio MCP servers in `foreman.toml`:

```toml
[[mcp.servers]]
name    = "my-server"
command = "npx"
args    = ["-y", "@company/my-mcp-server"]
allowed_tools      = ["query", "schema"]   # optional whitelist
restart_policy     = "on-failure"          # always | never | on-failure
max_restarts       = 3
restart_delay_secs = 2
[mcp.servers.env]
DB_URL = "${DATABASE_URL}"
```

**Scope boundaries (v1):** tools only — `resources` and `prompts` are not supported. HTTP/SSE transport is not implemented.
