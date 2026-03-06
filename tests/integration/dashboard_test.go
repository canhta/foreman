//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hashToken mirrors the unexported hashToken function in dashboard/auth.go.
func testHashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func newTestDashboardDB(t *testing.T) *db.SQLiteDB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.NewSQLiteDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		database.Close()
		os.RemoveAll(dir)
	})
	return database
}

// TestDashboardAuth_NoToken verifies that API requests without a token get 401.
func TestDashboardAuth_NoToken(t *testing.T) {
	database := newTestDashboardDB(t)

	srv := dashboard.NewServer(database, nil, nil, nil, models.CostConfig{}, "test", "127.0.0.1", 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestDashboardAuth_ValidToken verifies that a correctly hashed token gets through.
func TestDashboardAuth_ValidToken(t *testing.T) {
	database := newTestDashboardDB(t)
	ctx := context.Background()

	const rawToken = "supersecret-test-token"
	err := database.CreateAuthToken(ctx, testHashToken(rawToken), "test-token")
	require.NoError(t, err)

	srv := dashboard.NewServer(database, nil, nil, nil, models.CostConfig{}, "test", "127.0.0.1", 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestDashboardAuth_InvalidToken verifies that a wrong token gets 401.
func TestDashboardAuth_InvalidToken(t *testing.T) {
	database := newTestDashboardDB(t)
	ctx := context.Background()

	err := database.CreateAuthToken(ctx, testHashToken("correct-token"), "test-token")
	require.NoError(t, err)

	srv := dashboard.NewServer(database, nil, nil, nil, models.CostConfig{}, "test", "127.0.0.1", 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer wrong-token")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestDashboardTicketFilter_InvalidStatus verifies that an unknown status returns 400.
func TestDashboardTicketFilter_InvalidStatus(t *testing.T) {
	database := newTestDashboardDB(t)
	ctx := context.Background()

	const rawToken = "filter-test-token"
	err := database.CreateAuthToken(ctx, testHashToken(rawToken), "filter-token")
	require.NoError(t, err)

	srv := dashboard.NewServer(database, nil, nil, nil, models.CostConfig{}, "test", "127.0.0.1", 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/tickets?status=not_a_real_status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
