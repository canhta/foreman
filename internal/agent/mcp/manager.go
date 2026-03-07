package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// Manager coordinates multiple MCP server clients.
type Manager struct {
	clients map[string]Client
}

// NewManager creates a new MCP Manager.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]Client),
	}
}

// RegisterClient registers a named MCP client with the manager.
func (m *Manager) RegisterClient(name string, client Client) {
	m.clients[name] = client
}

// RegisterFromConfig creates and initializes an MCP client from the given config,
// then registers it with the manager. Supports transport="stdio" (default) and transport="http".
func (m *Manager) RegisterFromConfig(ctx context.Context, cfg MCPServerConfig) error {
	var client Client

	switch cfg.Transport {
	case "http":
		if cfg.URL == "" {
			return fmt.Errorf("mcp/http: url is required for transport=http (server %q)", cfg.Name)
		}
		c := NewHTTPClient(cfg.URL, cfg.AuthToken, cfg.Name)
		if err := c.Initialize(ctx); err != nil {
			_ = c.Close()
			return fmt.Errorf("mcp/http: initialize %q: %w", cfg.Name, err)
		}
		client = c
	case "", "stdio":
		// stdio transport requires launching a subprocess — callers must create the
		// StdioClient themselves (via NewStdioClientWithTransport) and register it
		// with RegisterClient. RegisterFromConfig cannot launch processes on behalf
		// of the caller because it has no command/args to run.
		return fmt.Errorf("mcp: RegisterFromConfig does not support transport %q; use RegisterClient for stdio", cfg.Transport)
	default:
		return fmt.Errorf("mcp: unsupported transport %q for server %q; supported: http, stdio", cfg.Transport, cfg.Name)
	}

	m.clients[cfg.Name] = client
	return nil
}

// AllTools aggregates tools from all registered MCP clients.
func (m *Manager) AllTools(ctx context.Context) []models.ToolDef {
	var all []models.ToolDef
	for _, c := range m.clients {
		tools, err := c.ListTools(ctx)
		if err != nil {
			continue
		}
		all = append(all, tools...)
	}
	return all
}

// IsMCPTool reports whether the tool name looks like an MCP tool (has mcp_ prefix).
func (m *Manager) IsMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp_")
}

// CallTool routes a tool call to the correct MCP server.
// It matches by finding a registered server whose name (normalized) appears
// after the "mcp_" prefix.
func (m *Manager) CallTool(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	if !strings.HasPrefix(toolName, "mcp_") {
		return "", fmt.Errorf("not an MCP tool: %s", toolName)
	}

	// Find the matching server by checking if the tool name starts with mcp_{normalized_server}_
	for name, client := range m.clients {
		prefix := "mcp_" + normalize(name) + "_"
		if strings.HasPrefix(toolName, prefix) {
			// Extract original tool name portion and pass it through
			originalTool := toolName[len(prefix):]
			return client.Call(ctx, originalTool, input)
		}
	}

	return "", fmt.Errorf("no MCP server found for tool %q", toolName)
}

// Close shuts down all registered MCP clients.
func (m *Manager) Close() error {
	var firstErr error
	for _, c := range m.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
