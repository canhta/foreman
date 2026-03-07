package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSSEServer is a minimal MCP HTTP+SSE test server.
// It implements the MCP SSE transport protocol:
//   - GET /sse → streams SSE events (first sends "endpoint" event, then routes responses)
//   - POST /messages → accepts JSON-RPC requests, queues responses to SSE stream
type testSSEServer struct {
	server         *httptest.Server
	requestHandler func(method string, params json.RawMessage) (interface{}, *jsonRPCErrorObj)
	capturedAuth   string
	sseClients     []chan string
	mu             sync.Mutex
}

type jsonRPCErrorObj struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func newTestSSEServer(t *testing.T) *testSSEServer {
	t.Helper()
	ts := &testSSEServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", ts.handleSSE)
	mux.HandleFunc("/messages", ts.handleMessages)
	ts.server = httptest.NewServer(mux)

	// Default handler: echo back initialize response
	ts.requestHandler = defaultRequestHandler

	t.Cleanup(func() { ts.server.Close() })
	return ts
}

func defaultRequestHandler(method string, _ json.RawMessage) (interface{}, *jsonRPCErrorObj) {
	switch method {
	case "initialize":
		return map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "test-http-server", "version": "1.0"},
		}, nil
	case "notifications/initialized":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "query_db",
					"description": "Query the database",
					"inputSchema": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"sql": map[string]interface{}{"type": "string"}},
					},
				},
			},
		}, nil
	case "tools/call":
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "tool result"},
			},
		}, nil
	case "shutdown":
		return map[string]interface{}{}, nil
	default:
		return nil, &jsonRPCErrorObj{Code: -32601, Message: "method not found"}
	}
}

func (ts *testSSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.capturedAuth = r.Header.Get("Authorization")
	ts.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send endpoint event — tells client where to POST
	postURL := ts.server.URL + "/messages"
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postURL)
	flusher.Flush()

	// Register client channel
	ch := make(chan string, 100)
	ts.mu.Lock()
	ts.sseClients = append(ts.sseClients, ch)
	ts.mu.Unlock()

	// Stream messages until client disconnects
	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (ts *testSSEServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.capturedAuth = r.Header.Get("Authorization")
	ts.mu.Unlock()

	var req struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)

	// Process request and send response to SSE clients
	result, rpcErr := ts.requestHandler(req.Method, req.Params)

	var resp interface{}
	if rpcErr != nil {
		resp = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   rpcErr,
		}
	} else {
		resp = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		}
	}

	data, _ := json.Marshal(resp)

	ts.mu.Lock()
	clients := make([]chan string, len(ts.sseClients))
	copy(clients, ts.sseClients)
	ts.mu.Unlock()

	for _, ch := range clients {
		select {
		case ch <- string(data):
		default:
		}
	}
}

// URL returns the base URL of the test server.
func (ts *testSSEServer) URL() string {
	return ts.server.URL
}

// TestHTTPClient_ListTools verifies that tools/list works over HTTP+SSE transport.
func TestHTTPClient_ListTools(t *testing.T) {
	srv := newTestSSEServer(t)

	client := mcp.NewHTTPClient(srv.URL(), "", "remote-db")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, client.Initialize(ctx))

	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "mcp_remote_db_query_db", tools[0].Name)
	assert.Equal(t, "Query the database", tools[0].Description)

	require.NoError(t, client.Close())
}

// TestHTTPClient_CallTool verifies tool invocation over HTTP+SSE transport.
func TestHTTPClient_CallTool(t *testing.T) {
	srv := newTestSSEServer(t)
	srv.requestHandler = func(method string, params json.RawMessage) (interface{}, *jsonRPCErrorObj) {
		if method == "initialize" {
			return defaultRequestHandler(method, params)
		}
		if method == "tools/call" {
			// Parse and echo back the tool name
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(params, &p)
			return map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "called:" + p.Name},
				},
			}, nil
		}
		return defaultRequestHandler(method, params)
	}

	client := mcp.NewHTTPClient(srv.URL(), "", "remote-db")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, client.Initialize(ctx))

	result, err := client.Call(ctx, "query_db", json.RawMessage(`{"sql":"SELECT 1"}`))
	require.NoError(t, err)
	assert.Equal(t, "called:query_db", result)

	require.NoError(t, client.Close())
}

