package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport implements mcp.Transport using channels.
type mockTransport struct {
	// requests sent by the client are captured here
	requests chan json.RawMessage
	// responses to be returned by Receive
	responses chan json.RawMessage
	closed    bool
	mu        sync.Mutex
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		requests:  make(chan json.RawMessage, 100),
		responses: make(chan json.RawMessage, 100),
	}
}

func (m *mockTransport) Send(msg json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("transport closed")
	}
	m.requests <- msg
	return nil
}

func (m *mockTransport) Receive() (json.RawMessage, error) {
	msg, ok := <-m.responses
	if !ok {
		return nil, fmt.Errorf("transport closed")
	}
	return msg, nil
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.responses)
	}
	return nil
}

// queueResponse adds a JSON-RPC response to the mock transport.
func (m *mockTransport) queueResponse(id int64, result interface{}) {
	resultBytes, _ := json.Marshal(result)
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, id, string(resultBytes))
	m.responses <- json.RawMessage(resp)
}

func TestStdioClient_Initialize(t *testing.T) {
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransport(mt, "test-server")

	// Queue initialize response
	mt.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "test",
			"version": "1.0",
		},
	})

	// Queue notifications/initialized response (client sends notification, no response needed)
	// The initialize call sends the request, gets the response, then sends notification

	err := client.Initialize(context.Background())
	require.NoError(t, err)

	assert.True(t, client.HasCapability("tools"))
	assert.False(t, client.HasCapability("resources"))

	require.NoError(t, client.Close())
}

func TestStdioClient_ListTools(t *testing.T) {
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransport(mt, "test-server")

	// Queue initialize response
	mt.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	})

	require.NoError(t, client.Initialize(context.Background()))

	// Queue tools/list response
	mt.queueResponse(2, map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "read_file",
				"description": "Reads a file",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	})

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "mcp_test_server_read_file", tools[0].Name)
	assert.Equal(t, "Reads a file", tools[0].Description)

	require.NoError(t, client.Close())
}

func TestStdioClient_ConcurrentCalls(t *testing.T) {
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransport(mt, "srv")

	// Queue initialize response
	mt.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "srv", "version": "1.0"},
	})
	require.NoError(t, client.Initialize(context.Background()))

	// Drain the initialize request + notification from the requests channel
	for len(mt.requests) > 0 {
		<-mt.requests
	}

	// Launch 3 concurrent calls
	var wg sync.WaitGroup
	results := make([]string, 3)
	errs := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			input, _ := json.Marshal(map[string]string{"key": fmt.Sprintf("val%d", idx)})
			results[idx], errs[idx] = client.Call(context.Background(), "tool_a", json.RawMessage(input))
		}(i)
	}

	// Wait for requests to arrive, then respond out of order
	// Read 3 requests (Call requests only)
	var ids []int64
	for i := 0; i < 3; i++ {
		req := <-mt.requests
		var parsed struct {
			ID int64 `json:"id"`
		}
		json.Unmarshal(req, &parsed)
		ids = append(ids, parsed.ID)
	}

	// Respond in reverse order
	for i := len(ids) - 1; i >= 0; i-- {
		mt.queueResponse(ids[i], map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("result_%d", ids[i])},
			},
		})
	}

	wg.Wait()

	for i := 0; i < 3; i++ {
		require.NoError(t, errs[i], "call %d should succeed", i)
		assert.NotEmpty(t, results[i], "call %d should have result", i)
	}

	require.NoError(t, client.Close())
}
