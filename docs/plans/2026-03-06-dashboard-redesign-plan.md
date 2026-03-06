# Dashboard Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Redesign the Foreman dashboard from a minimal two-panel layout to a three-zone layout with ticket deep-dives, team summary, operational controls, and enriched live feed.

**Architecture:** Progressive enhancement of the existing embedded dashboard. Alpine.js for reactivity and htmx for server interactions, both loaded from CDN. Backend additions are new API handlers, DB query methods, and enriched WebSocket payloads. No new database tables.

**Tech Stack:** Go 1.25+ (backend), Alpine.js + htmx (CDN, frontend), vanilla CSS, SQLite/PostgreSQL, gorilla/websocket.

---

## Task 1: Add `IsConnected()` to WhatsApp Channel and `ChannelHealthProvider` Interface

**Files:**
- Modify: `internal/channel/channel.go`
- Modify: `internal/channel/whatsapp/whatsapp.go`

**Step 1: Add `HealthChecker` interface to channel package**

In `internal/channel/channel.go`, add after the existing `CommandHandler` interface:

```go
// HealthChecker reports the health of a channel transport.
type HealthChecker interface {
	IsConnected() bool
}
```

**Step 2: Add `IsConnected()` method to WhatsAppChannel**

In `internal/channel/whatsapp/whatsapp.go`, add after the `Name()` method:

```go
// IsConnected reports whether the WhatsApp client is currently connected.
func (w *WhatsAppChannel) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}
```

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: Compiles without errors.

**Step 4: Commit**

```bash
git add internal/channel/channel.go internal/channel/whatsapp/whatsapp.go
git commit -m "feat(channel): add HealthChecker interface and IsConnected to WhatsApp"
```

---

## Task 2: Add New DB Interface Methods and SQLite Implementation

**Files:**
- Modify: `internal/db/db.go`
- Modify: `internal/db/sqlite.go`
- Modify: `internal/db/postgres.go` (if it exists — mirror SQLite implementation)

**Step 1: Add new methods to Database interface**

In `internal/db/db.go`, add to the `Database` interface before `io.Closer`:

```go
	// Dashboard aggregates
	GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error)
	GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error)
	GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error)
	GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error)
```

**Step 2: Add model structs**

In `internal/models/ticket.go`, add:

```go
// TeamStat represents aggregated ticket stats per submitter.
type TeamStat struct {
	ChannelSenderID string  `json:"channel_sender_id"`
	TicketCount     int     `json:"ticket_count"`
	CostUSD         float64 `json:"cost_usd"`
	FailedCount     int     `json:"failed_count"`
}

// TicketSummary is a Ticket with aggregated task counts for list views.
type TicketSummary struct {
	Ticket
	TasksTotal int `json:"tasks_total"`
	TasksDone  int `json:"tasks_done"`
}
```

**Step 3: Implement SQLite methods**

In `internal/db/sqlite.go`, add:

```go
func (s *SQLiteDB) GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT channel_sender_id,
		        COUNT(*) as ticket_count,
		        COALESCE(SUM(cost_usd), 0) as cost_usd,
		        SUM(CASE WHEN status IN ('failed', 'blocked', 'partial') THEN 1 ELSE 0 END) as failed_count
		 FROM tickets
		 WHERE channel_sender_id != '' AND created_at >= ?
		 GROUP BY channel_sender_id
		 ORDER BY ticket_count DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.TeamStat
	for rows.Next() {
		var s models.TeamStat
		if err := rows.Scan(&s.ChannelSenderID, &s.TicketCount, &s.CostUSD, &s.FailedCount); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (s *SQLiteDB) GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets
		 WHERE pr_url != ''
		 ORDER BY updated_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (s *SQLiteDB) GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error) {
	query := `SELECT t.id, t.external_id, t.title, t.description, t.status,
	                 t.parent_ticket_id, t.channel_sender_id, t.decompose_depth,
	                 t.cost_usd, t.created_at, t.updated_at,
	                 COALESCE(task_counts.total, 0),
	                 COALESCE(task_counts.done, 0)
	          FROM tickets t
	          LEFT JOIN (
	              SELECT ticket_id,
	                     COUNT(*) as total,
	                     SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) as done
	              FROM tasks GROUP BY ticket_id
	          ) task_counts ON task_counts.ticket_id = t.id
	          WHERE 1=1`
	var args []interface{}

	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, s := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, s)
		}
		query += ` AND t.status IN (` + placeholders + `)`
	}
	query += ` ORDER BY t.updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.TicketSummary
	for rows.Next() {
		var ts models.TicketSummary
		var status string
		if err := rows.Scan(&ts.ID, &ts.ExternalID, &ts.Title, &ts.Description, &status,
			&ts.ParentTicketID, &ts.ChannelSenderID, &ts.DecomposeDepth,
			&ts.CostUSD, &ts.CreatedAt, &ts.UpdatedAt,
			&ts.TasksTotal, &ts.TasksDone); err != nil {
			return nil, err
		}
		ts.Status = models.TicketStatus(status)
		summaries = append(summaries, ts)
	}
	return summaries, rows.Err()
}

func (s *SQLiteDB) GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.EventRecord
	for rows.Next() {
		var e models.EventRecord
		var taskID, details sql.NullString
		if err := rows.Scan(&e.ID, &e.TicketID, &taskID, &e.EventType, &e.Severity, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.TaskID = taskID.String
		e.Details = details.String
		events = append(events, e)
	}
	return events, rows.Err()
}
```

**Step 4: Implement PostgreSQL methods**

Mirror the SQLite methods in `internal/db/postgres.go`, replacing `?` with `$1, $2, ...` positional parameters.

**Step 5: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: Compiles without errors.

**Step 6: Commit**

```bash
git add internal/db/db.go internal/db/sqlite.go internal/db/postgres.go internal/models/ticket.go
git commit -m "feat(db): add team stats, recent PRs, ticket summaries, and global events queries"
```

---

## Task 3: Extend Dashboard API — Status with Channel Health

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`
- Modify: `internal/dashboard/api_test.go`

**Step 1: Write failing test for channel health in status**

In `internal/dashboard/api_test.go`, add:

```go
type mockChannelHealth struct {
	connected bool
}

func (m *mockChannelHealth) IsConnected() bool { return m.connected }

func TestAPIGetStatus_WithChannelHealth(t *testing.T) {
	db := &mockDashboardDB{}
	ch := &mockChannelHealth{connected: true}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true}, models.CostConfig{}, "1.0.0")
	api.SetChannelHealth("whatsapp", ch)

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	channels, ok := resp["channels"].(map[string]interface{})
	if !ok {
		t.Fatal("expected channels key in response")
	}
	wa, ok := channels["whatsapp"].(map[string]interface{})
	if !ok {
		t.Fatal("expected whatsapp key in channels")
	}
	if wa["connected"] != true {
		t.Errorf("expected connected=true, got %v", wa["connected"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -run TestAPIGetStatus_WithChannelHealth -v`
Expected: FAIL — `SetChannelHealth` method does not exist.

**Step 3: Implement channel health in API**

In `internal/dashboard/api.go`, add a field and method to the API struct:

```go
// Add to API struct:
channelHealth map[string]interface{ IsConnected() bool }

// Add method:
func (a *API) SetChannelHealth(name string, h interface{ IsConnected() bool }) {
	if a.channelHealth == nil {
		a.channelHealth = make(map[string]interface{ IsConnected() bool })
	}
	a.channelHealth[name] = h
}
```

Update `handleStatus` to include channel health:

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

	resp := map[string]interface{}{
		"status":       "running",
		"version":      a.version,
		"uptime":       time.Since(a.startedAt).String(),
		"daemon_state": daemonState,
	}

	if len(a.channelHealth) > 0 {
		channels := make(map[string]interface{})
		for name, h := range a.channelHealth {
			channels[name] = map[string]interface{}{
				"connected": h.IsConnected(),
			}
		}
		resp["channels"] = channels
	}

	writeJSON(w, http.StatusOK, resp)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -run TestAPIGetStatus -v`
Expected: All status tests PASS.

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): add channel health to status endpoint"
```

