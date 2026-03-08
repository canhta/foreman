package dashboard

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

// --- limitRequestBody middleware ---

func TestLimitRequestBody_POST_WithinLimit(t *testing.T) {
	body := strings.Repeat("x", 100)
	var readBody string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		readBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	handler := limitRequestBody(inner)

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if readBody != body {
		t.Errorf("expected full body to be read, got %q", readBody)
	}
}

func TestLimitRequestBody_GET_NotLimited(t *testing.T) {
	// GET requests should not have their body limited.
	// Confirm the middleware passes GET requests through unchanged.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := limitRequestBody(inner)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected inner handler to be called for GET request")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestLimitRequestBody_PUT_WithinLimit(t *testing.T) {
	body := strings.Repeat("y", 512)
	var readLen int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		readLen = len(b)
		w.WriteHeader(http.StatusOK)
	})
	handler := limitRequestBody(inner)

	req := httptest.NewRequestWithContext(t.Context(), "PUT", "/api/something", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if readLen != len(body) {
		t.Errorf("expected %d bytes read, got %d", len(body), readLen)
	}
}

func TestLimitRequestBody_POST_ExceedsLimit(t *testing.T) {
	// Send a body larger than 1 MiB — reading it should fail.
	oversized := strings.Repeat("z", maxRequestBodyBytes+1)
	var readErr error
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	handler := limitRequestBody(inner)

	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/tickets", strings.NewReader(oversized))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if readErr == nil {
		t.Error("expected an error when reading a body that exceeds the 1 MiB limit")
	}
}

// --- dbTicketRetrier ---

func TestDbTicketRetrier_SetsStatusQueued(t *testing.T) {
	var capturedStatus models.TicketStatus
	var capturedID string
	dbm := &mockCaptureDB{
		onUpdateTicketStatus: func(id string, status models.TicketStatus) {
			capturedID = id
			capturedStatus = status
		},
	}
	retrier := &dbTicketRetrier{db: dbm}
	if err := retrier.RetryTicket(context.Background(), "ticket-42"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID != "ticket-42" {
		t.Errorf("expected ticket-42, got %s", capturedID)
	}
	if capturedStatus != models.TicketStatusQueued {
		t.Errorf("expected status=queued, got %s", capturedStatus)
	}
}

// mockCaptureDB wraps mockDashboardDB and intercepts UpdateTicketStatus calls.
type mockCaptureDB struct {
	mockDashboardDB
	onUpdateTicketStatus func(id string, status models.TicketStatus)
}

func (m *mockCaptureDB) UpdateTicketStatus(_ context.Context, id string, status models.TicketStatus) error {
	if m.onUpdateTicketStatus != nil {
		m.onUpdateTicketStatus(id, status)
	}
	return nil
}
