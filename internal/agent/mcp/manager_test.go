package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// respondAfterRequest waits for a request on the mock transport, then queues the response.
func respondAfterRequest(mt *mockTransport, result interface{}) {
	go func() {
		// Wait for request to arrive
		req := <-mt.requests
		var parsed struct {
			ID int64 `json:"id"`
		}
		json.Unmarshal(req, &parsed)
		mt.queueResponse(parsed.ID, result)
	}()
}

func TestManager_AllTools(t *testing.T) {
	mt1 := newMockTransport()
	c1 := mcp.NewStdioClientWithTransport(mt1, "server-a")

	// Initialize server-a
	mt1.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "a", "version": "1.0"},
	})
	require.NoError(t, c1.Initialize(context.Background()))

	mt2 := newMockTransport()
	c2 := mcp.NewStdioClientWithTransport(mt2, "server-b")

	// Initialize server-b
	mt2.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "b", "version": "1.0"},
	})
	require.NoError(t, c2.Initialize(context.Background()))

	// Drain initialize/notification requests
	time.Sleep(10 * time.Millisecond)
	for len(mt1.requests) > 0 {
		<-mt1.requests
	}
	for len(mt2.requests) > 0 {
		<-mt2.requests
	}

	// Set up auto-responses for tools/list
	respondAfterRequest(mt1, map[string]interface{}{
		"tools": []map[string]interface{}{
			{"name": "tool1", "description": "Tool 1", "inputSchema": map[string]interface{}{"type": "object"}},
		},
	})
	respondAfterRequest(mt2, map[string]interface{}{
		"tools": []map[string]interface{}{
			{"name": "tool2", "description": "Tool 2", "inputSchema": map[string]interface{}{"type": "object"}},
		},
	})

	mgr := mcp.NewManager()
	mgr.RegisterClient("server-a", c1)
	mgr.RegisterClient("server-b", c2)

	tools := mgr.AllTools(context.Background())
	assert.Len(t, tools, 2)

	// Check tool names are properly prefixed
	names := make(map[string]bool)
	for _, td := range tools {
		names[td.Name] = true
	}
	assert.True(t, names["mcp_server_a_tool1"])
	assert.True(t, names["mcp_server_b_tool2"])

	require.NoError(t, mgr.Close())
}

func TestManager_IsMCPTool(t *testing.T) {
	mgr := mcp.NewManager()
	assert.True(t, mgr.IsMCPTool("mcp_server_tool"))
	assert.False(t, mgr.IsMCPTool("read_file"))
}

func TestManager_CallTool(t *testing.T) {
	mt := newMockTransport()
	c := mcp.NewStdioClientWithTransport(mt, "myserver")

	// Initialize
	mt.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "myserver", "version": "1.0"},
	})
	require.NoError(t, c.Initialize(context.Background()))

	// Drain init requests
	time.Sleep(10 * time.Millisecond)
	for len(mt.requests) > 0 {
		<-mt.requests
	}

	mgr := mcp.NewManager()
	mgr.RegisterClient("myserver", c)

	// Set up auto-response for tools/call
	respondAfterRequest(mt, map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": "hello world"},
		},
	})

	result, err := mgr.CallTool(context.Background(), "mcp_myserver_do_thing", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)

	require.NoError(t, mgr.Close())
}

func TestManager_CallTool_UnknownServer(t *testing.T) {
	mgr := mcp.NewManager()
	_, err := mgr.CallTool(context.Background(), "mcp_unknown_tool", json.RawMessage(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no MCP server")
}
