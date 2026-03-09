package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Same-origin requests may not send Origin header
		}
		// Allow if Origin matches the request Host (production)
		if strings.HasSuffix(origin, "://"+r.Host) {
			return true
		}
		// Allow localhost origins for dev (Vite on :5173 proxying to :8080)
		return strings.Contains(origin, "://localhost:") || strings.Contains(origin, "://127.0.0.1:")
	},
}

// extractWebSocketToken extracts the auth token from a WebSocket upgrade request.
// It checks, in order:
//  1. The Sec-WebSocket-Protocol header (preferred — not logged by servers).
//  2. The Authorization: Bearer header.
//  3. The ?token= query parameter (deprecated — logged in access logs).
func extractWebSocketToken(r *http.Request) string {
	// 1. Sec-WebSocket-Protocol header: clients send "bearer.<token>" as the
	//    subprotocol. This is the standard workaround for passing credentials
	//    during the WebSocket handshake without exposing them in URLs.
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		// Accept "bearer.<token>" or "bearer,<token>" or just the raw token
		// when the client only sends one protocol value.
		lower := strings.ToLower(proto)
		if strings.HasPrefix(lower, "bearer.") {
			return proto[len("bearer."):]
		}
		if strings.HasPrefix(lower, "bearer,") {
			return strings.TrimSpace(proto[len("bearer,"):])
		}
	}

	// 2. Standard Authorization header (works for same-origin WS upgrades).
	if token := extractBearerToken(r); token != "" {
		return token
	}

	// 3. Fallback: query parameter (deprecated — visible in server logs).
	if token := r.URL.Query().Get("token"); token != "" {
		log.Warn().Msg("WebSocket auth via ?token= query param is deprecated; use Sec-WebSocket-Protocol: bearer.<token> instead")
		return token
	}

	return ""
}

func (a *API) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := extractWebSocketToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := hashToken(token)
	valid, err := a.db.ValidateAuthToken(r.Context(), hash)
	if err != nil || !valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var respHeader http.Header
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); strings.HasPrefix(strings.ToLower(proto), "bearer.") {
		// Echo back the exact subprotocol the client offered so the browser accepts the connection
		respHeader = http.Header{"Sec-WebSocket-Protocol": []string{proto}}
	}
	conn, err := upgrader.Upgrade(w, r, respHeader)
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
		enriched := a.enrichEvent(r.Context(), evt)
		data, err := json.Marshal(enriched)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
}

// enrichedEvent is the WebSocket payload with ticket context.
// ticket_title is a snapshot at event time — titles rarely change.
//
//nolint:govet // fieldalignment: embedding EventRecord first prioritises readability
type enrichedEvent struct {
	TicketTitle string `json:"ticket_title,omitempty"`
	Submitter   string `json:"submitter,omitempty"`
	Runner      string `json:"runner,omitempty"`
	Model       string `json:"model,omitempty"`
	models.EventRecord
}

func (a *API) enrichEvent(ctx context.Context, evt *models.EventRecord) *enrichedEvent {
	enriched := &enrichedEvent{EventRecord: *evt}
	if evt.TicketID != "" {
		ticket, err := a.db.GetTicket(ctx, evt.TicketID)
		if err == nil && ticket != nil {
			enriched.TicketTitle = ticket.Title
			enriched.Submitter = ticket.ChannelSenderID
		}
	}

	// Extract runner and model from Details JSON if present.
	// Event metadata is always map[string]string (see telemetry.EventEmitter.Emit),
	// so absent keys return "" which is the desired zero value for omitempty.
	if evt.Details != "" {
		var details map[string]string
		if err := json.Unmarshal([]byte(evt.Details), &details); err == nil {
			enriched.Runner = details["runner"]
			enriched.Model = details["model"]
		}
	}

	return enriched
}
