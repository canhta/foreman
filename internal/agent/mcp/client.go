// Package mcp provides types and interfaces for MCP (Model Context Protocol) integration.
//
// Architecture note: For the Anthropic provider, MCP is handled API-side — pass
// MCPServerConfig.URL in the API request and Anthropic's infrastructure connects
// to the server. No client-side proxy is needed for Anthropic.
// For OpenAI/local providers, the Client interface is the right abstraction for
// client-side MCP proxying.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// MCPServerConfig holds connection config for a single MCP server.
// For Anthropic: set URL + AuthToken — configs are passed to the API request.
// For OpenAI/local: a future Client implementation proxies calls client-side.
type MCPServerConfig struct {
	Env              map[string]string `json:"env,omitempty"`
	MaxRestarts      *int              `json:"max_restarts,omitempty"`
	RestartDelaySecs *int              `json:"restart_delay_secs,omitempty"`
	Name             string            `json:"name"`
	URL              string            `json:"url,omitempty"`
	AuthToken        string            `json:"auth_token,omitempty"`
	Command          string            `json:"command,omitempty"`
	// Transport specifies the MCP transport to use: "stdio" (default) or "http".
	Transport     string   `json:"transport,omitempty"`
	RestartPolicy string   `json:"restart_policy,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	Args          []string `json:"args,omitempty"`
	// HealthCheckIntervalSecs is how often to send a ping to check server health.
	// 0 means use the default (30 seconds). Set to -1 to disable health checks.
	HealthCheckIntervalSecs int `json:"health_check_interval_secs,omitempty"`
}

// EffectiveRestartPolicy returns the restart policy, defaulting to "on-failure".
func (c MCPServerConfig) EffectiveRestartPolicy() string {
	if c.RestartPolicy != "" {
		return c.RestartPolicy
	}
	return "on-failure"
}

// EffectiveHealthCheckIntervalSecs returns the health check interval in seconds,
// defaulting to 30. A value of -1 disables health checks.
func (c MCPServerConfig) EffectiveHealthCheckIntervalSecs() int {
	if c.HealthCheckIntervalSecs != 0 {
		return c.HealthCheckIntervalSecs
	}
	return 30
}

// EffectiveMaxRestarts returns the max restarts, defaulting to 3.
func (c MCPServerConfig) EffectiveMaxRestarts() int {
	if c.MaxRestarts != nil {
		return *c.MaxRestarts
	}
	return 3
}

// EffectiveRestartDelaySecs returns the restart delay in seconds, defaulting to 2.
func (c MCPServerConfig) EffectiveRestartDelaySecs() int {
	if c.RestartDelaySecs != nil {
		return *c.RestartDelaySecs
	}
	return 2
}

// Client is the interface for client-side MCP proxying (OpenAI/local providers).
// For Anthropic, MCP is handled API-side via request params — Client is not used.
type Client interface {
	ListTools(ctx context.Context) ([]models.ToolDef, error)
	Call(ctx context.Context, name string, input json.RawMessage) (string, error)
	ListResources(ctx context.Context) ([]MCPResourceDef, error)
	ReadResource(ctx context.Context, uri string) (string, error)
	Close() error
}

// HealthChecker is an optional interface that clients may implement to expose
// their health state. If a client does not implement this interface, it is
// assumed to be healthy.
type HealthChecker interface {
	IsHealthy() bool
}

// NoopClient satisfies Client but does nothing. Placeholder until client-side
// MCP is implemented for non-Anthropic providers.
type NoopClient struct{}

func (n *NoopClient) ListTools(_ context.Context) ([]models.ToolDef, error) { return nil, nil }
func (n *NoopClient) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("MCP tool %q: client-side MCP not yet implemented", name)
}
func (n *NoopClient) ListResources(_ context.Context) ([]MCPResourceDef, error) {
	return []MCPResourceDef{}, nil
}
func (n *NoopClient) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("MCP resource %q: client-side MCP not yet implemented", uri)
}
func (n *NoopClient) Close() error { return nil }
