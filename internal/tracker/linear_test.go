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
