package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchTool_ParseInput(t *testing.T) {
	input := `{"tool_calls": [{"tool": "Read", "input": {"path": "main.go"}}, {"tool": "Glob", "input": {"pattern": "**/*.go"}}]}`

	calls, err := ParseBatchInput(input)
	require.NoError(t, err)
	assert.Len(t, calls, 2)
	assert.Equal(t, "Read", calls[0].Tool)
	assert.Equal(t, "Glob", calls[1].Tool)
}

func TestBatchTool_RejectsNesting(t *testing.T) {
	input := `{"tool_calls": [{"tool": "Batch", "input": {"tool_calls": []}}]}`

	_, err := ParseBatchInput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot nest")
}

func TestBatchTool_MaxCalls(t *testing.T) {
	calls := make([]BatchCall, 30)
	for i := range calls {
		calls[i] = BatchCall{Tool: "Read", Input: json.RawMessage(`{"path": "a.go"}`)}
	}

	err := ValidateBatchCalls(calls)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum 25")
}
