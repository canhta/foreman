# Phase 5: Dashboard + Polish — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the web dashboard with REST API + WebSocket live events, add OpenAI and OpenRouter LLM providers, implement Docker command runner with orphan cleanup, add Prometheus metrics, and wire up cross-platform builds.

**Architecture:** Dashboard is a standalone HTTP server using stdlib `net/http` with bearer token auth middleware, REST endpoints backed by the existing `db.Database` interface, and gorilla/websocket for live event streaming. LLM providers follow the existing `llm.LlmProvider` interface pattern (see `anthropic.go`). Docker runner implements the existing `runner.CommandRunner` interface. Prometheus metrics use `prometheus/client_golang` with a dedicated registry. Frontend is a single embedded HTML/JS/CSS page via `go:embed`.

**Tech Stack:** Go 1.23+, net/http (server), gorilla/websocket (WebSocket), prometheus/client_golang (metrics), go:embed (frontend assets), Docker CLI (container management)

---

### Task 1: Telemetry — Prometheus Metrics Registry

**Files:**
- Create: `internal/telemetry/metrics.go`
- Create: `internal/telemetry/metrics_test.go`

**Step 1: Write the failing test**

```go
// internal/telemetry/metrics_test.go
package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}

	// Verify counters are registered by gathering
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected registered metrics, got none")
	}
}

func TestMetricsRecordLlmCall(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordLlmCall("implementer", "anthropic:claude-sonnet-4-5-20250929", "success", 1000, 500, 0.015, 2500)

	families, _ := reg.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "foreman_llm_calls_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected foreman_llm_calls_total metric")
	}
}

func TestMetricsRecordTicket(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordTicket("completed")
	m.RecordTicket("failed")

	families, _ := reg.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "foreman_tickets_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected foreman_tickets_total metric")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/ -run TestNewMetrics -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/telemetry/metrics.go
package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for Foreman.
type Metrics struct {
	TicketsTotal       *prometheus.CounterVec
	TicketsActive      prometheus.Gauge
	TasksTotal         *prometheus.CounterVec
	LlmCallsTotal     *prometheus.CounterVec
	LlmTokensTotal    *prometheus.CounterVec
	LlmDuration       *prometheus.HistogramVec
	CostUSDTotal       *prometheus.CounterVec
	PipelineDuration   prometheus.Histogram
	TestRunsTotal      *prometheus.CounterVec
	RetriesTotal       *prometheus.CounterVec
	RateLimitsTotal    *prometheus.CounterVec
	TDDVerifyTotal     *prometheus.CounterVec
	PartialPRsTotal    prometheus.Counter
	ClarificationsTotal prometheus.Counter
	SecretsDetected    prometheus.Counter
	HookExecutions     *prometheus.CounterVec
	SkillExecutions    *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		TicketsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tickets_total",
			Help: "Total tickets by status",
		}, []string{"status"}),
		TicketsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "foreman_tickets_active",
			Help: "Currently active tickets",
		}),
		TasksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tasks_total",
			Help: "Total tasks by status",
		}, []string{"status"}),
		LlmCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_llm_calls_total",
			Help: "Total LLM calls by role, model, and status",
		}, []string{"role", "model", "status"}),
		LlmTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_llm_tokens_total",
			Help: "Total LLM tokens by direction and model",
		}, []string{"direction", "model"}),
		LlmDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "foreman_llm_duration_seconds",
			Help:    "LLM call duration by role and model",
			Buckets: prometheus.DefBuckets,
		}, []string{"role", "model"}),
		CostUSDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_cost_usd_total",
			Help: "Total cost in USD by model",
		}, []string{"model"}),
		PipelineDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "foreman_pipeline_duration_seconds",
			Help:    "Pipeline duration in seconds",
			Buckets: []float64{60, 120, 300, 600, 1200, 1800, 3600, 7200},
		}),
		TestRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_test_runs_total",
			Help: "Total test runs by result",
		}, []string{"result"}),
		RetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_retries_total",
			Help: "Total retries by role",
		}, []string{"role"}),
		RateLimitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_rate_limits_total",
			Help: "Total rate limits by provider",
		}, []string{"provider"}),
		TDDVerifyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tdd_verify_total",
			Help: "TDD verification results",
		}, []string{"result"}),
		PartialPRsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_partial_prs_total",
			Help: "Total partial PRs created",
		}),
		ClarificationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_clarifications_total",
			Help: "Total clarification requests",
		}),
		SecretsDetected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_secrets_detected_total",
			Help: "Total secrets detected and excluded",
		}),
		HookExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_hook_executions_total",
			Help: "Hook executions by hook point",
		}, []string{"hook"}),
		SkillExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_skill_executions_total",
			Help: "Skill executions by skill and status",
		}, []string{"skill", "status"}),
	}

	reg.MustRegister(
		m.TicketsTotal, m.TicketsActive, m.TasksTotal,
		m.LlmCallsTotal, m.LlmTokensTotal, m.LlmDuration,
		m.CostUSDTotal, m.PipelineDuration, m.TestRunsTotal,
		m.RetriesTotal, m.RateLimitsTotal, m.TDDVerifyTotal,
		m.PartialPRsTotal, m.ClarificationsTotal, m.SecretsDetected,
		m.HookExecutions, m.SkillExecutions,
	)

	return m
}

func (m *Metrics) RecordLlmCall(role, model, status string, tokensIn, tokensOut int, costUSD float64, durationMs int64) {
	m.LlmCallsTotal.WithLabelValues(role, model, status).Inc()
	m.LlmTokensTotal.WithLabelValues("input", model).Add(float64(tokensIn))
	m.LlmTokensTotal.WithLabelValues("output", model).Add(float64(tokensOut))
	m.CostUSDTotal.WithLabelValues(model).Add(costUSD)
	m.LlmDuration.WithLabelValues(role, model).Observe(float64(durationMs) / float64(time.Second/time.Millisecond))
}

func (m *Metrics) RecordTicket(status string) {
	m.TicketsTotal.WithLabelValues(status).Inc()
}

func (m *Metrics) RecordTask(status string) {
	m.TasksTotal.WithLabelValues(status).Inc()
}

func (m *Metrics) RecordTestRun(result string) {
	m.TestRunsTotal.WithLabelValues(result).Inc()
}

func (m *Metrics) RecordTDDVerify(result string) {
	m.TDDVerifyTotal.WithLabelValues(result).Inc()
}

func (m *Metrics) RecordRetry(role string) {
	m.RetriesTotal.WithLabelValues(role).Inc()
}

func (m *Metrics) RecordRateLimit(provider string) {
	m.RateLimitsTotal.WithLabelValues(provider).Inc()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/telemetry/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/metrics.go internal/telemetry/metrics_test.go
git commit -m "feat(telemetry): add Prometheus metrics registry"
```

