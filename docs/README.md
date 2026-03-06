# Foreman Documentation

Foreman is an autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests — with no human involvement between "ticket labelled" and "PR ready for review."

It connects to your existing issue tracker (Jira, GitHub Issues, Linear), plans and implements each ticket with LLM-guided TDD, runs quality gates, and opens a pull request — all as a 24/7 background daemon. You stay in control: every PR is a human review checkpoint before any code ships.

## Documentation Index

| Document | Description |
|---|---|
| [Getting Started](getting-started.md) | Installation, configuration, and first run |
| [Features](features.md) | Complete capability overview |
| [Configuration](configuration.md) | Full `foreman.toml` reference |
| [Integrations](integrations.md) | Issue trackers, LLM providers, and git hosts |
| [Pipeline](pipeline.md) | State machine, TDD verification, review gates, and retry loops |
| [Skills](skills.md) | YAML skill engine, hook points, and built-in skills |
| [Agent Runner](agent-runner.md) | Builtin runner, Claude Code, and Copilot integrations |
| [Architecture](architecture.md) | System design, package layout, and core principles |
| [Dashboard](dashboard.md) | Web UI, REST API, WebSocket, and authentication |
| [Deployment](deployment.md) | Docker Compose and systemd production setup |
| [Development](development.md) | Local dev setup, testing, contributing, and PR workflow |

## Navigation

### Getting started

- New to Foreman? Start with [Getting Started](getting-started.md).
- Want to know what Foreman can do? See [Features](features.md).
- Configuring Foreman for your stack? See [Configuration](configuration.md).
- Connecting to Jira, GitHub Issues, Linear, or an LLM? See [Integrations](integrations.md).
- Deploying to production? See [Deployment](deployment.md).

### Extending Foreman

- Adding custom workflow steps (security scans, changelogs, notifications)? See [Skills](skills.md).
- Plugging in Claude Code, GitHub Copilot, or a custom agent? See [Agent Runner](agent-runner.md).

### Contributing

- Setting up a development environment? See [Development](development.md).
- Understanding the codebase? See [Architecture](architecture.md).
- Understanding the processing pipeline? See [Pipeline](pipeline.md).
- Reporting a security vulnerability? See [SECURITY.md](../SECURITY.md).
- Contribution guidelines? See [CONTRIBUTING.md](../CONTRIBUTING.md).
