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
		assert.Equal(t, "/rest/api/3/search/jql", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Basic dXNlcjp0b2tlbg==", r.Header.Get("Authorization"))

		// v3 description is ADF, not a plain string.
		resp := map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"key": "PROJ-123",
					"fields": map[string]interface{}{
						"summary": "Add login page",
						"description": map[string]interface{}{
							"type":    "doc",
							"version": 1,
							"content": []map[string]interface{}{
								{
									"type": "paragraph",
									"content": []map[string]interface{}{
										{"type": "text", "text": "Build login page with email/password."},
									},
								},
							},
						},
						"labels":   []string{"foreman"},
						"priority": map[string]string{"name": "Medium"},
						"assignee": map[string]string{"displayName": "Alice"},
						"reporter": map[string]string{"displayName": "Bob"},
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
	assert.Contains(t, tickets[0].Description, "Build login page")
	assert.Equal(t, "Medium", tickets[0].Priority)
	assert.Equal(t, "Alice", tickets[0].Assignee)
}

func TestJiraTracker_FetchReadyTickets_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errorMessages":["unauthorized"],"errors":{}}`))
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "badtoken", "PROJ", "foreman")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.Error(t, err, "expected error on 401 response")
	assert.Nil(t, tickets)
	assert.Contains(t, err.Error(), "401")
}

func TestJiraTracker_AddComment(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/issue/PROJ-1/comment", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	err := tracker.AddComment(context.Background(), "PROJ-1", "Test comment")
	require.NoError(t, err)
	// Body must be ADF, not a plain string.
	body, ok := gotBody["body"].(map[string]interface{})
	require.True(t, ok, "expected body to be ADF object, got %T", gotBody["body"])
	assert.Equal(t, "doc", body["type"])
}

func TestJiraTracker_ProviderName(t *testing.T) {
	tracker := NewJiraTracker("http://localhost", "u", "t", "P", "f")
	assert.Equal(t, "jira", tracker.ProviderName())
}

func TestJiraTracker_GetTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/3/issue/PROJ-42")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": "PROJ-42",
			"fields": map[string]interface{}{
				"summary": "Get this ticket",
				"description": map[string]interface{}{
					"type": "doc", "version": 1,
					"content": []map[string]interface{}{
						{"type": "paragraph", "content": []map[string]interface{}{
							{"type": "text", "text": "Some description"},
						}},
					},
				},
				"labels":   []string{"foreman"},
				"priority": map[string]string{"name": "High"},
			},
		})
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	ticket, err := tracker.GetTicket(context.Background(), "PROJ-42")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-42", ticket.ExternalID)
	assert.Equal(t, "Get this ticket", ticket.Title)
	assert.Contains(t, ticket.Description, "Some description")
}

func TestJiraTracker_GetTicket_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errorMessages":["issue not found"]}`))
	}))
	defer srv.Close()

	tracker := NewJiraTracker(srv.URL, "user", "token", "PROJ", "foreman")
	ticket, err := tracker.GetTicket(context.Background(), "PROJ-99")
	require.Error(t, err)
	assert.Nil(t, ticket)
	assert.Contains(t, err.Error(), "404")
}

func TestJiraTracker_UpdateStatus(t *testing.T) {
	// UpdateStatus is currently a no-op stub (returns nil immediately).
	tracker := NewJiraTracker("http://localhost", "user", "token", "PROJ", "foreman")
	err := tracker.UpdateStatus(context.Background(), "PROJ-42", "done")
	require.NoError(t, err)
}

func TestJiraTracker_AttachPR(t *testing.T) {
	// AttachPR delegates to AddComment → POST /rest/api/3/issue/{id}/comment
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.True(t, strings.Contains(r.URL.Path, "/comment"), "expected comment endpoint, got %s", r.URL.Path)
		assert.Contains(t, r.URL.Path, "/rest/api/3/")
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
					"description": nil,
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
		assert.Equal(t, "/rest/api/3/issue", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"key": "PROJ-99"})
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

func TestAdfToText_PlainString(t *testing.T) {
	raw := json.RawMessage(`"plain text fallback"`)
	assert.Equal(t, "plain text fallback", adfToText(raw))
}

func TestAdfToText_Null(t *testing.T) {
	assert.Equal(t, "", adfToText(json.RawMessage(`null`)))
	assert.Equal(t, "", adfToText(json.RawMessage(nil)))
}

func TestAdfToText_Doc(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "doc", "version": 1,
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "Hello "},
				{"type": "text", "text": "world"}
			]},
			{"type": "paragraph", "content": [
				{"type": "text", "text": "Second paragraph"}
			]}
		]
	}`)
	result := adfToText(raw)
	assert.Contains(t, result, "Hello")
	assert.Contains(t, result, "world")
	assert.Contains(t, result, "Second paragraph")
}
