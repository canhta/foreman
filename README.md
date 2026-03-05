# Foreman

[![GitHub stars](https://img.shields.io/github/stars/canhta/foreman?style=social)](https://github.com/canhta/foreman/stargazers)
[![Go Version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Foreman polls your issue tracker, decomposes tickets into granular tasks, writes code via LLM with TDD discipline, runs deterministic checks and reviews, and opens pull requests with minimal human intervention.

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

## Tech Stack

### Core

- **Go 1.26+** - language/runtime
- **Go stdlib** - process execution, file I/O, HTTP, and concurrency primitives

### CLI and Configuration

- **Cobra** (`github.com/spf13/cobra`) - CLI framework
- **Viper** (`github.com/spf13/viper`) - configuration loading

### Storage

- **SQLite** (`github.com/mattn/go-sqlite3`) - local persistence backend
- **CGO** - required by `go-sqlite3`

### Testing

- **Testify** (`github.com/stretchr/testify`) - assertions and test helpers
- **Go test** - built-in test runner

### Tooling and Ops

- **Makefile** - common developer commands (`build`, `test`, `lint`)
- **Docker** - containerized runtime
- **Docker Compose** - local orchestration

## Star History

<p align="center">
  <a href="https://www.star-history.com/#canhta/foreman&type=date&legend=top-left">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=canhta/foreman&type=date&theme=dark&legend=top-left" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=canhta/foreman&type=date&legend=top-left" />
      <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=canhta/foreman&type=date&legend=top-left" />
    </picture>
  </a>
</p>

## Support the Project

If Foreman helps your team, please support it with a GitHub star.

[![Star Foreman](https://img.shields.io/badge/Support-Star%20on%20GitHub-black?logo=github)](https://github.com/canhta/foreman/stargazers)

## License

MIT (`LICENSE`)
