# Foreman

[![GitHub stars](https://img.shields.io/github/stars/canhta/foreman?style=social)](https://github.com/canhta/foreman/stargazers)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Foreman polls your issue tracker, breaks tickets into granular tasks, writes code with LLM-guided TDD, runs deterministic quality checks, and opens pull requests — autonomously.

## Quick Start

**Requirements (build from source):** Go 1.25+, Node.js 20+ (for dashboard assets), and a C toolchain (CGO is required for SQLite).

```bash
make build
cp system.example.toml ~/.foreman/config.toml
# Edit foreman.toml — set your tracker, LLM provider, and git tokens
./foreman doctor   # verify config
./foreman start    # start the daemon
```

Or with Docker Compose:

```bash
docker compose up --build
```

See [docs/getting-started.md](docs/getting-started.md) for a full walkthrough including environment variables, first-run checks, and generating an `AGENTS.md` for your repo.

## Documentation

| | |
|---|---|
| [Getting Started](docs/getting-started.md) | Installation, config, and first run |
| [Features](docs/features.md) | Full feature list |
| [Configuration](docs/configuration.md) | Complete `foreman.toml` reference |
| [Pipeline](docs/pipeline.md) | State machine and review gates |
| [Architecture](docs/architecture.md) | System design and package overview |
| [Integrations](docs/integrations.md) | Trackers, LLM providers, git backends |
| [Agent Runner](docs/agent-runner.md) | Builtin runner, Claude Code, Copilot, MCP |
| [Skills](docs/skills.md) | YAML skill engine and hook points |
| [Dashboard](docs/dashboard.md) | Web UI, REST API, and auth |
| [WhatsApp Channel](docs/configuration.md#messaging-channel-whatsapp) | Bidirectional WhatsApp messaging |
| [Development](docs/development.md) | Contributing and local setup |

## Star History

[![Star History Chart](https://api.star-history.com/image?repos=canhta/foreman&type=date&legend=top-left)](https://www.star-history.com/?repos=canhta%2Fforeman&type=date&legend=top-left)

## License

MIT (`LICENSE`)
