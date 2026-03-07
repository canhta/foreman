package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// Transport is the low-level message transport for JSON-RPC 2.0 communication.
type Transport interface {
	Send(msg json.RawMessage) error
	Receive() (json.RawMessage, error)
	Close() error
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	Params  interface{} `json:"params,omitempty"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	ID      int64       `json:"id,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	Error   *jsonRPCError   `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	ID      int64           `json:"id"`
}

type jsonRPCError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// pendingRequest tracks an in-flight JSON-RPC request.
type pendingRequest struct {
	resp chan jsonRPCResponse
}

// StdioClientOptions configures optional behavior for a StdioClient.
type StdioClientOptions struct {
	// RestartPolicy controls what happens after 3 consecutive ping failures.
	// "restart" calls Restart(); anything else (including "" or "none") just marks unhealthy.
	RestartPolicy string
	// HealthCheckIntervalSecs is how often to send a ping. 0 = disabled.
	HealthCheckIntervalSecs int
	// PingTimeoutSecs is how long to wait for a ping response before counting it
	// as a failure. 0 means use the default (5 seconds).
	PingTimeoutSecs int
}

// healthState tracks the health of a StdioClient.
type healthState struct {
	mu                  sync.RWMutex
	healthy             bool
	consecutiveFailures int
}

// StdioClient implements the Client interface over JSON-RPC 2.0 via a Transport.
type StdioClient struct {
	transport    Transport
	capabilities map[string]json.RawMessage
	readerDone   chan struct{}
	stopHealth   chan struct{}
	early        sync.Map
	pending      sync.Map
	serverName   string
	opts         StdioClientOptions
	health       healthState
	nextID       atomic.Int64
	stopOnce     sync.Once
	writeMu      sync.Mutex
	initialized  bool
}

// NewStdioClientWithTransport creates a new StdioClient with the given transport.
// Health checks are disabled by default. Use NewStdioClientWithTransportAndOptions
// to enable health monitoring.
func NewStdioClientWithTransport(t Transport, serverName string) *StdioClient {
	return NewStdioClientWithTransportAndOptions(t, serverName, StdioClientOptions{})
}

// NewStdioClientWithTransportAndOptions creates a new StdioClient with the given
// transport and optional health-check configuration. When
// opts.HealthCheckIntervalSecs > 0, a goroutine is launched immediately that
// sends a JSON-RPC "ping" every HealthCheckIntervalSecs seconds. After 3
// consecutive ping failures (timeout or error) the client is marked unhealthy.
// A successful ping resets the failure counter and marks the client healthy.
func NewStdioClientWithTransportAndOptions(t Transport, serverName string, opts StdioClientOptions) *StdioClient {
	c := &StdioClient{
		transport:  t,
		serverName: serverName,
		readerDone: make(chan struct{}),
		stopHealth: make(chan struct{}),
		opts:       opts,
	}
	c.health.healthy = true // start healthy until proven otherwise
	go c.readLoop()
	if opts.HealthCheckIntervalSecs > 0 {
		go c.healthLoop()
	}
	return c
}

// IsHealthy reports whether the server passed its most recent health check.
// Returns true for clients with health checks disabled (no false alarms).
func (c *StdioClient) IsHealthy() bool {
	c.health.mu.RLock()
	defer c.health.mu.RUnlock()
	return c.health.healthy
}

// pingTimeoutSecs returns the effective ping timeout in seconds (default 5).
func (c *StdioClient) pingTimeoutSecs() int {
	if c.opts.PingTimeoutSecs > 0 {
		return c.opts.PingTimeoutSecs
	}
	return 5
}

// healthLoop runs the periodic ping loop. It exits when stopHealth is closed.
func (c *StdioClient) healthLoop() {
	interval := time.Duration(c.opts.HealthCheckIntervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopHealth:
			return
		case <-ticker.C:
			c.runPing()
		}
	}
}

