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

// StdioClient implements the Client interface over JSON-RPC 2.0 via a Transport.
type StdioClient struct {
	transport    Transport
	capabilities map[string]json.RawMessage
	readerDone   chan struct{}
	// early stores responses that arrived before sendRequest observed its pending entry.
	// Key: request ID (int64), Value: jsonRPCResponse.
	early       sync.Map
	pending     sync.Map
	serverName  string
	nextID      atomic.Int64
	writeMu     sync.Mutex
	initialized bool
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