---

### Task 2: Telemetry — Structured Event Emitter

**Files:**
- Create: `internal/telemetry/events.go`
- Create: `internal/telemetry/events_test.go`

**Step 1: Write the failing test**

```go
// internal/telemetry/events_test.go
package telemetry

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockDB struct {
	events []*models.EventRecord
}

func (m *mockDB) RecordEvent(_ context.Context, e *models.EventRecord) error {
	m.events = append(m.events, e)
	return nil
}

func TestEventEmitter_Emit(t *testing.T) {
	db := &mockDB{}
	emitter := NewEventEmitter(db)

	emitter.Emit(context.Background(), "ticket-1", "task-1", "task_started", map[string]string{
		"task_title": "Add login",
	})

	if len(db.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(db.events))
	}
	if db.events[0].TicketID != "ticket-1" {
		t.Errorf("expected ticket-1, got %s", db.events[0].TicketID)
	}
	if db.events[0].EventType != "task_started" {
		t.Errorf("expected task_started, got %s", db.events[0].EventType)
	}
}

func TestEventEmitter_Subscribe(t *testing.T) {
	db := &mockDB{}
	emitter := NewEventEmitter(db)

	ch := emitter.Subscribe()
	defer emitter.Unsubscribe(ch)

	go emitter.Emit(context.Background(), "t1", "", "ticket_picked_up", nil)

	evt := <-ch
	if evt.EventType != "ticket_picked_up" {
		t.Errorf("expected ticket_picked_up, got %s", evt.EventType)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/ -run TestEventEmitter -v`
Expected: FAIL — NewEventEmitter not defined

**Step 3: Write minimal implementation**

```go
// internal/telemetry/events.go
package telemetry

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
)

// EventStore is a subset of db.Database for event recording.
type EventStore interface {
	RecordEvent(ctx context.Context, e *models.EventRecord) error
}

// EventEmitter writes events to the database and fans them out to WebSocket subscribers.
type EventEmitter struct {
	store       EventStore
	mu          sync.RWMutex
	subscribers map[chan *models.EventRecord]struct{}
}

func NewEventEmitter(store EventStore) *EventEmitter {
	return &EventEmitter{
		store:       store,
		subscribers: make(map[chan *models.EventRecord]struct{}),
	}
}

func (e *EventEmitter) Emit(ctx context.Context, ticketID, taskID, eventType string, metadata map[string]string) {
	var metaJSON string
	if metadata != nil {
		b, _ := json.Marshal(metadata)
		metaJSON = string(b)
	}

	evt := &models.EventRecord{
		ID:        uuid.New().String(),
		TicketID:  ticketID,
		TaskID:    taskID,
		EventType: eventType,
		Metadata:  metaJSON,
		CreatedAt: time.Now(),
	}

	_ = e.store.RecordEvent(ctx, evt)

	e.mu.RLock()
	defer e.mu.RUnlock()
	for ch := range e.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow
		}
	}
}

func (e *EventEmitter) Subscribe() chan *models.EventRecord {
	ch := make(chan *models.EventRecord, 64)
	e.mu.Lock()
	e.subscribers[ch] = struct{}{}
	e.mu.Unlock()
	return ch
}

func (e *EventEmitter) Unsubscribe(ch chan *models.EventRecord) {
	e.mu.Lock()
	delete(e.subscribers, ch)
	e.mu.Unlock()
	close(ch)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/telemetry/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/events.go internal/telemetry/events_test.go
git commit -m "feat(telemetry): add event emitter with pub/sub for WebSocket"
```

---

### Task 3: Dashboard — Auth Middleware

**Files:**
- Create: `internal/dashboard/auth.go`
- Create: `internal/dashboard/auth_test.go`

**Step 1: Write the failing test**

```go
// internal/dashboard/auth_test.go
package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockAuthDB struct {
	validHashes map[string]bool
}

func (m *mockAuthDB) ValidateAuthToken(_ context.Context, hash string) (bool, error) {
	return m.validHashes[hash], nil
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}
	// Pre-compute SHA256 of "test-token"
	db.validHashes[hashToken("test-token")] = true

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestAuthMiddleware -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/dashboard/auth.go
package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// AuthValidator is a subset of db.Database for token validation.
type AuthValidator interface {
	ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

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
			if err != nil || !valid {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/auth.go internal/dashboard/auth_test.go
git commit -m "feat(dashboard): add bearer token auth middleware"
```

---

### Task 4: Dashboard — REST API Endpoints

**Files:**
- Create: `internal/dashboard/api.go`
- Create: `internal/dashboard/api_test.go`

**Step 1: Write the failing test**

```go
// internal/dashboard/api_test.go
package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type mockDashboardDB struct {
	tickets []models.Ticket
	events  []models.EventRecord
}

func (m *mockDashboardDB) ValidateAuthToken(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (m *mockDashboardDB) ListTickets(_ context.Context, _ models.TicketFilter) ([]models.Ticket, error) {
	return m.tickets, nil
}

func (m *mockDashboardDB) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockDashboardDB) GetEvents(_ context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	var result []models.EventRecord
	for _, e := range m.events {
		if e.TicketID == ticketID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockDashboardDB) GetDailyCost(_ context.Context, _ string) (float64, error) {
	return 12.50, nil
}

func (m *mockDashboardDB) GetTicketCost(_ context.Context, _ string) (float64, error) {
	return 3.25, nil
}

func TestAPIGetStatus(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", resp["version"])
	}
}

func TestAPIListTickets(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{
			{ID: "t1", Title: "Add login", Status: models.TicketStatusImplementing, CreatedAt: time.Now()},
		},
	}
	api := NewAPI(db, nil, "1.0.0")

	req := httptest.NewRequest("GET", "/api/tickets", nil)
	rec := httptest.NewRecorder()
	api.handleListTickets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tickets []map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&tickets)
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestAPI -v`
Expected: FAIL — NewAPI not defined

**Step 3: Write minimal implementation**

