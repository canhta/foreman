package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
)

// TaskContextStats is an alias for db.TaskContextStats used in the dashboard package.
type TaskContextStats = db.TaskContextStats

// TaskContextResponse is the JSON response for GET /api/tasks/{id}/context.
type TaskContextResponse struct {
	Budget         int     `json:"budget"`
	Used           int     `json:"used"`
	UtilizationPct float64 `json:"utilization_pct"`
	FilesSelected  int     `json:"files_selected"`
	FilesTouched   int     `json:"files_touched"`
	CacheHits      int     `json:"cache_hits"`
}

// DashboardDB is a subset of db.Database needed by the dashboard.
type DashboardDB interface {
	AuthValidator
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	GetTicketCost(ctx context.Context, ticketID string) (float64, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error)
	GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error)
	GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error)
	GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error)
	DeleteTicket(ctx context.Context, id string) error
	GetTaskContextStats(ctx context.Context, taskID string) (TaskContextStats, error)
	UpdateTaskContextStats(ctx context.Context, taskID string, stats TaskContextStats) error
}

// EventSubscriber is the subset of EventEmitter needed for WebSocket.
type EventSubscriber interface {
	Subscribe() chan *models.EventRecord
	Unsubscribe(ch chan *models.EventRecord)
}

// DaemonStatusProvider is an optional interface for exposing daemon runtime state.
// Pass nil when running the dashboard without an attached daemon.
type DaemonStatusProvider interface {
	IsRunning() bool
	IsPaused() bool
}

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

// MCPHealthProvider exposes the health state of all registered MCP servers.
// Implement this interface to include MCP server health in dashboard responses.
type MCPHealthProvider interface {
	HealthStatus() map[string]bool
}

// API handles REST API requests for the dashboard.
type API struct {
	startedAt      time.Time
	db             DashboardDB
	emitter        EventSubscriber
	statusProvider DaemonStatusProvider
	controller     DaemonController
	retrier        TicketRetrier
	mcpHealth      MCPHealthProvider
	channelHealth  map[string]interface{ IsConnected() bool }
	version        string
	costCfg        models.CostConfig
}

// SetChannelHealth registers a HealthChecker for a named channel.
func (a *API) SetChannelHealth(name string, h interface{ IsConnected() bool }) {
	if a.channelHealth == nil {
		a.channelHealth = make(map[string]interface{ IsConnected() bool })
	}
	a.channelHealth[name] = h
}

// SetMCPHealthProvider wires a provider that exposes MCP server health.
func (a *API) SetMCPHealthProvider(p MCPHealthProvider) {
	a.mcpHealth = p
}

// SetDaemonController wires a DaemonController for pause/resume.
func (a *API) SetDaemonController(c DaemonController) {
	a.controller = c
	a.statusProvider = c
}

// SetTicketRetrier wires a TicketRetrier for ticket retry.
func (a *API) SetTicketRetrier(r TicketRetrier) {
	a.retrier = r
}

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

	if a.mcpHealth != nil {
		mcpStatus := a.mcpHealth.HealthStatus()
		if len(mcpStatus) > 0 {
			servers := make(map[string]interface{}, len(mcpStatus))
			for name, healthy := range mcpStatus {
				servers[name] = map[string]interface{}{
					"healthy": healthy,
				}
			}
			resp["mcp_servers"] = servers
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

var validTicketStatuses = map[models.TicketStatus]bool{
	models.TicketStatusQueued:              true,
	models.TicketStatusClarificationNeeded: true,
	models.TicketStatusPlanning:            true,
	models.TicketStatusPlanValidating:      true,
	models.TicketStatusImplementing:        true,
	models.TicketStatusReviewing:           true,
	models.TicketStatusPRCreated:           true,
	models.TicketStatusDone:                true,
	models.TicketStatusPartial:             true,
	models.TicketStatusFailed:              true,
	models.TicketStatusBlocked:             true,
	models.TicketStatusDecomposing:         true,
	models.TicketStatusDecomposed:          true,
	models.TicketStatusAwaitingMerge:       true,
	models.TicketStatusMerged:              true,
	models.TicketStatusPRClosed:            true,
}

func (a *API) handleListTickets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	filter := models.TicketFilter{}
	if status != "" {
		ts := models.TicketStatus(status)
		if !validTicketStatuses[ts] {
			http.Error(w, "invalid status filter", http.StatusBadRequest)
			return
		}
		filter.StatusIn = []models.TicketStatus{ts}
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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_daily_usd":       a.costCfg.MaxCostPerDayUSD,
		"max_monthly_usd":     a.costCfg.MaxCostPerMonthUSD,
		"max_ticket_usd":      a.costCfg.MaxCostPerTicketUSD,
		"alert_threshold_pct": a.costCfg.AlertThresholdPct,
	})
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

func (a *API) handleTeamStats(w http.ResponseWriter, r *http.Request) {
	since := time.Now().AddDate(0, 0, -7)
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

func (a *API) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	id := strings.TrimSuffix(path, "/retry")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}
	if err := a.db.UpdateTaskStatus(r.Context(), id, models.TaskStatusPending); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying", "task_id": id})
}

func (a *API) handleTaskContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	// Task IDs are UUIDs and never contain slashes, so TrimSuffix is safe here.
	id := strings.TrimSuffix(path, "/context")
	if id == "" || id == path {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}
	stats, err := a.db.GetTaskContextStats(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utilPct := 0.0
	if stats.Budget > 0 {
		utilPct = float64(stats.Used) / float64(stats.Budget) * 100.0
	}
	writeJSON(w, http.StatusOK, TaskContextResponse{
		Budget:         stats.Budget,
		Used:           stats.Used,
		UtilizationPct: utilPct,
		FilesSelected:  stats.FilesSelected,
		FilesTouched:   stats.FilesTouched,
		CacheHits:      stats.CacheHits,
	})
}

func (a *API) handleDeleteTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := extractPathParam(r.URL.Path, "/api/tickets/")
	if id == "" {
		http.Error(w, "missing ticket id", http.StatusBadRequest)
		return
	}
	if err := a.db.DeleteTicket(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "ticket_id": id})
}

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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func extractPathParam(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return rest
}
