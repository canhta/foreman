# Dashboard

Foreman includes a built-in web dashboard for monitoring pipeline state, inspecting LLM costs, and managing tickets. It exposes a REST API, WebSocket live event stream, and a Prometheus metrics endpoint.

## Starting the Dashboard

The dashboard starts automatically with the daemon when `enabled = true`:

```toml
[dashboard]
enabled    = true
port       = 3333
host       = "127.0.0.1"
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"
```

Generate a token:

```bash
./foreman token generate
```

Access the dashboard at `http://localhost:3333`. All endpoints require a bearer token.

> **Security note:** The default `host = "127.0.0.1"` binds to loopback only. Do not set `host = "0.0.0.0"` without placing the dashboard behind a reverse proxy with TLS.

## Authentication

All REST API endpoints require a bearer token in the `Authorization` header:

```
Authorization: Bearer <your-token>
```

Tokens are stored as SHA-256 hashes in the database. They can be revoked via the API.

The WebSocket endpoint supports three auth modes (in priority order):

1. `Sec-WebSocket-Protocol: bearer.<token>` (preferred)
2. `Authorization: Bearer <token>`
3. `?token=<token>` query parameter (deprecated)

Example (preferred):

```javascript
const token = '<your-token>';
const ws = new WebSocket('ws://localhost:3333/ws/events', [`bearer.${token}`]);
```

Deprecated query-param form still works:

```
ws://localhost:3333/ws/events?token=<your-token>
```

## REST API

### Status

**`GET /api/status`**

Returns dashboard process status, daemon runtime state, and optional channel/MCP connectivity.

```json
{
  "status": "running",
  "version": "0.1.0",
  "uptime": "3h24m12s",
  "daemon_state": "running",
  "channels": {
    "whatsapp": { "connected": true }
  },
  "mcp_servers": {
    "filesystem": { "healthy": true }
  }
}
```

---

### Tickets

**`GET /api/tickets`**

Returns a list of all tickets. Optional query parameter:

| Parameter | Values | Description |
|---|---|---|
| `status` | `queued`, `clarification_needed`, `planning`, `plan_validating`, `implementing`, `reviewing`, `pr_created`, `awaiting_merge`, `merged`, `pr_closed`, `decomposing`, `decomposed`, `done`, `failed`, `blocked`, `partial` | Filter by status |

```json
[
  {
    "id": "uuid",
    "external_id": "PROJ-123",
    "title": "Add user authentication",
    "status": "implementing",
    "cost_usd": 1.23,
    "created_at": "2026-03-05T10:00:00Z"
  }
]
```

---

**`GET /api/tickets/{id}`**

Returns full details for a single ticket by its internal UUID.

```json
{
  "id": "uuid",
  "external_id": "PROJ-123",
  "title": "Add user authentication",
  "description": "...",
  "acceptance_criteria": "...",
  "status": "implementing",
  "branch_name": "foreman/PROJ-123-add-auth",
  "pr_url": "",
  "cost_usd": 1.23,
  "tokens_input": 45000,
  "tokens_output": 8000,
  "total_llm_calls": 4,
  "is_partial": false,
  "last_completed_task_seq": 2,
  "created_at": "2026-03-05T10:00:00Z",
  "started_at": "2026-03-05T10:01:00Z"
}
```

---

**`GET /api/tickets/{id}/tasks`**

Returns the task list for a ticket.

```json
[
  {
    "id": "task-uuid",
    "ticket_id": "uuid",
    "sequence": 1,
    "title": "Create User model and migration",
    "status": "done",
    "implementation_attempts": 1,
    "spec_review_attempts": 1,
    "quality_review_attempts": 1,
    "total_llm_calls": 3,
    "commit_sha": "abc123",
    "cost_usd": 0.45
  }
]
```

---

**`GET /api/tickets/{id}/events`**

Returns the last 100 events for a ticket.

```json
[
  {
    "id": "evt-uuid",
    "ticket_id": "uuid",
    "task_id": "task-uuid",
    "event_type": "task_tdd_verify_pass",
    "severity": "info",
    "message": "TDD verification passed",
    "details": null,
    "created_at": "2026-03-05T10:05:00Z"
  }
]
```

---

**`GET /api/ticket-summaries`**

Returns compact ticket cards used by the dashboard list view.

---

**`GET /api/events?limit=50&offset=0`**

Returns global event feed entries across tickets.

---

**`DELETE /api/tickets/{id}`**

Permanently deletes a ticket and associated records.

```json
{ "status": "deleted", "ticket_id": "uuid" }
```

---

**`GET /api/tickets/{id}/llm-calls`**

Returns all recorded LLM calls for a ticket.

```json
[
  {
    "id": "call-uuid",
    "ticket_id": "uuid",
    "task_id": "task-uuid",
    "role": "implementer",
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "attempt": 1,
    "tokens_input": 12000,
    "tokens_output": 2500,
    "cost_usd": 0.21,
    "duration_ms": 8400,
    "status": "success",
    "created_at": "2026-03-05T10:03:00Z"
  }
]
```

---

**`POST /api/tickets/{id}/retry`**

Resets a failed or blocked ticket to `queued` so it will be picked up on the next daemon poll cycle.

```json
{ "status": "retrying", "ticket_id": "uuid" }
```

---

**`POST /api/tasks/{id}/retry`**

Resets a failed task to `pending` so the pipeline can retry it.

```json
{ "status": "retrying", "task_id": "task-uuid" }
```

---

**`GET /api/tasks/{id}/context`**

Returns context budget/utilization stats for a task.

