# Dashboard Dark Brutalism Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Redesign the Foreman dashboard with Dark Brutalism aesthetics (black background, `#FFE600` electric yellow, monospace, hard borders, no softness) and add a header strip showing three-state daemon status, cost-vs-budget with alert, and active pipeline count.

**Architecture:** Three backend wiring tasks (DaemonStatusProvider interface, CostConfig on API, updated signatures) followed by three frontend rewrites (index.html, style.css, app.js). The `daemon.Daemon` struct already satisfies the new interface via its existing `IsRunning()` and `IsPaused()` methods — no daemon changes needed.

**Tech Stack:** Go 1.23+ (net/http), vanilla JS (ES5-compatible), CSS custom properties

---

### Task 1: Backend — Add DaemonStatusProvider Interface + Update API Struct and handleStatus

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/api_test.go`

**Context:** `daemon.Daemon` already has `IsRunning() bool` and `IsPaused() bool` methods (see `internal/daemon/daemon.go`). We define a matching interface in the dashboard package to avoid a hard import dependency and allow nil in standalone mode.

**Step 1: Write the failing tests**

Add to `internal/dashboard/api_test.go` after the existing `mockDashboardDB` block:

```go
// mockDaemonStatus implements DaemonStatusProvider for tests.
type mockDaemonStatus struct {
	running bool
	paused  bool
}

func (m *mockDaemonStatus) IsRunning() bool { return m.running }
func (m *mockDaemonStatus) IsPaused() bool  { return m.paused }

func TestAPIGetStatus_DaemonRunning(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true, paused: false}, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "running" {
		t.Errorf("expected daemon_state=running, got %v", resp["daemon_state"])
	}
}

func TestAPIGetStatus_DaemonPaused(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true, paused: true}, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "paused" {
		t.Errorf("expected daemon_state=paused, got %v", resp["daemon_state"])
	}
}