---

## Task 4: Extend Dashboard API — Ticket Summaries, Team Stats, Recent PRs, Global Events

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`
- Modify: `internal/dashboard/api_test.go`

**Step 1: Update DashboardDB interface**

In `internal/dashboard/api.go`, add to the `DashboardDB` interface:

```go
	GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error)
	GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error)
	GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error)
	GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error)
```

**Step 2: Update mock in tests**

In `internal/dashboard/api_test.go`, add to `mockDashboardDB`:

```go
teamStats []models.TeamStat
summaries []models.TicketSummary

func (m *mockDashboardDB) GetTeamStats(_ context.Context, _ time.Time) ([]models.TeamStat, error) {
	return m.teamStats, nil
}

func (m *mockDashboardDB) GetRecentPRs(_ context.Context, _ int) ([]models.Ticket, error) {
	return m.tickets, nil
}

func (m *mockDashboardDB) GetTicketSummaries(_ context.Context, _ models.TicketFilter) ([]models.TicketSummary, error) {
	return m.summaries, nil
}

func (m *mockDashboardDB) GetGlobalEvents(_ context.Context, _, _ int) ([]models.EventRecord, error) {
	return m.events, nil
}
```

**Step 3: Write failing tests**

In `internal/dashboard/api_test.go`, add:

```go
func TestAPIGetTeamStats(t *testing.T) {
	db := &mockDashboardDB{
		teamStats: []models.TeamStat{
			{ChannelSenderID: "84123@s.whatsapp.net", TicketCount: 5, CostUSD: 10.0, FailedCount: 1},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/stats/team", nil)
	rec := httptest.NewRecorder()
	api.handleTeamStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetRecentPRs(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{
			{ID: "t1", Title: "PR ticket", PRURL: "https://github.com/repo/pull/1"},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/stats/recent-prs", nil)
	rec := httptest.NewRecorder()
	api.handleRecentPRs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketSummaries(t *testing.T) {
	db := &mockDashboardDB{
		summaries: []models.TicketSummary{
			{Ticket: models.Ticket{ID: "t1", Title: "Test", Status: models.TicketStatusImplementing}, TasksTotal: 6, TasksDone: 4},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/summaries", nil)
	rec := httptest.NewRecorder()
	api.handleTicketSummaries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetGlobalEvents(t *testing.T) {
	db := &mockDashboardDB{
		events: []models.EventRecord{
			{ID: "e1", TicketID: "t1", EventType: "task_started", Message: "Starting task"},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/events", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

**Step 4: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -run "TestAPIGet(TeamStats|RecentPRs|TicketSummaries|GlobalEvents)" -v`
Expected: FAIL — handler methods do not exist.

**Step 5: Implement handlers**

In `internal/dashboard/api.go`, add:

```go
func (a *API) handleTeamStats(w http.ResponseWriter, r *http.Request) {
	since := time.Now().AddDate(0, 0, -7) // current week
	stats, err := a.db.GetTeamStats(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) handleRecentPRs(w http.ResponseWriter, r *http.Request) {
	tickets, err := a.db.GetRecentPRs(r.Context(), 5)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tickets)
}

func (a *API) handleTicketSummaries(w http.ResponseWriter, r *http.Request) {
	filter := models.TicketFilter{}
	summaries, err := a.db.GetTicketSummaries(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (a *API) handleGlobalEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	events, err := a.db.GetGlobalEvents(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
```

**Step 6: Register routes**

In `internal/dashboard/server.go`, add after the existing `/api/costs/budgets` route:

```go
mux.Handle("/api/stats/team", auth(http.HandlerFunc(api.handleTeamStats)))
mux.Handle("/api/stats/recent-prs", auth(http.HandlerFunc(api.handleRecentPRs)))
mux.Handle("/api/tickets/summaries", auth(http.HandlerFunc(api.handleTicketSummaries)))
mux.Handle("/api/events", auth(http.HandlerFunc(api.handleGlobalEvents)))
```

Note: `/api/tickets/summaries` must be registered BEFORE the existing `/api/tickets/` catch-all handler. Move it above that line, or add a case in the existing switch for `strings.HasSuffix(path, "/summaries")`.

Actually, since `/api/tickets/summaries` starts with `/api/tickets/`, the existing catch-all route at `/api/tickets/` will match it. Add a case to the switch in the existing handler:

```go
case strings.HasSuffix(path, "/summaries"):
    api.handleTicketSummaries(w, r)
```

But this would conflict since `/summaries` looks like a ticket ID. Instead, register the new endpoint at a different path. Use `/api/ticket-summaries` to avoid the prefix conflict:

```go
mux.Handle("/api/ticket-summaries", auth(http.HandlerFunc(api.handleTicketSummaries)))
```

Update the test URL accordingly.

**Step 7: Run tests to verify they pass**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -v`
Expected: All tests PASS.

**Step 8: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): add team stats, recent PRs, ticket summaries, and global events endpoints"
```

---

## Task 5: Wire Daemon Pause/Resume and Ticket Retry

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/api_test.go`

**Step 1: Add `DaemonController` interface**

In `internal/dashboard/api.go`, add:

```go
// DaemonController allows the dashboard to control the daemon lifecycle.
type DaemonController interface {
	DaemonStatusProvider
	Pause()
	Resume()
}

// TicketRetrier re-queues a failed ticket for processing.
type TicketRetrier interface {
	RetryTicket(ctx context.Context, ticketID string) error
}
```

**Step 2: Add fields to API struct**

Update the `API` struct:

```go
type API struct {
	startedAt      time.Time
	db             DashboardDB
	emitter        EventSubscriber
	statusProvider DaemonStatusProvider
	controller     DaemonController
	retrier        TicketRetrier
	channelHealth  map[string]interface{ IsConnected() bool }
	version        string
	costCfg        models.CostConfig
}
```

Add setter methods:

```go
func (a *API) SetDaemonController(c DaemonController) {
	a.controller = c
	a.statusProvider = c
}

func (a *API) SetTicketRetrier(r TicketRetrier) {
	a.retrier = r
}
```

**Step 3: Write failing tests**

In `internal/dashboard/api_test.go`, add:

```go
type mockDaemonController struct {
	mockDaemonStatus
	pauseCalled  bool
	resumeCalled bool
}

func (m *mockDaemonController) Pause()  { m.pauseCalled = true }
func (m *mockDaemonController) Resume() { m.resumeCalled = true }

type mockTicketRetrier struct {
	retriedID string
}

func (m *mockTicketRetrier) RetryTicket(_ context.Context, id string) error {
	m.retriedID = id
	return nil
}

func TestAPIDaemonPause_Wired(t *testing.T) {
	ctrl := &mockDaemonController{mockDaemonStatus: mockDaemonStatus{running: true}}
	api := NewAPI(&mockDashboardDB{}, nil, ctrl, models.CostConfig{}, "1.0.0")
	api.SetDaemonController(ctrl)

	req := httptest.NewRequest("POST", "/api/daemon/pause", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonPause(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !ctrl.pauseCalled {
		t.Error("expected Pause() to be called")
	}
}

func TestAPIDaemonResume_Wired(t *testing.T) {
	ctrl := &mockDaemonController{mockDaemonStatus: mockDaemonStatus{running: true, paused: true}}
	api := NewAPI(&mockDashboardDB{}, nil, ctrl, models.CostConfig{}, "1.0.0")
	api.SetDaemonController(ctrl)

	req := httptest.NewRequest("POST", "/api/daemon/resume", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonResume(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !ctrl.resumeCalled {
		t.Error("expected Resume() to be called")
	}
}

func TestAPIRetryTicket_Wired(t *testing.T) {
	retrier := &mockTicketRetrier{}
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetTicketRetrier(retrier)

	req := httptest.NewRequest("POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if retrier.retriedID != "t1" {
		t.Errorf("expected retriedID=t1, got %s", retrier.retriedID)
	}
}
```

**Step 4: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -run "TestAPI(DaemonPause_Wired|DaemonResume_Wired|RetryTicket_Wired)" -v`
Expected: FAIL.

**Step 5: Implement handlers**

Replace the existing `handleDaemonPause`, `handleDaemonResume`, and `handleRetryTicket` in `internal/dashboard/api.go`:

```go
func (a *API) handleDaemonPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.controller == nil {
		http.Error(w, "daemon control not available", http.StatusServiceUnavailable)
		return
	}
	a.controller.Pause()
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (a *API) handleDaemonResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.controller == nil {
		http.Error(w, "daemon control not available", http.StatusServiceUnavailable)
		return
	}
	a.controller.Resume()
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (a *API) handleRetryTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.retrier == nil {
		http.Error(w, "retry not available", http.StatusServiceUnavailable)
		return
	}
	id := extractPathParam(r.URL.Path, "/api/tickets/")
	if idx := strings.Index(id, "/"); idx != -1 {
		id = id[:idx]
	}
	if err := a.retrier.RetryTicket(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying", "ticket_id": id})
}
```

**Step 6: Update existing tests that expect 501**

Update `TestAPIRetryTicket_NotImplemented` and `TestAPIDaemonPause`/`TestAPIDaemonResume` to expect 503 (ServiceUnavailable) instead of 501, since the handlers now return 503 when no controller/retrier is set:

```go
func TestAPIRetryTicket_NoRetrier(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
```

**Step 7: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -v`
Expected: All tests PASS.

**Step 8: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): wire daemon pause/resume and ticket retry"
```

---

## Task 6: Enrich WebSocket Event Payload

**Files:**
- Modify: `internal/dashboard/ws.go`
- Modify: `internal/dashboard/api.go`

**Step 1: Add ticket lookup to WebSocket enrichment**

In `internal/dashboard/ws.go`, update `handleWebSocket` to enrich events before sending:

```go
func (a *API) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := hashToken(token)
	valid, err := a.db.ValidateAuthToken(r.Context(), hash)
	if err != nil || !valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}
	defer conn.Close()

	if a.emitter == nil {
		return
	}

	ch := a.emitter.Subscribe()
	defer a.emitter.Unsubscribe(ch)

	for evt := range ch {
		enriched := a.enrichEvent(r.Context(), evt)
		data, err := json.Marshal(enriched)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
}

// enrichedEvent is the WebSocket payload with ticket context.
// ticket_title is a snapshot at event time — titles rarely change.
type enrichedEvent struct {
	models.EventRecord
	TicketTitle string `json:"ticket_title"`
	Submitter   string `json:"submitter"`
}

func (a *API) enrichEvent(ctx context.Context, evt *models.EventRecord) *enrichedEvent {
	enriched := &enrichedEvent{EventRecord: *evt}
	if evt.TicketID != "" {
		ticket, err := a.db.GetTicket(ctx, evt.TicketID)
		if err == nil && ticket != nil {
			enriched.TicketTitle = ticket.Title
			enriched.Submitter = ticket.ChannelSenderID
		}
	}
	return enriched
}
```

**Step 2: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -v`
Expected: All tests PASS.

**Step 3: Commit**

```bash
git add internal/dashboard/ws.go
git commit -m "feat(dashboard): enrich WebSocket events with ticket title and submitter"
```

---

## Task 7: Rewrite Frontend — HTML Structure (Three-Zone Layout)

**Files:**
- Modify: `internal/dashboard/web/index.html`

**Step 1: Rewrite index.html**

Replace the contents of `internal/dashboard/web/index.html` with the three-zone layout. Include Alpine.js and htmx from CDN:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FOREMAN</title>
    <link rel="stylesheet" href="/style.css">
    <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
    <script src="https://unpkg.com/htmx.org@2.0.4"></script>
</head>
<body x-data="foreman()" x-init="init()">
    <header>
        <span class="wordmark">FOREMAN</span>
        <div class="header-right">
            <span class="status-dot" :class="daemonDotClass"></span>
            <span x-text="daemonLabel"></span>
            <template x-if="whatsapp !== null">
                <span>
                    <span class="divider">|</span>
                    <span class="status-dot" :class="whatsapp ? 'running' : 'disconnected'"></span>
                    <span :class="whatsapp ? '' : 'over-budget'" x-text="whatsapp ? 'WA: OK' : 'WA: DOWN'"></span>
                </span>
            </template>
            <span class="divider">|</span>
            <span x-text="costLabel" :class="costOverBudget ? 'over-budget' : ''"></span>
            <span class="divider">|</span>
            <span x-text="'ACTIVE: ' + activeCount"></span>
            <span class="divider">|</span>
            <button class="header-btn" x-show="daemonState === 'running'" @click="pauseDaemon()">PAUSE</button>
            <button class="header-btn" x-show="daemonState !== 'running'" @click="resumeDaemon()">RESUME</button>
        </div>
    </header>

    <main>
        <!-- Left: Ticket List Sidebar -->
        <section id="tickets-panel">
            <div class="panel-header">TICKETS (<span x-text="filteredTickets.length"></span>)</div>
            <div class="ticket-filters">
                <input type="text" x-model="search" placeholder="Search tickets..." class="search-input">
                <div class="filter-tabs">
                    <button :class="filter === 'all' ? 'active' : ''" @click="filter = 'all'">ALL <span x-text="tickets.length"></span></button>
                    <button :class="filter === 'active' ? 'active' : ''" @click="filter = 'active'">ACTIVE <span x-text="countByFilter('active')"></span></button>
                    <button :class="filter === 'done' ? 'active' : ''" @click="filter = 'done'">DONE <span x-text="countByFilter('done')"></span></button>
                    <button :class="filter === 'fail' ? 'active' : ''" @click="filter = 'fail'">FAIL <span x-text="countByFilter('fail')"></span></button>
                </div>
            </div>
            <div id="tickets" class="ticket-list">
                <template x-for="t in filteredTickets" :key="t.ID">
                    <div class="ticket" :class="selectedTicket?.ID === t.ID ? 'selected' : ''" @click="selectTicket(t)">
                        <div class="ticket-title" x-text="t.Title || t.ID"></div>
                        <div class="ticket-meta">
                            <span class="ticket-status" :class="'status-' + t.Status" x-text="t.Status.toUpperCase()"></span>
                            <span class="ticket-submitter" x-text="t.ChannelSenderID ? formatSender(t.ChannelSenderID) : ''"></span>
                        </div>
                        <div class="ticket-progress" x-show="t.tasks_total > 0">
                            <span class="progress-bar-container">
                                <span class="progress-bar-fill" :style="'width:' + (t.tasks_done / t.tasks_total * 100) + '%'"></span>
                            </span>
                            <span class="progress-text" x-text="'$' + (t.CostUSD || 0).toFixed(2) + '  ' + t.tasks_done + '/' + t.tasks_total"></span>
                        </div>
                        <span class="ticket-marker" x-show="isFailed(t)">&#10007;</span>
                        <span class="ticket-marker clarification" x-show="t.Status === 'clarification_needed'">&#10067;</span>
                    </div>
                </template>
            </div>
        </section>

        <!-- Center: Detail or Team Summary -->
        <section id="detail-panel">
            <!-- Team Summary (no ticket selected) -->
            <template x-if="!selectedTicket">
                <div id="team-summary" x-data="teamSummary()" x-init="load()">
                    <!-- Content filled in Task 9 -->
                </div>
            </template>

            <!-- Ticket Detail (ticket selected) -->
            <template x-if="selectedTicket">
                <div id="ticket-detail">
                    <!-- Content filled in Task 8 -->
                </div>
            </template>
        </section>

        <!-- Right: Live Feed (collapsible) -->
        <section id="feed-panel" :class="feedCollapsed ? 'collapsed' : ''">
            <div class="panel-header">
                <span x-show="!feedCollapsed">LIVE FEED</span>
                <button class="collapse-btn" @click="feedCollapsed = !feedCollapsed" x-text="feedCollapsed ? '&#9654;' : '&#9664;'"></button>
            </div>
            <div class="feed-content" x-show="!feedCollapsed">
                <template x-for="evt in events" :key="evt.ID">
                    <div class="event-entry" :class="evt.isNew ? 'event-new' : ''">
                        <span class="event-time" x-text="formatTime(evt.CreatedAt)"></span>
                        <span class="event-severity" :class="'severity-' + evt.Severity" x-text="severityIcon(evt.Severity)"></span>
                        <span class="event-type" x-text="evt.event_type || evt.EventType"></span>
                        <div class="event-message" x-text="evt.Message" x-show="evt.Message"></div>
                        <div class="event-ticket-ref" x-show="evt.ticket_title || evt.TicketTitle">
                            <span class="event-ticket-link" @click.stop="selectTicketById(evt.ticket_id || evt.TicketID)" x-text="'[' + (evt.ticket_title || evt.TicketTitle || evt.TicketID) + ']'"></span>
                            <span class="event-submitter" x-text="formatSender(evt.submitter || evt.Submitter || '')"></span>
                        </div>
                    </div>
                </template>
            </div>
            <div class="feed-dots" x-show="feedCollapsed">
                <template x-for="evt in events.slice(0, 50)" :key="evt.ID">
                    <span class="feed-dot" :class="'severity-' + evt.Severity"></span>
                </template>
            </div>
        </section>
    </main>

    <footer>
        <span x-text="'DAILY: $' + dailyCost.toFixed(2) + ' / $' + dailyBudget.toFixed(0)"></span>
        <span class="divider">|</span>
        <span x-text="'WEEKLY: $' + weeklyCost.toFixed(2)"></span>
        <span class="divider">|</span>
        <span x-text="'MONTHLY: $' + monthlyCost.toFixed(2) + ' / $' + monthlyBudget.toFixed(0)"></span>
    </footer>

    <script src="/app.js"></script>
</body>
</html>
```

**Step 2: Verify file is saved**

Open the dashboard in a browser to confirm it loads (will be broken until CSS/JS are updated — that's expected).

**Step 3: Commit**

```bash
git add internal/dashboard/web/index.html
git commit -m "feat(dashboard): rewrite HTML to three-zone layout with Alpine.js and htmx"
```

---

## Task 8: Rewrite Frontend — CSS (Brutalist Three-Zone)

**Files:**
- Modify: `internal/dashboard/web/style.css`

**Step 1: Rewrite style.css**

Replace `internal/dashboard/web/style.css` with the full brutalist three-zone CSS. Key sections:

```css
:root {
    --bg: #0a0a0a;
    --surface: #111111;
    --border: #FFE600;
    --accent: #FFE600;
    --accent-muted: rgba(255, 230, 0, 0.15);
    --text: #F0F0F0;
    --muted: #888888;
    --danger: #FF4444;
    --success: #4CAF50;
    --shadow: 4px 4px 0 #FFE600;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

body {
    font-family: monospace;
    background: var(--bg);
    color: var(--text);
    min-height: 100vh;
    display: flex;
    flex-direction: column;
}

/* -- Header -- */
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

.header-btn {
    background: var(--surface);
    color: var(--accent);
    border: 2px solid var(--accent);
    padding: 0.2rem 0.6rem;
    font-family: monospace;
    font-size: 0.7rem;
    cursor: pointer;
    letter-spacing: 0.05em;
}
.header-btn:hover { background: var(--accent); color: var(--bg); }

.status-dot {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--muted);
    flex-shrink: 0;
}
.status-dot.running { background: var(--accent); }
.status-dot.paused { background: var(--muted); }
.status-dot.disconnected { background: var(--danger); }

.divider { color: var(--muted); }
.over-budget { color: var(--danger); }

/* -- Layout -- */
main {
    display: grid;
    grid-template-columns: 250px 1fr 300px;
    flex: 1;
    overflow: hidden;
}

main:has(#feed-panel.collapsed) {
    grid-template-columns: 250px 1fr 40px;
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
    display: flex;
    justify-content: space-between;
    align-items: center;
}

/* -- Footer -- */
footer {
    padding: 0.5rem 1.5rem;
    font-size: 0.75rem;
    letter-spacing: 0.05em;
    border-top: 2px solid var(--border);
    background: var(--surface);
    display: flex;
    gap: 1rem;
}

/* -- Ticket List Sidebar -- */
.ticket-filters {
    padding: 0.5rem;
    border-bottom: 2px solid var(--border);
    background: var(--surface);
}

.search-input {
    width: 100%;
    background: var(--bg);
    border: 2px solid var(--border);
    color: var(--text);
    font-family: monospace;
    font-size: 0.75rem;
    padding: 0.3rem 0.5rem;
    margin-bottom: 0.5rem;
}
.search-input::placeholder { color: var(--muted); }

.filter-tabs {
    display: flex;
    gap: 0.25rem;
}

.filter-tabs button {
    background: none;
    border: none;
    color: var(--muted);
    font-family: monospace;
    font-size: 0.65rem;
    cursor: pointer;
    padding: 0.2rem 0.4rem;
    letter-spacing: 0.05em;
}
.filter-tabs button.active {
    color: var(--accent);
    border-bottom: 2px solid var(--accent);
}

.ticket-list {
    overflow-y: auto;
    flex: 1;
    padding: 0.5rem;
}

.ticket {
    border: 2px solid var(--border);
    box-shadow: var(--shadow);
    background: var(--surface);
    padding: 0.6rem;
    margin-bottom: 0.75rem;
    cursor: pointer;
    position: relative;
}
.ticket.selected {
    background: var(--accent-muted);
}
.ticket:hover { background: rgba(255, 230, 0, 0.08); }

.ticket-title {
    font-size: 0.8rem;
    color: var(--text);
    margin-bottom: 0.3rem;
}
.ticket-title::before { content: '> '; color: var(--accent); }

.ticket-meta {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.3rem;
}

.ticket-status {
    display: inline-block;
    font-size: 0.6rem;
    letter-spacing: 0.08em;
    padding: 0.1rem 0.3rem;
    background: var(--accent);
    color: #0a0a0a;
}
.ticket-status.status-failed,
.ticket-status.status-blocked { background: var(--danger); color: var(--text); }
.ticket-status.status-queued,
.ticket-status.status-pending { background: #333333; color: var(--text); }
.ticket-status.status-done,
.ticket-status.status-merged { background: var(--success); color: var(--bg); }

.ticket-submitter {
    font-size: 0.65rem;
    color: var(--muted);
}

.ticket-progress {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.65rem;
    color: var(--muted);
}

.progress-bar-container {
    flex: 1;
    height: 4px;
    background: #333;
}
.progress-bar-fill {
    display: block;
    height: 100%;
    background: var(--accent);
}

.ticket-marker {
    position: absolute;
    top: 0.4rem;
    right: 0.4rem;
    font-size: 0.8rem;
    color: var(--danger);
}
.ticket-marker.clarification { color: var(--accent); }

/* -- Detail Panel -- */
#detail-panel {
    overflow-y: auto;
    padding: 1rem;
}

.detail-header {
    margin-bottom: 1rem;
}
.detail-title {
    font-size: 1rem;
    color: var(--accent);
    margin-bottom: 0.3rem;
}
.detail-meta {
    font-size: 0.75rem;
    color: var(--muted);
    margin-bottom: 0.5rem;
}
.detail-pr a {
    color: var(--accent);
    font-size: 0.75rem;
}

.retry-btn {
    background: var(--danger);
    color: var(--text);
    border: 2px solid var(--danger);
    padding: 0.3rem 0.8rem;
    font-family: monospace;
    font-size: 0.7rem;
    cursor: pointer;
    margin-top: 0.5rem;
}
.retry-btn:hover { background: #cc3333; }
.retry-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.retry-task-btn {
    background: none;
    color: var(--danger);
    border: 1px solid var(--danger);
    padding: 0.1rem 0.4rem;
    font-family: monospace;
    font-size: 0.6rem;
    cursor: pointer;
    margin-left: 0.5rem;
}

.detail-section {
    border: 2px solid var(--border);
    margin-bottom: 1rem;
}
.detail-section-header {
    padding: 0.4rem 0.75rem;
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    background: var(--surface);
    border-bottom: 2px solid var(--border);
    display: flex;
    justify-content: space-between;
}
.detail-section-body {
    padding: 0.5rem 0.75rem;
    font-size: 0.75rem;
}

/* Tasks */
.task-item {
    padding: 0.3rem 0;
    border-bottom: 1px solid #1a1a1a;
    cursor: pointer;
}
.task-icon { margin-right: 0.3rem; }
.task-icon.done { color: var(--success); }
.task-icon.failed { color: var(--danger); }
.task-icon.active { color: var(--accent); }
.task-complexity {
    font-size: 0.6rem;
    color: var(--muted);
    float: right;
}
.task-expanded {
    background: var(--bg);
    border: 1px solid #333;
    padding: 0.5rem;
    margin: 0.3rem 0;
    font-size: 0.7rem;
}

/* Cost bars */
.cost-bar-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.3rem;
    font-size: 0.7rem;
}
.cost-bar-label { width: 100px; color: var(--muted); }
.cost-bar-amount { width: 50px; text-align: right; }
.cost-bar {
    flex: 1;
    height: 8px;
    background: #333;
}
.cost-bar-fill {
    height: 100%;
    background: var(--accent);
}

/* Budget indicator */
.budget-bar {
    margin-top: 0.5rem;
    font-size: 0.7rem;
}

/* -- Team Summary -- */
.summary-section {
    border: 2px solid var(--border);
    box-shadow: var(--shadow);
    margin-bottom: 1rem;
    background: var(--surface);
}
.summary-header {
    padding: 0.4rem 0.75rem;
    font-size: 0.7rem;
    letter-spacing: 0.1em;
    border-bottom: 2px solid var(--border);
}
.summary-body {
    padding: 0.5rem 0.75rem;
    font-size: 0.75rem;
}

.bar-chart-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.2rem;
    font-size: 0.7rem;
}
.bar-chart-label { width: 30px; color: var(--muted); }
.bar-chart-bar {
    color: var(--accent);
    white-space: nowrap;
}

.team-row {
    display: flex;
    justify-content: space-between;
    padding: 0.2rem 0;
    font-size: 0.7rem;
    border-bottom: 1px solid #1a1a1a;
}

.pr-row {
    display: flex;
    justify-content: space-between;
    padding: 0.2rem 0;
    font-size: 0.7rem;
    border-bottom: 1px solid #1a1a1a;
}
.pr-row a { color: var(--accent); text-decoration: none; }

.attention-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.3rem 0;
    border-bottom: 1px solid #1a1a1a;
    font-size: 0.7rem;
    cursor: pointer;
}
.attention-item:hover { background: rgba(255, 230, 0, 0.05); }

/* -- Live Feed -- */
.feed-content {
    overflow-y: auto;
    flex: 1;
}

.event-entry {
    padding: 0.35rem 0.75rem;
    border-bottom: 1px solid #1a1a1a;
}
.event-entry:nth-child(even) { background: #0f0f0f; }

.event-time { color: var(--accent); font-size: 0.7rem; }
.event-severity { margin: 0 0.3rem; }
.severity-info { color: var(--muted); }
.severity-success { color: var(--success); }
.severity-error { color: var(--danger); }
.severity-warning { color: var(--accent); }
.event-type { font-size: 0.7rem; }
.event-message { font-size: 0.65rem; color: var(--muted); padding-left: 1.5rem; }
.event-ticket-ref { font-size: 0.65rem; color: var(--muted); padding-left: 1.5rem; }
.event-ticket-link { color: var(--accent); cursor: pointer; }
.event-ticket-link:hover { text-decoration: underline; }
.event-submitter { margin-left: 0.3rem; }

@keyframes event-slide-in {
    from { background: var(--accent-muted); opacity: 0.6; }
    to { background: transparent; opacity: 1; }
}
.event-new { animation: event-slide-in 1.2s ease-out forwards; }

/* Collapsed feed */
#feed-panel.collapsed { max-width: 40px; min-width: 40px; }
.collapse-btn {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.8rem;
    font-family: monospace;
}
.feed-dots {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 0.5rem 0;
    overflow-y: auto;
    flex: 1;
}
.feed-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
}

/* Clarification section */
.clarification-q { color: var(--accent); }
.clarification-a { color: var(--text); margin-top: 0.3rem; }
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/style.css
git commit -m "feat(dashboard): rewrite CSS for three-zone brutalist layout"
```

---

## Task 9: Rewrite Frontend — JavaScript (Alpine.js Application)

**Files:**
- Modify: `internal/dashboard/web/app.js`

**Step 1: Rewrite app.js**

Replace `internal/dashboard/web/app.js` with the full Alpine.js application. This is the largest file. Key structure:

```javascript
// Auth
var token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
if (token) localStorage.setItem('foreman_token', token);
var headers = { 'Authorization': 'Bearer ' + token };

function fetchJSON(url) {
    return fetch(url, { headers: headers }).then(function (r) {
        if (!r.ok) throw new Error(r.statusText);
        return r.json();
    });
}

function postJSON(url) {
    return fetch(url, { method: 'POST', headers: headers }).then(function (r) {
        if (!r.ok) throw new Error(r.statusText);
        return r.json();
    });
}

function formatSender(jid) {
    if (!jid) return '';
    // Strip @s.whatsapp.net suffix, show phone number
    return jid.replace(/@s\.whatsapp\.net$/, '');
}

function formatTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleTimeString();
}

function formatRelative(ts) {
    if (!ts) return '';
    var diff = Date.now() - new Date(ts).getTime();
    var mins = Math.floor(diff / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return mins + 'm ago';
    var hours = Math.floor(mins / 60);
    if (hours < 24) return hours + 'h ago';
    return Math.floor(hours / 24) + 'd ago';
}

function severityIcon(severity) {
    switch (severity) {
        case 'success': return '\u2713';
        case 'error': return '\u2717';
        case 'warning': return '\u2753';
        default: return '\u25CF';
    }
}

var ACTIVE_STATUSES = ['planning', 'plan_validating', 'implementing', 'reviewing',
    'pr_created', 'awaiting_merge', 'clarification_needed', 'decomposing'];
var DONE_STATUSES = ['done', 'merged'];
var FAIL_STATUSES = ['failed', 'blocked', 'partial'];
var STUCK_THRESHOLD_MS = 30 * 60 * 1000; // 30 minutes

// Main Alpine.js component
function foreman() {
    return {
        // State
        tickets: [],
        selectedTicket: null,
        ticketDetail: null,
        ticketTasks: [],
        ticketLlmCalls: [],
        ticketEvents: [],
        events: [],
        filter: 'all',
        search: '',
        feedCollapsed: localStorage.getItem('feed_collapsed') === 'true',
        expandedTasks: {},

        // Header state
        daemonState: 'stopped',
        whatsapp: null,
        dailyCost: 0,
        dailyBudget: 0,
        monthlyCost: 0,
        monthlyBudget: 0,
        weeklyCost: 0,
        activeCount: 0,

        // Team summary state
        teamStats: [],
        weekDays: [],
        recentPRs: [],

        // WebSocket
        ws: null,
        wsConnected: false,

        get daemonDotClass() {
            if (!this.wsConnected) return 'disconnected';
            if (this.daemonState === 'paused') return 'paused';
            if (this.daemonState === 'running') return 'running';
            return 'paused';
        },

        get daemonLabel() {
            if (!this.wsConnected) return 'DISCONNECTED';
            return this.daemonState.toUpperCase();
        },

        get costLabel() {
            if (this.dailyBudget > 0) {
                return 'COST: $' + this.dailyCost.toFixed(2) + ' / $' + Math.round(this.dailyBudget);
            }
            return 'COST: $' + this.dailyCost.toFixed(2);
        },

        get costOverBudget() {
            if (!this.dailyBudget) return false;
            return (this.dailyCost / this.dailyBudget) * 100 >= 80;
        },

        get filteredTickets() {
            var self = this;
            var list = this.tickets;

            // Filter by tab
            if (this.filter === 'active') {
                list = list.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; });
            } else if (this.filter === 'done') {
                list = list.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; });
            } else if (this.filter === 'fail') {
                list = list.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; });
            }

            // Search by title and submitter
            if (this.search) {
                var q = this.search.toLowerCase();
                list = list.filter(function (t) {
                    return (t.Title && t.Title.toLowerCase().indexOf(q) !== -1) ||
                        (t.ChannelSenderID && t.ChannelSenderID.toLowerCase().indexOf(q) !== -1);
                });
            }

            // Sort: failed pinned to top, then by UpdatedAt desc
            list.sort(function (a, b) {
                var aFail = self.isFailed(a) ? 0 : 1;
                var bFail = self.isFailed(b) ? 0 : 1;
                if (aFail !== bFail) return aFail - bFail;
                return new Date(b.UpdatedAt) - new Date(a.UpdatedAt);
            });

            return list;
        },

        get needsAttention() {
            var now = Date.now();
            return this.tickets.filter(function (t) {
                if (FAIL_STATUSES.indexOf(t.Status) !== -1) return true;
                if (t.Status === 'clarification_needed') return true;
                // Stuck: active but no event in 30+ min
                if (ACTIVE_STATUSES.indexOf(t.Status) !== -1 && t.UpdatedAt) {
                    var elapsed = now - new Date(t.UpdatedAt).getTime();
                    if (elapsed > STUCK_THRESHOLD_MS) return true;
                }
                return false;
            });
        },

        init: function () {
            this.loadStatus();
            this.loadTickets();
            this.loadCosts();
            this.loadActive();
            this.connectWS();

            var self = this;
            setInterval(function () { self.loadStatus(); }, 15000);
            setInterval(function () { self.loadTickets(); }, 10000);
            setInterval(function () { self.loadCosts(); }, 60000);
            setInterval(function () { self.loadActive(); }, 30000);

            // Watch feed collapse state
            this.$watch('feedCollapsed', function (val) {
                localStorage.setItem('feed_collapsed', val);
            });
        },

        // API calls
        loadStatus: function () {
            var self = this;
            fetchJSON('/api/status').then(function (data) {
                self.daemonState = data.daemon_state || 'stopped';
                if (data.channels && data.channels.whatsapp) {
                    self.whatsapp = data.channels.whatsapp.connected;
                }
            }).catch(function () {
                self.daemonState = 'stopped';
            });
        },

        loadTickets: function () {
            var self = this;
            fetchJSON('/api/ticket-summaries').then(function (data) {
                self.tickets = data || [];
            }).catch(function () {});
        },

        loadCosts: function () {
            var self = this;
            Promise.all([
                fetchJSON('/api/costs/today'),
                fetchJSON('/api/costs/budgets'),
                fetchJSON('/api/costs/month'),
                fetchJSON('/api/costs/week')
            ]).then(function (results) {
                self.dailyCost = results[0].cost_usd || 0;
                self.dailyBudget = results[1].max_daily_usd || 0;
                self.monthlyCost = results[2].cost_usd || 0;
                self.monthlyBudget = results[1].max_monthly_usd || 0;

                // Weekly total
                var week = results[3] || [];
                self.weeklyCost = week.reduce(function (sum, d) { return sum + (d.cost_usd || 0); }, 0);
                self.weekDays = week;
            }).catch(function () {});
        },

        loadActive: function () {
            var self = this;
            fetchJSON('/api/pipeline/active').then(function (data) {
                self.activeCount = Array.isArray(data) ? data.length : 0;
            }).catch(function () {});
        },

        // Ticket selection
        selectTicket: function (t) {
            this.selectedTicket = t;
            this.loadTicketDetail(t.ID);
        },

        selectTicketById: function (id) {
            var t = this.tickets.find(function (t) { return t.ID === id; });
            if (t) this.selectTicket(t);
        },

        loadTicketDetail: function (id) {
            var self = this;
            Promise.all([
                fetchJSON('/api/tickets/' + id),
                fetchJSON('/api/tickets/' + id + '/tasks'),
                fetchJSON('/api/tickets/' + id + '/llm-calls'),
                fetchJSON('/api/tickets/' + id + '/events')
            ]).then(function (results) {
                self.ticketDetail = results[0];
                self.ticketTasks = results[1] || [];
                self.ticketLlmCalls = results[2] || [];
                self.ticketEvents = results[3] || [];
                self.expandedTasks = {};
            }).catch(function () {});
        },

        // Filters
        countByFilter: function (f) {
            if (f === 'active') return this.tickets.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; }).length;
            if (f === 'done') return this.tickets.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; }).length;
            if (f === 'fail') return this.tickets.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; }).length;
            return this.tickets.length;
        },

        isFailed: function (t) {
            return FAIL_STATUSES.indexOf(t.Status) !== -1;
        },

        // Task helpers
        taskIcon: function (status) {
            switch (status) {
                case 'done': return '\u2713';
                case 'failed': return '\u2717';
                case 'implementing': case 'tdd_verifying': case 'testing':
                case 'spec_review': case 'quality_review': return '\u2699';
                case 'skipped': return '\u2298';
                default: return '\u25CB';
            }
        },

        taskIconClass: function (status) {
            if (status === 'done') return 'done';
            if (status === 'failed') return 'failed';
            if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].indexOf(status) !== -1) return 'active';
            return '';
        },

        toggleTask: function (taskId) {
            if (this.expandedTasks[taskId]) {
                delete this.expandedTasks[taskId];
            } else {
                this.expandedTasks[taskId] = true;
            }
            // Force reactivity
            this.expandedTasks = Object.assign({}, this.expandedTasks);
        },

        // Cost breakdown
        costByRole: function () {
            var roles = {};
            var total = 0;
            (this.ticketLlmCalls || []).forEach(function (c) {
                if (!roles[c.Role]) roles[c.Role] = 0;
                roles[c.Role] += c.CostUSD || 0;
                total += c.CostUSD || 0;
            });
            var result = [];
            for (var role in roles) {
                result.push({ role: role, cost: roles[role], pct: total > 0 ? (roles[role] / total * 100) : 0 });
            }
            result.sort(function (a, b) { return b.cost - a.cost; });
            return result;
        },

        llmSummary: function () {
            var calls = this.ticketLlmCalls || [];
            var totalTokens = 0;
            var models = {};
            var ok = 0;
            var retried = 0;
            calls.forEach(function (c) {
                totalTokens += (c.TokensInput || 0) + (c.TokensOutput || 0);
                models[c.Model] = true;
                if (c.Status === 'success') ok++;
                else retried++;
            });
            return {
                totalCalls: calls.length,
                ok: ok,
                retried: retried,
                totalTokens: totalTokens,
                model: Object.keys(models).join(', ') || '--'
            };
        },

        // Actions
        pauseDaemon: function () {
            if (!confirm('Pause the daemon?')) return;
            postJSON('/api/daemon/pause').catch(function (e) { alert('Failed: ' + e.message); });
        },

        resumeDaemon: function () {
            if (!confirm('Resume the daemon?')) return;
            postJSON('/api/daemon/resume').catch(function (e) { alert('Failed: ' + e.message); });
        },

        retryTicket: function (id) {
            if (!confirm('Retry this ticket?')) return;
            var self = this;
            postJSON('/api/tickets/' + id + '/retry').then(function () {
                self.loadTicketDetail(id);
                self.loadTickets();
            }).catch(function (e) { alert('Failed: ' + e.message); });
        },

        retryTask: function (taskId) {
            if (!confirm('Retry this task?')) return;
            var self = this;
            postJSON('/api/tasks/' + taskId + '/retry').then(function () {
                if (self.selectedTicket) self.loadTicketDetail(self.selectedTicket.ID);
            }).catch(function (e) { alert('Failed: ' + e.message); });
        },

        // WebSocket
        connectWS: function () {
            var self = this;
            var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            var ws = new WebSocket(proto + '//' + location.host + '/ws/events?token=' + encodeURIComponent(token));

            ws.onopen = function () {
                self.wsConnected = true;
            };

            ws.onmessage = function (e) {
                var evt = JSON.parse(e.data);
                evt.isNew = true;
                self.events.unshift(evt);
                if (self.events.length > 50) self.events.pop();

                // Clear isNew after animation
                setTimeout(function () { evt.isNew = false; }, 1200);
            };

            ws.onclose = function () {
                self.wsConnected = false;
                setTimeout(function () { self.connectWS(); }, 3000);
            };

            self.ws = ws;
        },

        formatSender: formatSender,
        formatTime: formatTime,
        formatRelative: formatRelative,
        severityIcon: severityIcon
    };
}

