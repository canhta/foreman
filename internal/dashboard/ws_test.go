package dashboard

import (
	"context"
	"fmt"
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
	db := &mockDashboardDB{}
	api := NewAPI(db, emitter, nil, models.CostConfig{}, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	// Valid token — mockDashboardDB.ValidateAuthToken always returns true
	token := "valid-token"
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + token
	ws, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if wsResp != nil {
		defer wsResp.Body.Close()
	}
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

func TestWebSocketAuth_MissingToken(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	// Attempt connection without token — should get 401
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected connection to be rejected, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		t.Errorf("expected 401, got %d", statusCode)
	}
}

func TestWebSocketCORS_CheckOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		host    string
		allowed bool
	}{
		{"no origin header", "", "localhost:8080", true},
		{"same origin", "http://localhost:8080", "localhost:8080", true},
		{"same origin https", "https://example.com", "example.com", true},
		{"cross origin", "http://evil.com", "localhost:8080", false},
		{"cross origin partial match", "http://notlocalhost:8080", "localhost:8080", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			got := upgrader.CheckOrigin(r)
			if got != tt.allowed {
				t.Errorf("CheckOrigin(%q, host=%q) = %v, want %v", tt.origin, tt.host, got, tt.allowed)
			}
		})
	}
}

func TestWebSocketAuth_InvalidToken(t *testing.T) {
	db := &mockInvalidAuthDB{}
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=bad-token"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected connection to be rejected, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		t.Errorf("expected 401, got %d", statusCode)
	}
}

// --- extractWebSocketToken unit tests ---

func TestExtractWebSocketToken_SecWebSocketProtocol_BearerDot(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "bearer.my-secret-token")
	got := extractWebSocketToken(req)
	if got != "my-secret-token" {
		t.Errorf("expected my-secret-token via bearer. prefix, got %q", got)
	}
}

func TestExtractWebSocketToken_SecWebSocketProtocol_BearerComma(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "bearer, my-secret-token")
	got := extractWebSocketToken(req)
	if got != "my-secret-token" {
		t.Errorf("expected my-secret-token via bearer, prefix, got %q", got)
	}
}

func TestExtractWebSocketToken_AuthorizationBearer(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events", nil)
	req.Header.Set("Authorization", "Bearer auth-header-token")
	got := extractWebSocketToken(req)
	if got != "auth-header-token" {
		t.Errorf("expected auth-header-token via Authorization header, got %q", got)
	}
}

func TestExtractWebSocketToken_QueryParam(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events?token=query-token", nil)
	got := extractWebSocketToken(req)
	if got != "query-token" {
		t.Errorf("expected query-token via ?token= param, got %q", got)
	}
}

func TestExtractWebSocketToken_NoToken(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ws/events", nil)
	got := extractWebSocketToken(req)
	if got != "" {
		t.Errorf("expected empty string when no token provided, got %q", got)
	}
}

// --- WebSocket connection via Sec-WebSocket-Protocol header ---

func TestWebSocketAuth_SecWebSocketProtocol(t *testing.T) {
	ch := make(chan *models.EventRecord, 10)
	emitter := &mockEmitter{ch: ch}
	db := &mockDashboardDB{}
	api := NewAPI(db, emitter, nil, models.CostConfig{}, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	headers := http.Header{
		"Sec-WebSocket-Protocol": []string{"bearer.valid-token"},
	}
	ws, wsResp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if wsResp != nil {
		defer wsResp.Body.Close()
	}
	if err != nil {
		t.Fatalf("expected connection to succeed via Sec-WebSocket-Protocol, got: %v", err)
	}
	defer ws.Close()

	// Send an event and confirm it's received over the connection.
	ch <- &models.EventRecord{ID: "e2", TicketID: "t1", EventType: "impl_started"}
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(msg), "impl_started") {
		t.Errorf("expected impl_started in message, got %s", string(msg))
	}
}

// --- handleWebSocket: nil emitter closes connection gracefully ---

func TestWebSocketNilEmitter_ConnectsAndCloses(t *testing.T) {
	db := &mockDashboardDB{} // ValidateAuthToken returns true
	api := NewAPI(db, nil, nil, models.CostConfig{}, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	token := "valid-token"
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + token
	ws, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if wsResp != nil {
		defer wsResp.Body.Close()
	}
	if err != nil {
		t.Fatalf("expected successful connection even with nil emitter, got: %v", err)
	}
	defer ws.Close()

	// Server closes immediately when emitter is nil — read should return an error.
	ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, readErr := ws.ReadMessage()
	if readErr == nil {
		t.Error("expected connection to be closed when emitter is nil")
	}
}

// --- enrichEvent: DB error is handled gracefully ---

// mockGetTicketErrorDB returns an error from GetTicket.
type mockGetTicketErrorDB struct {
	mockDashboardDB
}

func (m *mockGetTicketErrorDB) GetTicket(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, fmt.Errorf("db error")
}

func TestEnrichEvent_DBError_GracefulDegradation(t *testing.T) {
	api := NewAPI(&mockGetTicketErrorDB{}, nil, nil, models.CostConfig{}, "1.0.0")
	evt := &models.EventRecord{ID: "e1", TicketID: "t1", EventType: "task_started"}
	enriched := api.enrichEvent(context.Background(), evt)
	// DB error must not cause a panic and ticket_title should be empty.
	if enriched == nil {
		t.Fatal("expected enrichedEvent, got nil")
	}
	if enriched.TicketTitle != "" {
		t.Errorf("expected empty TicketTitle on DB error, got %q", enriched.TicketTitle)
	}
	if enriched.EventType != "task_started" {
		t.Errorf("expected original event type preserved, got %q", enriched.EventType)
	}
}
