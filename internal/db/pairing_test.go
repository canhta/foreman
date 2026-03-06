package db

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairingCRUD(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create pairing
	expiresAt := time.Now().Add(10 * time.Minute)
	err := database.CreatePairing(ctx, "XKCD-7291", "+84123456789", "whatsapp", expiresAt)
	require.NoError(t, err)

	// Get pairing
	p, err := database.GetPairing(ctx, "XKCD-7291")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "+84123456789", p.SenderID)
	assert.Equal(t, "whatsapp", p.Channel)

	// List pairings
	pairings, err := database.ListPairings(ctx, "whatsapp")
	require.NoError(t, err)
	assert.Len(t, pairings, 1)

	// Delete pairing
	err = database.DeletePairing(ctx, "XKCD-7291")
	require.NoError(t, err)
	p, err = database.GetPairing(ctx, "XKCD-7291")
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestDeleteExpiredPairings(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create one expired, one valid
	past := time.Now().Add(-1 * time.Minute)
	future := time.Now().Add(10 * time.Minute)
	require.NoError(t, database.CreatePairing(ctx, "EXPIRED1", "+84111", "whatsapp", past))
	require.NoError(t, database.CreatePairing(ctx, "VALID001", "+84222", "whatsapp", future))

	err := database.DeleteExpiredPairings(ctx)
	require.NoError(t, err)

	pairings, err := database.ListPairings(ctx, "whatsapp")
	require.NoError(t, err)
	require.Len(t, pairings, 1)
	assert.Equal(t, "VALID001", pairings[0].Code)
}

func TestFindActiveClarification(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create a ticket with channel_sender_id and clarification_needed status
	ticket := &models.Ticket{
		ID:              "t-1",
		ExternalID:      "ext-1",
		Title:           "Test ticket",
		Description:     "desc",
		Status:          models.TicketStatusClarificationNeeded,
		ChannelSenderID: "+84123456789",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	err := database.CreateTicket(ctx, ticket)
	require.NoError(t, err)

	// Find clarification for sender
	found, err := database.FindActiveClarification(ctx, "+84123456789")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "t-1", found.ID)

	// No clarification for unknown sender
	found, err = database.FindActiveClarification(ctx, "+84999999999")
	require.NoError(t, err)
	assert.Nil(t, found)
}
