package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// pendingRequest tracks an in-flight JSON-RPC request.
type pendingRequest struct {
	resp chan jsonRPCResponse
}

// StdioClient implements the Client interface over JSON-RPC 2.0 via a Transport.
type StdioClient struct {
	transport    Transport
	serverName   string
	nextID       atomic.Int64
	pending      sync.Map // map[int64]*pendingRequest
	writeMu      sync.Mutex
	capabilities map[string]json.RawMessage
	initialized  bool
	readerDone   chan struct{}
}

// NewStdioClientWithTransport creates a new StdioClient with the given transport.
func NewStdioClientWithTransport(t Transport, serverName string) *StdioClient {
	c := &StdioClient{
		transport:  t,
		serverName: serverName,
		readerDone: make(chan struct{}),
	}
	go c.readLoop()
	return c
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
			pr := val.(*pendingRequest)
			pr.resp <- resp
		}
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
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
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
		"arguments": json.RawMessage(input),
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
func (c *StdioClient) Close() error {
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