// TestHTTPClient_AuthHeader verifies that Authorization header is sent on both SSE and POST requests.
func TestHTTPClient_AuthHeader(t *testing.T) {
	srv := newTestSSEServer(t)

	const token = "test-bearer-token-xyz"
	client := mcp.NewHTTPClient(srv.URL(), token, "remote-db")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, client.Initialize(ctx))

	// Give SSE handshake time to register auth
	time.Sleep(20 * time.Millisecond)

	srv.mu.Lock()
	capturedAuth := srv.capturedAuth
	srv.mu.Unlock()

	assert.Equal(t, "Bearer "+token, capturedAuth)

	require.NoError(t, client.Close())
}

// TestHTTPClient_SSEReconnect verifies that the client reconnects after the SSE stream drops
// and can still call tools successfully on the new connection.
func TestHTTPClient_SSEReconnect(t *testing.T) {
	// sseState holds per-connection SSE state protected by a mutex.
	var (
		rsMu        sync.Mutex
		rsConns     int                   // total SSE connections seen
		rsCurrentCh chan string           // current active SSE client channel
		rsInitDone  = make(chan struct{}) // closed when Initialize has completed
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		rsMu.Lock()
		rsConns++
		connNum := rsConns
		rsMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send endpoint event so the client knows where to POST.
		postURL := "http://" + r.Host + "/messages"
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postURL)
		flusher.Flush()

		ch := make(chan string, 20)
		rsMu.Lock()
		rsCurrentCh = ch
		rsMu.Unlock()

		if connNum == 1 {
			// First connection: serve until Initialize completes, then drop it.
			// Stream any queued responses first, then wait for the "init done" signal.
			for {
				select {
				case msg := <-ch:
					fmt.Fprintf(w, "data: %s\n\n", msg)
					flusher.Flush()
				case <-rsInitDone:
					// Initialize is complete; drop the connection to trigger reconnect.
					return
				case <-r.Context().Done():
					return
				}
			}
		}

		// Second+ connection: stay alive until client disconnects.
		for {
			select {
			case msg := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)

		result, rpcErr := defaultRequestHandler(req.Method, req.Params)
		var resp interface{}
		if rpcErr != nil {
			resp = map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "error": rpcErr}
		} else {
			resp = map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result}
		}
		data, _ := json.Marshal(resp)

		rsMu.Lock()
		ch := rsCurrentCh
		rsMu.Unlock()
		if ch != nil {
			select {
			case ch <- string(data):
			default:
			}
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := mcp.NewHTTPClient(srv.URL, "", "reconnect-test")
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize on first connection — this exercises the normal path.
	require.NoError(t, client.Initialize(ctx))

	// Signal the server to drop the first SSE connection.
	close(rsInitDone)

	// Wait for the second SSE connection to be established.
	require.Eventually(t, func() bool {
		rsMu.Lock()
		n := rsConns
		rsMu.Unlock()
		return n >= 2
	}, 5*time.Second, 50*time.Millisecond, "client did not reconnect within 5s")

	// After reconnect, tool calls must still succeed.
	tools, err := client.ListTools(ctx)
	require.NoError(t, err, "ListTools should succeed after reconnect")
	require.NotEmpty(t, tools)
	assert.Equal(t, "mcp_reconnect_test_query_db", tools[0].Name)
}

// TestHTTPClient_ManagerIntegration verifies the Manager creates an HTTPClient for transport="http".

// TestHTTPClient_ManagerIntegration verifies the Manager creates an HTTPClient for transport="http".
func TestHTTPClient_ManagerIntegration(t *testing.T) {
	srv := newTestSSEServer(t)

	mgr := mcp.NewManager()

	cfg := mcp.MCPServerConfig{
		Name:      "remote-db",
		Transport: "http",
		URL:       srv.URL(),
		AuthToken: "",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.RegisterFromConfig(ctx, cfg)
	require.NoError(t, err)

	tools := mgr.AllTools(ctx)
	require.NotEmpty(t, tools)
	assert.True(t, strings.HasPrefix(tools[0].Name, "mcp_remote_db_"))

	require.NoError(t, mgr.Close())
}
