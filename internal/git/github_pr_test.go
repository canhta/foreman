// internal/git/github_pr_test.go
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

func TestGitHubPRCreator_CreatePR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/repos/org/repo/pulls")
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "[Foreman] PROJ-123: Add users", body["title"])
		assert.Equal(t, "foreman/PROJ-123", body["head"])
		assert.Equal(t, "main", body["base"])
		assert.Equal(t, true, body["draft"])

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number":   42,
			"url":      "https://api.github.com/repos/org/repo/pulls/42",
			"html_url": "https://github.com/org/repo/pull/42",
		})
	}))
	defer server.Close()

	client := NewGitHubPRCreator(server.URL, "test-token", "org", "repo")
	resp, err := client.CreatePR(context.Background(), PrRequest{
		Title:      "[Foreman] PROJ-123: Add users",
		Body:       "PR body",
		HeadBranch: "foreman/PROJ-123",
		BaseBranch: "main",
		Draft:      true,
	})

	require.NoError(t, err)
	assert.Equal(t, 42, resp.Number)
	assert.Equal(t, "https://github.com/org/repo/pull/42", resp.HTMLURL)
}

func TestGitHubPRCreator_CreatePR_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message": "Validation Failed"}`))
	}))
	defer server.Close()

	client := NewGitHubPRCreator(server.URL, "test-token", "org", "repo")
	_, err := client.CreatePR(context.Background(), PrRequest{
		Title:      "test",
		HeadBranch: "test-branch",
		BaseBranch: "main",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "422")
}