func TestAPIGetStatus_NilProvider(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "stopped" {
		t.Errorf("expected daemon_state=stopped, got %v", resp["daemon_state"])
	}
}
```

Also update every existing `NewAPI(db, nil, "1.0.0")` call in the test file to `NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")`. There are 9 such calls (lines 77, 100, 119, 132, 143, 158, 169, 180, 195 approximately).

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/dashboard/ -run "TestAPIGetStatus_Daemon|TestAPIGetStatus_Nil" -v`
Expected: FAIL — `NewAPI` wrong number of arguments

**Step 3: Implement the changes in api.go**

Add the `DaemonStatusProvider` interface and update the `API` struct and `NewAPI`:

```go
// DaemonStatusProvider is an optional interface for exposing daemon runtime state.
// Pass nil when running the dashboard without an attached daemon.
type DaemonStatusProvider interface {
	IsRunning() bool
	IsPaused() bool
}
```

Update the `API` struct:

```go
// API handles REST API requests for the dashboard.
type API struct {
	db             DashboardDB
	emitter        EventSubscriber
	statusProvider DaemonStatusProvider // nil = standalone (no daemon)
	costCfg        models.CostConfig
	version        string
	startedAt      time.Time
}
```

Update `NewAPI`:

```go
// NewAPI creates a new API instance.
func NewAPI(db DashboardDB, emitter EventSubscriber, statusProvider DaemonStatusProvider, costCfg models.CostConfig, version string) *API {
	return &API{
		db:             db,
		emitter:        emitter,
		statusProvider: statusProvider,
		costCfg:        costCfg,
		version:        version,
		startedAt:      time.Now(),
	}
}
```

Update `handleStatus`:

```go
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	daemonState := "stopped"
	if a.statusProvider != nil {
		if a.statusProvider.IsRunning() {
			if a.statusProvider.IsPaused() {
				daemonState = "paused"
			} else {
				daemonState = "running"
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "running",
		"version":      a.version,
		"uptime":       time.Since(a.startedAt).String(),
		"daemon_state": daemonState,
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): add DaemonStatusProvider interface and daemon_state to /api/status"
```

---

### Task 2: Backend — Wire CostConfig to handleCostsBudgets

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/api_test.go`

**Context:** The `API` struct now has `costCfg models.CostConfig` from Task 1. We just need to update `handleCostsBudgets` to use it and add a test. `models.CostConfig` has `MaxCostPerDayUSD float64` and `AlertThresholdPct int`.

**Step 1: Write the failing test**

Add to `internal/dashboard/api_test.go`:

```go
func TestAPIHandleCostsBudgets(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{
		MaxCostPerDayUSD:  150.0,
		AlertThresholdPct: 80,
	}, "1.0.0")

	req := httptest.NewRequest("GET", "/api/costs/budgets", nil)
	rec := httptest.NewRecorder()
	api.handleCostsBudgets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["max_daily_usd"] != 150.0 {
		t.Errorf("expected max_daily_usd=150, got %v", resp["max_daily_usd"])
	}
	if resp["alert_threshold_pct"] != float64(80) {
		t.Errorf("expected alert_threshold_pct=80, got %v", resp["alert_threshold_pct"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestAPIHandleCostsBudgets -v`
Expected: FAIL — response contains `note` field, not `max_daily_usd`

**Step 3: Update handleCostsBudgets in api.go**

Replace the existing `handleCostsBudgets` body:

```go
func (a *API) handleCostsBudgets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_daily_usd":       a.costCfg.MaxCostPerDayUSD,
		"alert_threshold_pct": a.costCfg.AlertThresholdPct,
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): wire CostConfig to /api/costs/budgets endpoint"
```

---

### Task 3: Backend — Update NewServer Signature and cmd/dashboard.go

**Files:**
- Modify: `internal/dashboard/server.go`
- Modify: `cmd/dashboard.go`

**Context:** `NewServer` calls `NewAPI(db, emitter, version)`. We need to thread `DaemonStatusProvider` and `models.CostConfig` through. In the standalone `dashboard` CLI command there is no running daemon, so pass `nil` for the status provider. Config already loaded in `cmd/dashboard.go`.

**Step 1: Update NewServer in server.go**

Change the signature and the `NewAPI` call inside it:

```go
// NewServer creates a new dashboard Server and registers all HTTP routes.
func NewServer(db DashboardDB, emitter EventSubscriber, statusProvider DaemonStatusProvider, reg *prometheus.Registry, costCfg models.CostConfig, version, host string, port int) *Server {
	api := NewAPI(db, emitter, statusProvider, costCfg, version)
	// ... rest unchanged
```

The full `NewServer` function signature line becomes:
```go
func NewServer(db DashboardDB, emitter EventSubscriber, statusProvider DaemonStatusProvider, reg *prometheus.Registry, costCfg models.CostConfig, version, host string, port int) *Server {
```

Add `"github.com/canhta/foreman/internal/models"` to the server.go imports if not already present.

**Step 2: Update cmd/dashboard.go**

Change the `NewServer` call to pass `nil` (no daemon in standalone mode) and `cfg.Cost`:

```go
srv := dashboard.NewServer(database, emitter, nil, reg, cfg.Cost, "0.1.0", host, port)
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/dashboard/server.go cmd/dashboard.go
git commit -m "feat(dashboard): thread DaemonStatusProvider and CostConfig through NewServer"
```

---

### Task 4: Frontend — Rewrite index.html

**Files:**
- Modify: `internal/dashboard/web/index.html`

**Context:** Current HTML is 17 lines. Full rewrite to add brutalist structure: sticky header strip with status dot + cost + active count, two-column main with labelled panels.

**Step 1: Replace index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FOREMAN</title>
    <link rel="stylesheet" href="/style.css">
</head>
<body>
    <header>
        <span class="wordmark">FOREMAN</span>
        <div class="header-right">
            <span id="status-dot" class="status-dot"></span>
            <span id="status-text">CONNECTING</span>
            <span class="divider">|</span>
            <span id="cost-display">COST: --</span>
            <span class="divider">|</span>
            <span id="active-display">ACTIVE: --</span>
        </div>
    </header>
    <main>
        <section id="tickets-panel">
            <div class="panel-header">TICKETS (<span id="ticket-count">0</span>)</div>
            <div id="tickets"></div>
        </section>
        <section id="events-panel">
            <div class="panel-header">LIVE EVENTS</div>
            <div id="event-log"></div>
        </section>
    </main>
    <script src="/app.js"></script>
</body>
</html>
```

**Step 2: Verify build (embed check)**

Run: `go build ./internal/dashboard/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/dashboard/web/index.html
git commit -m "feat(dashboard): brutalism HTML structure with header strip and panel layout"
```

---

### Task 5: Frontend — Rewrite style.css

**Files:**
- Modify: `internal/dashboard/web/style.css`

**Context:** Full rewrite. Dark brutalism: `#0a0a0a` background, `#FFE600` accent, `monospace` font, `2px solid #FFE600` borders, `4px 4px 0 #FFE600` card shadows, zero border-radius. Exception: the status dot uses `border-radius: 50%` (it's a circle indicator, not a UI component).

**Step 1: Replace style.css**

```css
:root {
    --bg: #0a0a0a;
    --surface: #111111;
    --border: #FFE600;
    --accent: #FFE600;
    --text: #F0F0F0;
    --muted: #888888;
    --danger: #FF4444;
    --shadow: 4px 4px 0 #FFE600;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

body {
    font-family: monospace;
    background: var(--bg);
    color: var(--text);
    min-height: 100vh;
}

/* ── Header ── */
header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 1.5rem;
    border-bottom: 2px solid var(--border);
    background: var(--bg);
    position: sticky;
    top: 0;
    z-index: 10;
}

.wordmark {
    font-size: 1.1rem;
    font-weight: bold;
    color: var(--accent);
    letter-spacing: 0.1em;
}

.header-right {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    font-size: 0.8rem;
    letter-spacing: 0.05em;
}

.status-dot {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--muted);
    flex-shrink: 0;
}

.status-dot.running      { background: var(--accent); }
.status-dot.paused       { background: var(--muted); }
.status-dot.disconnected { background: var(--danger); }

.divider { color: var(--muted); }

#cost-display.over-budget { color: var(--danger); }

/* ── Layout ── */
main {
    display: grid;
    grid-template-columns: 1fr 1fr;
    height: calc(100vh - 45px);
}

section {
    border-right: 2px solid var(--border);
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

section:last-child { border-right: none; }

.panel-header {
    padding: 0.5rem 1rem;
    font-size: 0.75rem;
    letter-spacing: 0.1em;
    border-bottom: 2px solid var(--border);
    background: var(--surface);
    flex-shrink: 0;
}

/* ── Ticket cards ── */
#tickets {
    padding: 1rem;
    overflow-y: auto;
    flex: 1;
}

.ticket {
    border: 2px solid var(--border);
    box-shadow: var(--shadow);
    background: var(--surface);
    padding: 0.75rem;
    margin-bottom: 1rem;
}

.ticket-title {
    font-size: 0.9rem;
    color: var(--text);
    margin-bottom: 0.4rem;
}

.ticket-title::before {
    content: '> ';
    color: var(--accent);
}

.ticket-status {
    display: inline-block;
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    padding: 0.15rem 0.4rem;
    background: var(--accent);
    color: #0a0a0a;
}

.ticket-status.status-failed,
.ticket-status.status-blocked {
    background: var(--danger);
    color: var(--text);
}

.ticket-status.status-queued,
.ticket-status.status-pending {
    background: #333333;
    color: var(--text);
}

/* ── Event log ── */
#event-log {
    overflow-y: auto;
    flex: 1;
    font-size: 0.8rem;
}

.event-entry {
    padding: 0.35rem 1rem;
    border-bottom: 1px solid #1a1a1a;
}

.event-entry:nth-child(even) { background: #0f0f0f; }

.event-time   { color: var(--accent); }
.event-type   { color: var(--text); }
.event-ticket {
    color: var(--muted);
    display: block;
    padding-left: 5.5rem;
}
```

**Step 2: Verify build**

Run: `go build ./internal/dashboard/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/dashboard/web/style.css
git commit -m "feat(dashboard): dark brutalism CSS — yellow accent, hard borders, monospace, zero softness"
```

---

### Task 6: Frontend — Rewrite app.js

**Files:**
- Modify: `internal/dashboard/web/app.js`

**Context:** Full rewrite. Adds `loadCosts()` (polls `/api/costs/today` + `/api/costs/budgets`, renders `$X.XX / $Y` with red threshold), `loadActive()` (polls `/api/pipeline/active`), `loadStatus()` (polls `/api/status` for `daemon_state`). WebSocket open/close sets `wsConnected`. `updateDot()` derives dot state from `{wsConnected, daemonState}`.

All intervals: status=15s, tickets=10s, costs=60s, active=30s. ES5-compatible (no arrow functions, no const/let, no template literals) to avoid any transpilation requirement.

**Step 1: Replace app.js**

```javascript
(function () {
    var token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
    if (token) localStorage.setItem('foreman_token', token);

    var headers = { 'Authorization': 'Bearer ' + token };
    var wsConnected = false;
    var daemonState = 'stopped';

    function fetchJSON(url) {
        return fetch(url, { headers: headers }).then(function (r) { return r.json(); });
    }

    /* ── Status dot ── */
    function updateDot() {
        var dot = document.getElementById('status-dot');
        var txt = document.getElementById('status-text');
        dot.className = 'status-dot';
        if (!wsConnected) {
            dot.classList.add('disconnected');
            txt.textContent = 'DISCONNECTED';
        } else if (daemonState === 'paused') {
            dot.classList.add('paused');
            txt.textContent = 'PAUSED';
        } else if (daemonState === 'running') {
            dot.classList.add('running');
            txt.textContent = 'RUNNING';
        } else {
            dot.classList.add('paused');
            txt.textContent = 'STOPPED';
        }
    }

    /* ── API polls ── */
    function loadStatus() {
        fetchJSON('/api/status').then(function (data) {
            daemonState = data.daemon_state || 'stopped';
            updateDot();
        }).catch(function () {
            daemonState = 'stopped';
            updateDot();
        });
    }

    function loadCosts() {
        Promise.all([
            fetchJSON('/api/costs/today'),
            fetchJSON('/api/costs/budgets')
        ]).then(function (results) {
            var today = results[0];
            var budget = results[1];
            var cost = today.cost_usd || 0;
            var el = document.getElementById('cost-display');
            if (budget.max_daily_usd) {
                var pct = (cost / budget.max_daily_usd) * 100;
                var threshold = budget.alert_threshold_pct || 80;
                el.textContent = 'COST: $' + cost.toFixed(2) + ' / $' + Math.round(budget.max_daily_usd);
                el.className = pct >= threshold ? 'over-budget' : '';
            } else {
                el.textContent = 'COST: $' + cost.toFixed(2);
                el.className = '';
            }
        }).catch(function () {});
    }

    function loadActive() {
        fetchJSON('/api/pipeline/active').then(function (tickets) {
            document.getElementById('active-display').textContent =
                'ACTIVE: ' + (Array.isArray(tickets) ? tickets.length : 0);
        }).catch(function () {});
    }

    function loadTickets() {
        fetchJSON('/api/tickets').then(function (tickets) {
            document.getElementById('ticket-count').textContent = tickets.length;
            document.getElementById('tickets').innerHTML = tickets.map(function (t) {
                var status = (t.Status || 'unknown').toLowerCase();
                return '<div class="ticket">' +
                    '<div class="ticket-title">' + (t.Title || t.ID) + '</div>' +
                    '<span class="ticket-status status-' + status + '">' +
                    status.toUpperCase() + '</span>' +
                    '</div>';
            }).join('');
        }).catch(function () {});
    }

    /* ── WebSocket ── */
    function connectWS() {
        var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        var ws = new WebSocket(proto + '//' + location.host + '/ws/events');
        var log = document.getElementById('event-log');

        ws.onopen = function () {
            wsConnected = true;
            updateDot();
        };

        ws.onmessage = function (e) {
            var evt = JSON.parse(e.data);
            var entry = document.createElement('div');
            entry.className = 'event-entry';
            entry.innerHTML =
                '<span class="event-time">' + new Date().toLocaleTimeString() + '</span>' +
                ' <span class="event-type">' + (evt.event_type || '') + '</span>' +
                '<span class="event-ticket">[' + (evt.ticket_id || '') + ']</span>';
            log.insertBefore(entry, log.firstChild);
            while (log.children.length > 200) { log.removeChild(log.lastChild); }
        };

        ws.onclose = function () {
            wsConnected = false;
            updateDot();
            setTimeout(connectWS, 3000);
        };
    }

    /* ── Boot ── */
    loadStatus();
    loadTickets();
    loadCosts();
    loadActive();

    setInterval(loadStatus,  15000);
    setInterval(loadTickets, 10000);
    setInterval(loadCosts,   60000);
    setInterval(loadActive,  30000);

    connectWS();
}());
```

**Step 2: Verify build**

Run: `go build ./internal/dashboard/`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/dashboard/web/app.js
git commit -m "feat(dashboard): brutalism JS — three-state dot, cost/budget indicator, active pipelines"
```
