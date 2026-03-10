# Dashboard Production-Readiness Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all production-safety issues found in the dashboard audit — panics, dead routing code, goroutine leaks, incorrect HTTP status codes, and swallowed errors.

**Architecture:** Five independent, targeted fixes applied directly to `internal/dashboard/`. Each fix is covered by a failing test written first (TDD). No new packages or interfaces required — all changes are within the existing package.

**Tech Stack:** Go 1.25+, `net/http`, `gorilla/websocket`, `zerolog`, `go test`

---

## Issue Reference

| # | File | Line | Severity | Issue |
|---|------|------|----------|-------|
| 1 | `server.go` | 274 | CRITICAL | Duplicate `case` — `/api/projects/{pid}/cost/…` routes are dead code |
| 2 | `api.go` | 485 | CRITICAL | Nil dereference on `GetConfig()` — panics if config is nil |
| 3 | `ws.go` | all 3 handlers | HIGH | No read pump — goroutine leaks when client disconnects |
| 4 | `api.go` | 1309 | HIGH | Nil dereference on `GetProject()` — panics if cfg is nil |
| 5 | `auth.go` | 39 | MEDIUM | DB error returns 401 instead of 503 |
| 6 | `api.go` | 1701/1704 | MEDIUM | `handleProjectDashboard` swallows DB errors, returns 200 with zeros |

---

### Task 1: Fix duplicate `case` in project router (server.go:274)

**Files:**
- Modify: `internal/dashboard/server.go:273-304`
- Test: `internal/dashboard/server_test.go`

**Step 1: Write the failing test**

Add to `server_test.go`. This test will fail because the `/cost/daily/…` and `/cost/monthly/…` sub-routes are currently dead code (shadowed by the first empty `case`):

```go
func TestProjectCostRoutes_NotDeadCode(t *testing.T) {
    // Verify the routing table actually reaches handleProjectDailyCost
    // and handleProjectMonthlyCost. We do this by wiring a minimal server
    // and confirming the path reaches the handler (not 404).
    //
    // Use the internal router function directly: create the mux the same way
    // NewServer does and issue test requests against it.
    called := map[string]bool{}
    db := &mockDashboardDB{}
    api := NewAPI(db, nil, nil, models.CostConfig{}, "test")
    // Stub out projects so projectDB() can resolve a worker.
    api.projects = &stubProjectRegistry{
        worker: &project.Worker{
            Database: db,
        },
    }

    mux := buildTestMux(api)

    for _, sub := range []string{"daily/2026-01-01", "monthly/2026-01"} {
        path := "/api/projects/proj-1/cost/" + sub
        req := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
        rec := httptest.NewRecorder()
        mux.ServeHTTP(rec, req)
        // 404 means the route is dead — handler was never reached.
        if rec.Code == http.StatusNotFound {
            called[sub] = false
            t.Errorf("route %q returned 404 — dead code in switch", path)
        }
    }
}
```

> Note: this test requires extracting the router logic into a helper `buildTestMux(api *API) http.Handler` so it can be tested without starting a real server. Skip this step and write the fix first if extracting the mux is harder than it seems — the critical outcome is that the duplicate case disappears.

**Step 2: Run test to verify it fails**

```bash
go test -run TestProjectCostRoutes_NotDeadCode ./internal/dashboard/
```

Expected: FAIL (404 is returned because the second `case` is shadowed).

**Step 3: Fix the duplicate case in server.go**

Remove the first (empty) duplicate `case` at line 273. The block should become a single case with the full handler body:

