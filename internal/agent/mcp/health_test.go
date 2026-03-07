package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStdioClient_IsHealthy_InitiallyHealthy verifies that a newly-created
// StdioClient reports healthy before any health checks run.
func TestStdioClient_IsHealthy_InitiallyHealthy(t *testing.T) {
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransport(mt, "test-server")
	defer client.Close()

	assert.True(t, client.IsHealthy(), "new client should start healthy")
}

// TestStdioClient_HealthCheck_MarksUnhealthyAfter3FailedPings verifies that
// after 3 consecutive ping timeouts the client is marked unhealthy.
func TestStdioClient_HealthCheck_MarksUnhealthyAfter3FailedPings(t *testing.T) {
	mt := newMockTransport()
	// Use a short health check interval so the test runs fast.
	client := mcp.NewStdioClientWithTransportAndOptions(mt, "test-server", mcp.StdioClientOptions{
		HealthCheckIntervalSecs: 1, // 1 second per tick
		PingTimeoutSecs:         1, // 1 second ping timeout
	})

	// Do NOT respond to any pings — they will all time out.

	// Wait long enough for 3 failed pings (3 × (interval + timeout) + buffer).
	// Each tick: wait 1s, send ping, wait up to 1s timeout → ~2s per cycle.
	// 3 cycles = ~6s. We use 10s to be safe.
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !client.IsHealthy() {
				// Marked unhealthy — test passes
				require.NoError(t, client.Close())
				return
			}
		case <-deadline:
			require.NoError(t, client.Close())
			t.Fatal("client was not marked unhealthy after 3 failed pings within 10s")
		}
	}
}

// TestStdioClient_HealthCheck_RecoverOnSuccess verifies that after being marked
// unhealthy, a successful ping resets the failure counter and marks the client healthy.
func TestStdioClient_HealthCheck_RecoverOnSuccess(t *testing.T) {
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransportAndOptions(mt, "test-server", mcp.StdioClientOptions{
		HealthCheckIntervalSecs: 1,
		PingTimeoutSecs:         1,
	})

	// Let the first 3 pings time out to get the client into unhealthy state.
	unhealthyDeadline := time.After(10 * time.Second)
	pollTicker := time.NewTicker(200 * time.Millisecond)
	defer pollTicker.Stop()

WaitUnhealthy:
	for {
		select {
		case <-pollTicker.C:
			if !client.IsHealthy() {
				break WaitUnhealthy
			}
		case <-unhealthyDeadline:
			client.Close()
			t.Fatal("client not unhealthy within 10s")
		}
	}

	// Now start responding to pings so the next successful ping resets the counter.
	go func() {
		for {
			select {
			case req := <-mt.requests:
				var parsed struct {
					Method string          `json:"method"`
					ID     json.RawMessage `json:"id"`
				}
				if json.Unmarshal(req, &parsed) == nil && parsed.Method == "ping" {
					resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, string(parsed.ID))
					mt.responses <- json.RawMessage(resp)
				}
			case <-mt.responses:
				// drain stray messages
			}
		}
	}()

	recoveryDeadline := time.After(5 * time.Second)
	for {
		select {
		case <-pollTicker.C:
			if client.IsHealthy() {
				require.NoError(t, client.Close())
				return
			}
		case <-recoveryDeadline:
			client.Close()
			t.Fatal("client did not recover to healthy after successful ping within 5s")
		}
	}
}

// TestStdioClient_MCPServerConfig_HealthCheckIntervalDefault verifies the default value.
func TestStdioClient_MCPServerConfig_HealthCheckIntervalDefault(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "test"}
	assert.Equal(t, 30, cfg.EffectiveHealthCheckIntervalSecs())
}

// TestStdioClient_MCPServerConfig_HealthCheckIntervalOverride verifies override.
func TestStdioClient_MCPServerConfig_HealthCheckIntervalOverride(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "test", HealthCheckIntervalSecs: 60}
	assert.Equal(t, 60, cfg.EffectiveHealthCheckIntervalSecs())
}

// TestManager_HealthStatus_ExposesAllServers verifies that Manager.HealthStatus
// returns a map of server name → is_healthy for all registered servers.
func TestManager_HealthStatus_ExposesAllServers(t *testing.T) {
	mt1 := newMockTransport()
	c1 := mcp.NewStdioClientWithTransport(mt1, "server-a")

	mt1.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"serverInfo":      map[string]interface{}{"name": "a", "version": "1.0"},
	})
	require.NoError(t, c1.Initialize(context.Background()))

	mt2 := newMockTransport()
	c2 := mcp.NewStdioClientWithTransport(mt2, "server-b")

	mt2.queueResponse(1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"serverInfo":      map[string]interface{}{"name": "b", "version": "1.0"},
	})
	require.NoError(t, c2.Initialize(context.Background()))

	mgr := mcp.NewManager()
	mgr.RegisterClient("server-a", c1)
	mgr.RegisterClient("server-b", c2)

	status := mgr.HealthStatus()

	assert.Contains(t, status, "server-a", "HealthStatus should contain server-a")
	assert.Contains(t, status, "server-b", "HealthStatus should contain server-b")
	// Both are freshly created, so they should be healthy
	assert.True(t, status["server-a"], "server-a should be healthy")
	assert.True(t, status["server-b"], "server-b should be healthy")

	require.NoError(t, mgr.Close())
}

// TestManager_HealthStatus_ReflectsUnhealthyServer verifies that the health map
// reports false for a client whose IsHealthy() returns false.
func TestManager_HealthStatus_ReflectsUnhealthyServer(t *testing.T) {
	// Use a StdioClient with a short ping timeout that will fail immediately.
	mt := newMockTransport()
	client := mcp.NewStdioClientWithTransportAndOptions(mt, "bad-server", mcp.StdioClientOptions{
		HealthCheckIntervalSecs: 1,
		PingTimeoutSecs:         1,
	})

	mgr := mcp.NewManager()
	mgr.RegisterClient("bad-server", client)

	// Wait for client to become unhealthy
	deadline := time.After(12 * time.Second)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			status := mgr.HealthStatus()
			if !status["bad-server"] {
				// Correctly reports unhealthy
				require.NoError(t, mgr.Close())
				return
			}
		case <-deadline:
			mgr.Close()
			t.Fatal("manager HealthStatus never reflected unhealthy server within 12s")
		}
	}
}