// Team summary sub-component
function teamSummary() {
    return {
        teamStats: [],
        weekDays: [],
        recentPRs: [],
        todayStats: { total: 0, merged: 0, failed: 0, active: 0 },

        load: function () {
            var self = this;
            var root = this.$root.__x_dataStack ? this.$root.__x_dataStack[0] : Alpine.$data(this.$root);

            Promise.all([
                fetchJSON('/api/stats/team'),
                fetchJSON('/api/stats/recent-prs')
            ]).then(function (results) {
                self.teamStats = results[0] || [];
                self.recentPRs = results[1] || [];
            }).catch(function () {});

            // Derive today stats from ticket list
            this.$nextTick(function () {
                self.computeTodayStats();
            });

            setInterval(function () {
                Promise.all([
                    fetchJSON('/api/stats/team'),
                    fetchJSON('/api/stats/recent-prs')
                ]).then(function (results) {
                    self.teamStats = results[0] || [];
                    self.recentPRs = results[1] || [];
                }).catch(function () {});
                self.computeTodayStats();
            }, 60000);
        },

        computeTodayStats: function () {
            // Access parent component data via Alpine
            var parent = Alpine.$data(this.$el.closest('[x-data]'));
            if (!parent || !parent.tickets) return;

            var today = new Date().toISOString().slice(0, 10);
            var todayTickets = parent.tickets.filter(function (t) {
                return t.CreatedAt && t.CreatedAt.slice(0, 10) === today;
            });
            this.todayStats = {
                total: todayTickets.length,
                merged: todayTickets.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; }).length,
                failed: todayTickets.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; }).length,
                active: todayTickets.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; }).length
            };
            this.weekDays = parent.weekDays || [];
        },

        maxWeekCost: function () {
            var max = 0;
            (this.weekDays || []).forEach(function (d) { if (d.cost_usd > max) max = d.cost_usd; });
            return max || 1;
        },

        barWidth: function (cost) {
            var max = this.maxWeekCost();
            var chars = Math.round((cost / max) * 16);
            var bar = '';
            for (var i = 0; i < chars; i++) bar += '\u2588';
            return bar;
        },

        dayLabel: function (dateStr) {
            var days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
            return days[new Date(dateStr).getDay()];
        }
    };
}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/app.js
git commit -m "feat(dashboard): rewrite JavaScript as Alpine.js application"
```

---

## Task 10: Add Ticket Detail and Team Summary HTML Templates

**Files:**
- Modify: `internal/dashboard/web/index.html`

**Step 1: Fill in the ticket detail template**

Replace the `<!-- Content filled in Task 8 -->` placeholder inside the `x-if="selectedTicket"` template:

```html
<div id="ticket-detail">
    <div class="detail-header">
        <div class="detail-title" x-text="ticketDetail?.Title || selectedTicket.Title"></div>
        <div class="detail-meta">
            <span x-text="selectedTicket.Status"></span>
            <span x-text="' \u00B7 ' + formatSender(selectedTicket.ChannelSenderID)"></span>
            <span x-text="' \u00B7 started ' + formatRelative(selectedTicket.StartedAt || selectedTicket.CreatedAt)"></span>
        </div>
        <div class="detail-pr" x-show="ticketDetail?.PRURL">
            PR: <a :href="ticketDetail?.PRURL" target="_blank" x-text="'#' + (ticketDetail?.PRNumber || '')"></a>
        </div>
        <button class="retry-btn"
                x-show="isFailed(selectedTicket) || selectedTicket.Status === 'partial'"
                @click="retryTicket(selectedTicket.ID)">RETRY TICKET</button>
    </div>

    <!-- Clarification -->
    <div class="detail-section" x-show="ticketDetail?.ClarificationRequestedAt">
        <div class="detail-section-header">CLARIFICATION</div>
        <div class="detail-section-body">
            <div class="clarification-q" x-text="'\u2753 ' + (ticketDetail?.ErrorMessage || 'Clarification requested')"></div>
            <template x-if="ticketDetail?.Comments && ticketDetail.Comments.length > 0">
                <div class="clarification-a" x-text="'\uD83D\uDCAC ' + ticketDetail.Comments[ticketDetail.Comments.length - 1].Body"></div>
            </template>
        </div>
    </div>

    <!-- Tasks -->
    <div class="detail-section">
        <div class="detail-section-header">
            <span>TASKS</span>
            <span x-text="ticketTasks.filter(t => t.Status === 'done').length + '/' + ticketTasks.length"></span>
        </div>
        <div class="detail-section-body">
            <template x-for="task in ticketTasks" :key="task.ID">
                <div>
                    <div class="task-item" @click="toggleTask(task.ID)">
                        <span class="task-icon" :class="taskIconClass(task.Status)" x-text="taskIcon(task.Status)"></span>
                        <span x-text="task.Sequence + '. ' + task.Title"></span>
                        <span class="task-complexity" x-text="task.EstimatedComplexity"></span>
                        <button class="retry-task-btn"
                                x-show="task.Status === 'failed'"
                                @click.stop="retryTask(task.ID)">[retry]</button>
                    </div>
                    <div class="task-expanded" x-show="expandedTasks[task.ID]">
                        <div>Status: <span x-text="task.Status"></span>
                            <span x-show="task.ImplementationAttempts > 0" x-text="' (attempt ' + task.ImplementationAttempts + ')'"></span>
                        </div>
                        <div x-show="task.FilesToModify && task.FilesToModify.length">Files: <span x-text="(task.FilesToModify || []).join(', ')"></span></div>
                        <div x-show="task.ErrorMessage" style="color: var(--danger);">Error: <span x-text="task.ErrorMessage"></span></div>
                        <div>Cost: $<span x-text="(task.CostUSD || 0).toFixed(2)"></span></div>
                    </div>
                </div>
            </template>
        </div>
    </div>

    <!-- Cost Breakdown -->
    <div class="detail-section">
        <div class="detail-section-header">
            <span>COST BREAKDOWN</span>
            <span x-text="'$' + (ticketDetail?.CostUSD || 0).toFixed(2)"></span>
        </div>
        <div class="detail-section-body">
            <template x-for="item in costByRole()" :key="item.role">
                <div class="cost-bar-row">
                    <span class="cost-bar-label" x-text="item.role"></span>
                    <span class="cost-bar-amount" x-text="'$' + item.cost.toFixed(2)"></span>
                    <span class="cost-bar">
                        <span class="cost-bar-fill" :style="'width:' + item.pct + '%'"></span>
                    </span>
                </div>
            </template>
            <div style="margin-top: 0.5rem; color: var(--muted); font-size: 0.7rem;">
                <span x-text="'Model: ' + llmSummary().model"></span> |
                <span x-text="Math.round(llmSummary().totalTokens / 1000) + 'k tokens'"></span> |
                <span x-text="llmSummary().totalCalls + ' calls (' + llmSummary().ok + ' ok, ' + llmSummary().retried + ' retried)'"></span>
            </div>
        </div>
    </div>

    <!-- Events for this ticket -->
    <div class="detail-section">
        <div class="detail-section-header">EVENTS</div>
        <div class="detail-section-body">
            <template x-for="evt in ticketEvents.slice(0, 20)" :key="evt.ID">
                <div class="event-entry">
                    <span class="event-time" x-text="formatTime(evt.CreatedAt)"></span>
                    <span class="event-severity" :class="'severity-' + evt.Severity" x-text="severityIcon(evt.Severity)"></span>
                    <span class="event-type" x-text="evt.EventType"></span>
                    <span class="event-message" x-text="evt.Message" x-show="evt.Message"></span>
                </div>
            </template>
        </div>
    </div>
