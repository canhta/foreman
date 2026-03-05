# Phase 6: Open Source Readiness — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prepare Foreman for open source release: CI/CD pipeline, release automation, documentation, example config, and contributor guide.

**Architecture:** GitHub Actions for CI (test, lint, build) and CD (release binaries + Docker image on tag push). Documentation covers install, quickstart, config reference. Contributing guide covers dev setup and PR process.

**Tech Stack:** GitHub Actions, goreleaser, Docker (ghcr.io), golangci-lint

---

### Task 1: CI Pipeline — GitHub Actions

**Files:**
- Create: `.github/workflows/ci.yml`

**Step 1: Create CI workflow**

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Install dependencies
        run: go mod download
      - name: Run tests
        run: go test ./... -v -race -coverprofile=coverage.out
      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest

  build:
    runs-on: ubuntu-latest
    needs: [test, lint]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Build
        run: go build -o foreman .
      - name: Verify binary
        run: ./foreman --help
```

**Step 2: Commit**

```bash
mkdir -p .github/workflows
git add .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions CI pipeline (test, lint, build)"
```

---

### Task 2: Release Automation — GitHub Actions + goreleaser

**Files:**
- Create: `.github/workflows/release.yml`
- Create: `.goreleaser.yml`

**Step 1: Create goreleaser config**

```yaml
# .goreleaser.yml
version: 2
before:
  hooks:
    - go mod tidy
builds:
  - env:
      - CGO_ENABLED=1
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    binary: foreman
    ldflags:
      - -s -w -X main.version={{.Version}} -X main.commit={{.Commit}}

archives:
  - format: tar.gz
    name_template: "foreman-{{ .Version }}-{{ .Os }}-{{ .Arch }}"

dockers:
  - image_templates:
      - "ghcr.io/canhta/foreman:{{ .Version }}"
      - "ghcr.io/canhta/foreman:latest"
    dockerfile: Dockerfile
    build_flag_templates:
      - "--pull"
      - "--label=org.opencontainers.image.source=https://github.com/canhta/foreman"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^ci:"
```

**Step 2: Create release workflow**

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: write
  packages: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Login to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Step 3: Commit**

```bash
git add .github/workflows/release.yml .goreleaser.yml
git commit -m "ci: add release automation with goreleaser and Docker publishing"
```

---

### Task 3: golangci-lint Configuration

**Files:**
- Create: `.golangci.yml`

**Step 1: Create linter config**

```yaml
# .golangci.yml
run:
  timeout: 5m

linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gofmt
    - goimports
    - misspell
    - unconvert
    - bodyclose
    - noctx

linters-settings:
  errcheck:
    check-type-assertions: true
  govet:
    enable-all: true

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
```

**Step 2: Run lint locally**

Run: `golangci-lint run ./...`
Expected: Clean or only minor warnings (fix any errors)

**Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "chore: add golangci-lint configuration"
```

---

### Task 4: Example Configuration

**Files:**
- Create: `foreman.example.toml`

**Step 1: Create example config**