```go
// internal/dashboard/api.go
package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// DashboardDB is a subset of db.Database needed by the dashboard.
type DashboardDB interface {
	AuthValidator
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	GetTicketCost(ctx context.Context, ticketID string) (float64, error)
}

type API struct {
	db        DashboardDB
	emitter   EventSubscriber
	version   string
	startedAt time.Time
}

// EventSubscriber is the subset of EventEmitter needed for WebSocket.
type EventSubscriber interface {
	Subscribe() chan *models.EventRecord
	Unsubscribe(ch chan *models.EventRecord)
}

func NewAPI(db DashboardDB, emitter EventSubscriber, version string) *API {
	return &API{
		db:        db,
		emitter:   emitter,
		version:   version,
		startedAt: time.Now(),
	}
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "running",
		"version": a.version,
		"uptime":  time.Since(a.startedAt).String(),
	})
}

func (a *API) handleListTickets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	filter := models.TicketFilter{}
	if status != "" {
		filter.StatusIn = []models.TicketStatus{models.TicketStatus(status)}
	}

	tickets, err := a.db.ListTickets(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tickets)
}

func (a *API) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/tickets/")
	if id == "" {
		http.Error(w, "missing ticket id", http.StatusBadRequest)
		return
	}

	ticket, err := a.db.GetTicket(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ticket == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (a *API) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	// Path: /api/tickets/{id}/events
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tickets/"), "/")
	if len(parts) < 2 {
		http.Error(w, "missing ticket id", http.StatusBadRequest)
		return
	}
	ticketID := parts[0]

	events, err := a.db.GetEvents(r.Context(), ticketID, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *API) handleCostsToday(w http.ResponseWriter, r *http.Request) {
	date := time.Now().Format("2006-01-02")
	cost, err := a.db.GetDailyCost(r.Context(), date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"date":     date,
		"cost_usd": cost,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func extractPathParam(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return rest
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go
git commit -m "feat(dashboard): add REST API endpoints for tickets, events, costs"
```

---

### Task 5: Dashboard — WebSocket Live Events

**Files:**
- Create: `internal/dashboard/ws.go`
- Create: `internal/dashboard/ws_test.go`

**Step 1: Write the failing test**

```go
// internal/dashboard/ws_test.go
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/gorilla/websocket"
)

type mockEmitter struct {
	ch chan *models.EventRecord
}

func (m *mockEmitter) Subscribe() chan *models.EventRecord {
	return m.ch
}

func (m *mockEmitter) Unsubscribe(ch chan *models.EventRecord) {}

func TestWebSocketEvents(t *testing.T) {
	ch := make(chan *models.EventRecord, 10)
	emitter := &mockEmitter{ch: ch}
	api := NewAPI(nil, emitter, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	// Send an event
	ch <- &models.EventRecord{
		ID:        "e1",
		TicketID:  "t1",
		EventType: "task_started",
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(msg), "task_started") {
		t.Errorf("expected task_started in message, got %s", string(msg))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestWebSocket -v`
Expected: FAIL — handleWebSocket not defined

**Step 3: Write minimal implementation**

```go
// internal/dashboard/ws.go
package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (a *API) handleWebSocket(w http.ResponseWriter, r *http.Request) {
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
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/ws.go internal/dashboard/ws_test.go
git commit -m "feat(dashboard): add WebSocket live event streaming"
```

---

### Task 6: Dashboard — HTTP Server with Embedded Frontend

**Files:**
- Create: `internal/dashboard/server.go`
- Create: `web/index.html`
- Create: `web/app.js`
- Create: `web/style.css`

**Step 1: Create minimal embedded frontend**

```html
<!-- web/index.html -->
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Foreman Dashboard</title>
    <link rel="stylesheet" href="/style.css">
</head>
<body>
    <header><h1>Foreman</h1><span id="status">connecting...</span></header>
    <main>
        <section id="tickets"></section>
        <section id="events"><h2>Events</h2><ul id="event-log"></ul></section>
    </main>
    <script src="/app.js"></script>
</body>
</html>
```

```css
/* web/style.css */
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: system-ui, sans-serif; background: #0d1117; color: #c9d1d9; }
header { display: flex; justify-content: space-between; align-items: center; padding: 1rem 2rem; border-bottom: 1px solid #30363d; }
header h1 { font-size: 1.2rem; color: #58a6ff; }
#status { font-size: 0.85rem; padding: 0.25rem 0.5rem; border-radius: 4px; background: #161b22; }
main { padding: 2rem; display: grid; grid-template-columns: 1fr 1fr; gap: 2rem; }
section { background: #161b22; border-radius: 8px; padding: 1rem; border: 1px solid #30363d; }
h2 { font-size: 1rem; margin-bottom: 0.5rem; color: #8b949e; }
ul { list-style: none; max-height: 60vh; overflow-y: auto; }
li { padding: 0.25rem 0; font-size: 0.85rem; border-bottom: 1px solid #21262d; }
.ticket { padding: 0.5rem; margin-bottom: 0.5rem; border-radius: 4px; background: #0d1117; }
.ticket .title { color: #58a6ff; font-weight: 600; }
.ticket .status { font-size: 0.75rem; color: #8b949e; }
```

```javascript
// web/app.js
(function() {
    const token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
    if (token) localStorage.setItem('foreman_token', token);

    const headers = { 'Authorization': 'Bearer ' + token };

    async function fetchJSON(url) {
        const res = await fetch(url, { headers });
        return res.json();
    }

    async function loadStatus() {
        const data = await fetchJSON('/api/status');
        document.getElementById('status').textContent = data.status + ' · ' + data.uptime;
    }

    async function loadTickets() {
        const tickets = await fetchJSON('/api/tickets');
        const el = document.getElementById('tickets');
        el.innerHTML = '<h2>Tickets (' + tickets.length + ')</h2>' +
            tickets.map(t => '<div class="ticket"><div class="title">' + t.Title +
                '</div><div class="status">' + t.Status + '</div></div>').join('');
    }

    function connectWS() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(proto + '//' + location.host + '/ws/events');
        const log = document.getElementById('event-log');

        ws.onmessage = function(e) {
            const evt = JSON.parse(e.data);
            const li = document.createElement('li');
            li.textContent = new Date().toLocaleTimeString() + ' ' + evt.event_type + ' [' + evt.ticket_id + ']';
            log.prepend(li);
            while (log.children.length > 200) log.removeChild(log.lastChild);
        };

        ws.onclose = function() {
            document.getElementById('status').textContent = 'disconnected';
            setTimeout(connectWS, 3000);
        };
    }

    loadStatus();
    loadTickets();
    setInterval(loadTickets, 10000);
    connectWS();
})();
```

**Step 2: Write the dashboard server**

