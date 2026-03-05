package mcp_test

import (
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
)

func TestMCPToolName_Basic(t *testing.T) {
	got := mcp.MCPToolName("my-server", "read-file")
	assert.Equal(t, "mcp_my_server_read_file", got)
}

func TestMCPToolName_ReplacesDotsAndSpaces(t *testing.T) {
	got := mcp.MCPToolName("srv.one", "do thing")
	assert.Equal(t, "mcp_srv_one_do_thing", got)
}

func TestMCPToolName_CapsAt64Chars(t *testing.T) {
	// Very long names should be truncated to 64 chars max
	server := "a]very-long-server-name-that-exceeds"
	tool := "an-extremely-long-tool-name-that-also-exceeds-normal-limits-by-far"
	got := mcp.MCPToolName(server, tool)
	assert.LessOrEqual(t, len(got), 64, "tool name must be <= 64 chars")
	assert.Contains(t, got, "mcp_")
}

func TestMCPToolName_ShortNamesUnchanged(t *testing.T) {
	got := mcp.MCPToolName("fs", "list")
	assert.Equal(t, "mcp_fs_list", got)
}