</div>
```

**Step 2: Fill in the team summary template**

Replace the `<!-- Content filled in Task 9 -->` placeholder inside the `x-if="!selectedTicket"` template:

```html
<div id="team-summary" x-data="teamSummary()" x-init="load()">
    <!-- Today -->
    <div class="summary-section">
        <div class="summary-header">TODAY</div>
        <div class="summary-body">
            <div>
                <span x-text="todayStats.total + ' tickets'"></span>
                <span x-text="' \u2713' + todayStats.merged + ' merged'"></span>
                <span x-text="' \u2717' + todayStats.failed + ' failed'"></span>
                <span x-text="' \u2699' + todayStats.active + ' active'"></span>
            </div>
            <div class="budget-bar" x-data="{ p: $root.__x_dataStack ? $root.__x_dataStack[0] : {} }">
                <div x-text="'Daily: $' + ($root.dailyCost || 0).toFixed(2) + ' / $' + Math.round($root.dailyBudget || 0)"></div>
                <div x-text="'Monthly: $' + ($root.monthlyCost || 0).toFixed(2) + ' / $' + Math.round($root.monthlyBudget || 0)"></div>
            </div>
        </div>
    </div>

    <!-- This Week -->
    <div class="summary-section">
        <div class="summary-header">THIS WEEK</div>
        <div class="summary-body">
            <template x-for="day in weekDays" :key="day.date">
                <div class="bar-chart-row">
                    <span class="bar-chart-label" x-text="dayLabel(day.date)"></span>
                    <span class="bar-chart-bar" x-text="barWidth(day.cost_usd || 0)"></span>
                    <span x-text="'$' + (day.cost_usd || 0).toFixed(2)"></span>
                </div>
            </template>
            <div style="margin-top: 0.3rem; color: var(--muted);" x-text="'Total: $' + ($root.weeklyCost || 0).toFixed(2)"></div>
        </div>
    </div>

    <!-- Team -->
    <div class="summary-section">
        <div class="summary-header">TEAM</div>
        <div class="summary-body">
            <template x-for="stat in teamStats" :key="stat.channel_sender_id">
                <div class="team-row">
                    <span x-text="formatSender(stat.channel_sender_id)"></span>
                    <span x-text="stat.ticket_count + ' tickets'"></span>
                    <span x-text="'$' + stat.cost_usd.toFixed(2)"></span>
                    <span x-show="stat.failed_count > 0" style="color: var(--danger);" x-text="'\u2717' + stat.failed_count"></span>
                </div>
            </template>
        </div>
    </div>

    <!-- Recent PRs -->
    <div class="summary-section">
        <div class="summary-header">RECENT PRS</div>
        <div class="summary-body">
            <template x-for="pr in recentPRs" :key="pr.ID">
                <div class="pr-row">
                    <a :href="pr.PRURL" target="_blank" x-text="'#' + pr.PRNumber + ' ' + pr.Title"></a>
                    <span x-text="pr.Status + ' ' + formatRelative(pr.UpdatedAt)"></span>
                </div>
            </template>
        </div>
    </div>

    <!-- Needs Attention -->
    <div class="summary-section" x-show="$root.needsAttention && $root.needsAttention.length > 0">
        <div class="summary-header">NEEDS ATTENTION</div>
        <div class="summary-body">
            <template x-for="t in ($root.needsAttention || [])" :key="t.ID">
                <div class="attention-item" @click="$root.selectTicket(t)">
                    <span>
                        <span x-show="['failed','blocked','partial'].indexOf(t.Status) !== -1" style="color: var(--danger);">\u2717</span>
                        <span x-show="t.Status === 'clarification_needed'" style="color: var(--accent);">\u2753</span>
                        <span x-text="t.Title"></span>
                    </span>
                    <span style="color: var(--muted);" x-text="t.Status + ' \u00B7 ' + formatSender(t.ChannelSenderID)"></span>
                </div>
            </template>
        </div>
    </div>
