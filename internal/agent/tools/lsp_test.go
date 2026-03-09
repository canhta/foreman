package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPToolSchema(t *testing.T) {
	schema := LSPToolSchema()
	assert.Contains(t, string(schema), "operation")
	assert.Contains(t, string(schema), "filePath")
	assert.Contains(t, string(schema), "line")
	assert.Contains(t, string(schema), "character")
}

func TestLSPOperations(t *testing.T) {
	ops := SupportedLSPOperations()
	assert.Contains(t, ops, "goToDefinition")
	assert.Contains(t, ops, "findReferences")
	assert.Contains(t, ops, "hover")
	assert.Contains(t, ops, "documentSymbol")
	assert.Contains(t, ops, "workspaceSymbol")
}

func TestBuildGoplsCommand(t *testing.T) {
	cmd := BuildLSPCommand("goToDefinition", "/path/to/file.go", 10, 5)
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Args, "definition")
}
