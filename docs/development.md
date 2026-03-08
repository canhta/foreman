# Development Guide

This guide covers setting up a development environment, the project's conventions, testing strategy, and how to contribute changes.

> **New contributor?** The fastest path to a first PR:
> 1. Run `make setup-hooks && make setup-dev && make test` to verify your environment.
> 2. Pick a [good first issue](https://github.com/canhta/foreman/issues?q=is%3Aopen+label%3A%22good+first+issue%22) or open a discussion for your idea.
> 3. Branch from `main`, make your change, and submit a PR using the checklist at the bottom of this page.

## Quick Reference

| Command | What it does |
|---|---|
| `make build` | Build dashboard assets, then build the `./foreman` binary |
| `make dashboard-build` | Build Svelte dashboard assets into `internal/dashboard/dist/` |
| `make dashboard-dev` | Start Vite dev server for dashboard frontend |
| `make test` | Run all tests with the race detector |
| `make lint` | Run `go vet` + `golangci-lint` |
| `make dev` | Hot-reload daemon on file changes (requires `make setup-dev`) |
| `make debug` | Start Delve debugger on `:2345` (requires `make setup-dev`) |
| `./foreman doctor` | Validate config and API credentials |
| `./foreman run LOCAL-1` | Process a single ticket from the local tracker |

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.25+ | Module: `github.com/canhta/foreman` |
| Node.js | 20+ | Required for dashboard frontend build (`npm ci`, `vite build`) |
| C toolchain | Any | Required by `go-sqlite3` (CGO) |
| `git` | 2.x+ | Used as subprocess by the git module |
| `golangci-lint` | Latest | Optional, for lint checks |

On macOS, the C toolchain is provided by Xcode Command Line Tools:

```bash
xcode-select --install
```

On Debian/Ubuntu:

```bash
apt-get install -y build-essential
```

## Bootstrap

```bash
git clone https://github.com/canhta/foreman.git
cd foreman

# Install git hooks (run once)
make setup-hooks

# Install dev tools: air (hot reload) + dlv (debugger) — run once
make setup-dev

# Build the binary
make build

# Run all tests
make test

# Create a local config
cp foreman.example.toml foreman.toml
```

## Build Targets

| Command | Description |
|---|---|
| `make build` | Build dashboard assets, then build binary to `./foreman` |
| `make dashboard-build` | Build dashboard assets to `internal/dashboard/dist/` |
| `make dashboard-dev` | Run Vite dev server for dashboard frontend |
| `make test` | Run all tests with `-race` flag |
| `make lint` | Run `go vet` + `golangci-lint` |
| `make clean` | Remove `./foreman` binary |
| `make setup-hooks` | Install git hooks from `.githooks/` (run once after cloning) |
| `make setup-dev` | Install dev tools: `air` (hot reload) + `dlv` (debugger) |
| `make dev` | Hot-reload: rebuild & restart on file changes (requires `make setup-dev`) |
| `make debug` | Debug build + launch under Delve on `:2345` (requires `make setup-dev`) |
| `make release` | Cross-compile for linux/darwin/windows amd64+arm64 |
| `make docker` | Build Docker image `foreman:latest` |

### Building Without Make

```bash
cd internal/dashboard/web && npm ci && npm run build && cd ../../..
go build -o foreman ./main.go
go test ./...                              # all packages
go test ./internal/pipeline/...           # single package
go test -run TestPlanValidator ./internal/pipeline/  # single test
go vet ./...
```

## Running Locally

```bash
./foreman run          # process one ticket from the queue
./foreman doctor       # validate config and connectivity
./foreman start        # start the background daemon (dashboard on :8080)
./foreman status       # show daemon status
```

### Dashboard Development (Two Terminals)

The dashboard uses a split dev setup: Go serves the API, Vite serves the frontend with HMR.

```bash
# Terminal 1 — Go backend with hot reload (serves API on :8080)
make dev

# Terminal 2 — Vite frontend with HMR (serves UI on :5173, proxies /api + /ws → :8080)
make dashboard-dev
```

Open **http://localhost:5173** in your browser during development.

Both commands read the port from `PORT` (default `8080`). To use a custom port:

```bash
make dev PORT=9090
make dashboard-dev PORT=9090
```

### Port Configuration Precedence

The dashboard port is resolved in this order (highest wins):

1. `--dashboard-port` CLI flag
2. `FOREMAN_DASHBOARD_PORT` environment variable
3. `[dashboard].port` in `foreman.toml`
4. Default: `8080`

The host follows a similar pattern: `FOREMAN_DASHBOARD_HOST` env var → `[dashboard].host` in TOML → default `127.0.0.1`.

For Docker, `FOREMAN_DASHBOARD_HOST` is set to `0.0.0.0` in `docker-compose.yml` so the container is reachable from the host.

## Debug Mode & Hot Reload

### Debug Logging

Set `log_level = "debug"` in `foreman.toml` to enable verbose structured output:

```toml
[daemon]
log_level = "debug"   # debug, info, warn, error
log_format = "pretty" # pretty is easier to read during development
```

### Hot Reload with Air

[Air](https://github.com/air-verse/air) watches `.go` and `.toml` files and automatically rebuilds and restarts the binary on changes. The project ships a pre-configured [`.air.toml`](.air.toml).

```bash
# Install air + dlv (once)
make setup-dev

# Start the daemon with hot reload (default)
make dev

# Start on a custom port (temporary override)
make dev PORT=9090
make dashboard-dev PORT=9090

# Run a single ticket with hot reload
make dev CMD="run LOCAL-1"
```

> **Note:** CGO is required by `go-sqlite3`. The `.air.toml` sets `CGO_ENABLED=1` explicitly so air works out of the box on macOS and Linux.

### Breakpoint Debugging with Delve

[Delve](https://github.com/go-delve/delve) is the standard Go debugger. `make debug` compiles without optimizations and launches a headless Delve server on port `2345`.

```bash
# Install air + dlv (once)
make setup-dev

# Build (debug symbols, no optimizations) and start Delve server
make debug
```

Then connect from your IDE or the Delve CLI in a second terminal:

```bash
dlv connect 127.0.0.1:2345
```

**VS Code:** Add this to `.vscode/launch.json` to attach automatically:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Attach to foreman (dlv)",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "remotePath": "${workspaceFolder}",
      "port": 2345,
      "host": "127.0.0.1"
    }
  ]
}
```

## Package Structure

```
main.go              Entry point, cobra root command
cmd/                 CLI commands (run, start, stop, status, ps, cost, dashboard, token, logs)
internal/
  agent/             AgentRunner interface + builtin, claudecode, copilot implementations
  agent/tools/       Typed tool registry (14 tools)
  agent/mcp/         MCP manager, stdio client, and health monitoring
  config/            TOML/Viper config loading + validation
  context/           Context assembler, file selector, token budget, secrets scanner
  daemon/            Scheduler, clarification gate, file reservations, crash recovery
  dashboard/         HTTP server, REST API handlers, WebSocket, auth
  db/                Database interface, SQLite + PostgreSQL backends
  git/               Git interface + native CLI implementation
  llm/               LlmProvider interface + Anthropic, OpenAI, OpenRouter, local implementations
  models/            Domain models: Ticket, Task, LlmCall, events, pipeline states
  pipeline/          State machine orchestrator + all stage implementations
  runner/            CommandRunner interface + local and Docker implementations
  skills/            YAML skill engine, step executor, hook dispatcher
  telemetry/         Cost controller, Prometheus metrics, structured events
  tracker/           IssueTracker interface + GitHub, Jira, Linear, local_file implementations