</div>
```

**Step 3: Commit**

```bash
git add internal/dashboard/web/index.html
git commit -m "feat(dashboard): add ticket detail and team summary templates"
```

---

## Task 11: Add `/api/costs/budgets` Monthly Budget and Register Task Retry Route

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/server.go`
- Modify: `internal/dashboard/api_test.go`

**Step 1: Update handleCostsBudgets to include monthly budget**

In `internal/dashboard/api.go`, update `handleCostsBudgets`:

```go
func (a *API) handleCostsBudgets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_daily_usd":       a.costCfg.MaxCostPerDayUSD,
		"max_monthly_usd":     a.costCfg.MaxCostPerMonthUSD,
		"max_ticket_usd":      a.costCfg.MaxCostPerTicketUSD,
		"alert_threshold_pct": a.costCfg.AlertThresholdPct,
	})
}
```

**Step 2: Add task retry route**

In `internal/dashboard/server.go`, inside the `/api/tickets/` switch, routes are matched by suffix. Add a new top-level route for task retry:

```go
mux.Handle("/api/tasks/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.HasSuffix(r.URL.Path, "/retry") {
        api.handleRetryTask(w, r)
        return
    }
    http.NotFound(w, r)
})))
```

**Step 3: Implement handleRetryTask**

In `internal/dashboard/api.go`, add:

