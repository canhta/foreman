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
	db := &mockDashboardDB{}
	api := NewAPI(db, emitter, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	// Valid token — mockDashboardDB.ValidateAuthToken always returns true
	token := "valid-token"
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=" + token
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

func TestWebSocketAuth_MissingToken(t *testing.T) {
	db := &mockDashboardDB{}
	api := NewAPI(db, nil, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	// Attempt connection without token — should get 401
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
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

func TestWebSocketAuth_InvalidToken(t *testing.T) {
	db := &mockInvalidAuthDB{}
	api := NewAPI(db, nil, "1.0.0")

	srv := httptest.NewServer(http.HandlerFunc(api.handleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events?token=bad-token"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
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
