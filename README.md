# Foreman

[![GitHub stars](https://img.shields.io/github/stars/canhta/foreman?style=social)](https://github.com/canhta/foreman/stargazers)
[![Go Version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

An autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests.

Foreman polls your issue tracker, decomposes tickets into granular tasks, writes code via LLM with TDD discipline, runs deterministic checks and reviews, and opens pull requests with minimal human intervention.

If Foreman is useful to your team, support the project by starring the repository.

## Why Foreman

- **Stateless LLM calls:** every call is made with explicitly assembled context.
- **Deterministic scaffolding:** git operations, linting, and test execution are mechanical.
- **Quality gates:** TDD verification, spec review, quality review, and final review.
- **Pluggable architecture:** tracker, LLM provider, git backend, and runner are interface-driven.

## How It Works

```
Ticket → Clarification Check → Planning → Plan Validation →
  Per-Task: [Implement (TDD) → Lint → Spec Review → Quality Review → Commit] →
Final Review → PR Creation
```

## Status

Phase 1 complete. Core foundations already implemented:

| Package | Status |
|---------|--------|
| `internal/models` | Domain models (Ticket, Task, pipeline states, config) |
| `internal/llm` | LLM provider interface + Anthropic implementation |
| `internal/runner` | Command runner (local) |
| `internal/pipeline` | Output parser with fuzzy search/replace |
| `internal/context` | Secrets scanner + token budget |
| `internal/db` | SQLite database with full schema |
| `internal/config` | Config loading via Viper |

See `docs/spec.md` for the complete architecture and implementation roadmap.

## Quick Start

**Requirements:** Go 1.26+, CGO (for SQLite)

```bash
# Build
make build

# Run tests
make test

# Configure
cp foreman.example.toml foreman.toml
# Edit foreman.toml and set required tokens and provider settings

# Run
./foreman --help
```

## Run with Docker

```bash
# Build and run with Docker Compose
docker compose up --build
```

Default dashboard port mapping: `3333:3333`.

Set required environment variables before startup (for example `ANTHROPIC_API_KEY` and `FOREMAN_DASHBOARD_TOKEN`).

## Configuration

Start from `foreman.example.toml` and configure tracker, git, and LLM sections.

Minimal example:

```toml
[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[git]
clone_url = "https://github.com/your-org/your-repo.git"

[tracker]
provider = "github"  # github, jira, linear, local_file
```

For full settings and behavior, refer to `docs/spec.md`.

## Tech Stack

- **Go 1.26+** — runtime
- **SQLite** (`go-sqlite3`) — persistence
- **Cobra/Viper** — CLI and config
- **Anthropic API** — default LLM provider

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=canhta/foreman&type=Date)](https://star-history.com/#canhta/foreman&Date)

## License

MIT (`LICENSE`)
