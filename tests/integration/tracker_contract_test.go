package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackerContract_LocalFile(t *testing.T) {
	dir := t.TempDir()
	// LocalFileTracker.FetchReadyTickets reads from <dir>/tickets; create it so
	// an empty directory returns no tickets without error.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tickets"), 0o755))
	tr := tracker.NewLocalFileTracker(dir, "foreman-ready")
	runTrackerReadSuite(t, tr)
}

func TestTrackerContract_GitHub(t *testing.T) {
	issueNumber := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Query().Get("labels") != "":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"number": issueNumber, "title": "Test ticket", "body": "Description",
					"labels": []map[string]string{{"name": "foreman-ready"}}},
			})
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"number": issueNumber})
		}
	}))
	defer srv.Close()

	tr := tracker.NewGitHubIssuesTracker(srv.URL, "token", "org", "repo", "foreman-ready")
	tickets, err := tr.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, tickets)
	assert.Equal(t, "Test ticket", tickets[0].Title)
}

func runTrackerReadSuite(t *testing.T, tr tracker.IssueTracker) {
	t.Helper()
	ctx := context.Background()
	assert.NotEmpty(t, tr.ProviderName())
	_, err := tr.FetchReadyTickets(ctx)
	require.NoError(t, err)
}