```go
// internal/dashboard/server.go
package dashboard

import (
	"context"
	"embed"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

//go:embed web
var webFS embed.FS

// Server is the HTTP server for the Foreman dashboard.
type Server struct {
	api    *API
	db     DashboardDB
	reg    *prometheus.Registry
	server *http.Server
}

// NewServer creates a new dashboard Server and registers all HTTP routes.
func NewServer(db DashboardDB, emitter EventSubscriber, reg *prometheus.Registry, version, host string, port int) *Server {
	api := NewAPI(db, emitter, version)

	mux := http.NewServeMux()

	// Auth-protected API routes
	auth := authMiddleware(db)

	mux.Handle("/api/status", auth(http.HandlerFunc(api.handleStatus)))
	mux.Handle("/api/tickets", auth(http.HandlerFunc(api.handleListTickets)))
	mux.Handle("/api/tickets/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/tasks"):
			api.handleGetTasks(w, r)
		case strings.HasSuffix(path, "/events"):
			api.handleGetEvents(w, r)
		case strings.HasSuffix(path, "/llm-calls"):
			api.handleGetLlmCalls(w, r)
		case strings.HasSuffix(path, "/retry"):
			api.handleRetryTicket(w, r)
		default:
			api.handleGetTicket(w, r)
		}
	})))
	mux.Handle("/api/costs/today", auth(http.HandlerFunc(api.handleCostsToday)))
	mux.Handle("/api/pipeline/active", auth(http.HandlerFunc(api.handleActivePipelines)))
	mux.Handle("/api/costs/week", auth(http.HandlerFunc(api.handleCostsWeek)))
	mux.Handle("/api/costs/month", auth(http.HandlerFunc(api.handleCostsMonth)))
	mux.Handle("/api/costs/budgets", auth(http.HandlerFunc(api.handleCostsBudgets)))
	mux.Handle("/api/daemon/pause", auth(http.HandlerFunc(api.handleDaemonPause)))
	mux.Handle("/api/daemon/resume", auth(http.HandlerFunc(api.handleDaemonResume)))

	// Metrics endpoint (no auth — Prometheus scraper)
	if reg != nil {
		mux.Handle("/api/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	}

	// WebSocket (auth via token query param)
	mux.HandleFunc("/ws/events", api.handleWebSocket)

	// Static frontend files embedded at build time
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load embedded web assets")
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	return &Server{
		api: api,
		db:  db,
		reg: reg,
		server: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
}

// Start begins listening for HTTP connections. Blocks until the server stops.
func (s *Server) Start() error {
	log.Info().Str("addr", s.server.Addr).Msg("Dashboard server starting")
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
```

> **Note:** The `go:embed web` directive works because the `web/` directory is located at `internal/dashboard/web/` (relative to the `server.go` file). Task 15 (context import) is already included in the import block above.

**Step 3: Verify build**

Run: `go build ./internal/dashboard/`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/dashboard/server.go web/index.html web/app.js web/style.css
git commit -m "feat(dashboard): add HTTP server with embedded frontend and Prometheus metrics"
```

---

### Task 7: OpenAI LLM Provider

**Files:**
- Create: `internal/llm/openai.go`
- Create: `internal/llm/openai_test.go`

**Step 1: Write the failing test**

```go
// internal/llm/openai_test.go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}

		resp := map[string]interface{}{
			"id":    "chatcmpl-123",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Hello!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("test-key", srv.URL)

	resp, err := provider.Complete(context.Background(), LlmRequest{
		Model:       "gpt-4o",
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Hi",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected Hello!, got %s", resp.Content)
	}
	if resp.TokensInput != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.TokensInput)
	}
}

func TestOpenAIProvider_ProviderName(t *testing.T) {
	p := NewOpenAIProvider("key", "")
	if p.ProviderName() != "openai" {
		t.Errorf("expected openai, got %s", p.ProviderName())
	}
}

