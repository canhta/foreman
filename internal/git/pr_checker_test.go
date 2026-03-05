package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubPRChecker_GetPRStatus_Merged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/pulls/42", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":     "closed",
			"merged":    true,
			"merged_at": "2026-03-06T12:00:00Z",
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 42)

	require.NoError(t, err)
	assert.Equal(t, "merged", status.State)
	assert.NotNil(t, status.MergedAt)
}

func TestGitHubPRChecker_GetPRStatus_Open(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":  "open",
			"merged": false,
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, "open", status.State)
}

func TestGitHubPRChecker_GetPRStatus_ClosedNotMerged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":     "closed",
			"merged":    false,
			"closed_at": "2026-03-06T12:00:00Z",
		})
	}))
	defer server.Close()

	checker := NewGitHubPRChecker(server.URL, "token", "owner", "repo")
	status, err := checker.GetPRStatus(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, "closed", status.State)
	assert.NotNil(t, status.ClosedAt)
}