```toml
# foreman.example.toml — Copy to foreman.toml and fill in your values.

[daemon]
poll_interval_secs = 60
idle_poll_interval_secs = 300
max_parallel_tickets = 3
work_dir = "~/.foreman/work"
log_level = "info"
log_format = "json"

[dashboard]
enabled = true
port = 8080
host = "127.0.0.1"

[tracker]
provider = "github"            # github | jira | linear | local
pickup_label = "foreman"
clarification_label = "foreman:clarification"
clarification_timeout_hours = 24

[git]
provider = "github"
backend = "native"             # native | gogit
default_branch = "main"
auto_push = true
pr_draft = true
branch_prefix = "foreman/"
rebase_before_pr = true

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[llm.openai]
api_key = "${OPENAI_API_KEY}"

[llm.openrouter]
api_key = "${OPENROUTER_API_KEY}"

[llm.local]
base_url = "http://localhost:11434"

[llm.outage]
max_connection_retries = 3
connection_retry_delay_secs = 30
fallback_provider = ""

[models]
planner = "anthropic:claude-sonnet-4-5-20250929"
implementer = "anthropic:claude-sonnet-4-5-20250929"
spec_reviewer = "anthropic:claude-haiku-4-5-20251001"
quality_reviewer = "anthropic:claude-haiku-4-5-20251001"
final_reviewer = "anthropic:claude-sonnet-4-5-20250929"
clarifier = "anthropic:claude-haiku-4-5-20251001"

[cost]
max_cost_per_ticket_usd = 15.00
max_cost_per_day_usd = 150.00
max_cost_per_month_usd = 3000.00
alert_threshold_percent = 80
max_llm_calls_per_task = 8

[limits]
max_tasks_per_ticket = 20
max_implementation_retries = 2
max_spec_review_cycles = 2
max_quality_review_cycles = 1
max_task_duration_secs = 600
max_total_duration_secs = 7200
context_token_budget = 80000
enable_partial_pr = true
enable_clarification = true
enable_tdd_verification = true
search_replace_similarity = 0.92
search_replace_min_context_lines = 3

[pipeline.hooks]
post_lint = []
pre_pr = []
post_pr = []

[secrets]
enabled = true
extra_patterns = []
always_exclude = [".env", ".env.*", "*.pem", "*.key", "*.p12"]

[rate_limit]
requests_per_minute = 50
burst_size = 10
backoff_base_ms = 1000
backoff_max_ms = 60000
jitter_percent = 25

[runner]
mode = "local"                  # local | docker

[runner.docker]
image = "node:22-slim"
persist_per_ticket = true
network = "none"
cpu_limit = "2.0"
memory_limit = "4g"
auto_reinstall_deps = true

[runner.local]
allowed_commands = ["npm", "yarn", "pnpm", "cargo", "go", "pytest", "make", "bun"]
forbidden_paths = [".env", ".ssh", ".aws", ".gnupg", "*.key", "*.pem"]

[database]
driver = "sqlite"

[database.sqlite]
path = "~/.foreman/foreman.db"
busy_timeout_ms = 5000
wal_mode = true
event_flush_interval_ms = 500

[database.postgres]
url = "${DATABASE_URL}"
max_connections = 10
```

**Step 2: Commit**

```bash
git add foreman.example.toml
git commit -m "docs: add example configuration file"
```

---

### Task 5: README

**Files:**
- Create: `README.md`

**Step 1: Create README**

```markdown
# Foreman

An autonomous software development daemon that turns issue tracker tickets into tested, reviewed pull requests.

## How It Works

1. **Polls** your issue tracker (GitHub Issues, Jira, Linear) for tickets labeled `foreman`
2. **Plans** — decomposes the ticket into implementation tasks using an LLM
3. **Implements** — writes code via TDD (test first, then implementation)
4. **Reviews** — spec review, quality review, and final review with feedback loops
5. **Ships** — creates a draft PR with all changes

## Quick Start

```bash
# Install
go install github.com/canhta/foreman@latest

# Initialize config
foreman init

# Edit foreman.toml with your API keys and repo settings
# Then run a single ticket:
foreman run "PROJ-123"

# Or start the daemon:
foreman start
```

## Configuration

Copy `foreman.example.toml` to `foreman.toml` and configure:

- **LLM Provider**: Anthropic (default), OpenAI, OpenRouter, or local (Ollama)
- **Issue Tracker**: GitHub Issues, Jira, Linear, or local file
- **Git**: Native CLI (default) or go-git
- **Runner**: Local or Docker (recommended for production)

See `foreman.example.toml` for all options with documentation.

## CLI Commands

```bash
foreman start                    # Start daemon (foreground)
foreman start --daemon           # Start daemon (background)
foreman stop                     # Stop daemon
foreman status                   # Show status + active pipelines
foreman run "PROJ-123"           # Run a single ticket
foreman run --dry-run "PROJ-123" # Plan only (show tasks, cost estimate)
foreman ps                       # Active pipelines
foreman cost today               # Today's cost breakdown
foreman dashboard                # Start web dashboard
foreman doctor                   # Health check all providers
foreman token generate --name me # Generate dashboard auth token
```

## Dashboard

```bash
foreman token generate --name "my-dashboard"
foreman dashboard --port 8080
```

Open `http://localhost:8080` and enter your token. Live pipeline events via WebSocket.

## Architecture

- **Stateless LLM calls** — every call gets fully assembled context; no accumulated memory
- **Deterministic scaffolding** — git, lint, tests, PRs are deterministic Go code
- **Pluggable interfaces** — swap LLM, tracker, git, runner, and database backends
- **TDD enforcement** — mechanical verification that tests fail before implementation

## Cost Controls

- Per-ticket, daily, and monthly budget limits
- Max 8 LLM calls per task (configurable)
- Alert at 80% of any budget threshold
- Automatic pause when budget exceeded

## Security

- Pre-flight secrets scanning on all files before LLM context
- Bearer token authentication for dashboard
- Docker isolation mode with no network access
- Never force-pushes or pushes to default branch
- PRs are always drafts by default

