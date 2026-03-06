# Getting Started

## Requirements

- **Go 1.25+** with a C toolchain (CGO is required by `go-sqlite3`)
- **Git** (the `git` CLI must be available on `$PATH`)
- An **LLM API key** for your chosen provider (Anthropic, OpenAI, or OpenRouter)
- An **issue tracker** account (Jira, GitHub, Linear, or a local file for testing)

## Installation

### From Source

```bash
git clone https://github.com/canhta/foreman.git
cd foreman
make build
./foreman --help
```

### Download a Release Binary

Pre-built binaries for Linux (amd64/arm64) and macOS (amd64/arm64) are available on the [GitHub Releases page](https://github.com/canhta/foreman/releases).

```bash
# Example: macOS arm64
curl -L https://github.com/canhta/foreman/releases/latest/download/foreman-<version>-darwin-arm64.tar.gz | tar -xz
chmod +x foreman
./foreman --help
```

### Docker

```bash
docker pull ghcr.io/canhta/foreman:latest
```

## Configuration

Copy the example config and edit it:

```bash
cp foreman.example.toml foreman.toml
```

Minimal working config using GitHub Issues + Anthropic:

```toml
[daemon]
work_dir = "~/.foreman/work"

[tracker]
provider = "github"

[tracker.github]
owner    = "your-org"
repo     = "your-repo"
token    = "${GITHUB_TOKEN}"
pickup_label = "foreman-ready"

[git]
provider   = "github"
clone_url  = "git@github.com:your-org/your-repo.git"

[git.github]
token = "${GITHUB_TOKEN}"

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[dashboard]
enabled    = true
port       = 3333
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"
```

Set required environment variables:

```bash
export GITHUB_TOKEN=ghp_...
export ANTHROPIC_API_KEY=sk-ant-...
export FOREMAN_DASHBOARD_TOKEN=$(./foreman token generate)
```

For a complete config reference, see [Configuration](configuration.md).

## First Run

### Health Check

```bash
./foreman doctor
```

This verifies that your config is valid, all API credentials are reachable, and the database can be written.

### Run a Single Ticket (Manual)

```bash
./foreman run "PROJ-123"
# Or with a GitHub issue number:
./foreman run "42"
# Dry run (plan only, no code changes):
./foreman run "PROJ-123" --dry-run
```

### Start the Daemon

```bash
# Run in the foreground:
./foreman start

# Or as a background daemon:
./foreman start --daemon
```

The daemon polls your issue tracker at the configured interval (default: 60 seconds) and processes any ticket with the `foreman-ready` label.

### Check Status

```bash
./foreman status    # Overall daemon status
./foreman ps        # List active pipelines
./foreman ps --all  # Include completed and failed
./foreman logs      # Tail logs
./foreman logs PROJ-123  # Logs for a specific ticket
```

### View Costs

```bash
./foreman cost today
./foreman cost week
./foreman cost month
./foreman cost ticket PROJ-123
```

### Open the Dashboard

```bash
./foreman dashboard          # Print the dashboard URL
./foreman dashboard --port 8080  # Override port
```

Or navigate to `http://localhost:3333` in your browser. Use the token from `FOREMAN_DASHBOARD_TOKEN` to authenticate.

## Running with Docker Compose

```bash
# Build and start:
docker compose up --build

# Set required environment variables before starting:
export ANTHROPIC_API_KEY=sk-ant-...
export GITHUB_TOKEN=ghp_...
export FOREMAN_DASHBOARD_TOKEN=your-token
```

The default `docker-compose.yml` maps the dashboard to port `3333`. The config file `foreman.example.toml` is mounted as `/app/foreman.toml` inside the container — replace it with your own `foreman.toml`.

## Adding Project Context

### Generating AGENTS.md

The quickest way to create an `AGENTS.md` for your target repository is to let Foreman generate one:

```bash
# LLM-powered (recommended — uses your configured provider)
./foreman context generate

# Static analysis only, no LLM call
./foreman context generate --offline

# Preview without writing
./foreman context generate --dry-run
```

Foreman scans your repository (config files, entry points, key sources), assembles the content within a configurable token budget, and calls your LLM provider to produce an agent-optimised `AGENTS.md` file.

To update `AGENTS.md` after merges with learned patterns:

```bash
./foreman context update
```

### Authoring AGENTS.md manually

Foreman injects project context into every agent call automatically. Create an `AGENTS.md` at the root of your target repository:

```markdown
# Project Conventions

## Code Style
- Use ESM imports (no CommonJS)
- Prefer async/await over callbacks
- All error handling must use Result types

## Architecture
- API handlers live in src/routes/
- Business logic lives in src/services/
- Do not import directly from src/db/ — use repositories

## Testing
- Jest + ts-jest
- Test files live adjacent to source: foo.test.ts next to foo.ts
- Minimum 80% coverage required
```

Foreman pre-assembles this file into the system prompt for all agent runner implementations.

## Triggering Work

Label a ticket `foreman-ready` in your issue tracker. Foreman will pick it up on the next poll cycle, plan the work, implement it task by task, and open a pull request.

For Jira, ensure the ticket has a description with acceptance criteria. For GitHub Issues, use a checklist or description. For ambiguous tickets, Foreman will comment with a clarification request before proceeding.

## Stopping the Daemon

```bash
./foreman stop
```

Active pipelines finish their current task before shutting down. Interrupted pipelines are automatically resumed on the next `foreman start`.
