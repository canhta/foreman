package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/command"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TaskContextStatsDB is a local alias for test readability.
type TaskContextStatsDB = TaskContextStats

type mockDashboardDB struct {
	tickets           []models.Ticket
	events            []models.EventRecord
	teamStats         []models.TeamStat
	summaries         []models.TicketSummary
	contextStats      map[string]TaskContextStatsDB
	savedDAGState     *db.DAGState
	taskStatusUpdates []struct {
		id     string
		status models.TaskStatus
	}
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

func (m *mockDashboardDB) ListTasks(_ context.Context, ticketID string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockDashboardDB) ListLlmCalls(_ context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	return nil, nil
}

func (m *mockDashboardDB) GetMonthlyCost(_ context.Context, yearMonth string) (float64, error) {
	return 250.0, nil
}

func (m *mockDashboardDB) UpdateTaskStatus(_ context.Context, id string, status models.TaskStatus) error {
	m.taskStatusUpdates = append(m.taskStatusUpdates, struct {
		id     string
		status models.TaskStatus
	}{id, status})
	return nil
}

func (m *mockDashboardDB) SaveDAGState(_ context.Context, _ string, state db.DAGState) error {
	m.savedDAGState = &state
	return nil
}

func (m *mockDashboardDB) UpdateTicketStatus(_ context.Context, _ string, _ models.TicketStatus) error {
	return nil
}

func (m *mockDashboardDB) DeleteTicket(_ context.Context, _ string) error { return nil }

func (m *mockDashboardDB) AppendTicketDescription(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockDashboardDB) GetTaskContextStats(_ context.Context, taskID string) (TaskContextStats, error) {
	if m.contextStats != nil {
		if s, ok := m.contextStats[taskID]; ok {
			return s, nil
		}
	}
	return TaskContextStats{}, nil
}

func (m *mockDashboardDB) UpdateTaskContextStats(_ context.Context, _ string, _ TaskContextStats) error {
	return nil
}

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

func (m *mockDashboardDB) GetLlmCallAggregates(_ context.Context, _ time.Time) ([]db.RunnerAggregate, []db.ModelAggregate, []db.RoleAggregate, error) {
	return nil, nil, nil, nil
}

func (m *mockDashboardDB) GetRecentLlmCalls(_ context.Context, _ int) ([]db.RecentLlmCall, error) {
	return nil, nil
}

func (m *mockDashboardDB) CreateChatMessage(_ context.Context, _ *models.ChatMessage) error {
	return nil
}

func (m *mockDashboardDB) GetChatMessages(_ context.Context, _ string, _ int) ([]models.ChatMessage, error) {
	return nil, nil
}

// mockDaemonStatus implements DaemonStatusProvider for tests.
type mockDaemonStatus struct {
	running bool
	paused  bool
}

func (m *mockDaemonStatus) IsRunning() bool { return m.running }
func (m *mockDaemonStatus) IsPaused() bool  { return m.paused }

// mockInvalidAuthDB always rejects auth token validation.
type mockInvalidAuthDB struct {
	mockDashboardDB
}

func (m *mockInvalidAuthDB) ValidateAuthToken(_ context.Context, _ string) (bool, error) {
	return false, nil
}

type emittedEvent struct {
	metadata  map[string]string
	ticketID  string
	taskID    string
	eventType string
	severity  string
	message   string
}

type mockRetryEventEmitter struct {
	emitted []emittedEvent
}

func (m *mockRetryEventEmitter) Subscribe() chan *models.EventRecord {
	return make(chan *models.EventRecord)
}

func (m *mockRetryEventEmitter) Unsubscribe(_ chan *models.EventRecord) {}

func (m *mockRetryEventEmitter) Emit(_ context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string) {
	m.emitted = append(m.emitted, emittedEvent{
		ticketID:  ticketID,
		taskID:    taskID,
		eventType: eventType,
		severity:  severity,
		message:   message,
		metadata:  metadata,
	})
}

func TestAPIGetStatus(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
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
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets", nil)
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

func TestAPIGetTicket(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{{ID: "t1", Title: "Test", Status: models.TicketStatusImplementing}},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleGetTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketNotFound(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/nonexistent", nil)
	rec := httptest.NewRecorder()
	api.handleGetTicket(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAPIGetEvents(t *testing.T) {
	db := &mockDashboardDB{
		events: []models.EventRecord{
			{ID: "e1", TicketID: "t1", EventType: "task_started"},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1/events", nil)
	rec := httptest.NewRecorder()
	api.handleGetEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPICostsToday(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/costs/today", nil)
	rec := httptest.NewRecorder()
	api.handleCostsToday(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketTasks(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1/tasks", nil)
	rec := httptest.NewRecorder()
	api.handleGetTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetCostsWeek(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/costs/week", nil)
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
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/pipeline/active", nil)
	rec := httptest.NewRecorder()
	api.handleActivePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetStatus_DaemonRunning(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true, paused: false}, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
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

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "paused" {
		t.Errorf("expected daemon_state=paused, got %v", resp["daemon_state"])
	}
}

func TestAPIGetStatus_NilProvider(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "stopped" {
		t.Errorf("expected daemon_state=stopped, got %v", resp["daemon_state"])
	}
}

func TestAPIGetStatus_DaemonStopped(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, &mockDaemonStatus{running: false, paused: false}, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["daemon_state"] != "stopped" {
		t.Errorf("expected daemon_state=stopped, got %v", resp["daemon_state"])
	}
}

func TestAPIGetLlmCalls(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1/llm-calls", nil)
	rec := httptest.NewRecorder()
	api.handleGetLlmCalls(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPICostsMonth(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/costs/month", nil)
	rec := httptest.NewRecorder()
	api.handleCostsMonth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["month"]; !ok {
		t.Errorf("expected 'month' key in response, got %v", resp)
	}
	if _, ok := resp["cost_usd"]; !ok {
		t.Errorf("expected 'cost_usd' key in response, got %v", resp)
	}
}

func TestAPIRetryTicket_NoRetrier(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestAPIDaemonPause_NoController(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/pause", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonPause(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestAPIDaemonResume_NoController(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/resume", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonResume(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

type mockChannelHealth struct {
	connected bool
}

func (m *mockChannelHealth) IsConnected() bool { return m.connected }

func TestAPIGetStatus_WithChannelHealth(t *testing.T) {
	db := &mockDashboardDB{}
	ch := &mockChannelHealth{connected: true}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true}, models.CostConfig{}, "1.0.0")
	api.SetChannelHealth("whatsapp", ch)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
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

func TestAPIGetTeamStats(t *testing.T) {
	db := &mockDashboardDB{
		teamStats: []models.TeamStat{
			{ChannelSenderID: "84123@s.whatsapp.net", TicketCount: 5, CostUSD: 10.0, FailedCount: 1},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/stats/team", nil)
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
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/stats/recent-prs", nil)
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
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/ticket-summaries", nil)
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
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/events", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

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

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/pause", nil)
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

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/resume", nil)
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

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if retrier.retriedID != "t1" {
		t.Errorf("expected retriedID=t1, got %s", retrier.retriedID)
	}
}

func TestAPIRetryTicket_EmitsActivityEvent(t *testing.T) {
	retrier := &mockTicketRetrier{}
	emitter := &mockRetryEventEmitter{}
	api := NewAPI(&mockDashboardDB{}, emitter, nil, models.CostConfig{}, "1.0.0")
	api.SetTicketRetrier(retrier)

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(emitter.emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(emitter.emitted))
	}
	evt := emitter.emitted[0]
	if evt.ticketID != "t1" {
		t.Errorf("expected ticketID=t1, got %s", evt.ticketID)
	}
	if evt.eventType != "ticket_retried" {
		t.Errorf("expected eventType=ticket_retried, got %s", evt.eventType)
	}
	if evt.severity != "info" {
		t.Errorf("expected severity=info, got %s", evt.severity)
	}
	if evt.message != "Retry requested from dashboard" {
		t.Errorf("unexpected message: %s", evt.message)
	}
}

func TestAPIRetryTask(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tasks/task-1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTaskContext_ReturnsUtilization(t *testing.T) {
	db := &mockDashboardDB{
		contextStats: map[string]TaskContextStatsDB{
			"task-123": {Budget: 100000, Used: 75000, FilesSelected: 12, FilesTouched: 8, CacheHits: 3},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tasks/task-123/context", nil)
	rec := httptest.NewRecorder()
	api.handleTaskContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TaskContextResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Budget != 100000 {
		t.Errorf("expected budget=100000, got %d", resp.Budget)
	}
	if resp.Used != 75000 {
		t.Errorf("expected used=75000, got %d", resp.Used)
	}
	if resp.UtilizationPct != 75.0 {
		t.Errorf("expected utilization_pct=75.0, got %f", resp.UtilizationPct)
	}
	if resp.FilesSelected != 12 {
		t.Errorf("expected files_selected=12, got %d", resp.FilesSelected)
	}
	if resp.FilesTouched != 8 {
		t.Errorf("expected files_touched=8, got %d", resp.FilesTouched)
	}
	if resp.CacheHits != 3 {
		t.Errorf("expected cache_hits=3, got %d", resp.CacheHits)
	}
}

func TestHandleTaskContext_ZeroBudget_NoDiv(t *testing.T) {
	db := &mockDashboardDB{
		contextStats: map[string]TaskContextStatsDB{
			"task-456": {Budget: 0, Used: 0},
		},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tasks/task-456/context", nil)
	rec := httptest.NewRecorder()
	api.handleTaskContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TaskContextResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.UtilizationPct != 0.0 {
		t.Errorf("expected utilization_pct=0.0 for zero budget, got %f", resp.UtilizationPct)
	}
}

func TestHandleTaskContext_MethodNotAllowed(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tasks/task-123/context", nil)
	rec := httptest.NewRecorder()
	api.handleTaskContext(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAPIHandleCostsBudgets(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{
		MaxCostPerDayUSD:  150.0,
		AlertThresholdPct: 80,
	}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/costs/budgets", nil)
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

// mockMCPHealthProvider implements MCPHealthProvider for tests.
type mockMCPHealthProvider struct {
	status map[string]bool
}

func (m *mockMCPHealthProvider) HealthStatus() map[string]bool { return m.status }

func TestAPIGetStatus_WithMCPHealth(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, &mockDaemonStatus{running: true}, models.CostConfig{}, "1.0.0")
	api.SetMCPHealthProvider(&mockMCPHealthProvider{
		status: map[string]bool{
			"filesystem": true,
			"github":     false,
		},
	})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	mcpServers, ok := resp["mcp_servers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mcp_servers key in response")
	}

	fs, ok := mcpServers["filesystem"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filesystem key in mcp_servers")
	}
	if fs["healthy"] != true {
		t.Errorf("expected filesystem healthy=true, got %v", fs["healthy"])
	}

	gh, ok := mcpServers["github"].(map[string]interface{})
	if !ok {
		t.Fatal("expected github key in mcp_servers")
	}
	if gh["healthy"] != false {
		t.Errorf("expected github healthy=false, got %v", gh["healthy"])
	}
}

func TestAPIGetStatus_WithoutMCPHealth(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	// No MCP health provider — mcp_servers key should be absent

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["mcp_servers"]; ok {
		t.Error("expected no mcp_servers key when no MCPHealthProvider registered")
	}
}

// mockPromptQuerier is a test double for PromptSnapshotQuerier.
type mockPromptQuerier struct {
	err       error
	snapshots []db.PromptSnapshot
}

func (m *mockPromptQuerier) GetPromptSnapshots(_ context.Context) ([]db.PromptSnapshot, error) {
	return m.snapshots, m.err
}

func TestAPIPromptVersions_NoQuerier(t *testing.T) {
	dbm := &mockDashboardDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	// No SetPromptSnapshotQuerier called — should return empty array.

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/prompts/versions", nil)
	rec := httptest.NewRecorder()
	api.handlePromptVersions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %v", result)
	}
}

func TestAPIPromptVersions_WithSnapshots(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	snapshots := []db.PromptSnapshot{
		{ID: "snap1", TemplateName: "implementer.md.j2", SHA256: "abc123", RecordedAt: now},
		{ID: "snap2", TemplateName: "planner.md.j2", SHA256: "def456", RecordedAt: now},
	}
	dbm := &mockDashboardDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetPromptSnapshotQuerier(&mockPromptQuerier{snapshots: snapshots})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/prompts/versions", nil)
	rec := httptest.NewRecorder()
	api.handlePromptVersions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(result))
	}
}

func TestAPIPromptVersions_MethodNotAllowed(t *testing.T) {
	dbm := &mockDashboardDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/prompts/versions", nil)
	rec := httptest.NewRecorder()
	api.handlePromptVersions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- handleDeleteTicket ---

func TestAPIDeleteTicket_Success(t *testing.T) {
	dbm := &mockDashboardDB{
		tickets: []models.Ticket{{ID: "t1", Title: "Test"}},
	}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "DELETE", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleDeleteTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", resp["status"])
	}
	if resp["ticket_id"] != "t1" {
		t.Errorf("expected ticket_id=t1, got %v", resp["ticket_id"])
	}
}

func TestAPIDeleteTicket_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleDeleteTicket(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAPIDeleteTicket_MissingID(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "DELETE", "/api/tickets/", nil)
	rec := httptest.NewRecorder()
	api.handleDeleteTicket(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAPIDeleteTicket_DBError(t *testing.T) {
	dbm := &mockErrorDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "DELETE", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleDeleteTicket(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// mockErrorDB extends mockDashboardDB but returns errors from write operations.
type mockErrorDB struct {
	mockDashboardDB
}

func (m *mockErrorDB) DeleteTicket(_ context.Context, _ string) error {
	return fmt.Errorf("db error")
}

func (m *mockErrorDB) AppendTicketDescription(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("db error")
}

func (m *mockErrorDB) UpdateTicketStatus(_ context.Context, _ string, _ models.TicketStatus) error {
	return fmt.Errorf("db error")
}

func (m *mockErrorDB) UpdateTaskStatus(_ context.Context, _ string, _ models.TaskStatus) error {
	return fmt.Errorf("db error")
}

func (m *mockErrorDB) GetTaskContextStats(_ context.Context, _ string) (TaskContextStats, error) {
	return TaskContextStats{}, db.ErrNotFound
}

// --- handleListTickets status filter ---

func TestAPIListTickets_InvalidStatus(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets?status=notavalidstatus", nil)
	rec := httptest.NewRecorder()
	api.handleListTickets(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status filter, got %d", rec.Code)
	}
}

func TestAPIListTickets_ValidStatusFilter(t *testing.T) {
	dbm := &mockDashboardDB{
		tickets: []models.Ticket{
			{ID: "t1", Status: models.TicketStatusFailed},
		},
	}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets?status=failed", nil)
	rec := httptest.NewRecorder()
	api.handleListTickets(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- handleGetTicket missing ID ---

func TestAPIGetTicket_MissingID(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/", nil)
	rec := httptest.NewRecorder()
	api.handleGetTicket(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- handleRetryTicket method check ---

func TestAPIRetryTicket_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- handleRetryTask method/id checks ---

func TestAPIRetryTask_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tasks/task-1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAPIRetryTask_MissingID(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	// Path trims "/api/tasks/" then "/retry" — empty string remains.
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tasks//retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAPIRetryTask_DBError(t *testing.T) {
	api := NewAPI(&mockErrorDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tasks/task-1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// --- handleDaemonPause / handleDaemonResume method checks ---

func TestAPIDaemonPause_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/daemon/pause", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonPause(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAPIDaemonResume_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/daemon/resume", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonResume(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// --- handleTaskContext db.ErrNotFound ---

func TestHandleTaskContext_NotFound(t *testing.T) {
	api := NewAPI(&mockErrorDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tasks/nonexistent/context", nil)
	rec := httptest.NewRecorder()
	api.handleTaskContext(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when task not found, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleGlobalEvents pagination ---

// mockGlobalEventsDB records the limit/offset values passed to GetGlobalEvents.
type mockGlobalEventsDB struct {
	mockDashboardDB
	lastLimit  int
	lastOffset int
}

func (m *mockGlobalEventsDB) GetGlobalEvents(_ context.Context, limit, offset int) ([]models.EventRecord, error) {
	m.lastLimit = limit
	m.lastOffset = offset
	return m.events, nil
}

func TestAPIGetGlobalEvents_DefaultPagination(t *testing.T) {
	dbm := &mockGlobalEventsDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/events", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if dbm.lastLimit != 50 {
		t.Errorf("expected default limit=50, got %d", dbm.lastLimit)
	}
	if dbm.lastOffset != 0 {
		t.Errorf("expected default offset=0, got %d", dbm.lastOffset)
	}
}

func TestAPIGetGlobalEvents_CustomPagination(t *testing.T) {
	dbm := &mockGlobalEventsDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/events?limit=25&offset=10", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if dbm.lastLimit != 25 {
		t.Errorf("expected limit=25, got %d", dbm.lastLimit)
	}
	if dbm.lastOffset != 10 {
		t.Errorf("expected offset=10, got %d", dbm.lastOffset)
	}
}

func TestAPIGetGlobalEvents_LimitCappedAt100(t *testing.T) {
	dbm := &mockGlobalEventsDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/events?limit=200", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// 200 exceeds the max (100), so the default (50) is kept.
	if dbm.lastLimit != 50 {
		t.Errorf("expected limit capped to default=50 for out-of-range value, got %d", dbm.lastLimit)
	}
}

func TestAPIGetGlobalEvents_InvalidLimitIgnored(t *testing.T) {
	dbm := &mockGlobalEventsDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/events?limit=notanumber", nil)
	rec := httptest.NewRecorder()
	api.handleGlobalEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Non-numeric limit is ignored; default is used.
	if dbm.lastLimit != 50 {
		t.Errorf("expected default limit=50 for non-numeric input, got %d", dbm.lastLimit)
	}
}

// --- handlePromptVersions querier error ---

func TestAPIPromptVersions_QuerierError(t *testing.T) {
	dbm := &mockDashboardDB{}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetPromptSnapshotQuerier(&mockPromptQuerier{err: fmt.Errorf("db connection lost")})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/prompts/versions", nil)
	rec := httptest.NewRecorder()
	api.handlePromptVersions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on querier error, got %d", rec.Code)
	}
}

// --- SetChannelHealth multiple channels ---

func TestAPISetChannelHealth_MultipleChannels(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, &mockDaemonStatus{running: true}, models.CostConfig{}, "1.0.0")
	api.SetChannelHealth("whatsapp", &mockChannelHealth{connected: true})
	api.SetChannelHealth("telegram", &mockChannelHealth{connected: false})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	api.handleStatus(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	channels, ok := resp["channels"].(map[string]interface{})
	if !ok {
		t.Fatal("expected channels key in response")
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}
	wa := channels["whatsapp"].(map[string]interface{})
	if wa["connected"] != true {
		t.Errorf("expected whatsapp connected=true")
	}
	tg := channels["telegram"].(map[string]interface{})
	if tg["connected"] != false {
		t.Errorf("expected telegram connected=false")
	}
}

// --- handleReplyToTicket ---

func TestAPIReplyToTicket_Success(t *testing.T) {
	dbm := &mockDashboardDB{
		tickets: []models.Ticket{
			{ID: "t1", Title: "Test", Status: models.TicketStatusClarificationNeeded},
		},
	}
	api := NewAPI(dbm, nil, nil, models.CostConfig{}, "1.0.0")
	body := strings.NewReader(`{"message":"here is the clarification"}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/t1/reply", body)
	rec := httptest.NewRecorder()
	api.handleReplyToTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "queued" {
		t.Errorf("expected status=queued, got %v", resp["status"])
	}
}

func TestAPIReplyToTicket_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/tickets/t1/reply", nil)
	rec := httptest.NewRecorder()
	api.handleReplyToTicket(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAPIReplyToTicket_EmptyMessage(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	body := strings.NewReader(`{"message":""}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/t1/reply", body)
	rec := httptest.NewRecorder()
	api.handleReplyToTicket(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAPIReplyToTicket_TicketNotFound(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	body := strings.NewReader(`{"message":"reply text"}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets/nonexistent/reply", body)
	rec := httptest.NewRecorder()
	api.handleReplyToTicket(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// retryTestDB wraps mockDashboardDB to override ListTasks and track ticket status.
type retryTestDB struct {
	*mockDashboardDB
	ticketStatus models.TicketStatus
	tasks        []models.Task
}

func (r *retryTestDB) ListTasks(_ context.Context, _ string) ([]models.Task, error) {
	return r.tasks, nil
}

func (r *retryTestDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	r.ticketStatus = status
	return r.mockDashboardDB.UpdateTicketStatus(ctx, id, status)
}

// --- handleDaemonSync ---

// mockTrackerSyncer records whether TriggerSync was called.
type mockTrackerSyncer struct {
	triggered bool
}

func (m *mockTrackerSyncer) TriggerSync() { m.triggered = true }

func TestAPIDaemonSync_NoSyncer(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/sync", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonSync(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when no syncer wired, got %d", rec.Code)
	}
}

func TestAPIDaemonSync_MethodNotAllowed(t *testing.T) {
	syncer := &mockTrackerSyncer{}
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetTrackerSyncer(syncer)
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/daemon/sync", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonSync(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", rec.Code)
	}
	if syncer.triggered {
		t.Error("expected TriggerSync not to be called on wrong method")
	}
}

func TestAPIDaemonSync_Triggers(t *testing.T) {
	syncer := &mockTrackerSyncer{}
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetTrackerSyncer(syncer)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/daemon/sync", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonSync(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	if !syncer.triggered {
		t.Error("expected TriggerSync to be called")
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "sync triggered" {
		t.Errorf("expected status=sync triggered, got %v", resp["status"])
	}
}

func TestSmartRetrier_ResetsTasksAndSavesDagState(t *testing.T) {
	tasks := []models.Task{
		{ID: "t-done", TicketID: "ticket-1", Status: models.TaskStatusDone},
		{ID: "t-failed", TicketID: "ticket-1", Status: models.TaskStatusFailed},
		{ID: "t-skip1", TicketID: "ticket-1", Status: models.TaskStatusSkipped},
		{ID: "t-skip2", TicketID: "ticket-1", Status: models.TaskStatusSkipped},
	}

	mdb := &mockDashboardDB{}
	retryDB := &retryTestDB{mockDashboardDB: mdb, tasks: tasks}
	retrier := &smartRetrier{db: retryDB}

	err := retrier.RetryTicket(context.Background(), "ticket-1")
	require.NoError(t, err)

	// dag_state saved with only the done task ID.
	require.NotNil(t, mdb.savedDAGState)
	assert.Equal(t, []string{"t-done"}, mdb.savedDAGState.CompletedTasks)

	// failed and skipped tasks reset to pending.
	pendingIDs := map[string]bool{}
	for _, u := range mdb.taskStatusUpdates {
		if u.status == models.TaskStatusPending {
			pendingIDs[u.id] = true
		}
	}
	assert.True(t, pendingIDs["t-failed"], "t-failed should be reset to pending")
	assert.True(t, pendingIDs["t-skip1"], "t-skip1 should be reset to pending")
	assert.True(t, pendingIDs["t-skip2"], "t-skip2 should be reset to pending")
	assert.False(t, pendingIDs["t-done"], "t-done should NOT be reset")

	// ticket re-queued.
	assert.Equal(t, models.TicketStatusQueued, retryDB.ticketStatus)
}

// mockConfigProvider implements ConfigProvider for tests.
type mockConfigProvider struct {
	cfg *models.Config
}

func (m *mockConfigProvider) GetConfig() *models.Config { return m.cfg }

func TestHandleConfigSummary_NilProvider_Returns503(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	// No SetConfigProvider called — configProvider is nil.

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/config/summary", nil)
	rec := httptest.NewRecorder()
	api.handleConfigSummary(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when configProvider is nil, got %d", rec.Code)
	}
}

func TestHandleConfigSummary_ValidConfig_Returns200WithRedactedKey(t *testing.T) {
	cfg := &models.Config{
		LLM: models.LLMConfig{
			DefaultProvider: "anthropic",
			Anthropic:       models.LLMProviderConfig{APIKey: "sk-ant-api03-longkeyvalue"},
		},
		Git: models.GitConfig{
			Provider:     "github",
			CloneURL:     "https://github.com/org/repo",
			BranchPrefix: "foreman/",
		},
		AgentRunner: models.AgentRunnerConfig{
			Provider:        "builtin",
			MaxTurnsDefault: 10,
		},
	}
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetConfigProvider(&mockConfigProvider{cfg: cfg})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/config/summary", nil)
	rec := httptest.NewRecorder()
	api.handleConfigSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	llm, ok := resp["llm"].(map[string]interface{})
	if !ok {
		t.Fatal("expected llm key in response")
	}
	apiKey, ok := llm["api_key"].(string)
	if !ok {
		t.Fatal("expected api_key in llm section")
	}
	if !strings.Contains(apiKey, "...") {
		t.Errorf("expected api_key to be redacted (contain '...'), got %q", apiKey)
	}
}

func TestHandleConfigSummary_ShortAPIKey_ShowsRedacted(t *testing.T) {
	cfg := &models.Config{
		LLM: models.LLMConfig{
			DefaultProvider: "openai",
			OpenAI:          models.LLMProviderConfig{APIKey: "short"},
		},
	}
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetConfigProvider(&mockConfigProvider{cfg: cfg})

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/config/summary", nil)
	rec := httptest.NewRecorder()
	api.handleConfigSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	llm := resp["llm"].(map[string]interface{})
	apiKey := llm["api_key"].(string)
	if apiKey != "****" {
		t.Errorf("expected api_key='****' for short key, got %q", apiKey)
	}
}

func TestHandleActivityBreakdown_ValidJSONStructure(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/usage/activity", nil)
	rec := httptest.NewRecorder()
	api.handleActivityBreakdown(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp["by_runner"]; !ok {
		t.Error("expected by_runner key in response")
	}
	if _, ok := resp["by_model"]; !ok {
		t.Error("expected by_model key in response")
	}
	if _, ok := resp["by_role"]; !ok {
		t.Error("expected by_role key in response")
	}
	if _, ok := resp["recent_calls"]; !ok {
		t.Error("expected recent_calls key in response")
	}
}

func TestAPIListCommands_Empty(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	// No registry wired — should return empty list.
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/commands", nil)
	rec := httptest.NewRecorder()
	api.handleListCommands(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var items []interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&items))
	assert.Len(t, items, 0)
}

func TestAPIListCommands_WithRegistry(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetCommandRegistry(newTestCommandRegistry())

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/commands", nil)
	rec := httptest.NewRecorder()
	api.handleListCommands(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var items []map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&items))
	require.Len(t, items, 2)
	// Commands are sorted by name — "explain" before "review".
	assert.Equal(t, "explain", items[0]["name"])
	assert.Equal(t, "review", items[1]["name"])
}

func TestAPIRenderCommand_Success(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetCommandRegistry(newTestCommandRegistry())

	body := strings.NewReader(`{"args":["some diff text"]}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/commands/review", body)
	rec := httptest.NewRecorder()
	api.handleRenderCommand(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	rendered, ok := resp["rendered"]
	require.True(t, ok, "expected 'rendered' key in response")
	assert.Contains(t, rendered, "some diff text")
}

func TestAPIRenderCommand_NotFound(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetCommandRegistry(newTestCommandRegistry())

	body := strings.NewReader(`{"args":[]}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/commands/nonexistent", body)
	rec := httptest.NewRecorder()
	api.handleRenderCommand(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAPIRenderCommand_NoRegistry(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	// No registry wired.
	body := strings.NewReader(`{"args":[]}`)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/commands/review", body)
	rec := httptest.NewRecorder()
	api.handleRenderCommand(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAPIRenderCommand_MethodNotAllowed(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	api.SetCommandRegistry(newTestCommandRegistry())

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/commands/review", nil)
	rec := httptest.NewRecorder()
	api.handleRenderCommand(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// newTestCommandRegistry returns a *command.Registry pre-loaded with two test commands.
func newTestCommandRegistry() *command.Registry {
	r := command.NewRegistry()
	r.Register(command.Command{
		Name:        "review",
		Description: "Review changes",
		Template:    "Review the following diff:\n$ARGUMENTS",
		Source:      "builtin",
	})
	r.Register(command.Command{
		Name:        "explain",
		Description: "Explain code",
		Template:    "Explain the following:\n$ARGUMENTS",
		Source:      "builtin",
	})
	return r
}

// ── flattenProjectConfig / expandProjectConfigDTO ──────────────────────────────

func TestFlattenProjectConfig_JiraTracker(t *testing.T) {
	cfg := &project.ProjectConfig{}
	cfg.Project.Name = "My Project"
	cfg.Project.Description = "desc"
	cfg.Git.CloneURL = "git@github.com:org/repo.git"
	cfg.Git.DefaultBranch = "main"
	cfg.Git.Provider = "github"
	cfg.Git.GitHub.Token = "ghp_git"
	cfg.Tracker.Provider = "jira"
	cfg.Tracker.PickupLabel = "foreman-ready"
	cfg.Tracker.Jira.APIToken = "jira-tok"
	cfg.Tracker.Jira.ProjectKey = "PROJ"
	cfg.Tracker.Jira.BaseURL = "https://company.atlassian.net"
	cfg.Tracker.Jira.Email = "bot@company.com"
	cfg.AgentRunner.Provider = "builtin"
	cfg.Models.Planner = "anthropic:claude-sonnet-4-6"
	cfg.Models.Implementer = "anthropic:claude-sonnet-4-6"
	cfg.Limits.MaxParallelTickets = 3
	cfg.Limits.MaxTasksPerTicket = 20
	cfg.Cost.MaxCostPerTicketUSD = 10.0

	dto := flattenProjectConfig(cfg)

	if dto.Name != "My Project" {
		t.Errorf("Name: got %q want My Project", dto.Name)
	}
	if dto.GitCloneURL != "git@github.com:org/repo.git" {
		t.Errorf("GitCloneURL: got %q", dto.GitCloneURL)
	}
	if dto.GitDefaultBranch != "main" {
		t.Errorf("GitDefaultBranch: got %q", dto.GitDefaultBranch)
	}
	if dto.TrackerProvider != "jira" {
		t.Errorf("TrackerProvider: got %q", dto.TrackerProvider)
	}
	if dto.TrackerToken != "jira-tok" {
		t.Errorf("TrackerToken: got %q want jira-tok", dto.TrackerToken)
	}
	if dto.TrackerProjectKey != "PROJ" {
		t.Errorf("TrackerProjectKey: got %q want PROJ", dto.TrackerProjectKey)
	}
	if dto.TrackerURL != "https://company.atlassian.net" {
		t.Errorf("TrackerURL: got %q", dto.TrackerURL)
	}
	// tracker_email is the key field added in this session
	if dto.TrackerEmail != "bot@company.com" {
		t.Errorf("TrackerEmail: got %q want bot@company.com", dto.TrackerEmail)
	}
	if dto.MaxParallelTickets != 3 {
		t.Errorf("MaxParallelTickets: got %d want 3", dto.MaxParallelTickets)
	}
	if dto.MaxCostPerTicket != 10.0 {
		t.Errorf("MaxCostPerTicket: got %f want 10.0", dto.MaxCostPerTicket)
	}
}

func TestFlattenProjectConfig_GitHubTracker(t *testing.T) {
	cfg := &project.ProjectConfig{}
	cfg.Tracker.Provider = "github"
	cfg.Tracker.GitHub.Token = "ghp_tracker"
	cfg.Tracker.GitHub.Owner = "myorg"
	cfg.Tracker.GitHub.Repo = "myrepo"
	cfg.Tracker.GitHub.BaseURL = "https://api.github.com"

	dto := flattenProjectConfig(cfg)

	if dto.TrackerProvider != "github" {
		t.Errorf("TrackerProvider: got %q want github", dto.TrackerProvider)
	}
	if dto.TrackerToken != "ghp_tracker" {
		t.Errorf("TrackerToken: got %q want ghp_tracker", dto.TrackerToken)
	}
	if dto.TrackerProjectKey != "myorg/myrepo" {
		t.Errorf("TrackerProjectKey: got %q want myorg/myrepo", dto.TrackerProjectKey)
	}
	if dto.TrackerURL != "https://api.github.com" {
		t.Errorf("TrackerURL: got %q", dto.TrackerURL)
	}
	// email must be empty for non-Jira trackers
	if dto.TrackerEmail != "" {
		t.Errorf("TrackerEmail: expected empty for github, got %q", dto.TrackerEmail)
	}
}

func TestFlattenProjectConfig_LinearTracker(t *testing.T) {
	cfg := &project.ProjectConfig{}
	cfg.Tracker.Provider = "linear"
	cfg.Tracker.Linear.APIKey = "lin_api_abc"
	cfg.Tracker.Linear.TeamID = "TEAM1"
	cfg.Tracker.Linear.BaseURL = "https://api.linear.app"

	dto := flattenProjectConfig(cfg)

	if dto.TrackerProvider != "linear" {
		t.Errorf("TrackerProvider: got %q", dto.TrackerProvider)
	}
	if dto.TrackerToken != "lin_api_abc" {
		t.Errorf("TrackerToken: got %q", dto.TrackerToken)
	}
	if dto.TrackerProjectKey != "TEAM1" {
		t.Errorf("TrackerProjectKey: got %q want TEAM1", dto.TrackerProjectKey)
	}
	if dto.TrackerURL != "https://api.linear.app" {
		t.Errorf("TrackerURL: got %q", dto.TrackerURL)
	}
	if dto.TrackerEmail != "" {
		t.Errorf("TrackerEmail: expected empty for linear, got %q", dto.TrackerEmail)
	}
}

func TestExpandProjectConfigDTO_JiraTracker(t *testing.T) {
	dto := projectConfigDTO{
		Name:               "Expanded",
		Description:        "desc",
		GitCloneURL:        "git@github.com:org/repo.git",
		GitDefaultBranch:   "main",
		GitProvider:        "github",
		GitToken:           "ghp_git",
		TrackerProvider:    "jira",
		TrackerLabels:      "foreman-ready",
		TrackerToken:       "jira-tok",
		TrackerProjectKey:  "PROJ",
		TrackerURL:         "https://company.atlassian.net",
		TrackerEmail:       "bot@company.com",
		AgentRunner:        "builtin",
		ModelPlanner:       "anthropic:claude-sonnet-4-6",
		ModelImplementer:   "anthropic:claude-sonnet-4-6",
		MaxParallelTickets: 4,
		MaxTasksPerTicket:  15,
		MaxCostPerTicket:   12.0,
	}

	cfg := expandProjectConfigDTO(dto)

	if cfg.Project.Name != "Expanded" {
		t.Errorf("project.name: got %q", cfg.Project.Name)
	}
	if cfg.Git.CloneURL != "git@github.com:org/repo.git" {
		t.Errorf("git.clone_url: got %q", cfg.Git.CloneURL)
	}
	if cfg.Git.DefaultBranch != "main" {
		t.Errorf("git.default_branch: got %q", cfg.Git.DefaultBranch)
	}
	if cfg.Tracker.Provider != "jira" {
		t.Errorf("tracker.provider: got %q", cfg.Tracker.Provider)
	}
	if cfg.Tracker.Jira.APIToken != "jira-tok" {
		t.Errorf("tracker.jira.api_token: got %q", cfg.Tracker.Jira.APIToken)
	}
	if cfg.Tracker.Jira.ProjectKey != "PROJ" {
		t.Errorf("tracker.jira.project_key: got %q", cfg.Tracker.Jira.ProjectKey)
	}
	if cfg.Tracker.Jira.BaseURL != "https://company.atlassian.net" {
		t.Errorf("tracker.jira.base_url: got %q", cfg.Tracker.Jira.BaseURL)
	}
	// tracker_email is the key field
	if cfg.Tracker.Jira.Email != "bot@company.com" {
		t.Errorf("tracker.jira.email: got %q want bot@company.com", cfg.Tracker.Jira.Email)
	}
	if cfg.Limits.MaxParallelTickets != 4 {
		t.Errorf("limits.max_parallel_tickets: got %d want 4", cfg.Limits.MaxParallelTickets)
	}
	if cfg.Limits.MaxTasksPerTicket != 15 {
		t.Errorf("limits.max_tasks_per_ticket: got %d want 15", cfg.Limits.MaxTasksPerTicket)
	}
	if cfg.Cost.MaxCostPerTicketUSD != 12.0 {
		t.Errorf("cost.max_cost_per_ticket_usd: got %f want 12.0", cfg.Cost.MaxCostPerTicketUSD)
	}
}

func TestExpandProjectConfigDTO_GitHubTracker_SplitsOwnerRepo(t *testing.T) {
	dto := projectConfigDTO{
		TrackerProvider:   "github",
		TrackerToken:      "ghp_tracker",
		TrackerProjectKey: "myorg/myrepo",
		TrackerURL:        "https://api.github.com",
	}

	cfg := expandProjectConfigDTO(dto)

	if cfg.Tracker.GitHub.Token != "ghp_tracker" {
		t.Errorf("tracker.github.token: got %q", cfg.Tracker.GitHub.Token)
	}
	if cfg.Tracker.GitHub.Owner != "myorg" {
		t.Errorf("tracker.github.owner: got %q want myorg", cfg.Tracker.GitHub.Owner)
	}
	if cfg.Tracker.GitHub.Repo != "myrepo" {
		t.Errorf("tracker.github.repo: got %q want myrepo", cfg.Tracker.GitHub.Repo)
	}
	if cfg.Tracker.GitHub.BaseURL != "https://api.github.com" {
		t.Errorf("tracker.github.base_url: got %q", cfg.Tracker.GitHub.BaseURL)
	}
}

func TestExpandProjectConfigDTO_GitHubTracker_NoSlash_DoesNotSplit(t *testing.T) {
	dto := projectConfigDTO{
		TrackerProvider:   "github",
		TrackerProjectKey: "noslash",
	}
	cfg := expandProjectConfigDTO(dto)
	// When there is no "/" the split is skipped; Owner and Repo remain empty
	if cfg.Tracker.GitHub.Owner != "" || cfg.Tracker.GitHub.Repo != "" {
		t.Errorf("expected empty owner/repo for no-slash key, got owner=%q repo=%q",
			cfg.Tracker.GitHub.Owner, cfg.Tracker.GitHub.Repo)
	}
}

func TestExpandProjectConfigDTO_LinearTracker(t *testing.T) {
	dto := projectConfigDTO{
		TrackerProvider:   "linear",
		TrackerToken:      "lin_api_abc",
		TrackerProjectKey: "TEAM1",
		TrackerURL:        "https://api.linear.app",
	}

	cfg := expandProjectConfigDTO(dto)

	if cfg.Tracker.Linear.APIKey != "lin_api_abc" {
		t.Errorf("tracker.linear.api_key: got %q", cfg.Tracker.Linear.APIKey)
	}
	if cfg.Tracker.Linear.TeamID != "TEAM1" {
		t.Errorf("tracker.linear.team_id: got %q want TEAM1", cfg.Tracker.Linear.TeamID)
	}
	if cfg.Tracker.Linear.BaseURL != "https://api.linear.app" {
		t.Errorf("tracker.linear.base_url: got %q", cfg.Tracker.Linear.BaseURL)
	}
}

// TestFlattenThenExpand_RoundTrip verifies that flatten → expand is lossless
// for all three tracker providers.
func TestFlattenThenExpand_RoundTrip_Jira(t *testing.T) {
	orig := &project.ProjectConfig{}
	orig.Project.Name = "Round Trip"
	orig.Git.CloneURL = "git@github.com:org/repo.git"
	orig.Git.DefaultBranch = "main"
	orig.Git.Provider = "github"
	orig.Git.GitHub.Token = "ghp_git"
	orig.Tracker.Provider = "jira"
	orig.Tracker.PickupLabel = "foreman-ready"
	orig.Tracker.Jira.APIToken = "jira-tok"
	orig.Tracker.Jira.ProjectKey = "PROJ"
	orig.Tracker.Jira.BaseURL = "https://company.atlassian.net"
	orig.Tracker.Jira.Email = "bot@company.com"
	orig.AgentRunner.Provider = "builtin"
	orig.Limits.MaxParallelTickets = 3
	orig.Limits.MaxTasksPerTicket = 20
	orig.Cost.MaxCostPerTicketUSD = 10.0

	dto := flattenProjectConfig(orig)
	got := expandProjectConfigDTO(dto)

	if got.Tracker.Jira.Email != orig.Tracker.Jira.Email {
		t.Errorf("tracker.jira.email round-trip: got %q want %q",
			got.Tracker.Jira.Email, orig.Tracker.Jira.Email)
	}
	if got.Tracker.Jira.APIToken != orig.Tracker.Jira.APIToken {
		t.Errorf("tracker.jira.api_token round-trip: got %q", got.Tracker.Jira.APIToken)
	}
	if got.Git.CloneURL != orig.Git.CloneURL {
		t.Errorf("git.clone_url round-trip: got %q", got.Git.CloneURL)
	}
	if got.Limits.MaxParallelTickets != orig.Limits.MaxParallelTickets {
		t.Errorf("limits.max_parallel_tickets round-trip: got %d", got.Limits.MaxParallelTickets)
	}
}