## Development

```bash
git clone https://github.com/canhta/foreman.git
cd foreman
go mod download
make test      # Run all tests
make lint      # Run linter
make build     # Build binary
```

## License

MIT
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with quickstart and architecture overview"
```

---

### Task 6: CONTRIBUTING Guide

**Files:**
- Create: `CONTRIBUTING.md`

**Step 1: Create contributing guide**

```markdown
# Contributing to Foreman

## Development Setup

1. **Prerequisites**: Go 1.23+, git, Docker (optional)
2. Clone and build:

```bash
git clone https://github.com/canhta/foreman.git
cd foreman
go mod download
make build
```

3. Run tests:

```bash
make test     # All tests with race detector
make lint     # golangci-lint
```

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- Wrap errors with context: `fmt.Errorf("doing X: %w", err)`
- Use `zerolog` for all logging (structured JSON)
- All external dependencies behind interfaces in `internal/`
- Tests go in `_test.go` files alongside the code

## Pull Request Process

1. Fork and create a feature branch
2. Write tests first (TDD)
3. Ensure `make test` and `make lint` pass
4. Submit a PR against `main`
5. PRs require review before merge

## Architecture

See `docs/spec.md` for the complete system design. Key packages:

| Package | Purpose |
|---------|---------|
| `internal/pipeline` | State machine orchestrator |
| `internal/llm` | LLM provider implementations |
| `internal/context` | Context assembly for LLM calls |
| `internal/git` | Git operations |
| `internal/runner` | Command execution (local/Docker) |
| `internal/db` | Database (SQLite/PostgreSQL) |
| `internal/dashboard` | Web UI + REST API |
| `internal/telemetry` | Metrics + events |

## Adding a New LLM Provider

1. Create `internal/llm/yourprovider.go` implementing `LlmProvider`
2. Add tests in `internal/llm/yourprovider_test.go`
3. Register in `NewProviderFromConfig()` in `internal/llm/provider.go`
4. Add config struct in `internal/models/config.go`

## Adding a New Issue Tracker

1. Create `internal/tracker/yourtracker.go` implementing `IssueTracker`
2. Add tests
3. Register in tracker factory function
```

**Step 2: Commit**

```bash
git add CONTRIBUTING.md
git commit -m "docs: add CONTRIBUTING guide"
```

---

### Task 7: LICENSE File

**Files:**
- Create: `LICENSE`

**Step 1: Create MIT license**

```
MIT License

Copyright (c) 2026 Foreman Contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

**Step 2: Commit**

```bash
git add LICENSE
git commit -m "docs: add MIT license"
```

---

### Task 8: .gitignore Update

**Files:**
- Create or modify: `.gitignore`

**Step 1: Create/update .gitignore**

```
# Binaries
foreman
dist/

# Database
*.db

# Config with secrets
foreman.toml

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
Thumbs.db

# Go
vendor/

# Coverage
coverage.out

# Docker
.docker/
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```

---

### Task 9: Version Injection via ldflags

**Files:**
- Modify: `main.go` — add version variable

**Step 1: Update main.go**

Add version variable to `main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/canhta/foreman/cmd"
)

var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 2: Add version command to cmd/root.go**

```go
// In cmd/root.go, add:
var appVersion string

func SetVersion(v string) {
	appVersion = v
	rootCmd.Version = v
}
```

**Step 3: Verify**

Run: `go build -ldflags="-X main.version=0.1.0" -o foreman . && ./foreman --version`
Expected: `foreman version 0.1.0`

**Step 4: Commit**

```bash
git add main.go cmd/root.go
git commit -m "feat: add version injection via ldflags"
```

---

### Task 10: Integration Tests + Fixtures

**Files:**
- Create: `tests/integration/pipeline_test.go`
- Create: `tests/integration/llm_mock_test.go`
- Create: `tests/fixtures/sample_repo/main.go`
- Create: `tests/fixtures/sample_repo/go.mod`
- Create: `tests/fixtures/sample_tickets/LOCAL-1.json`

**Step 1: Create fixture sample repo**

```go
// tests/fixtures/sample_repo/main.go
package main

import "fmt"

func main() {
	fmt.Println("Hello from sample repo")
}

func Add(a, b int) int {
	return a + b
}
```

```
// tests/fixtures/sample_repo/go.mod
module sample_repo