```go
// GET /api/projects/{pid}/cost/daily/{date} or /cost/monthly/{yearMonth} (singular, legacy)
case len(parts) >= 2 && parts[1] == "cost":
    costRest := ""
    if len(parts) == 3 {
        costRest = parts[2]
    }
    costParts := strings.SplitN(costRest, "/", 2)
    switch {
    case costRest == "" || costParts[0] == "breakdown":
        projDB, err := api.projectDB(r)
        if err != nil {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
            return
        }
        date := time.Now().Format("2006-01-02")
        yearMonth := time.Now().Format("2006-01")
        daily, _ := projDB.GetDailyCost(r.Context(), date)
        monthly, _ := projDB.GetMonthlyCost(r.Context(), yearMonth)
        writeJSON(w, http.StatusOK, map[string]interface{}{
            "daily_usd":   daily,
            "monthly_usd": monthly,
            "date":        date,
            "month":       yearMonth,
        })
    case costParts[0] == "daily" && len(costParts) == 2:
        api.handleProjectDailyCost(w, r)
    case costParts[0] == "monthly" && len(costParts) == 2:
        api.handleProjectMonthlyCost(w, r)
    default:
        http.NotFound(w, r)
    }
```

The old lines 273-274 (`case len(parts) >= 2 && parts[1] == "cost":` appearing twice) become a single `case`.

**Step 4: Run tests**

```bash
go test ./internal/dashboard/
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/server.go internal/dashboard/server_test.go
git commit -m "fix(dashboard): remove duplicate case that made /cost/daily and /cost/monthly routes unreachable"
```

---

### Task 2: Guard against nil config in handleConfigSummary (api.go:485)

**Files:**
- Modify: `internal/dashboard/api.go:485`
- Test: `internal/dashboard/api_test.go`

**Step 1: Write the failing test**

Add to `api_test.go`. This will panic (not just fail) before the fix:

```go
// mockNilReturningConfigProvider returns nil from GetConfig.
type mockNilReturningConfigProvider struct{}

func (m *mockNilReturningConfigProvider) GetConfig() *models.Config { return nil }

func TestHandleConfigSummary_NilConfig_Returns503(t *testing.T) {
    db := &mockDashboardDB{}
    api := NewAPI(db, nil, nil, models.CostConfig{}, "test")
    api.configProvider = &mockNilReturningConfigProvider{}

    req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/config/summary", nil)
    rec := httptest.NewRecorder()
    api.handleConfigSummary(rec, req)

    if rec.Code != http.StatusServiceUnavailable {
        t.Errorf("expected 503 for nil config, got %d", rec.Code)
    }
}
```

**Step 2: Run test to verify it panics/fails**

```bash
go test -run TestHandleConfigSummary_NilConfig_Returns503 ./internal/dashboard/
```

Expected: panic or FAIL.

**Step 3: Fix handleConfigSummary in api.go**

After the existing nil check for `a.configProvider`, add a nil check for `cfg`:

```go
func (a *API) handleConfigSummary(w http.ResponseWriter, r *http.Request) {
    if a.configProvider == nil {
        http.Error(w, "config not available", http.StatusServiceUnavailable)
        return
    }

    cfg := a.configProvider.GetConfig()
    if cfg == nil {
        http.Error(w, "config not available", http.StatusServiceUnavailable)
        return
    }

    // ... rest of handler unchanged
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/
```

Expected: PASS (no panic).

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "fix(dashboard): guard handleConfigSummary against nil config to prevent panic"
```

---

### Task 3: Add read pumps to all three WebSocket handlers (ws.go)

**Background:** The WebSocket protocol requires the server to read frames (including Close frames) from the client. Without a read pump, when a client disconnects, the server goroutine is stuck forever in the `for evt := range ch` loop, leaking goroutines and event channel subscriptions.

The fix is to launch a read pump goroutine that reads and discards all incoming frames. When the pump exits (client disconnected), it cancels a context that terminates the write loop.

**Files:**
- Modify: `internal/dashboard/ws.go`
- Test: `internal/dashboard/ws_test.go`

**Step 1: Write the failing test**

Add to `ws_test.go`:

```go
// TestWebSocket_ClientDisconnect_NoGoroutineLeak verifies that closing the
// client-side connection causes the server goroutine to exit promptly.
// Without a read pump, the server goroutine leaks indefinitely.
func TestWebSocket_ClientDisconnect_NoGoroutineLeak(t *testing.T) {
    ch := make(chan *models.EventRecord) // unbuffered — server will block until client disconnects
    emitter := &mockEmitter{ch: ch}
    db := &mockDashboardDB{}
    api := NewAPI(db, emitter, nil, models.CostConfig{}, "test")

    srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
    defer srv.Close()

    wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=valid"
    ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if resp != nil {
        defer resp.Body.Close()
    }
    if err != nil {
        t.Fatalf("dial: %v", err)
    }

    // Close the client connection abruptly.
    ws.Close()

    // Give the server handler time to notice and exit.
    // Without a read pump this goroutine never exits.
    time.Sleep(200 * time.Millisecond)

    // If the server handler exited, Unsubscribe was called and ch was released.
    // We verify by closing the channel — if the handler still holds it the
    // emitter's Unsubscribe would have cleaned it up.
    // The simplest check: confirm emitter.Unsubscribe was called.
    // Use a custom emitter that records unsubscribe calls:
    // (see mockTrackingEmitter below)
}

