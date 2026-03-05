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
