// internal/context/assembler_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssemblePlannerContext(t *testing.T) {
	workDir := setupTestRepo(t)
	ticket := &models.Ticket{
		Title:              "Add user endpoint",
		Description:        "Create a REST endpoint for user management",
		AcceptanceCriteria: "GET /users returns list of users",
	}

	ctx, err := AssemblePlannerContext(workDir, ticket, 30000)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.NotEmpty(t, ctx.UserPrompt)
	assert.Contains(t, ctx.UserPrompt, "Add user endpoint")
	assert.Contains(t, ctx.UserPrompt, "REST endpoint")
}

func TestAssembleImplementerContext(t *testing.T) {
	workDir := setupTestRepo(t)

	task := &models.Task{
		Title:              "Add user handler",
		Description:        "Create the handler function for user endpoint",
		AcceptanceCriteria: []string{"Handler returns 200"},
		TestAssertions:     []string{"TestGetUsers returns status 200"},
		FilesToRead:        []string{"internal/models/user.go"},
		FilesToModify:      []string{"internal/handler.go"},
	}

	ctx, err := AssembleImplementerContext(workDir, task, nil, 60000)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.NotEmpty(t, ctx.UserPrompt)
	assert.Contains(t, ctx.UserPrompt, "Add user handler")
	assert.Contains(t, ctx.UserPrompt, "Handler returns 200")
}

func TestAssembleImplementerContext_WithFeedback(t *testing.T) {
	workDir := setupTestRepo(t)

	task := &models.Task{
		Title:              "Fix handler",
		Description:        "Fix the handler",
		AcceptanceCriteria: []string{"No error"},
		TestAssertions:     []string{"Test passes"},
		FilesToModify:      []string{"internal/handler.go"},
	}

	fb := &FeedbackContext{
		Attempt:       2,
		MaxAttempts:   3,
		PreviousError: "nil pointer dereference",
	}

	ctx, err := AssembleImplementerContext(workDir, task, fb, 60000)
	require.NoError(t, err)
	assert.Contains(t, ctx.UserPrompt, "RETRY")
	assert.Contains(t, ctx.UserPrompt, "nil pointer dereference")
	assert.Contains(t, ctx.UserPrompt, "attempt 2")
}

func TestAssembleSpecReviewerContext(t *testing.T) {
	ctx := AssembleSpecReviewerContext(
		"Add user handler",
		[]string{"Handler returns 200", "Handles errors"},
		"diff --git a/handler.go\n+func GetUsers() {}",
		"PASS: all tests",
	)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.UserPrompt, "Handler returns 200")
	assert.Contains(t, ctx.UserPrompt, "diff --git")
}

func TestAssembleQualityReviewerContext(t *testing.T) {
	ctx := AssembleQualityReviewerContext(
		"diff --git a/handler.go\n+func GetUsers() {}",
		"go, stdlib, standard go conventions",
	)
	assert.NotEmpty(t, ctx.SystemPrompt)
	assert.Contains(t, ctx.UserPrompt, "diff --git")
	assert.Contains(t, ctx.UserPrompt, "standard go conventions")
}

func TestAssembleContext_SecretsFiltered(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "internal/handler.go"), []byte("package internal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".env"), []byte("API_KEY=sk-ant-secret123456789"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	task := &models.Task{
		FilesToRead:   []string{".env"},
		FilesToModify: []string{"internal/handler.go"},
	}

	ctx, err := AssembleImplementerContext(workDir, task, nil, 60000)
	require.NoError(t, err)
	// .env should not appear in the context
	assert.NotContains(t, ctx.UserPrompt, "sk-ant-secret")
}