```go
func (a *API) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/tasks/{id}/retry
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	id := strings.TrimSuffix(path, "/retry")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}
	// For now, reset task status to pending via DB
	if err := a.db.UpdateTaskStatus(r.Context(), id, models.TaskStatusPending); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying", "task_id": id})
}
```

Note: This requires adding `UpdateTaskStatus` to the `DashboardDB` interface:

```go
UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
```

And to the mock:

```go
func (m *mockDashboardDB) UpdateTaskStatus(_ context.Context, _ string, _ models.TaskStatus) error {
    return nil
}
```

**Step 4: Write test for task retry**

```go
func TestAPIRetryTask(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("POST", "/api/tasks/task-1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

**Step 5: Run all tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/dashboard/ -v`
Expected: All tests PASS.

**Step 6: Build entire project**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: Compiles without errors.

**Step 7: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): add monthly budget, task retry endpoint, and task retry route"
```

---

## Task 12: Integration Smoke Test

**Files:** None (verification only)

**Step 1: Run all project tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1`
Expected: All tests PASS.

**Step 2: Build the binary**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build -o foreman ./cmd/foreman`
Expected: Binary compiles successfully.

**Step 3: Verify embedded assets**

Run: `cd /Users/canh/Projects/Indies/Foreman && ls -la internal/dashboard/web/`
Expected: `index.html`, `app.js`, `style.css` all present.

**Step 4: Quick manual check (if possible)**

Start the daemon with dashboard enabled and open `http://localhost:3333` in a browser. Verify:
- Three-zone layout renders
- Alpine.js initializes (no console errors)
- Auth token prompt appears
- Header shows status indicators
- API calls succeed (check Network tab)

**Step 5: Commit all remaining changes (if any)**

```bash
git status
# If clean, no commit needed
```

---

## Summary

| Task | Description | Estimated Effort |
|------|-------------|-----------------|
| 1 | WhatsApp `IsConnected()` + `HealthChecker` interface | Small |
| 2 | New DB methods (team stats, recent PRs, summaries, global events) | Medium |
| 3 | Status endpoint with channel health | Small |
| 4 | New API endpoints (team stats, recent PRs, summaries, global events) | Medium |
| 5 | Wire daemon pause/resume + ticket retry | Medium |
| 6 | Enrich WebSocket payload | Small |
| 7 | HTML rewrite (three-zone layout) | Medium |
| 8 | CSS rewrite (brutalist three-zone) | Medium |
| 9 | JavaScript rewrite (Alpine.js app) | Large |
| 10 | Ticket detail + team summary HTML templates | Medium |
| 11 | Monthly budget, task retry route | Small |
| 12 | Integration smoke test | Small |

**Execution order:** Tasks 1-6 (backend) can be done first, then 7-10 (frontend), then 11-12 (cleanup + verification). Tasks 1-2 and 7-8 can be parallelized.