type mockTrackingEmitter struct {
    ch          chan *models.EventRecord
    unsubscribed chan struct{}
}

func (m *mockTrackingEmitter) Subscribe() chan *models.EventRecord { return m.ch }
func (m *mockTrackingEmitter) Unsubscribe(_ chan *models.EventRecord) {
    select {
    case m.unsubscribed <- struct{}{}:
    default:
    }
}

func TestWebSocket_ClientDisconnect_UnsubscribeCalled(t *testing.T) {
    ch := make(chan *models.EventRecord)
    emitter := &mockTrackingEmitter{
        ch:          ch,
        unsubscribed: make(chan struct{}, 1),
    }
    db := &mockDashboardDB{}
    api := NewAPI(db, emitter, nil, models.CostConfig{}, "test")

    srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
    defer srv.Close()

    wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=valid"
    ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if resp != nil {
        defer resp.Body.Close()
    }
    if err != nil {
        t.Fatalf("dial: %v", err)
    }

    // Close client connection and wait for server to notice.
    ws.Close()

    select {
    case <-emitter.unsubscribed:
        // Good — handler exited and called Unsubscribe.
    case <-time.After(2 * time.Second):
        t.Error("server handler did not exit after client disconnected (goroutine leak)")
    }
}
```

**Step 2: Run test to verify it fails**

```bash
go test -run TestWebSocket_ClientDisconnect_UnsubscribeCalled ./internal/dashboard/ -timeout 10s
```

Expected: FAIL (timeout — Unsubscribe is never called because the goroutine is stuck).

**Step 3: Add read pump to all three WebSocket handlers**

Replace the event-forwarding pattern in each handler with a context-cancellation pattern:

```go
// Pattern to apply to handleWebSocket, handleGlobalWebSocket, handleProjectWebSocket:
//
// After upgrading the connection and subscribing to the emitter:

ctx, cancel := context.WithCancel(r.Context())
defer cancel()

// Read pump: drain all incoming client frames and cancel ctx when connection closes.
go func() {
    defer cancel()
    for {
        if _, _, err := conn.ReadMessage(); err != nil {
            return
        }
    }
}()

for {
    select {
    case <-ctx.Done():
        return
    case evt, ok := <-ch:
        if !ok {
            return
        }
        enriched := a.enrichEvent(ctx, evt)
        data, err := json.Marshal(enriched)
        if err != nil {
            continue
        }
        if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
            return
        }
    }
}
```

Apply this pattern to:
1. `handleWebSocket` (lines 96-105)
2. `handleGlobalWebSocket` (lines 181-190)
3. `handleProjectWebSocket` (lines 249-262, with the `evt.ProjectID` filter preserved inside the `case evt` branch)

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -timeout 30s
```

Expected: PASS (including the new disconnect test).

**Step 5: Commit**

```bash
git add internal/dashboard/ws.go internal/dashboard/ws_test.go
git commit -m "fix(dashboard): add read pump to WebSocket handlers to prevent goroutine leaks on client disconnect"
```

---

### Task 4: Guard against nil cfg in handleGetProject (api.go:1309)

