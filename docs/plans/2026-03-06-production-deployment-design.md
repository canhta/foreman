# Foreman Production Deployment Design

**Date:** 2026-03-06
**Goal:** Wire the daemon poll loop end-to-end and ship production-ready deployment infrastructure for both Docker Compose and systemd native binary paths.

## 1. Daemon Poll Loop + Orchestrator

### Architecture

Two components with a clean boundary:

- **Daemon** (`internal/daemon/daemon.go`) — owns _when_ and _how many_. Poll loop, concurrency control, graceful shutdown.
- **Orchestrator** (`internal/daemon/orchestrator.go`) — owns _what happens to one ticket_. Full lifecycle from pickup to PR.

The daemon calls the orchestrator through a `TicketProcessor` interface, making both sides independently testable.

### Orchestrator

```go
type TicketProcessor interface {
    ProcessTicket(ctx context.Context, ticket models.Ticket) error
}

type Orchestrator struct {
    db        db.Database
    tracker   tracker.IssueTracker
    git       git.GitProvider
    llm       llm.LlmProvider
    runner    runner.CommandRunner
    costCtrl  *telemetry.CostController
    scheduler *Scheduler
    config    OrchestratorConfig
}
```

**ProcessTicket lifecycle:**

1. `shouldPickUp()` guard — skip duplicates
2. `db.UpdateTicketStatus(queued -> planning)`
3. `tracker.UpdateStatus()` + `tracker.AddComment("Foreman picked up this ticket")`
4. `CheckTicketClarity()` — if unclear: add clarification label, set `clarification_needed`, return
5. `Planner.Plan()` -> `[]PlannedTask`
6. `TopologicalSort()` -> ordered tasks
7. `db.CreateTasks()`
8. `scheduler.TryReserve()` — if conflict: set status back to `queued`, return (retry next cycle)
9. `db.UpdateTicketStatus(planning -> implementing)`
10. Create `DAGExecutor` + `DAGTaskAdapter` -> `executor.Execute()`
11. Results: all pass -> rebase -> final review -> create PR -> `tracker.AttachPR()` -> `awaiting_merge`
12. Partial failure -> status `partial` or `failed`, comment on ticket
13. `scheduler.Release()` — release file reservations

**Error handling:** Deferred error handler at top of `ProcessTicket`:

```go
func (o *Orchestrator) ProcessTicket(ctx context.Context, ticket models.Ticket) (err error) {
    defer func() {
        if err != nil {
            o.db.UpdateTicketStatus(ctx, ticket.ID, models.StatusFailed)
            o.tracker.AddComment(ctx, ticket.ExternalID, "Failed: "+err.Error())
            o.scheduler.Release(ctx, ticket.ID)
        }
    }()
    // ... lifecycle steps
}
```

### Daemon Poll Loop

Replace the stub at `daemon.go:155`:

```go
case <-ticker.C:
    if d.paused.Load() { continue }

    // 1. Check clarification timeouts
    checkClarificationTimeouts(ctx, d.db, d.tracker, ...)

    // 2. Fetch ready tickets from tracker
    trackerTickets, err := d.tracker.FetchReadyTickets(ctx)
    // Insert new ones into DB as queued (deduplicate by external ID)

    // 3. Fetch queued tickets from DB (not from tracker response)
    queued, _ := d.db.ListTickets(ctx, models.TicketFilter{
        StatusIn: []models.TicketStatus{models.TicketStatusQueued},
    })

    // 4. Spawn bounded goroutines
    for _, ticket := range queued {
        if d.active.Load() >= int32(d.config.MaxParallelTickets) {
            break
        }
        d.active.Add(1)
        go func(t models.Ticket) {
            defer d.active.Add(-1)
            d.orchestrator.ProcessTicket(ctx, t)
        }(ticket)
    }
```

**Graceful shutdown:** Replace `atomic.Int32` with `sync.WaitGroup`. Add `WaitForDrain(ctx)`:

