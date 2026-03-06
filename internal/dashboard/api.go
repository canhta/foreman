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
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
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

// API handles REST API requests for the dashboard.
type API struct {
	startedAt      time.Time
	db             DashboardDB
	emitter        EventSubscriber
	statusProvider DaemonStatusProvider
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

	writeJSON(w, http.StatusOK, resp)
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
		"alert_threshold_pct": a.costCfg.AlertThresholdPct,
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