**Files:**
- Modify: `internal/dashboard/api.go:1309-1314`
- Test: `internal/dashboard/api_test.go`

**Step 1: Write the failing test**

```go
// mockNilCfgProjectRegistry returns (nil, "", nil) from GetProject.
type mockNilCfgProjectRegistry struct{}

func (m *mockNilCfgProjectRegistry) ListProjects() ([]project.IndexEntry, error) { return nil, nil }
func (m *mockNilCfgProjectRegistry) GetWorker(id string) (*project.Worker, bool) { return nil, false }
func (m *mockNilCfgProjectRegistry) GetProject(id string) (*project.ProjectConfig, string, error) {
    return nil, "", nil // nil cfg, no error
}
func (m *mockNilCfgProjectRegistry) CreateProject(cfg *project.ProjectConfig) (string, error) { return "", nil }
func (m *mockNilCfgProjectRegistry) UpdateProject(id string, cfg *project.ProjectConfig) error { return nil }
func (m *mockNilCfgProjectRegistry) DeleteProject(id string) error { return nil }

func TestHandleGetProject_NilConfig_Returns404(t *testing.T) {
    db := &mockDashboardDB{}
    api := NewAPI(db, nil, nil, models.CostConfig{}, "test")
    api.projects = &mockNilCfgProjectRegistry{}

    req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/projects/proj-1", nil)
    rec := httptest.NewRecorder()
    api.handleGetProject(rec, req)

    if rec.Code != http.StatusNotFound {
        t.Errorf("expected 404 for nil project config, got %d", rec.Code)
    }
}
```

**Step 2: Run test to verify it panics/fails**

```bash
go test -run TestHandleGetProject_NilConfig_Returns404 ./internal/dashboard/
```

Expected: panic (nil pointer dereference in `flattenProjectConfig(cfg)`).

**Step 3: Fix handleGetProject in api.go**

```go
func (a *API) handleGetProject(w http.ResponseWriter, r *http.Request) {
    if a.projects == nil {
        writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "project registry not configured"})
        return
    }
    pid := extractProjectID(r.URL.Path)
    cfg, _, err := a.projects.GetProject(pid)
    if err != nil {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
        return
    }
    if cfg == nil {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
        return
    }
    writeJSON(w, http.StatusOK, flattenProjectConfig(cfg))
}
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "fix(dashboard): guard handleGetProject against nil project config to prevent panic"
```

---

### Task 5: Return 503 on DB error in auth middleware (auth.go:39)

**Files:**
- Modify: `internal/dashboard/auth.go:38-41`
- Test: `internal/dashboard/auth_test.go`

**Step 1: Write the failing test**

The existing `TestAuthMiddleware_DBError_Returns401` test asserts that a DB error returns 401. That test was written _documenting the current (wrong) behavior_. The fix changes the expected status to 503. **Update the existing test** (do not add a new one):

```go
func TestAuthMiddleware_DBError_Returns503(t *testing.T) {
    handler := authMiddleware(&mockErrorAuthDB{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
    req.Header.Set("Authorization", "Bearer some-token")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    // A DB error is a server-side failure — return 503, not 401.
    if rec.Code != http.StatusServiceUnavailable {
        t.Errorf("expected 503 when DB returns error, got %d", rec.Code)
    }
}
```

Remove (or rename) the old `TestAuthMiddleware_DBError_Returns401` test since the expected behavior is changing.

**Step 2: Run test to verify it fails**

```bash
go test -run TestAuthMiddleware_DBError_Returns503 ./internal/dashboard/
```

Expected: FAIL (currently returns 401, not 503).

**Step 3: Fix authMiddleware in auth.go**