```go
func (d *Daemon) WaitForDrain(ctx context.Context) {
    done := make(chan struct{})
    go func() { d.wg.Wait(); close(done) }()
    select {
    case <-done:
    case <-ctx.Done():
    }
}
```

## 2. CLI Command Wiring

### Shared helper — `cmd/helpers.go`

```go
func loadConfigAndDB() (*config.Config, db.Database, error) {
    cfg, err := config.Load()
    if err != nil {
        return nil, nil, fmt.Errorf("config: %w - run 'foreman doctor' to validate", err)
    }
    database, err := db.Open(cfg.Database)
    if err != nil {
        return nil, nil, fmt.Errorf("database: %w - has 'foreman start' been run?", err)
    }
    return cfg, database, nil
}
```

### `cmd/start.go` — Full daemon bootstrap

1. Load config
2. Initialize: DB, tracker, git, LLM, runner, cost controller
3. Construct Orchestrator with all deps
4. Construct Daemon, wire orchestrator + DB + tracker + PRChecker + HookRunner
5. Create signal context (SIGINT, SIGTERM)
6. Start dashboard in background goroutine (if enabled) — uses signal context
7. `daemon.Start(ctx)` — blocks
8. After Start returns: `WaitForDrain()` with 30s hard timeout

### `cmd/doctor.go` — Real validation

Checks (sequential, each prints pass/fail):
- LLM provider: `llmProv.HealthCheck(ctx)`
- Issue tracker: `tracker.FetchReadyTickets(ctx)`
- Git: `gitProv.EnsureRepo(ctx, workDir)`
- Database: `database.Ping(ctx)`
- Skills: `skills.ValidateAll(skillDir)`
- Cost config: `costCtrl.ValidateConfig()`

New `--quick` flag: only checks DB connectivity (no network calls, no API cost). Used by Docker HEALTHCHECK.

Exit code 1 if any check fails.

### `cmd/ps.go` — Query DB

```
ID          External    Status         Duration    Tasks
abc-123     GH-42       implementing   12m         3/5
def-456     GH-55       awaiting_merge 45m         5/5
```

`--all` includes completed/failed tickets.

### `cmd/cost.go` — Query DB with limits comparison

```
Period       Spent      Limit       Usage
Today        $4.20      $150.00     2.8%
This month   $87.50     $3000.00    2.9%
```

Supports: `today`, `week`, `month`, `per-ticket`.

## 3. Deployment Infrastructure

### 3.1 Dockerfile changes

Add git to runtime image (daemon needs it for repo operations):

```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tini \
    git \
    && rm -rf /var/lib/apt/lists/*
```

Binary already installed to `/usr/local/bin/foreman` (on PATH for healthcheck + exec).

Add healthcheck:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["foreman", "doctor", "--quick"]
```

### 3.2 docker-compose.yml changes

Add healthcheck:

```yaml
healthcheck:
  test: ["CMD", "foreman", "doctor", "--quick"]
  interval: 30s
  timeout: 5s
  start_period: 10s
  retries: 3
```

### 3.3 Systemd service — `deploy/foreman.service`

```ini
[Unit]
Description=Foreman Autonomous Coding Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=foreman
Group=foreman
WorkingDirectory=/var/lib/foreman
ExecStart=/usr/local/bin/foreman start
Restart=always
RestartSec=10
EnvironmentFile=-/etc/foreman/env

NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/foreman
ReadWritePaths=/tmp
StateDirectory=foreman
LogsDirectory=foreman
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Paired with `deploy/install-systemd.sh`:
1. Create foreman user/group
2. Copy binary to `/usr/local/bin/`
3. Create `/var/lib/foreman/`, `/etc/foreman/`
4. Install service file, `systemctl daemon-reload && enable`
5. Print instructions

### 3.4 SSL setup — `scripts/setup-ssl.sh`

