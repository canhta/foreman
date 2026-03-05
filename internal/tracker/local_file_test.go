package tracker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFileTracker_FetchReadyTickets(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id":         "LOCAL-1",
		"title":               "Add user endpoint",
		"description":         "Create a REST endpoint for user management.",
		"acceptance_criteria": "GET /users returns 200",
		"labels":              []string{"foreman-ready"},
		"priority":            "medium",
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-1.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Len(t, tickets, 1)
	assert.Equal(t, "LOCAL-1", tickets[0].ExternalID)
	assert.Equal(t, "Add user endpoint", tickets[0].Title)
}

func TestLocalFileTracker_FetchReadyTickets_NoLabel(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-2",
		"title":       "Not ready",
		"description": "This ticket has no foreman label.",
		"labels":      []string{"other-label"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-2.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	tickets, err := tracker.FetchReadyTickets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestLocalFileTracker_GetTicket(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-3",
		"title":       "Fix bug",
		"description": "Fix the nil pointer bug in handler.",
		"labels":      []string{"foreman-ready"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-3.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	result, err := tracker.GetTicket(context.Background(), "LOCAL-3")
	require.NoError(t, err)
	assert.Equal(t, "Fix bug", result.Title)
}

func TestLocalFileTracker_GetTicket_NotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tickets"), 0o755))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	_, err := tracker.GetTicket(context.Background(), "NOPE-999")
	assert.Error(t, err)
}

func TestLocalFileTracker_AddComment(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-4",
		"title":       "Test",
		"description": "Test ticket.",
		"labels":      []string{},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-4.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	err := tracker.AddComment(context.Background(), "LOCAL-4", "Foreman started working")
	require.NoError(t, err)

	// Check comment was saved
	commentsFile := filepath.Join(ticketsDir, "LOCAL-4.comments.json")
	_, err = os.Stat(commentsFile)
	assert.NoError(t, err)
}

func TestLocalFileTracker_AddLabel(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, "tickets")
	require.NoError(t, os.MkdirAll(ticketsDir, 0o755))

	ticket := map[string]interface{}{
		"external_id": "LOCAL-5",
		"title":       "Test",
		"description": "Test ticket.",
		"labels":      []string{"existing"},
	}
	data, _ := json.MarshalIndent(ticket, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(ticketsDir, "LOCAL-5.json"), data, 0o644))

	tracker := NewLocalFileTracker(dir, "foreman-ready")
	err := tracker.AddLabel(context.Background(), "LOCAL-5", "new-label")
	require.NoError(t, err)

	hasLabel, err := tracker.HasLabel(context.Background(), "LOCAL-5", "new-label")
	require.NoError(t, err)
	assert.True(t, hasLabel)
}

func TestLocalFileTracker_ProviderName(t *testing.T) {
	tracker := NewLocalFileTracker("/tmp", "foreman-ready")
	assert.Equal(t, "local_file", tracker.ProviderName())
}
