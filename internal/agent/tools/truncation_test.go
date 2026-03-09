package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateOutput_Small(t *testing.T) {
	output := "small output"
	result, truncated := TruncateOutput(output, 2000, 50000)
	assert.Equal(t, output, result)
	assert.False(t, truncated)
}

func TestTruncateOutput_TooManyLines(t *testing.T) {
	lines := make([]string, 3000)
	for i := range lines {
		lines[i] = "line content"
	}
	output := strings.Join(lines, "\n")

	result, truncated := TruncateOutput(output, 2000, 500000)
	assert.True(t, truncated)
	resultLines := strings.Split(result, "\n")
	assert.LessOrEqual(t, len(resultLines), 2005)
}

func TestTruncateOutput_TooLarge(t *testing.T) {
	output := strings.Repeat("x", 60000)

	result, truncated := TruncateOutput(output, 2000, 50000)
	assert.True(t, truncated)
	assert.LessOrEqual(t, len(result), 55000)
}

func TestSaveTruncatedOutput(t *testing.T) {
	dir := t.TempDir()
	output := strings.Repeat("x", 60000)

	path, err := SaveTruncatedOutput(dir, "read_abc123", output)
	require.NoError(t, err)
	assert.Contains(t, path, "read_abc123")
}
