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

// mockInvalidAuthDB always rejects auth token validation.
type mockInvalidAuthDB struct {
	mockDashboardDB
}

func (m *mockInvalidAuthDB) ValidateAuthToken(_ context.Context, _ string) (bool, error) {
	return false, nil
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

func TestAPIGetTicket(t *testing.T) {
	db := &mockDashboardDB{
		tickets: []models.Ticket{{ID: "t1", Title: "Test", Status: models.TicketStatusImplementing}},
	}
	api := NewAPI(db, nil, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/t1", nil)
	rec := httptest.NewRecorder()
	api.handleGetTicket(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIGetTicketNotFound(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")
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
	api := NewAPI(db, nil, "1.0.0")
	req := httptest.NewRequest("GET", "/api/tickets/t1/events", nil)
	rec := httptest.NewRecorder()
	api.handleGetEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPICostsToday(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")
	req := httptest.NewRequest("GET", "/api/costs/today", nil)
	rec := httptest.NewRecorder()
	api.handleCostsToday(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
