package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// HTTPClient implements the Client interface over HTTP+SSE (MCP SSE transport).
//
// Protocol:
//   - GET <baseURL>/sse → SSE stream for receiving JSON-RPC responses
//   - POST <endpointURL> → sends JSON-RPC requests (URL provided by first SSE event)
//   - Authorization: Bearer <token> header on all requests
type HTTPClient struct {
	sseCtx        context.Context
	sseCancel     context.CancelFunc
	httpClient    *http.Client
	sseDone       chan struct{}
	endpointReady chan struct{} // closed when endpoint is set; re-created on reconnect
	pending       sync.Map
	endpoint      string // guarded by mu; reset to "" on each SSE reconnect
	serverName    string
	authToken     string
	baseURL       string
	nextID        atomic.Int64
	mu            sync.RWMutex // guards endpoint and endpointReady
}

// NewHTTPClient creates a new HTTPClient for the given server.
// baseURL is the root URL (e.g. "https://mcp.example.com").
// authToken is optional; if non-empty it is sent as "Authorization: Bearer <token>".
// serverName is used for tool name prefixing (same as StdioClient).
func NewHTTPClient(baseURL, authToken, serverName string) *HTTPClient {
	ctx, cancel := context.WithCancel(context.Background())
	c := &HTTPClient{
		httpClient:    &http.Client{Timeout: 0}, // no timeout — SSE streams are long-lived
		baseURL:       baseURL,
		authToken:     authToken,
		serverName:    serverName,
		sseCtx:        ctx,
		sseCancel:     cancel,
		sseDone:       make(chan struct{}),
		endpointReady: make(chan struct{}),
	}
	go c.sseLoop()
	return c
}

// sseLoop maintains the SSE connection and routes incoming messages to pending requests.
func (c *HTTPClient) sseLoop() {
	defer close(c.sseDone)

	url := c.baseURL + "/sse"
	first := true
	for {
		if c.sseCtx.Err() != nil {
			return
		}

		// On reconnect (not the first attempt) reset endpoint and create a fresh
		// endpointReady channel so waitForEndpoint callers block until the new
		// "endpoint" event arrives. On the first attempt the channel was already
		// created by NewHTTPClient.
		if !first {
			c.mu.Lock()
			c.endpoint = ""
			c.endpointReady = make(chan struct{})
			c.mu.Unlock()
		}
		first = false

		err := c.connectSSE(url)

		// Drain all in-flight requests — they are bound to the now-dead SSE stream.
		// Callers receive a retriable error rather than hanging until context expiry.
		c.pending.Range(func(key, val interface{}) bool {
			if pr, ok := val.(*pendingRequest); ok {
				select {
				case pr.resp <- jsonRPCResponse{Error: &jsonRPCError{Code: -32000, Message: "SSE connection lost, retry"}}:
				default:
				}
			}
			c.pending.Delete(key)
			return true
		})

		if c.sseCtx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("url", url).Msg("mcp/http: SSE connection error, reconnecting")
			select {
			case <-c.sseCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// connectSSE opens one SSE connection and reads until it's closed or errors.
func (c *HTTPClient) connectSSE(url string) error {
	req, err := http.NewRequestWithContext(c.sseCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect SSE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE connect: unexpected status %d", resp.StatusCode)
	}

	return c.readSSEStream(resp.Body)
}

// readSSEStream parses SSE events from the response body.
// SSE format: lines of "field: value", events separated by blank lines.
func (c *HTTPClient) readSSEStream(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	// Increase buffer to 1 MB to handle large tool results delivered via SSE.
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// End of event — process it
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				c.handleSSEEvent(eventType, data)
			}
			eventType = ""
			dataLines = nil
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		// Ignore other fields (id:, retry:, comments)
	}

	if err := scanner.Err(); err != nil {
		if c.sseCtx.Err() != nil {
			return nil // context cancelled — not an error
		}
		return fmt.Errorf("SSE stream error: %w", err)
	}
	return nil
}

// handleSSEEvent processes a single SSE event.
func (c *HTTPClient) handleSSEEvent(eventType, data string) {
	switch eventType {
	case "endpoint":
		// First event on each (re)connection: data is the POST URL to use.
		// We use a mutex-protected field (not sync.Once) so that the URL is
		// updated on every reconnect — servers may rotate the POST URL.
		url := strings.TrimSpace(data)
		c.mu.Lock()
		c.endpoint = url
		ready := c.endpointReady
		c.mu.Unlock()
		// Signal waiters only once per ready channel.
		select {
		case <-ready:
			// already closed — no-op (shouldn't happen in normal flow)
		default:
			close(ready)
		}
	default:
		// Assume it's a JSON-RPC response (eventType may be "" or "message")
		if data == "" {
			return
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			log.Debug().Err(err).Str("data", data).Msg("mcp/http: failed to parse SSE data")
			return
		}
		if val, ok := c.pending.LoadAndDelete(resp.ID); ok {
			if pr, ok := val.(*pendingRequest); ok {
				pr.resp <- resp
			}
		}
	}
}

// waitForEndpoint waits until the SSE endpoint URL is known.
func (c *HTTPClient) waitForEndpoint(ctx context.Context) (string, error) {
	c.mu.RLock()
	ready := c.endpointReady
	c.mu.RUnlock()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-ready:
		c.mu.RLock()
		ep := c.endpoint
		c.mu.RUnlock()
		return ep, nil
	}
}

