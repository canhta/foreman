# Foreman Documentation

Foreman is an autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests — with no human involvement between "ticket labelled" and "PR ready for review."

It connects to your existing issue tracker (Jira, GitHub Issues, Linear), plans and implements each ticket with LLM-guided TDD, runs quality gates, and opens a pull request — all as a 24/7 background daemon. You stay in control: every PR is a human review checkpoint before any code ships.

## How does it work?

1. **Label a ticket** `foreman-ready` in your issue tracker (Jira, GitHub Issues, or Linear)
2. **Foreman picks it up** on the next poll cycle (default: every 60 seconds)
3. **Planning** — Foreman reads the ticket, explores your codebase, and produces an ordered task plan with file lists and acceptance criteria
4. **Implementation** — each task is implemented with TDD: tests first (red), then implementation (green), then reviewed
5. **Pull Request** — after all tasks pass review, Foreman opens a PR and notifies you

You control the code review. Foreman controls the implementation loop.

For the full details, see [How Foreman Works](features.md).

---

## Documentation Index

| Document | Description |
|---|---|
| [Getting Started](getting-started.md) | Installation, configuration, and first run |
| [Features](features.md) | Complete capability overview |
| [Configuration](configuration.md) | Global (`~/.foreman/config.toml`) and project config reference |
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