func TestOpenAIProvider_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("key", srv.URL)
	_, err := provider.Complete(context.Background(), LlmRequest{Model: "gpt-4o", UserPrompt: "hi", MaxTokens: 10})

	if _, ok := err.(*RateLimitError); !ok {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestOpenAI -v`
Expected: FAIL — NewOpenAIProvider not defined

**Step 3: Write minimal implementation**

```go
// internal/llm/openai.go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *OpenAIProvider) ProviderName() string { return "openai" }

type openaiRequest struct {
	Model       string           `json:"model"`
	Messages    []openaiMessage  `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error) {
	messages := []openaiMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, openaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openaiMessage{Role: "user", Content: req.UserPrompt})

	body := openaiRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, &ConnectionError{Attempt: 1, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode == 429 {
		retryAfter := 60
		if ra := httpResp.Header.Get("Retry-After"); ra != "" {
			if v, err := strconv.Atoi(ra); err == nil {
				retryAfter = v
			}
		}
		return nil, &RateLimitError{RetryAfterSecs: retryAfter}
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		return nil, &AuthError{Message: "invalid API key"}
	}

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("openai API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var resp openaiResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var content string
	var stopReason string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		stopReason = resp.Choices[0].FinishReason
	}

	return &LlmResponse{
		Content:      content,
		TokensInput:  resp.Usage.PromptTokens,
		TokensOutput: resp.Usage.CompletionTokens,
		Model:        resp.Model,
		DurationMs:   durationMs,
		StopReason:   stopReason,
	}, nil
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, LlmRequest{
		Model:      "gpt-4o-mini",
		UserPrompt: "ping",
		MaxTokens:  5,
	})
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestOpenAI -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/openai.go internal/llm/openai_test.go
git commit -m "feat(llm): add OpenAI provider"
```

---

### Task 8: OpenRouter LLM Provider

**Files:**
- Create: `internal/llm/openrouter.go`
- Create: `internal/llm/openrouter_test.go`

**Step 1: Write the failing test**

```go
// internal/llm/openrouter_test.go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouterProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer or-key" {
			t.Errorf("expected Bearer or-key, got %s", r.Header.Get("Authorization"))
		}

		// OpenRouter uses the same OpenAI-compatible format
		resp := map[string]interface{}{
			"id":    "gen-123",
			"model": "anthropic/claude-3.5-sonnet",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"role": "assistant", "content": "Routed!"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewOpenRouterProvider("or-key", srv.URL)
	if provider.ProviderName() != "openrouter" {
		t.Errorf("expected openrouter, got %s", provider.ProviderName())
	}

	resp, err := provider.Complete(context.Background(), LlmRequest{
		Model:      "anthropic/claude-3.5-sonnet",
		UserPrompt: "Hi",
		MaxTokens:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Routed!" {
		t.Errorf("expected Routed!, got %s", resp.Content)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestOpenRouter -v`
Expected: FAIL — NewOpenRouterProvider not defined

**Step 3: Write minimal implementation**

OpenRouter uses the OpenAI-compatible chat completions API, so we reuse the OpenAI types. The only differences are default base URL and provider name.

```go
// internal/llm/openrouter.go
package llm

// OpenRouterProvider wraps OpenAIProvider with OpenRouter defaults.
// OpenRouter's API is OpenAI-compatible (same /v1/chat/completions endpoint).
type OpenRouterProvider struct {
	inner *OpenAIProvider
}

func NewOpenRouterProvider(apiKey, baseURL string) *OpenRouterProvider {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api"
	}
	return &OpenRouterProvider{
		inner: NewOpenAIProvider(apiKey, baseURL),
	}
}

func (p *OpenRouterProvider) ProviderName() string { return "openrouter" }

func (p *OpenRouterProvider) Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error) {
	return p.inner.Complete(ctx, req)
}

func (p *OpenRouterProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}
```

> **Note:** Need to add `import "context"` at the top of the file.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestOpenRouter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/openrouter.go internal/llm/openrouter_test.go
git commit -m "feat(llm): add OpenRouter provider (OpenAI-compatible wrapper)"
```

---

### Task 9: Local/Ollama LLM Provider

**Files:**
- Create: `internal/llm/local.go`
- Create: `internal/llm/local_test.go`

**Step 1: Write the failing test**

```go
// internal/llm/local_test.go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local provider uses OpenAI-compatible format (Ollama, LM Studio, etc.)
		resp := map[string]interface{}{
			"id":    "local-1",
			"model": "llama3",
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"role": "assistant", "content": "Local reply"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := NewLocalProvider(srv.URL)
	if provider.ProviderName() != "local" {
		t.Errorf("expected local, got %s", provider.ProviderName())
	}

	resp, err := provider.Complete(context.Background(), LlmRequest{
		Model:      "llama3",
		UserPrompt: "Hi",
		MaxTokens:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Local reply" {
		t.Errorf("expected Local reply, got %s", resp.Content)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestLocalProvider -v`
Expected: FAIL — NewLocalProvider not defined

**Step 3: Write minimal implementation**

```go
// internal/llm/local.go
package llm

import "context"

// LocalProvider wraps OpenAIProvider for local OpenAI-compatible servers (Ollama, LM Studio).
type LocalProvider struct {
	inner *OpenAIProvider
}

func NewLocalProvider(baseURL string) *LocalProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	// Local servers don't require an API key
	return &LocalProvider{
		inner: NewOpenAIProvider("", baseURL),
	}
}

func (p *LocalProvider) ProviderName() string { return "local" }

func (p *LocalProvider) Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error) {
	return p.inner.Complete(ctx, req)
}

func (p *LocalProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestLocalProvider -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/local.go internal/llm/local_test.go
git commit -m "feat(llm): add local/Ollama provider (OpenAI-compatible)"
```

---

### Task 10: Docker Command Runner

**Files:**
- Create: `internal/runner/docker.go`
- Create: `internal/runner/docker_test.go`

**Step 1: Write the failing test**

```go
// internal/runner/docker_test.go
package runner

import (
	"context"
	"testing"
)

func TestDockerRunner_FormatRunArgs(t *testing.T) {
	r := &DockerRunner{
		image:       "node:22-slim",
		network:     "none",
		cpuLimit:    "2.0",
		memoryLimit: "4g",
	}

	args := r.formatRunArgs("/work", "t1")
	expected := []string{
		"run", "--rm",
		"--label", "foreman-ticket=t1",
		"--network", "none",
		"--cpus", "2.0",
		"--memory", "4g",
		"-v", "/work:/work",
		"-w", "/work",
		"node:22-slim",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, a := range expected {
		if args[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, args[i])
		}
	}
}

func TestDockerRunner_CommandExists(t *testing.T) {
	r := NewDockerRunner("node:22-slim", false, "none", "2.0", "4g", false)
	// CommandExists for Docker always returns true — commands are inside the container
	if !r.CommandExists(context.Background(), "npm") {
		t.Error("expected CommandExists to return true for Docker runner")
	}
}

// Note: Run() requires SetTicketID() to be called first for proper container labeling.
func TestDockerRunner_SetTicketID(t *testing.T) {
	r := NewDockerRunner("node:22-slim", false, "none", "2.0", "4g", false)
	r.SetTicketID("ticket-123")
	args := r.formatRunArgs("/work", r.currentTicketID)
	found := false
	for i, a := range args {
		if a == "--label" && i+1 < len(args) && args[i+1] == "foreman-ticket=ticket-123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected foreman-ticket label with ticket ID")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestDocker -v`
Expected: FAIL — DockerRunner not defined

**Step 3: Write minimal implementation**

```go
// internal/runner/docker.go
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rs/zerolog/log"
)

type DockerRunner struct {
	image            string
	persistPerTicket bool
	network          string
	cpuLimit         string
	memoryLimit      string
	autoReinstall    bool
	currentTicketID  string
}

func NewDockerRunner(image string, persistPerTicket bool, network, cpuLimit, memoryLimit string, autoReinstall bool) *DockerRunner {
	return &DockerRunner{
		image:            image,
		persistPerTicket: persistPerTicket,
		network:          network,
		cpuLimit:         cpuLimit,
		memoryLimit:      memoryLimit,
		autoReinstall:    autoReinstall,
	}
}

// SetTicketID sets the current ticket ID on the runner. One runner instance is
// created per ticket in the daemon; this allows orphan tracking via Docker labels.
func (r *DockerRunner) SetTicketID(id string) {
	r.currentTicketID = id
}

func (r *DockerRunner) formatRunArgs(workDir, ticketID string) []string {
	args := []string{"run", "--rm"}
	args = append(args, "--label", "foreman-ticket="+ticketID)
	if r.network != "" {
		args = append(args, "--network", r.network)
	}
	if r.cpuLimit != "" {
		args = append(args, "--cpus", r.cpuLimit)
	}
	if r.memoryLimit != "" {
		args = append(args, "--memory", r.memoryLimit)
	}
	args = append(args, "-v", workDir+":"+workDir, "-w", workDir)
	args = append(args, r.image)
	return args
}

func (r *DockerRunner) Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error) {
	if r.currentTicketID == "" {
		log.Warn().Msg("DockerRunner.Run called without a ticket ID set — container will not be labeled correctly")
	}

	if timeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
		defer cancel()
	}

	// Build docker run command: docker run ... image command args...
	dockerArgs := r.formatRunArgs(workDir, r.currentTicketID)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	output := &CommandOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		output.TimedOut = true
		output.ExitCode = -1
		return output, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
			return output, nil
		}
		return nil, fmt.Errorf("docker run failed: %w", err)
	}

	return output, nil
}

func (r *DockerRunner) CommandExists(_ context.Context, _ string) bool {
	// In Docker mode, we assume all commands are available inside the container.
	return true
}

