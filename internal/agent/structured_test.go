package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStructuredOutputTool(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["APPROVED", "REJECTED"]},
			"issues": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["status"]
	}`)

	tool := BuildStructuredOutputTool(schema)
	assert.Equal(t, "structured_output", tool.Name)
	assert.NotNil(t, tool.InputSchema)
}

func TestValidateStructuredOutput(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"status": {"type": "string"}
		},
		"required": ["status"]
	}`)

	// Valid output
	err := ValidateStructuredOutput(schema, `{"status": "APPROVED"}`)
	require.NoError(t, err)

	// Invalid output — not JSON
	err = ValidateStructuredOutput(schema, "not json")
	assert.Error(t, err)
}
