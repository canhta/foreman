package mcp

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const maxToolNameLen = 64

// MCPToolName builds a normalized tool name from a server name and tool name.
// Replaces -, ., and space with underscore, prefixes with "mcp_", and caps at 64 chars.
func MCPToolName(server, tool string) string {
	server = normalize(server)
	tool = normalize(tool)

	full := "mcp_" + server + "_" + tool
	if len(full) <= maxToolNameLen {
		return full
	}

	// Truncation strategy: server to 20, tool to 30, 6-char hash suffix
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(server+"_"+tool)))[:6]
	if len(server) > 20 {
		server = server[:20]
	}
	if len(tool) > 30 {
		tool = tool[:30]
	}
	return "mcp_" + server + "_" + tool + "_" + hash
}

func normalize(s string) string {
	r := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return r.Replace(s)
}
