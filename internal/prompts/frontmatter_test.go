// internal/prompts/frontmatter_test.go
package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_Basic(t *testing.T) {
	input := `---
name: implementer
description: "Expert engineer"
max_tokens: 8192
temperature: 0.0
includes:
  - fragments/tdd-rules.md
---

You are an expert engineer.

## Task
**{{ task_title }}**`

	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Equal(t, "implementer", fm["name"])
	assert.Equal(t, "Expert engineer", fm["description"])
	assert.Equal(t, 8192, fm["max_tokens"])
	assert.Contains(t, body, "You are an expert engineer.")
	assert.Contains(t, body, "{{ task_title }}")
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	input := "Just plain markdown content"
	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Empty(t, fm)
	assert.Equal(t, "Just plain markdown content", body)
}

func TestParseFrontmatter_EmptyFrontmatter(t *testing.T) {
	input := "---\n---\nBody here"
	fm, body, err := ParseFrontmatter(input)
	require.NoError(t, err)
	assert.Empty(t, fm)
	assert.Equal(t, "Body here", body)
}

func TestParseFrontmatter_InvalidYAML(t *testing.T) {
	input := "---\n: invalid: yaml: [[\n---\nBody"
	_, _, err := ParseFrontmatter(input)
	assert.Error(t, err)
}
