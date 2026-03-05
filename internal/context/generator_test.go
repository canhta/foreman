package context

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMProvider implements llm.LlmProvider for testing.
type mockLLMProvider struct {
	lastRequest *models.LlmRequest
	response    *models.LlmResponse
	err         error
}

func (m *mockLLMProvider) Complete(ctx context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.lastRequest = &req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockLLMProvider) ProviderName() string      { return "mock" }
func (m *mockLLMProvider) HealthCheck(context.Context) error { return nil }

func TestGenerator_GenerateOnline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21"), 0o644))

	mock := &mockLLMProvider{
		response: &models.LlmResponse{
			Content:    "# Generated AGENTS.md\n\nThis is a Go project.",
			StopReason: models.StopReasonEndTurn,
		},
	}

	gen := NewGenerator(mock, "test-model")
	result, err := gen.Generate(context.Background(), dir, GenerateOptions{MaxTokens: 100000})

	require.NoError(t, err)
	assert.Contains(t, result, "Generated AGENTS.md")
	// Verify LLM was called
	require.NotNil(t, mock.lastRequest)
	assert.Contains(t, mock.lastRequest.SystemPrompt, "agent")
	assert.Equal(t, "test-model", mock.lastRequest.Model)
}

func TestGenerator_GenerateOffline(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21"), 0o644))

	// No LLM provider needed for offline mode
	gen := NewGenerator(nil, "")
	result, err := gen.Generate(context.Background(), dir, GenerateOptions{
		MaxTokens: 100000,
		Offline:   true,
	})

	require.NoError(t, err)
	assert.Contains(t, result, "go", "offline output should mention detected language")
	assert.True(t, len(result) > 0, "offline output should not be empty")
}

func TestGenerator_GenerateOffline_ContainsRepoInfo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main"), 0o644))

	gen := NewGenerator(nil, "")
	result, err := gen.Generate(context.Background(), dir, GenerateOptions{Offline: true})

	require.NoError(t, err)
	// Should contain markdown structure
	assert.True(t, strings.Contains(result, "#"), "offline output should contain markdown headers")
}
