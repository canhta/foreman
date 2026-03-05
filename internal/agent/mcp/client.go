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
	Name         string   `json:"name"`
	URL          string   `json:"url,omitempty"`          // Anthropic API-side MCP
	AuthToken    string   `json:"auth_token,omitempty"`   // Anthropic API-side MCP
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Command      string   `json:"command,omitempty"` // future: stdio transport
	Args         []string `json:"args,omitempty"`    // future: stdio transport
}

// Client is the interface for client-side MCP proxying (OpenAI/local providers).
// For Anthropic, MCP is handled API-side via request params — Client is not used.
type Client interface {
	ListTools(ctx context.Context) ([]models.ToolDef, error)
	Call(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// NoopClient satisfies Client but does nothing. Placeholder until client-side
// MCP is implemented for non-Anthropic providers.
type NoopClient struct{}

func (n *NoopClient) ListTools(_ context.Context) ([]models.ToolDef, error) { return nil, nil }
func (n *NoopClient) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("MCP tool %q: client-side MCP not yet implemented", name)
}
