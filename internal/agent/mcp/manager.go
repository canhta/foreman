package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/rs/zerolog/log"
)

// secretContentPatterns are the same patterns used by the tools package guard.
// We replicate them here to avoid a circular import (mcp ↔ tools).
var secretContentPatterns = []string{"BEGIN RSA PRIVATE KEY", "BEGIN EC PRIVATE KEY", "BEGIN OPENSSH PRIVATE KEY"}
var secretPathSuffixes = []string{".env", ".key", ".pem", ".p12", ".pfx", "id_rsa", "id_ed25519", ".secret"}

// checkResourceSecrets applies the same secrets-detection logic as tools.CheckSecrets.
func checkResourceSecrets(uri, content string) error {
	base := strings.ToLower(filepath.Base(uri))
	for _, pat := range secretPathSuffixes {
		if strings.HasSuffix(base, pat) || base == strings.TrimPrefix(pat, ".") {
			return fmt.Errorf("resource %q is not allowed (sensitive file pattern)", uri)
		}
	}
	for _, pat := range secretContentPatterns {
		if strings.Contains(content, pat) {
			return fmt.Errorf("resource content contains sensitive pattern %q — read blocked", pat)
		}
	}
	return nil
}

// MCPToolSummary holds the essential metadata for a single MCP tool.
// It includes both the normalized name (used for LLM calls) and the original
// name from the MCP server, as well as the server it belongs to.
type MCPToolSummary struct {
	NormalizedName string `json:"normalized_name"`
	OriginalName   string `json:"original_name"`
	ServerName     string `json:"server_name"`
	Description    string `json:"description"`
}

// Manager coordinates multiple MCP server clients.
//
// Thread-safety contract:
//   - clients is populated once during buildMCPManager (before any concurrent
//     use) and is never modified after that point. No mutex is needed for reads.
//   - toolCache is populated after init (CacheToolSummaries / SetToolCache) and
//     may be read concurrently; all accesses are protected by mu.
type Manager struct {
	clients          map[string]Client
	metrics          *telemetry.Metrics
	toolCache        []MCPToolSummary
	toolDefCache     []models.ToolDef
	mu               sync.RWMutex
	resourceMaxBytes int // 0 = use default (512 KB)
}

// NewManager creates a new MCP Manager.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]Client),
	}
}

// WithMetrics attaches a Metrics instance for instrumentation.
func (m *Manager) WithMetrics(met *telemetry.Metrics) *Manager {
	m.metrics = met
	return m
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
			result, err := client.Call(ctx, originalTool, input)
			if m.metrics != nil {
				status := "success"
				if err != nil {
					status = "error"
				}
				m.metrics.MCPToolCallsTotal.WithLabelValues(name, originalTool, status).Inc()
			}
			return result, err
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

// HealthStatus returns the health state of all registered MCP servers.
// The map key is the server name; the value is true if healthy, false if not.
// Clients that do not implement the HealthChecker interface are assumed healthy.
func (m *Manager) HealthStatus() map[string]bool {
	status := make(map[string]bool, len(m.clients))
	for name, c := range m.clients {
		if hc, ok := c.(HealthChecker); ok {
			status[name] = hc.IsHealthy()
		} else {
			// No health-check capability → assume healthy
			status[name] = true
		}
	}
	return status
}

// SetToolCache replaces the in-memory tool cache with the provided summaries.
// This is used during initialisation (after MCP servers are ready) and in tests.
// Safe for concurrent use.
func (m *Manager) SetToolCache(summaries []MCPToolSummary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCache = summaries
}

// ListToolSummaries returns the cached MCP tool summaries without making any
// network calls. Returns an empty (non-nil) slice when no tools are cached.
// Safe for concurrent use.
func (m *Manager) ListToolSummaries() []MCPToolSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.toolCache == nil {
		return []MCPToolSummary{}
	}
	return m.toolCache
}

// CacheToolSummaries queries every registered MCP client for its tools and
// stores the results in the in-memory cache.  This is the only method that
// makes network calls; all subsequent ListToolSummaries calls are in-memory.
// Safe for concurrent use.
func (m *Manager) CacheToolSummaries(ctx context.Context) {
	var summaries []MCPToolSummary
	var allDefs []models.ToolDef
	for serverName, c := range m.clients {
		toolDefs, err := c.ListTools(ctx)
		if err != nil {
			log.Warn().Err(err).Str("server", serverName).Msg("mcp: CacheToolSummaries: ListTools failed")
			continue
		}
		allDefs = append(allDefs, toolDefs...)
		for _, td := range toolDefs {
			// td.Name is already the normalized form (MCPToolName(server, tool))
			// We reverse-derive the original name by stripping the server prefix.
			prefix := "mcp_" + normalize(serverName) + "_"
			originalName := td.Name
			if strings.HasPrefix(td.Name, prefix) {
				originalName = td.Name[len(prefix):]
			}
			summaries = append(summaries, MCPToolSummary{
				NormalizedName: td.Name,
				OriginalName:   originalName,
				ServerName:     serverName,
				Description:    td.Description,
			})
		}
	}
	if summaries == nil {
		summaries = []MCPToolSummary{}
	}
	if allDefs == nil {
		allDefs = []models.ToolDef{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCache = summaries
	m.toolDefCache = allDefs
}

// CachedToolDefs returns the full ToolDef list (including input schemas) for all
// MCP tools cached by the last CacheToolSummaries call.
// Returns an empty (non-nil) slice when no tools are cached.
// Safe for concurrent use.
func (m *Manager) CachedToolDefs() []models.ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.toolDefCache == nil {
		return []models.ToolDef{}
	}
	return m.toolDefCache
}

// defaultResourceMaxBytes is the default maximum resource response size (512 KB).
const defaultResourceMaxBytes = 512 * 1024

// SetResourceMaxBytes configures the maximum resource response size in bytes.
// A value of 0 resets to the default (512 KB). Used in tests and via config.
func (m *Manager) SetResourceMaxBytes(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resourceMaxBytes = n
}

// effectiveMaxBytes returns the configured limit or the default.
func (m *Manager) effectiveMaxBytes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.resourceMaxBytes > 0 {
		return m.resourceMaxBytes
	}
	return defaultResourceMaxBytes
}

// ListResources lists all resources available on the named MCP server.
func (m *Manager) ListResources(ctx context.Context, serverName string) ([]MCPResourceDef, error) {
	c, ok := m.clients[serverName]
	if !ok {
		return nil, fmt.Errorf("no MCP server registered with name %q", serverName)
	}
	return c.ListResources(ctx)
}

// ReadResource reads the content of a resource identified by URI from the named server.
// The content is subject to secrets scanning and max size enforcement.
func (m *Manager) ReadResource(ctx context.Context, serverName, uri string) (string, error) {
	c, ok := m.clients[serverName]
	if !ok {
		return "", fmt.Errorf("no MCP server registered with name %q", serverName)
	}

	content, err := c.ReadResource(ctx, uri)
	if err != nil {
		return "", err
	}

	maxBytes := m.effectiveMaxBytes()
	if len(content) > maxBytes {
		return "", fmt.Errorf("resource %q response exceeds max size (%d > %d bytes)", uri, len(content), maxBytes)
	}

	if err := checkResourceSecrets(uri, content); err != nil {
		return "", err
	}

	return content, nil
}
