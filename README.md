# Foreman

[![GitHub stars](https://img.shields.io/github/stars/canhta/foreman?style=social)](https://github.com/canhta/foreman/stargazers)
[![Go Version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Foreman polls your issue tracker, breaks tickets into granular tasks, writes code with LLM-guided TDD, runs deterministic quality checks, and opens pull requests with minimal human intervention.

## Why Foreman

- **Stateless LLM calls:** every call is made with explicitly assembled context.
- **Deterministic scaffolding:** git operations, linting, and test execution are mechanical.
- **Quality gates:** TDD verification, spec review, quality review, and final review.
- **Pluggable architecture:** tracker, LLM provider, git backend, and runner are interface-driven.

## How It Works

1. **Pick up a ticket** from Jira, GitHub Issues, or Linear.
2. **Check for clarity** and request clarification when requirements are ambiguous.
3. **Plan the work** into small, ordered tasks.
4. **Validate the plan** before execution (files, dependencies, and limits).
5. **Implement each task with TDD** and run lint/test checks.
6. **Run spec and quality reviews** before committing.
7. **Run final review** and create a PR synced back to the issue tracker.

## Quick Start

**Requirements:** Go 1.26+ and a C toolchain (CGO is required for SQLite).

```bash
# Build
make build

# Run tests
make test

# Configure
cp foreman.example.toml foreman.toml
# Edit foreman.toml and set required tokens/provider settings

# Run
./foreman --help
```

## Run with Docker

```bash
# Build and run with Docker Compose
docker compose up --build
```

Default dashboard port mapping is `3333:3333`.

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

For local runs, use `foreman.toml`. For Docker Compose in this repo, `foreman.example.toml` is mounted as `/app/foreman.toml` by default.

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
- **go test** - built-in test runner

### Tooling and Ops

- **Makefile** - common developer commands (`build`, `test`, `lint`)
- **Docker** - containerized runtime
- **Docker Compose** - local orchestration

## Star History

[![Star History Chart](https://api.star-history.com/image?repos=canhta/foreman&type=date&legend=top-left)](https://www.star-history.com/?repos=canhta%2Fforeman&type=date&legend=top-left)

## Support the Project

If Foreman helps your team, please support it with a GitHub star.

[![Star Foreman](https://img.shields.io/badge/Support-Star%20on%20GitHub-black?logo=github)](https://github.com/canhta/foreman/stargazers)

## License

MIT (`LICENSE`)
