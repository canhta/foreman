package mcp_test

import (
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
)

func TestProcessTransport_InvalidCommand(t *testing.T) {
	cfg := mcp.MCPServerConfig{
		Name:    "bad",
		Command: "/nonexistent/binary",
		Args:    []string{"--foo"},
	}
	_, err := mcp.NewProcessTransport(cfg)
	assert.Error(t, err)
}