// parseContainerList parses the output of `docker ps` with tab-separated
// containerID and ticketID fields and returns a map of containerID -> ticketID.
func parseContainerList(output []byte) map[string]string {
	result := make(map[string]string)
	for _, line := range bytes.Split(output, []byte("\n")) {
		parts := bytes.SplitN(line, []byte("\t"), 2)
		if len(parts) != 2 {
			continue
		}
		containerID := string(parts[0])
		ticketID := string(parts[1])
		if containerID == "" {
			continue
		}
		result[containerID] = ticketID
	}
	return result
}

// CleanupOrphanContainers removes Docker containers labeled with foreman-ticket
// that don't match any active ticket ID.
func (r *DockerRunner) CleanupOrphanContainers(ctx context.Context, activeTicketIDs map[string]bool) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "label=foreman-ticket", "--format", "{{.ID}}\t{{index .Labels \"foreman-ticket\"}}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for containerID, ticketID := range parseContainerList(out.Bytes()) {
		if !activeTicketIDs[ticketID] {
			if err := exec.CommandContext(ctx, "docker", "rm", "-f", containerID).Run(); err != nil {
				log.Warn().Err(err).Str("container_id", containerID).Msg("failed to remove orphan container")
			}
		}
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/ -run TestDocker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runner/docker.go internal/runner/docker_test.go
git commit -m "feat(runner): add Docker command runner with orphan cleanup"
```

---

### Task 11: LLM Provider Router

**Files:**
- Modify: `internal/llm/provider.go` — add `NewProviderFromConfig` factory function

**Step 1: Write the failing test**

```go
// Add to internal/llm/provider_test.go (create if needed)
package llm

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestNewProviderFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		cfg      models.LLMConfig
		wantName string
		wantErr  bool
	}{
		{
			name:     "anthropic",
			provider: "anthropic",
			cfg: models.LLMConfig{
				Anthropic: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "anthropic",
		},
		{
			name:     "openai",
			provider: "openai",
			cfg: models.LLMConfig{
				OpenAI: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "openai",
		},
		{
			name:     "openrouter",
			provider: "openrouter",
			cfg: models.LLMConfig{
				OpenRouter: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "openrouter",
		},
		{
			name:     "local",
			provider: "local",
			cfg: models.LLMConfig{
				Local: models.LLMProviderConfig{BaseURL: "http://localhost:11434"},
			},
			wantName: "local",
		},
		{
			name:     "unknown",
			provider: "unknown",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewProviderFromConfig(tt.provider, tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.ProviderName() != tt.wantName {
				t.Errorf("expected %s, got %s", tt.wantName, p.ProviderName())
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestNewProviderFromConfig -v`
Expected: FAIL — NewProviderFromConfig not defined

**Step 3: Add factory function to provider.go**

Append to `internal/llm/provider.go`:

```go
import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

func NewProviderFromConfig(providerName string, cfg models.LLMConfig) (LlmProvider, error) {
	switch providerName {
	case "anthropic":
		return NewAnthropicProvider(cfg.Anthropic.APIKey, cfg.Anthropic.BaseURL), nil
	case "openai":
		return NewOpenAIProvider(cfg.OpenAI.APIKey, cfg.OpenAI.BaseURL), nil
	case "openrouter":
		return NewOpenRouterProvider(cfg.OpenRouter.APIKey, cfg.OpenRouter.BaseURL), nil
	case "local":
		return NewLocalProvider(cfg.Local.BaseURL), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", providerName)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/provider.go internal/llm/provider_test.go
git commit -m "feat(llm): add provider factory with router for all backends"
```

---

### Task 12: CLI — `dashboard` and `token` Commands

**Files:**
- Create: `cmd/dashboard.go`
- Create: `cmd/token.go`

**Step 1: Create dashboard command**

```go
// cmd/dashboard.go
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Try loading from default config path; fall back to defaults if not found.
		cfg, err := config.LoadDefaults()
		if err != nil {
			return err
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		reg := prometheus.NewRegistry()
		_ = telemetry.NewMetrics(reg)

		emitter := telemetry.NewEventEmitter(database)

		port := dashboardPort
		if port == 0 {
			port = cfg.Dashboard.Port
		}
		if port == 0 {
			port = 8080
		}

		host := cfg.Dashboard.Host
		if host == "" {
			host = "127.0.0.1"
		}

		srv := dashboard.NewServer(database, emitter, reg, "0.1.0", host, port)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv.Shutdown(shutCtx)
		}()

		log.Info().Int("port", port).Msg("Starting dashboard")
		return srv.Start()
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 0, "Override dashboard port")
	rootCmd.AddCommand(dashboardCmd)
}
```

**Step 2: Create token command**

```go
// cmd/token.go
package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage dashboard auth tokens",
}

var tokenName string

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new dashboard auth token",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadDefaults()
		if err != nil {
			return err
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		// Generate random token
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return fmt.Errorf("failed to generate token: %w", err)
		}
		token := hex.EncodeToString(tokenBytes)

		// Store hash
		hash := sha256.Sum256([]byte(token))
		hashStr := hex.EncodeToString(hash[:])

		if err := database.CreateAuthToken(cmd.Context(), hashStr, tokenName); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}

		fmt.Printf("Token generated (save this — it won't be shown again):\n\n  %s\n\n", token)
		fmt.Printf("Name: %s\n", tokenName)
		return nil
	},
}

func init() {
	tokenGenerateCmd.Flags().StringVar(&tokenName, "name", "default", "Token name for identification")
	tokenCmd.AddCommand(tokenGenerateCmd)
	rootCmd.AddCommand(tokenCmd)
}
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/dashboard.go cmd/token.go
git commit -m "feat(cli): add dashboard and token generate commands"
```

---

### Task 13: Cross-Platform Build Setup

**Files:**
- Create: `Dockerfile`
- Modify: `Makefile` — add cross-compilation targets

**Step 1: Create Dockerfile**

```dockerfile
# Dockerfile
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /foreman .

FROM alpine:3.19
RUN apk add --no-cache git ca-certificates docker-cli
COPY --from=builder /foreman /usr/local/bin/foreman
ENTRYPOINT ["foreman"]
```

**Step 2: Add cross-compilation targets to Makefile**

Append to existing Makefile:

```makefile
# Cross-platform builds
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64

.PHONY: release
release: $(PLATFORMS)

linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-linux-amd64 .

linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 go build -o dist/foreman-linux-arm64 .

darwin-amd64:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-darwin-amd64 .

darwin-arm64:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -o dist/foreman-darwin-arm64 .

windows-amd64:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o dist/foreman-windows-amd64.exe .

.PHONY: docker
docker:
	docker build -t foreman:latest .
```

**Step 3: Verify local build works**

Run: `go build -o foreman . && ./foreman --help`
Expected: Help text prints

**Step 4: Commit**

```bash
git add Dockerfile Makefile
git commit -m "chore: add Dockerfile and cross-platform build targets"
```

---

### Task 14: Install New Dependencies

**Step 1: Install gorilla/websocket and prometheus**

```bash
cd /Users/canh/Projects/Indies/Foreman
go get github.com/gorilla/websocket@v1.5.1
go get github.com/prometheus/client_golang@v1.18.0
go mod tidy
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gorilla/websocket and prometheus dependencies"
```

> **Note:** Run this task BEFORE Tasks 1-6, since they depend on these packages.

---

### Task 15: ~~Add `context` Import to Dashboard Server~~

> **Already handled** — The `context` import is included in the Task 6 server.go implementation above. Skip this task.

---

### Task 16: Complete Dashboard REST API Endpoints

**Files:**
- Modify: `internal/dashboard/api.go`
- Modify: `internal/dashboard/api_test.go`

Adds all missing endpoints from spec §13.2 not covered in Task 4.

**Step 1: Write failing tests for missing endpoints**

```go
// Add to internal/dashboard/api_test.go

func TestAPIGetTicketTasks(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")

	req := httptest.NewRequest("GET", "/api/tickets/t1/tasks", nil)
	rec := httptest.NewRecorder()
	api.handleGetTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetCostsWeek(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")

	req := httptest.NewRequest("GET", "/api/costs/week", nil)
	rec := httptest.NewRecorder()
	api.handleCostsWeek(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetActivePipelines(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{
			{ID: "t1", Title: "Active", Status: models.TicketStatusImplementing},
		},
	}
	api := NewAPI(db, nil, "1.0.0")

	req := httptest.NewRequest("GET", "/api/pipeline/active", nil)
	rec := httptest.NewRecorder()
	api.handleActivePipelines(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

Update `mockDashboardDB` to implement the additional interface methods:

```go
func (m *mockDashboardDB) ListTasks(_ context.Context, ticketID string) ([]models.Task, error) {
	return nil, nil
}
func (m *mockDashboardDB) ListLlmCalls(_ context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	return nil, nil
}
func (m *mockDashboardDB) GetMonthlyCost(_ context.Context, yearMonth string) (float64, error) {
	return 250.0, nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run "TestAPIGetTicketTasks|TestAPIGetCostsWeek|TestAPIGetActivePipelines" -v`
Expected: FAIL — methods not defined

**Step 3: Add missing handlers to api.go**

Extend `DashboardDB` interface:

```go
type DashboardDB interface {
	AuthValidator
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	GetTicketCost(ctx context.Context, ticketID string) (float64, error)
	// New methods:
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
}
```

Add handler methods:

```go
func (a *API) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tickets/"), "/")
	if len(parts) < 2 {
		http.Error(w, "missing ticket id", http.StatusBadRequest)
		return
	}
	tasks, err := a.db.ListTasks(r.Context(), parts[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (a *API) handleGetLlmCalls(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tickets/"), "/")
	if len(parts) < 2 {
		http.Error(w, "missing ticket id", http.StatusBadRequest)
		return
	}
	calls, err := a.db.ListLlmCalls(r.Context(), parts[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, calls)
}

func (a *API) handleActivePipelines(w http.ResponseWriter, r *http.Request) {
	active := []models.TicketStatus{
		models.TicketStatusPlanning, models.TicketStatusImplementing,
		models.TicketStatusReviewing, models.TicketStatusPlanValidating,
	}
	tickets, err := a.db.ListTickets(r.Context(), models.TicketFilter{StatusIn: active})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tickets)
}

func (a *API) handleCostsWeek(w http.ResponseWriter, r *http.Request) {
	var costs []map[string]interface{}
	for i := 6; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		cost, err := a.db.GetDailyCost(r.Context(), date)
		entry := map[string]interface{}{"date": date, "cost_usd": cost}
		if err != nil {
			entry["error"] = "unavailable"
		}
		costs = append(costs, entry)
	}
	writeJSON(w, http.StatusOK, costs)
}

func (a *API) handleCostsMonth(w http.ResponseWriter, r *http.Request) {
	yearMonth := time.Now().Format("2006-01")
	cost, err := a.db.GetMonthlyCost(r.Context(), yearMonth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"month": yearMonth, "cost_usd": cost})
}

func (a *API) handleCostsBudgets(w http.ResponseWriter, r *http.Request) {
	// Returns budget status — requires config injection (add to API struct if needed)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"note": "Budget status requires config — wire during integration",
	})
}

func (a *API) handleRetryTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "retry not yet wired to pipeline state machine", http.StatusNotImplemented)
}

func (a *API) handleDaemonPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "daemon pause not yet wired", http.StatusNotImplemented)
}

func (a *API) handleDaemonResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "daemon resume not yet wired", http.StatusNotImplemented)
}
```

Also register routes in `server.go`:

```go
mux.Handle("/api/pipeline/active", auth(http.HandlerFunc(api.handleActivePipelines)))
mux.Handle("/api/costs/week", auth(http.HandlerFunc(api.handleCostsWeek)))
mux.Handle("/api/costs/month", auth(http.HandlerFunc(api.handleCostsMonth)))
mux.Handle("/api/costs/budgets", auth(http.HandlerFunc(api.handleCostsBudgets)))
mux.Handle("/api/daemon/pause", auth(http.HandlerFunc(api.handleDaemonPause)))
mux.Handle("/api/daemon/resume", auth(http.HandlerFunc(api.handleDaemonResume)))
```

And update the ticket sub-route handler to dispatch tasks, events, llm-calls, retry, cancel:

```go
mux.Handle("/api/tickets/", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/tasks"):
		api.handleGetTasks(w, r)
	case strings.HasSuffix(path, "/events"):
		api.handleGetEvents(w, r)
	case strings.HasSuffix(path, "/llm-calls"):
		api.handleGetLlmCalls(w, r)
	case strings.HasSuffix(path, "/retry"):
		api.handleRetryTicket(w, r)
	default:
		api.handleGetTicket(w, r)
	}
})))
```

**Step 4: Run tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/api_test.go internal/dashboard/server.go
git commit -m "feat(dashboard): add remaining API endpoints (tasks, llm-calls, costs, pipeline, daemon controls)"
```

---

### Task 17: Add Missing Prometheus Metrics

**Files:**
- Modify: `internal/telemetry/metrics.go`
- Modify: `internal/telemetry/metrics_test.go`

Add the 6 missing counters from spec §18.

**Step 1: Write the failing test**

```go
// Add to metrics_test.go
func TestMetrics_AllCountersRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	_ = m

	families, _ := reg.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	required := []string{
		"foreman_clarification_timeouts_total",
		"foreman_file_reservation_conflicts_total",
		"foreman_search_block_fuzzy_matches_total",
		"foreman_search_block_misses_total",
		"foreman_provider_outages_total",
		"foreman_crash_recoveries_total",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing metric: %s", name)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/ -run TestMetrics_AllCountersRegistered -v`
Expected: FAIL — missing metrics

**Step 3: Add missing counters to Metrics struct and NewMetrics**

```go
// Add to Metrics struct:
ClarificationTimeouts     prometheus.Counter
FileReservationConflicts  prometheus.Counter
SearchBlockFuzzyMatches   prometheus.Counter
SearchBlockMisses         prometheus.Counter
ProviderOutages           *prometheus.CounterVec
CrashRecoveries           prometheus.Counter
```

```go
// Add to NewMetrics():
ClarificationTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_clarification_timeouts_total",
	Help: "Total clarification timeouts",
}),
FileReservationConflicts: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_file_reservation_conflicts_total",
	Help: "Total file reservation conflicts",
}),
SearchBlockFuzzyMatches: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_search_block_fuzzy_matches_total",
	Help: "Total fuzzy matches in SEARCH blocks",
}),
SearchBlockMisses: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_search_block_misses_total",
	Help: "Total SEARCH block misses",
}),
ProviderOutages: prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "foreman_provider_outages_total",
	Help: "Total provider outages by provider",
}, []string{"provider"}),
CrashRecoveries: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_crash_recoveries_total",
	Help: "Total crash recoveries",
}),
```

Add them to the `MustRegister` call.

**Step 4: Run tests**

Run: `go test ./internal/telemetry/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/metrics.go internal/telemetry/metrics_test.go
git commit -m "feat(telemetry): add remaining Prometheus counters from spec §18"
```

---

### Task 18: Wire Docker Orphan Cleanup to Daemon Startup

**Files:**
- Modify: `internal/daemon/daemon.go`

**Step 1: Add cleanup call to daemon startup**

In the daemon `Start()` function (Task 7), add at the beginning:

```go
// On startup, clean up orphaned Docker containers from previous crashes.
d.mu.Lock()
database := d.db
runnerMode := d.config.RunnerMode
d.mu.Unlock()

if database != nil && runnerMode == "docker" {
	activeTickets, err := database.ListTickets(ctx, models.TicketFilter{
		StatusIn: []models.TicketStatus{
			models.TicketStatusPlanning,
			models.TicketStatusPlanValidating,
			models.TicketStatusImplementing,
			models.TicketStatusReviewing,
		},
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list active tickets for Docker orphan cleanup")
	} else {
		activeIDs := make(map[string]bool, len(activeTickets))
		for _, t := range activeTickets {
			activeIDs[t.ID] = true
		}
		dockerRunner := runner.NewDockerRunner("", false, "", "", "", false)
		if err := dockerRunner.CleanupOrphanContainers(ctx, activeIDs); err != nil {
			log.Warn().Err(err).Msg("Failed to cleanup orphan containers on startup")
		}
	}
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat(daemon): wire Docker orphan cleanup to daemon startup"
```

---

### Task 19: Add DB Methods for Dashboard (ListTasks, ListLlmCalls, GetMonthlyCost)

**Files:**
- Modify: `internal/db/db.go` — add methods to interface
- Modify: `internal/db/sqlite.go` — implement
- Modify: `internal/db/sqlite_test.go` — add tests

**Step 1: Write the failing test**

```go
// Add to sqlite_test.go
func TestSQLiteDB_GetMonthlyCost(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	db.RecordDailyCost(context.Background(), "2026-03-01", 10.0)
	db.RecordDailyCost(context.Background(), "2026-03-02", 5.0)

	cost, err := db.GetMonthlyCost(context.Background(), "2026-03")
	require.NoError(t, err)
	assert.InDelta(t, 15.0, cost, 0.01)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestSQLiteDB_GetMonthlyCost -v`
Expected: FAIL — GetMonthlyCost not defined

**Step 3: Add methods**

Add to `db.Database` interface:

```go
GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
```

Implement in sqlite.go:

```go
func (s *SQLiteDB) GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_usd), 0) FROM cost_daily WHERE date LIKE ?`,
		yearMonth+"%",
	).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (s *SQLiteDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, sequence, title, description, status, created_at FROM tasks WHERE ticket_id=? ORDER BY sequence`,
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		rows.Scan(&t.ID, &t.TicketID, &t.Sequence, &t.Title, &t.Description, &t.Status, &t.CreatedAt)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (s *SQLiteDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt, tokens_input, tokens_output, cost_usd, duration_ms, status, created_at
		 FROM llm_calls WHERE ticket_id=? ORDER BY created_at DESC`,
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var calls []models.LlmCallRecord
	for rows.Next() {
		var c models.LlmCallRecord
		rows.Scan(&c.ID, &c.TicketID, &c.TaskID, &c.Role, &c.Provider, &c.Model, &c.Attempt,
			&c.TokensInput, &c.TokensOutput, &c.CostUSD, &c.DurationMs, &c.Status, &c.CreatedAt)
		calls = append(calls, c)
	}
	return calls, nil
}
```

Also implement in `postgres.go` (Task 14):

```go
func (p *PostgresDB) GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error) {
	var cost float64
	err := p.db.GetContext(ctx, &cost,
		`SELECT COALESCE(SUM(total_usd), 0) FROM cost_daily WHERE date LIKE $1`,
		yearMonth+"%")
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (p *PostgresDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	var tasks []models.Task
	err := p.db.SelectContext(ctx, &tasks,
		`SELECT id, ticket_id, sequence, title, description, status, created_at FROM tasks WHERE ticket_id=$1 ORDER BY sequence`, ticketID)
	return tasks, err
}

func (p *PostgresDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	var calls []models.LlmCallRecord
	err := p.db.SelectContext(ctx, &calls,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt, tokens_input, tokens_output, cost_usd, duration_ms, status, created_at
		 FROM llm_calls WHERE ticket_id=$1 ORDER BY created_at DESC`, ticketID)
	return calls, err
}
```

**Step 4: Run tests**

Run: `go test ./internal/db/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/db.go internal/db/sqlite.go internal/db/sqlite_test.go internal/db/postgres.go
git commit -m "feat(db): add ListTasks, ListLlmCalls, GetMonthlyCost methods"
```
