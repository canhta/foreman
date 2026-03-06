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
	tickets   []models.Ticket
	events    []models.EventRecord
	teamStats []models.TeamStat
	summaries []models.TicketSummary
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

func (m *mockDashboardDB) UpdateTaskStatus(_ context.Context, _ string, _ models.TaskStatus) error {
	return nil
}

func (m *mockDashboardDB) UpdateTicketStatus(_ context.Context, _ string, _ models.TicketStatus) error {
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

func TestAPIGetStatus(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

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
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

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

func TestAPIGetTicket(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{{ID: "t1", Title: "Test", Status: models.TicketStatusImplementing}},
	}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleGetTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketNotFound(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/nonexistent", nil)
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
	req := httptest.NewRequest("GET", "/api/tickets/t1/events", nil)
	rec := httptest.NewRecorder()
	api.handleGetEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPICostsToday(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/costs/today", nil)
	rec := httptest.NewRecorder()
	api.handleCostsToday(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketTasks(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/t1/tasks", nil)
	rec := httptest.NewRecorder()
	api.handleGetTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetCostsWeek(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
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
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/pipeline/active", nil)
	rec := httptest.NewRecorder()
	api.handleActivePipelines(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

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

	req := httptest.NewRequest("GET", "/api/status", nil)
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

	req := httptest.NewRequest("GET", "/api/status", nil)
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
	req := httptest.NewRequest("GET", "/api/tickets/t1/llm-calls", nil)
	rec := httptest.NewRecorder()
	api.handleGetLlmCalls(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPICostsMonth(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("GET", "/api/costs/month", nil)
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
	req := httptest.NewRequest("POST", "/api/tickets/t1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTicket(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestAPIDaemonPause_NoController(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("POST", "/api/daemon/pause", nil)
	rec := httptest.NewRecorder()
	api.handleDaemonPause(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestAPIDaemonResume_NoController(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("POST", "/api/daemon/resume", nil)
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
	req := httptest.NewRequest("GET", "/api/ticket-summaries", nil)
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

func TestAPIRetryTask(t *testing.T) {
	api := NewAPI(&mockDashboardDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	req := httptest.NewRequest("POST", "/api/tasks/task-1/retry", nil)
	rec := httptest.NewRecorder()
	api.handleRetryTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

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