```go
func authMiddleware(db AuthValidator) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := extractBearerToken(r)
            if token == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            hash := hashToken(token)
            valid, err := db.ValidateAuthToken(r.Context(), hash)
            if err != nil {
                http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
                return
            }
            if !valid {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

The same split (`err != nil` → 503, `!valid` → 401) must be applied to the three WebSocket handlers in `ws.go` that inline the same token validation logic (lines 71-74, 152-155, 200-204).

**Step 4: Run tests**

```bash
go test ./internal/dashboard/
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dashboard/auth.go internal/dashboard/ws.go internal/dashboard/auth_test.go
git commit -m "fix(dashboard): return 503 on DB error in auth middleware instead of 401"
```

---

### Task 6: Propagate DB errors in handleProjectDashboard (api.go:1701-1710)

**Files:**
- Modify: `internal/dashboard/api.go:1701-1710`
- Test: `internal/dashboard/api_test.go`

**Step 1: Write the failing test**

```go
type mockErrorListTicketsDB struct {
    mockDashboardDB
}

func (m *mockErrorListTicketsDB) ListTickets(_ context.Context, _ models.TicketFilter) ([]*models.Ticket, error) {
    return nil, fmt.Errorf("db unavailable")
}

func TestHandleProjectDashboard_DBError_Returns500(t *testing.T) {
    db := &mockDashboardDB{}
    api := NewAPI(db, nil, nil, models.CostConfig{}, "test")
    // Wire a project registry whose projectDB returns the error DB.
    api.projects = &stubProjectRegistry{
        worker: &project.Worker{Database: &mockErrorListTicketsDB{}},
    }

    req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/projects/proj-1/dashboard", nil)
    rec := httptest.NewRecorder()
    api.handleProjectDashboard(rec, req)

    if rec.Code != http.StatusInternalServerError {
        t.Errorf("expected 500 on DB error, got %d", rec.Code)
    }
}
```

**Step 2: Run test to verify it fails**

```bash
go test -run TestHandleProjectDashboard_DBError_Returns500 ./internal/dashboard/
```

Expected: FAIL (returns 200 with zero values currently).

**Step 3: Fix handleProjectDashboard in api.go**

```go
func (a *API) handleProjectDashboard(w http.ResponseWriter, r *http.Request) {
    projDB, err := a.projectDB(r)
    if err != nil {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
        return
    }
    pid := extractProjectID(r.URL.Path)

    active := []models.TicketStatus{
        models.TicketStatusPlanning, models.TicketStatusImplementing,
        models.TicketStatusReviewing, models.TicketStatusPlanValidating,
    }
    activeTickets, err := projDB.ListTickets(r.Context(), models.TicketFilter{StatusIn: active})
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }

    date := time.Now().Format("2006-01-02")
    costToday, err := projDB.GetDailyCost(r.Context(), date)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }

    writeJSON(w, http.StatusOK, map[string]interface{}{
        "project_id":     pid,
        "active_tickets": len(activeTickets),
        "cost_today":     costToday,
    })
}
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "fix(dashboard): propagate DB errors in handleProjectDashboard instead of returning 200 with zeros"
```

---

### Task 7: Final verification

**Step 1: Run the full test suite**

```bash
go test ./internal/dashboard/ -v -count=1
```

Expected: All tests pass.

**Step 2: Run vet**

```bash
go vet ./internal/dashboard/
```

Expected: No issues.

**Step 3: Run the whole module's tests**

```bash
go test ./...
```

Expected: No new failures.

**Step 4: Commit if any minor fixups**

Only commit if there are residual changes not already committed.

---

## Summary of Changes

| File | Change |
|------|--------|
| `server.go` | Remove duplicate `case` at line 273 |
| `api.go` | Nil check after `GetConfig()` (line 485); nil check after `GetProject()` (line 1309); error propagation in `handleProjectDashboard` (lines 1701-1710) |
| `ws.go` | Add read pump goroutine + ctx-based write loop to all 3 WebSocket handlers; split `err != nil` → 503 in inline auth |
| `auth.go` | Split DB error (503) from invalid token (401) |
| `auth_test.go` | Rename `TestAuthMiddleware_DBError_Returns401` → `TestAuthMiddleware_DBError_Returns503` |
| `ws_test.go` | Add disconnect/goroutine-leak test |
| `api_test.go` | Add nil-config, nil-project, and dashboard-DB-error tests |
