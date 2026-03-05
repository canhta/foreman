package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubIssuesTracker_FetchReadyTickets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Query().Get("labels"), "foreman-ready")
		assert.Equal(t, "open", r.URL.Query().Get("state"))

		issues := []map[string]interface{}{
			{
				"number": 42,
				"title":  "Add user endpoint",
				"body":   "Create REST endpoint\n\n## Acceptance Criteria\n- GET /users returns 200",
				"labels": []map[string]string{{"name": "foreman-ready"}},
			},
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "42", tickets[0].ExternalID)
	assert.Equal(t, "Add user endpoint", tickets[0].Title)
}

func TestGitHubIssuesTracker_AddComment(t *testing.T) {
	var postedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/issues/42/comments")
		json.NewDecoder(r.Body).Decode(&postedBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]int{"id": 1})
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	err := tracker.AddComment(context.Background(), "42", "Foreman started")
	require.NoError(t, err)
	assert.Equal(t, "Foreman started", postedBody["body"])
}

func TestGitHubIssuesTracker_AddLabel(t *testing.T) {
	var reqBody map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/issues/42/labels")
		json.NewDecoder(r.Body).Decode(&reqBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]string{{"name": "new-label"}})
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	err := tracker.AddLabel(context.Background(), "42", "new-label")
	require.NoError(t, err)
	assert.Equal(t, []string{"new-label"}, reqBody["labels"])
}

func TestGitHubIssuesTracker_CreateTicket(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/owner/repo/issues" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"number": 42,
				"title":  "Child ticket 1",
				"body":   "Parent: #10\n\nImplement login form",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "owner", "repo", "foreman-ready")
	ticket, err := tracker.CreateTicket(context.Background(), CreateTicketRequest{
		Title:       "Child ticket 1",
		Description: "Implement login form",
		Labels:      []string{"foreman-ready-pending"},
		ParentID:    "10",
	})

	require.NoError(t, err)
	assert.Equal(t, "42", ticket.ExternalID)
	assert.Equal(t, "Child ticket 1", ticket.Title)
	labels := receivedBody["labels"].([]interface{})
	assert.Equal(t, "foreman-ready-pending", labels[0])
	assert.Contains(t, receivedBody["body"], "Parent: #10")
}

func TestGitHubIssuesTracker_ProviderName(t *testing.T) {
	tracker := NewGitHubIssuesTracker("", "", "", "", "")
	assert.Equal(t, "github", tracker.ProviderName())
}

func TestGitHubIssuesTracker_UpdateStatus(t *testing.T) {
	var method string
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"number": 42, "state": "closed"})
	}))
	defer server.Close()

	tracker := NewGitHubIssuesTracker(server.URL, "test-token", "org", "repo", "foreman-ready")
	err := tracker.UpdateStatus(context.Background(), "42", "done")
	require.NoError(t, err)
	assert.Equal(t, "PATCH", method)
	assert.Equal(t, "closed", body["state"])
}