prompts/             Jinja2 (.j2) prompt templates
skills/              Built-in and community YAML skill files
```

## Coding Conventions

### Interface-First Design

Every external dependency is behind a Go interface. All implementations are in the same `internal/` sub-package as the interface. This makes unit testing straightforward and implementations swappable.

```go
// Define the interface in internal/llm/provider.go
type LlmProvider interface { ... }

// Implement in internal/llm/anthropic.go
type AnthropicProvider struct { ... }
```

### Error Handling

Wrap errors with context using `fmt.Errorf`:

```go
// Good
return fmt.Errorf("loading config: %w", err)

// Bad
return err
```

Never panic in normal control flow. Return typed or wrapped errors.

### Logging

Use `zerolog` exclusively. Always attach contextual fields:

```go
log.Info().
    Str("ticket_id", ticket.ID).
    Int("task_seq", task.Sequence).
    Msg("starting implementation")
```

Do not use the standard library `log` package.

### Configuration

Use Viper. Support `${ENV_VAR}` substitution in TOML values. Never hard-code credentials or configuration defaults that an operator might need to override.

### Tool Schemas

Write JSON Schema for tool definitions by hand — no reflection. This makes schemas explicit and stable.

### Package Layout Rules

- Nothing is exported outside the Go module except `cmd/` (CLI entry points) and `main.go`.
- `models/` contains only plain data structs — no business logic.
- `pipeline/` owns the state machine. Other packages do not modify ticket/task state directly.

## Testing

### Running Tests

```bash
make test                             # all packages, race detector enabled
go test ./internal/pipeline/... -v    # verbose output for one package
go test -run TestName ./internal/...  # single test by name
```

### Test Conventions

- Every behavioral change must have a corresponding test.
- Bug fixes require a regression test.
- Tests must be deterministic and independent (no shared mutable state between tests, no test ordering dependencies).
- Use `t.TempDir()` for file system operations.
- Use the mock implementations in `internal/*/mock.go` or build simple fakes in `_test.go` files.

### Mock Objects

Several packages provide mock implementations for testing:

| Mock | Location |
|---|---|
| `MockLlmProvider` | `internal/llm/` |
| `MockAgentRunner` | `internal/agent/mock.go` |
| `MockIssueTracker` | `internal/tracker/` |
| `MockDatabase` | `internal/db/` |
| `MockCommandRunner` | `internal/runner/` |

### Integration Tests

Tests that require a database use the SQLite in-memory driver. No external services are required to run the test suite.

## Adding a New Feature

### New LLM Provider

1. Implement `llm.LlmProvider` in `internal/llm/<provider>.go`.
2. Register the provider in `internal/llm/factory.go`.
3. Add a config section to `foreman.example.toml`.
4. Add doc entry to `docs/integrations.md`.

### New Issue Tracker

1. Implement `tracker.IssueTracker` in `internal/tracker/<name>.go`.
2. Register in `internal/tracker/factory.go`.
3. Add config section and doc entry.

### New Agent Tool

1. Implement `tools.Tool` in `internal/agent/tools/<name>.go`.
2. Register in the tools registry in `internal/agent/tools/registry.go`.
3. Update `docs/agent-runner.md` with the new tool entry.

### New Skill Step Type

1. Add a new case to the step executor in `internal/skills/executor.go`.
2. Define the YAML schema in the step type documentation.
3. Update `docs/skills.md`.

## Git Hooks

Hooks live in `.githooks/` and are version-controlled. Activate them once after cloning:

```bash
make setup-hooks
```

| Hook | Trigger | Checks |
|---|---|---|
| `pre-commit` | `git commit` | `gofmt` (staged files only), `go vet`, `golangci-lint` |
| `pre-push` | `git push` | `go test ./... -race` |

To bypass in emergencies:

```bash
SKIP_HOOKS=1 git commit -m "..."
SKIP_HOOKS=1 git push
```

## Commit Messages

Use imperative, specific messages:

```
add sqlite busy-timeout validation
implement planner yaml fallback parser
fix fuzzy search replace threshold handling
refactor context assembler token budget calculation
```

Avoid vague messages like `fix bug`, `update code`, `changes`.

## Branch and PR Workflow

1. Branch from `main` using a descriptive name:
   - `feat/<short-description>`
   - `fix/<short-description>`
   - `chore/<short-description>`
2. Keep changes scoped to a single concern per PR.
3. Run `make setup-hooks` once so the pre-commit and pre-push checks run automatically.
4. Open a PR with:
   - A clear summary of what changed and why
   - Linked issue or ticket when applicable
   - Test evidence (commands run, outcomes)
   - Notes on tradeoffs or follow-up items

## Pull Request Checklist

Before opening a PR, verify:

- [ ] `make test` passes (all packages, race detector)
- [ ] `make lint` passes with no new warnings
- [ ] New or changed behaviour has a corresponding test
- [ ] Bug fixes include a regression test
- [ ] `foreman.example.toml` updated if new config keys were added
- [ ] Relevant docs in `docs/` updated for any user-facing change
- [ ] No secrets, credentials, or API keys in code or tests
- [ ] PR description explains what changed, why, and links the relevant issue

## CI Pipeline

GitHub Actions runs on every pull request:

1. **Test** — `go test ./... -race`
2. **Lint** — `go vet ./...` + `golangci-lint run`
3. **Build** — `go build ./...`

All three jobs must pass before merge.

## Release Process

Releases are created by pushing a `v*` tag. `goreleaser` handles:

- Cross-compilation for linux/darwin/windows on amd64 + arm64
- Archive generation (`.tar.gz` / `.zip`)
- GitHub Release creation with changelogs
- Docker image push to GHCR (`ghcr.io/canhta/foreman`)

```bash
git tag v0.2.0
git push origin v0.2.0
```

## Security

- Never commit credentials, API keys, or secrets to the repository.
- Do not post sensitive data in issues or PRs.
- To report a security vulnerability, follow the process in [SECURITY.md](../SECURITY.md) — please do not open a public issue.
- All user input that reaches LLM prompts is treated as untrusted. The secrets scanner blocks known credential patterns before context assembly.

## Getting Help

- Read the existing tests — they are the most accurate documentation of expected behaviour.
- Check `docs/` for architecture, pipeline, and configuration references.
- Open a [GitHub Discussion](https://github.com/canhta/foreman/discussions) for questions or ideas.
- Open a [GitHub Issue](https://github.com/canhta/foreman/issues) with a clear problem statement and reproduction steps for bugs.

---

## See Also

- [Architecture](architecture.md) — package layout and design principles
- [Pipeline](pipeline.md) — the full ticket state machine
- [Configuration](configuration.md) — `foreman.toml` reference
- [CONTRIBUTING.md](../CONTRIBUTING.md) — contribution agreement and process
