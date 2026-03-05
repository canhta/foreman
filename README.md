# Foreman

An autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests.

Foreman polls your issue tracker, decomposes tickets into tasks, writes code via LLM using TDD, runs reviews, and opens PRs — all without human intervention.

## How It Works

```
Ticket → Clarification Check → Planning → Plan Validation →
  Per-Task: [Implement (TDD) → Lint → Spec Review → Quality Review → Commit] →
Final Review → PR Creation
```

## Status

Phase 1 complete — core foundations built:

| Package | Status |
|---------|--------|
| `internal/models` | Domain models (Ticket, Task, pipeline states, config) |
| `internal/llm` | LLM provider interface + Anthropic implementation |
| `internal/runner` | Command runner (local) |
| `internal/pipeline` | Output parser with fuzzy search/replace |
| `internal/context` | Secrets scanner + token budget |
| `internal/db` | SQLite database with full schema |
| `internal/config` | Config loading via Viper |

## Getting Started

**Requirements:** Go 1.26+, CGO (for SQLite)

```bash
# Build
make build

# Run tests
make test

# Configure
cp foreman.example.toml foreman.toml
# Edit foreman.toml — set ANTHROPIC_API_KEY, tracker, git settings

# Run
./foreman --help
```

## Configuration

Copy `foreman.example.toml` to `foreman.toml` and set:

```toml
[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[git]
clone_url = "https://github.com/your-org/your-repo.git"

[tracker]
provider = "github"  # github, jira, linear, local_file
```

See `foreman.example.toml` for all options with defaults.

## Tech Stack

- **Go 1.26+** — runtime
- **SQLite** (`go-sqlite3`) — persistence
- **Cobra/Viper** — CLI and config
- **Anthropic API** — default LLM provider

## License

MIT