Usage: `./scripts/setup-ssl.sh --domain foreman.example.com --email you@email.com`

Steps:
1. DNS pre-flight: verify domain resolves to this server's IP (curl ifconfig.me vs dig)
2. Install nginx + certbot if missing
3. Write `/etc/nginx/sites-available/foreman` — reverse proxy to 127.0.0.1:3333 with websocket upgrade headers
4. Enable site, `nginx -t`, reload
5. `certbot --nginx -d $DOMAIN --non-interactive --agree-tos -m $EMAIL`
6. Reload nginx
7. Print success message

Header documents DNS prerequisite.

### 3.5 Deploy guide — `docs/deployment.md`

Sections:
1. Prerequisites (server specs, DNS, API keys)
2. Option A: Docker Compose (`docker compose up -d`, verify with `docker compose exec foreman foreman doctor`)
3. Option B: Systemd native binary (`deploy/install-systemd.sh`, configure `/etc/foreman/env`)
4. SSL setup (`scripts/setup-ssl.sh`)
5. Observability (logs, dashboard, `foreman ps`, `foreman cost today`)
6. Updating — with warning: never `docker compose down -v`. Native: `foreman doctor` before `systemctl restart`.

## 4. Testing Strategy

### Unit tests

**Orchestrator** (`internal/daemon/orchestrator_test.go`):
- `TestProcessTicket_HappyPath` — full flow to `awaiting_merge`
- `TestProcessTicket_SkipDuplicate` — shouldPickUp returns false
- `TestProcessTicket_ClarificationNeeded` — unclear ticket path
- `TestProcessTicket_PlanFailure` — error -> failed status, comment, release
- `TestProcessTicket_FileConflict` — TryReserve fails -> back to queued
- `TestProcessTicket_DAGPartialFailure` — some tasks fail -> partial
- `TestProcessTicket_CostBudgetExceeded` — budget check fails
- `TestProcessTicket_ContextCancelled` — graceful cleanup

**Daemon** (`internal/daemon/daemon_test.go`):
- `TestDaemon_PollFetchesAndQueues` — tracker tickets inserted to DB
- `TestDaemon_RespectsMaxParallel` — bounded goroutines
- `TestDaemon_SkipWhenPaused` — no processing when paused
- `TestDaemon_GracefulShutdown` — Start returns, WaitForDrain completes
- `TestDaemon_DeduplicatesTickets` — same external ID not re-inserted
- `TestDaemon_ClarificationTimeoutCheck` — called each cycle

**CLI commands**: mock providers, verify output format, exit codes.

### Integration test

`tests/integration/daemon_integration_test.go`:
- Real SQLite (in-memory) + mock tracker + mock LLM
- Insert ticket via mock tracker
- Deterministic plan (2 tasks, no deps)
- Start daemon with 100ms poll interval
- Assert ticket transitions -> `awaiting_merge`
- Assert PR creation called
- Cancel context, verify clean shutdown

### Deployment scripts

Manual testing only. Procedure documented in `docs/deployment.md`.

## 5. File Inventory

### New files
- `internal/daemon/orchestrator.go`
- `internal/daemon/orchestrator_test.go`
- `cmd/helpers.go`
- `deploy/foreman.service`
- `deploy/install-systemd.sh`
- `scripts/setup-ssl.sh`
- `docs/deployment.md`
- `tests/integration/daemon_integration_test.go`

### Modified files
- `internal/daemon/daemon.go` — wire poll loop, add WaitForDrain, SetOrchestrator
- `internal/daemon/daemon_test.go` — new poll loop tests
- `cmd/start.go` — full bootstrap
- `cmd/doctor.go` — real validation + --quick flag
- `cmd/ps.go` — DB query + table output
- `cmd/cost.go` — DB query + limits comparison
- `Dockerfile` — add git to runtime, add HEALTHCHECK
- `docker-compose.yml` — add healthcheck section
