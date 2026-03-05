# Foreman Documentation

Foreman is an autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests — with minimal human intervention.

## Contents

| Document | Description |
|---|---|
| [Getting Started](getting-started.md) | Installation, quick start, and Docker setup |
| [Features](features.md) | Full feature list with descriptions |
| [Architecture](architecture.md) | System design, package overview, and core principles |
| [Pipeline](pipeline.md) | State machine, TDD verification, review gates, and feedback loops |
| [Configuration](configuration.md) | Complete `foreman.toml` reference |
| [Integrations](integrations.md) | Issue trackers, LLM providers, and git backends |
| [Skills](skills.md) | YAML skill engine, hook points, and built-in skills |
| [Agent Runner](agent-runner.md) | AgentRunner interface, builtin runner, Claude Code, and Copilot |
| [Dashboard](dashboard.md) | Web UI, REST API, WebSocket, and authentication |
| [Development](development.md) | Contributing, testing, and local setup guide |

## Quick Navigation

### For Users
- New to Foreman? Start with [Getting Started](getting-started.md).
- Want to know what Foreman can do? See [Features](features.md).
- Configuring Foreman? See [Configuration](configuration.md).
- Connecting to Jira, GitHub, Linear, or an LLM? See [Integrations](integrations.md).

### For Contributors
- Setting up a dev environment? See [Development](development.md).
- Understanding the codebase? See [Architecture](architecture.md).
- Understanding the pipeline? See [Pipeline](pipeline.md).

### For Extensibility
- Adding custom workflow steps? See [Skills](skills.md).
- Plugging in a different AI agent? See [Agent Runner](agent-runner.md).