```json
{
  "budget": 18000,
  "used": 12450,
  "utilization_pct": 69.1,
  "files_selected": 14,
  "files_touched": 6,
  "cache_hits": 3
}
```

---

### Active Pipelines

**`GET /api/pipeline/active`**

Returns currently executing pipelines with their current stage.

```json
[
  {
    "ticket_id": "uuid",
    "external_id": "PROJ-123",
    "title": "Add user authentication",
    "current_stage": "implementing",
    "current_task_seq": 2,
    "total_tasks": 4,
    "started_at": "2026-03-05T10:01:00Z"
  }
]
```

---

### Costs

**`GET /api/costs/today`**

Returns total spend for today.

```json
{
  "date": "2026-03-05",
  "cost_usd": 12.45
}
```

---

**`GET /api/costs/week`**

Returns spend for the past 7 days as a flat day-by-day array.

```json
[
  { "date": "2026-02-27", "cost_usd": 11.20 },
  { "date": "2026-02-28", "cost_usd": 9.80 }
]
```

---

**`GET /api/costs/month`**

Returns spend for the current calendar month.

```json
{
  "month": "2026-03",
  "cost_usd": 245.10
}
```

---

**`GET /api/costs/budgets`**

Returns configured budget limits.

```json
{
  "max_daily_usd": 150,
  "max_monthly_usd": 3000,
  "max_ticket_usd": 100,
  "alert_threshold_pct": 80
}
```

---

### Team and PR Stats

**`GET /api/stats/team`** returns 7-day submitter aggregates.

**`GET /api/stats/recent-prs`** returns recent merged/created PR-linked tickets.

---

### Prompt Versions

**`GET /api/prompts/versions`** returns prompt-template snapshot hashes for auditability.

---

### Daemon Control

**`POST /api/daemon/pause`**

Pauses the daemon scheduler. Active pipelines finish their current task then stop.

```json
{ "message": "daemon paused" }
```

---

**`POST /api/daemon/resume`**

Resumes a paused daemon.

```json
{ "message": "daemon resumed" }
```

---

## WebSocket Live Events

Connect to `/ws/events` to receive real-time pipeline events as JSON objects.

```javascript
const token = '<your-token>';
const ws = new WebSocket('ws://localhost:3333/ws/events', [`bearer.${token}`]);

ws.onmessage = (event) => {
  const evt = JSON.parse(event.data);
  console.log(evt.event_type, evt.message);
};
```

Each message has the same shape as events returned by `GET /api/tickets/{id}/events`, enriched with ticket context fields:

```json
{
  "id": "evt-uuid",
  "ticket_id": "uuid",
  "task_id": "task-uuid",
  "seq": 42,
  "event_type": "task_spec_review_pass",
  "severity": "info",
  "message": "Spec review passed",
  "details": null,
  "created_at": "2026-03-05T10:06:00Z",
  "ticket_title": "Add user authentication",
  "submitter": "123456789@s.whatsapp.net"
}
```

The WebSocket endpoint broadcasts all pipeline events to all connected clients. There is no per-ticket subscription filtering on the server side.

---

## Prometheus Metrics

The `/api/metrics` endpoint exposes Prometheus-compatible metrics. It requires the same bearer token authentication as other API routes.

```yaml
# prometheus.yml scrape config
scrape_configs:
  - job_name: foreman
    static_configs:
      - targets: ['localhost:3333']
    metrics_path: /api/metrics
```

### Available Metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `foreman_tickets_total` | Counter | `status` | Total tickets by final status |
| `foreman_tickets_active` | Gauge | — | Currently active pipelines |
| `foreman_tasks_total` | Counter | `status` | Total tasks by final status |
| `foreman_llm_calls_total` | Counter | `role`, `model`, `status` | Total LLM calls |
| `foreman_llm_tokens_total` | Counter | `direction`, `model` | Total tokens (input/output) |
| `foreman_llm_duration_seconds` | Histogram | `role`, `model` | LLM call latency |
| `foreman_cost_usd_total` | Counter | `model` | Total cost by model |
| `foreman_pipeline_duration_seconds` | Histogram | — | End-to-end pipeline duration |
| `foreman_test_runs_total` | Counter | `result` | Test run outcomes |
| `foreman_retries_total` | Counter | `role` | Retry counts by pipeline role |
| `foreman_rate_limits_total` | Counter | `provider` | Rate limit hits by provider |
| `foreman_tdd_verify_total` | Counter | `result` | TDD verification outcomes |
| `foreman_partial_prs_total` | Counter | — | Partial PR count |
| `foreman_clarifications_total` | Counter | — | Clarification requests issued |
| `foreman_secrets_detected_total` | Counter | — | Secrets detected and excluded |
| `foreman_hook_executions_total` | Counter | `hook` | Hook point executions |
| `foreman_skill_executions_total` | Counter | `skill`, `status` | Skill executions by outcome |

---

## Web UI

The web UI is a Svelte 5 + TypeScript single-page app built with Vite and Tailwind CSS 4. Build output is embedded into the binary at build time (`//go:embed dist`) and served from `/`.

Developer commands:

```bash
make web-dev          # run Vite dev server
make web-build        # build web/dist assets
make build            # builds dashboard assets + foreman binary
```

The UI provides:
- Ticket list with status indicators and filtering
- Ticket detail view with task breakdown, cost summary, and event log
- Live event feed via WebSocket
- Cost overview (today, week, month) with budget indicators
- Active pipeline monitor

---

## See Also

- [Configuration](configuration.md#dashboard) — `[dashboard]` config reference
- [Deployment](deployment.md) — exposing the dashboard over HTTPS in production
- [Getting Started](getting-started.md) — generating a dashboard token during initial setup
