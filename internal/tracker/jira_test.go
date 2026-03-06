package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraTracker_FetchReadyTickets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "/rest/api/2/search")
		assert.Equal(t, "Basic dXNlcjp0b2tlbg==", r.Header.Get("Authorization"))

		resp := map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"key": "PROJ-123",
					"fields": map[string]interface{}{
						"summary":     "Add login page",
						"description": "Build login page with email/password.",
						"labels":      []string{"foreman"},
						"priority":    map[string]string{"name": "Medium"},
						"assignee":    map[string]string{"displayName": "Alice"},
						"reporter":    map[string]string{"displayName": "Bob"},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "PROJ-123", tickets[0].ExternalID)
	assert.Equal(t, "Add login page", tickets[0].Title)
}

func TestJiraTracker_AddComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/2/issue/PROJ-1/comment")
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	err := tracker.AddComment(context.Background(), "PROJ-1", "Test comment")
	require.NoError(t, err)
}

func TestJiraTracker_ProviderName(t *testing.T) {
	tracker := NewJiraTracker("http://localhost", "u", "t", "P", "f")
	assert.Equal(t, "jira", tracker.ProviderName())
}

func TestJiraTracker_GetTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "PROJ-42")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"summary":     "Get this ticket",
				"description": "Some description",
				"labels":      []string{"foreman"},
				"priority":    map[string]string{"name": "High"},
			},
		})
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	ticket, err := tracker.GetTicket(context.Background(), "PROJ-42")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-42", ticket.ExternalID)
	assert.Equal(t, "Get this ticket", ticket.Title)
}

func TestJiraTracker_UpdateStatus(t *testing.T) {
	// UpdateStatus is currently a no-op stub in jira.go (returns nil immediately).
	// Verify it succeeds without error.
	tracker := NewJiraTracker("http://localhost", "user", "token", "PROJ", "foreman")
	err := tracker.UpdateStatus(context.Background(), "PROJ-42", "done")
	require.NoError(t, err)
}

func TestJiraTracker_AttachPR(t *testing.T) {
	// AttachPR delegates to AddComment, which POSTs to /rest/api/2/issue/{id}/comment
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.True(t, strings.Contains(r.URL.Path, "/comment"), "expected comment endpoint, got %s", r.URL.Path)
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	err := tracker.AttachPR(context.Background(), "PROJ-42", "https://github.com/org/repo/pull/5")
	require.NoError(t, err)
	assert.True(t, called, "POST to comment endpoint was not called")
}

func TestJiraTracker_AddAndHasAndRemoveLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"key": "PROJ-42",
				"fields": map[string]interface{}{
					"summary":     "t",
					"description": "d",
					"labels":      []string{"existing-label"},
				},
			})
		case "PUT":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")

	has, err := tracker.HasLabel(context.Background(), "PROJ-42", "existing-label")
	require.NoError(t, err)
	assert.True(t, has)

	has, err = tracker.HasLabel(context.Background(), "PROJ-42", "missing-label")
	require.NoError(t, err)
	assert.False(t, has)

	require.NoError(t, tracker.AddLabel(context.Background(), "PROJ-42", "new-label"))
	require.NoError(t, tracker.RemoveLabel(context.Background(), "PROJ-42", "existing-label"))
}

func TestJiraTracker_CreateTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": "PROJ-99",
		})
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	ticket, err := tracker.CreateTicket(context.Background(), CreateTicketRequest{
		Title:       "New child ticket",
		Description: "desc",
		Labels:      []string{"foreman-pending"},
	})
	require.NoError(t, err)
	assert.Equal(t, "PROJ-99", ticket.ExternalID)
	assert.Equal(t, "New child ticket", ticket.Title)
}