// sendRequest sends a JSON-RPC request over HTTP POST and waits for the response on SSE.
func (c *HTTPClient) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	endpointURL, err := c.waitForEndpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for SSE endpoint: %w", err)
	}

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	pr := &pendingRequest{resp: make(chan jsonRPCResponse, 1)}
	c.pending.Store(id, pr)
	defer c.pending.Delete(id)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("POST request: %w", err)
	}
	httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("POST unexpected status: %d", httpResp.StatusCode)
	}

	// Wait for the response to arrive on the SSE stream
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-pr.resp:
		if resp.Error != nil {
			return nil, fmt.Errorf("jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *HTTPClient) sendNotification(ctx context.Context, method string, params interface{}) error {
	endpointURL, err := c.waitForEndpoint(ctx)
	if err != nil {
		return fmt.Errorf("wait for SSE endpoint: %w", err)
	}

	req := struct {
		Params  interface{} `json:"params,omitempty"`
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create notification POST: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("notification POST: %w", err)
	}
	httpResp.Body.Close()
	return nil
}

// Initialize sends the MCP initialize request and waits for the server's capabilities.
func (c *HTTPClient) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "foreman",
			"version": "1.0.0",
		},
	}

	_, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification (best effort)
	_ = c.sendNotification(ctx, "notifications/initialized", nil)

	return nil
}

// ListTools calls tools/list and returns normalized ToolDefs.
func (c *HTTPClient) ListTools(ctx context.Context) ([]models.ToolDef, error) {
	result, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var listResult struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	defs := make([]models.ToolDef, len(listResult.Tools))
	for i, t := range listResult.Tools {
		defs[i] = models.ToolDef{
			Name:        MCPToolName(c.serverName, t.Name),
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return defs, nil
}

// Call invokes a tool on the MCP server over HTTP.
func (c *HTTPClient) Call(ctx context.Context, name string, input json.RawMessage) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": input,
	}

	result, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("tools/call: %w", err)
	}

	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &callResult); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}

	var text string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return text, nil
}

// Close shuts down the HTTP client and cancels the SSE connection.
func (c *HTTPClient) Close() error {
	// Send shutdown (best effort)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.sendRequest(ctx, "shutdown", nil)

	c.sseCancel()
	<-c.sseDone
	return nil
}

// ListResources calls resources/list and returns all available resources.
// If the server doesn't implement resources (-32601), an empty list is returned.
func (c *HTTPClient) ListResources(ctx context.Context) ([]MCPResourceDef, error) {
	result, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		if strings.Contains(err.Error(), "-32601") {
			return []MCPResourceDef{}, nil
		}
		return nil, fmt.Errorf("resources/list: %w", err)
	}

	var listResult struct {
		Resources []MCPResourceDef `json:"resources"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse resources/list: %w", err)
	}
	if listResult.Resources == nil {
		return []MCPResourceDef{}, nil
	}
	return listResult.Resources, nil
}

// ReadResource calls resources/read for the given URI and returns the text content.
func (c *HTTPClient) ReadResource(ctx context.Context, uri string) (string, error) {
	params := map[string]interface{}{
		"uri": uri,
	}

	result, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return "", fmt.Errorf("resources/read: %w", err)
	}

	var readResult struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
			Blob string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(result, &readResult); err != nil {
		return "", fmt.Errorf("parse resources/read: %w", err)
	}

	if len(readResult.Contents) == 0 {
		return "", fmt.Errorf("resources/read: no content returned for %q", uri)
	}

	item := readResult.Contents[0]
	if item.Text != "" {
		return item.Text, nil
	}
	if item.Blob != "" {
		decoded, err := base64.StdEncoding.DecodeString(item.Blob)
		if err != nil {
			return "", fmt.Errorf("resources/read: base64 decode blob: %w", err)
		}
		return string(decoded), nil
	}

	return "", nil
}
