package db

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryReserveFiles_RepoLockBlocksOtherTickets(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"ticket-A", "ticket-B"} {
		require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
			ID: id, ExternalID: id, Title: "t", Description: "d",
			Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
	}

	// Ticket A reserves repo lock
	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Ticket B tries to reserve specific files — blocked by repo lock
	conflicts, err = db.TryReserveFiles(ctx, "ticket-B", []string{"handler.go"})
	require.NoError(t, err)
	assert.NotEmpty(t, conflicts)
	assert.Contains(t, conflicts[0], RepoLockSentinel)
}

func TestTryReserveFiles_SpecificFilesBlockRepoLock(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for _, id := range []string{"ticket-A", "ticket-B"} {
		require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
			ID: id, ExternalID: id, Title: "t", Description: "d",
			Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
	}

	// Ticket A reserves specific files
	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{"handler.go"})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Ticket B tries repo lock — blocked by ticket A's files
	conflicts, err = db.TryReserveFiles(ctx, "ticket-B", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.NotEmpty(t, conflicts)
}

func TestTryReserveFiles_SameTicketRepoLockIdempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, db.CreateTicket(ctx, &models.Ticket{
		ID: "ticket-A", ExternalID: "ticket-A", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Same ticket re-reserving — should be fine
	conflicts, err = db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}
