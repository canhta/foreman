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

// EventSubscriber is the subset of EventEmitter needed for WebSocket.
type EventSubscriber interface {
	Subscribe() chan *models.EventRecord
	Unsubscribe(ch chan *models.EventRecord)
}

// API handles REST API requests for the dashboard.
type API struct {
	db        DashboardDB
	emitter   EventSubscriber
	version   string
	startedAt time.Time
}

// NewAPI creates a new API instance.
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