go 1.23
```

**Step 2: Create fixture sample ticket**

```json
// tests/fixtures/sample_tickets/LOCAL-1.json
{
  "external_id": "LOCAL-1",
  "title": "Add user endpoint",
  "description": "Create a REST endpoint for user management. GET /users returns a list of users from an in-memory store.",
  "acceptance_criteria": "GET /users returns 200 with JSON array. POST /users creates a new user.",
  "labels": ["foreman"],
  "priority": "medium"
}
```

**Step 3: Create integration test skeleton**

```go
// tests/integration/pipeline_test.go
package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixturesExist(t *testing.T) {
	fixtures := []string{
		"../fixtures/sample_repo/main.go",
		"../fixtures/sample_repo/go.mod",
		"../fixtures/sample_tickets/LOCAL-1.json",
	}
	for _, f := range fixtures {
		path := filepath.Join(".", f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("fixture missing: %s", f)
		}
	}
}
```

```go
// tests/integration/llm_mock_test.go
package integration

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/llm"
)

type MockLlmProvider struct {
	responses map[string]string
}

func (m *MockLlmProvider) Complete(_ context.Context, req llm.LlmRequest) (*llm.LlmResponse, error) {
	content, ok := m.responses[req.Model]
	if !ok {
		content = "mock response"
	}
	return &llm.LlmResponse{
		Content:      content,
		TokensInput:  100,
		TokensOutput: 50,
		Model:        req.Model,
		DurationMs:   100,
		StopReason:   "end_turn",
	}, nil
}

func (m *MockLlmProvider) ProviderName() string              { return "mock" }
func (m *MockLlmProvider) HealthCheck(_ context.Context) error { return nil }

func TestMockLlmProvider(t *testing.T) {
	mock := &MockLlmProvider{responses: map[string]string{
		"test-model": "hello world",
	}}
	resp, err := mock.Complete(context.Background(), llm.LlmRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected hello world, got %s", resp.Content)
	}
}
```

**Step 4: Run tests**

Run: `go test ./tests/integration/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/ tests/fixtures/
git commit -m "test: add integration test skeleton and fixture files"
```

---

### Task 11: docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

**Step 1: Create docker-compose**

```yaml
# docker-compose.yml
version: "3.8"

services:
  foreman:
    build: .
    volumes:
      - foreman-data:/root/.foreman
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
    command: ["start"]
    restart: unless-stopped

  # Optional PostgreSQL backend
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: foreman
      POSTGRES_USER: foreman
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-foreman}
    volumes:
      - pg-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    profiles:
      - postgres

volumes:
  foreman-data:
  pg-data:
```

**Step 2: Commit**

```bash
git add docker-compose.yml
git commit -m "chore: add docker-compose for local development"
```

---

### Task 12: Skill Validation in `doctor` Command

**Files:**
- Modify: `cmd/doctor.go` (created in Phase 4)

**Step 1: Add skill validation to doctor**

In the doctor command's `RunE` function, after provider health checks, add:

```go
// Validate skill files
fmt.Print("Skills... ")
skillDir := filepath.Join(".", "skills")
if _, err := os.Stat(skillDir); os.IsNotExist(err) {
	fmt.Println("no skills/ directory (OK)")
} else {
	entries, _ := os.ReadDir(skillDir)
	validCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".yml" || filepath.Ext(e.Name()) == ".yaml" {
			validCount++
		}
	}
	fmt.Printf("%d skill files found (OK)\n", validCount)
	// Full validation via skills.ValidateAll() when skills engine is wired
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/doctor.go
git commit -m "feat(cli): add skill validation to doctor command"
```

---

### Task 13: Install Script

**Files:**
- Create: `install.sh`

**Step 1: Create install script**

```bash
#!/bin/bash
# install.sh — Install Foreman binary
set -euo pipefail

REPO="canhta/foreman"
INSTALL_DIR="/usr/local/bin"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
URL="https://github.com/${REPO}/releases/download/${LATEST}/foreman-${LATEST#v}-${OS}-${ARCH}.tar.gz"

echo "Installing Foreman ${LATEST} (${OS}/${ARCH})..."
TMP=$(mktemp -d)
curl -fsSL "$URL" -o "${TMP}/foreman.tar.gz"
tar -xzf "${TMP}/foreman.tar.gz" -C "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/foreman" "$INSTALL_DIR/foreman"
else
  sudo mv "${TMP}/foreman" "$INSTALL_DIR/foreman"
fi

rm -rf "$TMP"
echo "Foreman installed to ${INSTALL_DIR}/foreman"
foreman --version
```

**Step 2: Commit**

```bash
chmod +x install.sh
git add install.sh
git commit -m "docs: add install.sh for one-liner installation"
```

---
