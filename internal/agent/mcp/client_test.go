package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
)

func TestNoopClient_ListTools_ReturnsEmpty(t *testing.T) {
	c := &mcp.NoopClient{}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tool list, got %d", len(tools))
	}
}

func TestNoopClient_Call_ReturnsError(t *testing.T) {
	c := &mcp.NoopClient{}
	_, err := c.Call(context.Background(), "some-tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from noop client")
	}
}

func TestMCPServerConfig_URLFields(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "fs", URL: "https://mcp.example.com/sse", AuthToken: "tok"}
	if cfg.Name != "fs" || cfg.URL == "" || cfg.AuthToken == "" {
		t.Error("expected URL-based config fields to be set")
	}
}
