package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHealthInterval = 20 * time.Millisecond
	testPingTimeout    = 20 * time.Millisecond
	testRestartTimeout = 500 * time.Millisecond
)

func newFastHealthClient(mt *mockTransport, restartPolicy string) *mcp.StdioClient {
	return mcp.NewStdioClientWithTransportAndOptions(mt, "test-server", mcp.StdioClientOptions{
		HealthCheckInterval: testHealthInterval,
		PingTimeout:         testPingTimeout,
		RestartTimeout:      testRestartTimeout,
		RestartPolicy:       restartPolicy,
	})
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-deadline:
			t.Fatal(message)
		}
	}
}

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
	client := newFastHealthClient(mt, "")
	defer client.Close()

	waitUntil(t, 1*time.Second, func() bool { return !client.IsHealthy() }, "client was not marked unhealthy after failed pings")
}

// TestStdioClient_HealthCheck_RecoverOnSuccess verifies that after being marked
// unhealthy, a successful ping resets the failure counter and marks the client healthy.
func TestStdioClient_HealthCheck_RecoverOnSuccess(t *testing.T) {
	mt := newMockTransport()
	client := newFastHealthClient(mt, "")
	defer client.Close()

	waitUntil(t, 1*time.Second, func() bool { return !client.IsHealthy() }, "client not unhealthy in time")

	// Now start responding to pings so the next successful ping resets the counter.
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case req, ok := <-mt.requests:
				if !ok {
					return
				}
				var parsed struct {
					Method string          `json:"method"`
					ID     json.RawMessage `json:"id"`
				}
				if json.Unmarshal(req, &parsed) == nil && parsed.Method == "ping" {
					resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, string(parsed.ID))
					mt.responses <- json.RawMessage(resp)
				}
			case <-done:
				return
			}
		}
	}()

	waitUntil(t, 1*time.Second, func() bool { return client.IsHealthy() }, "client did not recover to healthy after successful ping")
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

// TestMCPServerConfig_EffectiveRestartPolicy_DefaultIsNone verifies that the
// default restart policy is "none" per the spec (not "on-failure").
func TestMCPServerConfig_EffectiveRestartPolicy_DefaultIsNone(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "test"}
	assert.Equal(t, "none", cfg.EffectiveRestartPolicy())
}

// TestMCPServerConfig_EffectiveRestartPolicy_RestartOverride verifies that
// setting restart_policy="restart" is returned correctly.
func TestMCPServerConfig_EffectiveRestartPolicy_RestartOverride(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "test", RestartPolicy: "restart"}
	assert.Equal(t, "restart", cfg.EffectiveRestartPolicy())
}

// TestStdioClient_RestartPolicy_RestartTriggeredAfter3Failures verifies that when
// restart_policy="restart", after 3 consecutive ping failures the client calls
// Restart() (Stop+Start cycle) and resets health state to healthy.
//
// Detection strategy: wrap the mock transport to count Close() invocations.
// A Restart() always calls Stop() → transport.Close(), so observing a Close()
// after the pings start proves Restart was triggered.
func TestStdioClient_RestartPolicy_RestartTriggeredAfter3Failures(t *testing.T) {
	mt := newMockTransport()

	// closedCh is closed the first time transport.Close() is called (during Restart).
	closedCh := make(chan struct{})
	var closeOnce sync.Once
	mt.onClose = func() {
		closeOnce.Do(func() { close(closedCh) })
	}

	client := mcp.NewStdioClientWithTransportAndOptions(mt, "test-server", mcp.StdioClientOptions{
		HealthCheckInterval: testHealthInterval,
		PingTimeout:         testPingTimeout,
		RestartTimeout:      testRestartTimeout,
		RestartPolicy:       "restart",
	})

	select {
	case <-closedCh:
		// Restart was triggered — success.
	case <-time.After(2 * time.Second):
		client.Close()
		t.Fatal("Restart() was not triggered in time (transport.Close never called)")
	}

	waitUntil(t, 1*time.Second, func() bool { return client.IsHealthy() }, "client did not recover to healthy after Restart()")
	client.Close()
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
		HealthCheckInterval: testHealthInterval,
		PingTimeout:         testPingTimeout,
	})

	mgr := mcp.NewManager()
	mgr.RegisterClient("bad-server", client)

	waitUntil(t, 1*time.Second, func() bool {
		status := mgr.HealthStatus()
		return !status["bad-server"]
	}, "manager HealthStatus never reflected unhealthy server")
	require.NoError(t, mgr.Close())
}
