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

func TestLinearTracker_FetchReadyTickets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer lin-key", r.Header.Get("Authorization"))

		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"issues": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"identifier":  "ENG-42",
							"title":       "Fix dashboard crash",
							"description": "The dashboard crashes when clicking settings.",
							"priority":    float64(2),
							"labels": map[string]interface{}{
								"nodes": []map[string]interface{}{
									{"name": "foreman"},
								},
							},
							"assignee": map[string]interface{}{
								"name": "Alice",
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tracker := NewLinearTracker("lin-key", "foreman", srv.URL)
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "ENG-42", tickets[0].ExternalID)
	assert.Equal(t, "Fix dashboard crash", tickets[0].Title)
}

func TestLinearTracker_ProviderName(t *testing.T) {
	tracker := NewLinearTracker("key", "foreman", "")
	assert.Equal(t, "linear", tracker.ProviderName())
}

func TestLinearTracker_CreateTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issueCreate": map[string]interface{}{
					"success": true,
					"issue": map[string]interface{}{
						"identifier":  "ENG-101",
						"title":       "Child task",
						"description": "do the thing",
					},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	ticket, err := tracker.CreateTicket(context.Background(), CreateTicketRequest{
		Title:       "Child task",
		Description: "do the thing",
	})
	require.NoError(t, err)
	assert.Equal(t, "ENG-101", ticket.ExternalID)
	assert.Equal(t, "Child task", ticket.Title)
	assert.Equal(t, "do the thing", ticket.Description)
}

func TestLinearTracker_CreateTicket_WithParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issueCreate": map[string]interface{}{
					"success": true,
					"issue": map[string]interface{}{
						"identifier":  "ENG-102",
						"title":       "Sub task",
						"description": "Parent: ENG-1\n\ndo sub thing",
					},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	ticket, err := tracker.CreateTicket(context.Background(), CreateTicketRequest{
		Title:       "Sub task",
		Description: "do sub thing",
		ParentID:    "ENG-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "ENG-102", ticket.ExternalID)
}

func TestLinearTracker_GetTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"identifier":  "ENG-42",
					"title":       "Fix the bug",
					"description": "details here",
					"priority":    float64(2),
					"labels": map[string]interface{}{
						"nodes": []map[string]string{{"name": "foreman-ready"}},
					},
					"assignee": map[string]interface{}{
						"name": "Bob",
					},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	ticket, err := tracker.GetTicket(context.Background(), "ENG-42")
	require.NoError(t, err)
	assert.Equal(t, "ENG-42", ticket.ExternalID)
	assert.Equal(t, "Fix the bug", ticket.Title)
	assert.Equal(t, "details here", ticket.Description)
	assert.Equal(t, "Bob", ticket.Assignee)
	assert.Contains(t, ticket.Labels, "foreman-ready")
}

func TestLinearTracker_GetTicket_NoAssignee(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"identifier":  "ENG-50",
					"title":       "Unassigned task",
					"description": "",
					"priority":    float64(0),
					"labels":      map[string]interface{}{"nodes": []interface{}{}},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	ticket, err := tracker.GetTicket(context.Background(), "ENG-50")
	require.NoError(t, err)
	assert.Equal(t, "ENG-50", ticket.ExternalID)
	assert.Empty(t, ticket.Assignee)
}

func TestLinearTracker_UpdateStatus(t *testing.T) {
	// UpdateStatus is a no-op in the current implementation — no HTTP call is made.
	tracker := NewLinearTracker("api-key", "foreman-ready", "http://localhost:0")
	err := tracker.UpdateStatus(context.Background(), "ENG-42", "done")
	require.NoError(t, err)
}

func TestLinearTracker_AddLabel(t *testing.T) {
	// AddLabel is a no-op in the current implementation — returns nil without HTTP call.
	tracker := NewLinearTracker("api-key", "foreman-ready", "http://localhost:0")
	err := tracker.AddLabel(context.Background(), "ENG-42", "new-label")
	require.NoError(t, err)
}

func TestLinearTracker_RemoveLabel(t *testing.T) {
	// RemoveLabel is a no-op in the current implementation — returns nil without HTTP call.
	tracker := NewLinearTracker("api-key", "foreman-ready", "http://localhost:0")
	err := tracker.RemoveLabel(context.Background(), "ENG-42", "old-label")
	require.NoError(t, err)
}

func TestLinearTracker_HasLabel_True(t *testing.T) {
	// HasLabel delegates to GetTicket, so we need a real mock server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"identifier":  "ENG-42",
					"title":       "Some task",
					"description": "",
					"priority":    float64(0),
					"labels": map[string]interface{}{
						"nodes": []map[string]string{{"name": "foreman-ready"}, {"name": "bug"}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	has, err := tracker.HasLabel(context.Background(), "ENG-42", "foreman-ready")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestLinearTracker_HasLabel_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issue": map[string]interface{}{
					"identifier":  "ENG-42",
					"title":       "Some task",
					"description": "",
					"priority":    float64(0),
					"labels": map[string]interface{}{
						"nodes": []map[string]string{{"name": "bug"}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	has, err := tracker.HasLabel(context.Background(), "ENG-42", "foreman-ready")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestLinearTracker_AddComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"commentCreate": map[string]interface{}{"success": true},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	err := tracker.AddComment(context.Background(), "ENG-42", "work started")
	require.NoError(t, err)
}

func TestLinearTracker_AttachPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"commentCreate": map[string]interface{}{"success": true},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("api-key", "foreman-ready", srv.URL)
	err := tracker.AttachPR(context.Background(), "ENG-42", "https://github.com/org/repo/pull/1")
	require.NoError(t, err)
}

func TestLinearTracker_GraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{
				{"message": "not authorized"},
			},
		})
	}))
	defer srv.Close()

	tracker := NewLinearTracker("bad-key", "foreman-ready", srv.URL)
	_, err := tracker.GetTicket(context.Background(), "ENG-42")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")
}

func TestLinearTracker_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer srv.Close()

	tracker := NewLinearTracker("bad-key", "foreman-ready", srv.URL)
	_, err := tracker.GetTicket(context.Background(), "ENG-42")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