// runPing sends a single ping and updates the health state.
// After 3 consecutive failures, if restart_policy is "restart", Restart() is called
// and the failure counter is reset. Otherwise the client is simply marked unhealthy.
func (c *StdioClient) runPing() {
	timeout := time.Duration(c.pingTimeoutSecs()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err := c.sendRequest(ctx, "ping", map[string]interface{}{})

	c.health.mu.Lock()

	if err != nil {
		c.health.consecutiveFailures++
		if c.health.consecutiveFailures >= 3 {
			c.health.healthy = false
			if c.opts.RestartPolicy == "restart" {
				c.health.consecutiveFailures = 0
				c.health.mu.Unlock()
				restartCtx, restartCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer restartCancel()
				_ = c.Restart(restartCtx)
				return
			}
		}
	} else {
		c.health.consecutiveFailures = 0
		c.health.healthy = true
	}

	c.health.mu.Unlock()
}

// readLoop reads responses from the transport and routes them to pending requests.
func (c *StdioClient) readLoop() {
	defer close(c.readerDone)
	for {
		msg, err := c.transport.Receive()
		if err != nil {
			return
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}
		// Route to pending request
		if val, ok := c.pending.LoadAndDelete(resp.ID); ok {
			if pr, ok := val.(*pendingRequest); ok {
				pr.resp <- resp
			}
			continue
		}
		// Response arrived before the caller could observe pending registration.
		// Buffer it so sendRequest can consume it immediately.
		c.early.Store(resp.ID, resp)
	}
}

// sendRequest sends a JSON-RPC request and waits for the response.
func (c *StdioClient) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
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

	c.writeMu.Lock()
	err = c.transport.Send(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Handle the edge case where readLoop consumed the response before this
	// goroutine entered its select.
	if val, ok := c.early.LoadAndDelete(id); ok {
		if resp, ok := val.(jsonRPCResponse); ok {
			if resp.Error != nil {
				return nil, fmt.Errorf("jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

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

// sendNotification sends a JSON-RPC notification (no ID, no response expected).
func (c *StdioClient) sendNotification(method string, params interface{}) error {
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
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.transport.Send(data)
}

// Initialize sends the initialize request and caches server capabilities.
func (c *StdioClient) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "foreman",
			"version": "1.0.0",
		},
	}

	result, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var initResult struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	c.capabilities = initResult.Capabilities
	c.initialized = true

	// Send initialized notification
	_ = c.sendNotification("notifications/initialized", nil)

	return nil
}

// HasCapability reports whether the server advertised the named capability.
func (c *StdioClient) HasCapability(name string) bool {
	if c.capabilities == nil {
		return false
	}
	_, ok := c.capabilities[name]
	return ok
}

// ListTools calls tools/list and returns normalized ToolDefs.
func (c *StdioClient) ListTools(ctx context.Context) ([]models.ToolDef, error) {
	result, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
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

// Call invokes a tool on the MCP server.
func (c *StdioClient) Call(ctx context.Context, name string, input json.RawMessage) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": input,
	}

	result, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return "", err
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

	// Concatenate text content
	var text string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return text, nil
}

// Close sends shutdown/exit notifications per the MCP spec, then closes the transport.
// It also stops the health-check goroutine if one is running.
func (c *StdioClient) Close() error {
	// Stop the health-check goroutine (if any).
	c.stopOnce.Do(func() { close(c.stopHealth) })
	// Send shutdown request (best effort, short timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = c.sendRequest(ctx, "shutdown", nil)
	cancel()
	// Send exit notification (no response expected)
	_ = c.sendNotification("exit", nil)
	err := c.transport.Close()
	<-c.readerDone
	return err
}

// Restart stops the client and restarts it with a fresh transport connection.
// It calls Stop (which closes the transport and stops the health loop) then Start
// (which resets internal state and relaunches the read and health loops).
// This is called automatically when restart_policy="restart" and 3 consecutive
// ping failures have occurred.
func (c *StdioClient) Restart(ctx context.Context) error {
	// Tear down existing connection.
	if err := c.Stop(); err != nil {
		return fmt.Errorf("restart stop: %w", err)
	}
	// Restart with a fresh state.
	return c.Start(ctx)
}

// Stop closes the transport, stops the health-check goroutine, and waits for the
// read loop to finish. After Stop, the StdioClient can be restarted via Start.
func (c *StdioClient) Stop() error {
	c.stopOnce.Do(func() { close(c.stopHealth) })
	err := c.transport.Close()
	<-c.readerDone
	return err
}

// Start resets internal state and relaunches the read and health-check goroutines.
// It should only be called after Stop (or as part of Restart).
func (c *StdioClient) Start(_ context.Context) error {
	// Reset channels and state for the new session.
	c.readerDone = make(chan struct{})
	c.stopHealth = make(chan struct{})
	c.stopOnce = sync.Once{}

	c.health.mu.Lock()
	c.health.healthy = true
	c.health.consecutiveFailures = 0
	c.health.mu.Unlock()

	go c.readLoop()
	if c.opts.HealthCheckIntervalSecs > 0 {
		go c.healthLoop()
	}
	return nil
}

// MCPResourceDef describes a single resource exposed by an MCP server.
type MCPResourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// ListResources calls resources/list and returns all available resources.
// If the server doesn't implement resources (-32601), an empty list is returned.
func (c *StdioClient) ListResources(ctx context.Context) ([]MCPResourceDef, error) {
	result, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		// -32601 = method not found: server doesn't support resources
		if strings.Contains(err.Error(), "-32601") {
			return []MCPResourceDef{}, nil
		}
		return nil, err
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
// If the content is base64-encoded blob, it is decoded to a string.
func (c *StdioClient) ReadResource(ctx context.Context, uri string) (string, error) {
	params := map[string]interface{}{
		"uri": uri,
	}

	result, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return "", err
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
