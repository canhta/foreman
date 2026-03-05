package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDB implements the minimal DB interface needed for scheduler tests.
type mockDB struct {
	reservations map[string]string   // path → ticketID
	reserved     map[string][]string // ticketID → paths
}

func newMockDB() *mockDB {
	return &mockDB{
		reservations: make(map[string]string),
		reserved:     make(map[string][]string),
	}
}

func (m *mockDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	return m.reservations, nil
}

func (m *mockDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	for _, p := range paths {
		m.reservations[p] = ticketID
	}
	m.reserved[ticketID] = paths
	return nil
}

func (m *mockDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	for _, p := range m.reserved[ticketID] {
		delete(m.reservations, p)
	}
	delete(m.reserved, ticketID)
	return nil
}

func TestScheduler_TryReserve_NoConflict(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	err := sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go", "src/models.go"})
	require.NoError(t, err)

	// Verify files are reserved
	reserved, _ := db.GetReservedFiles(context.Background())
	assert.Equal(t, "ticket-1", reserved["src/handler.go"])
	assert.Equal(t, "ticket-1", reserved["src/models.go"])
}

func TestScheduler_TryReserve_Conflict(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	// First ticket reserves files
	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))

	// Second ticket conflicts
	err := sched.TryReserve(context.Background(), "ticket-2", []string{"src/handler.go", "src/other.go"})
	assert.Error(t, err)

	var conflictErr *FileConflictError
	assert.ErrorAs(t, err, &conflictErr)
	assert.Len(t, conflictErr.Conflicts, 1)
	assert.Contains(t, conflictErr.Conflicts[0], "src/handler.go")
}

func TestScheduler_Release(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))
	sched.Release(context.Background(), "ticket-1")

	// After release, another ticket can reserve the same file
	err := sched.TryReserve(context.Background(), "ticket-2", []string{"src/handler.go"})
	assert.NoError(t, err)
}

func TestScheduler_TryReserve_SameTicket(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	require.NoError(t, sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go"}))

	// Same ticket re-reserving should not conflict
	err := sched.TryReserve(context.Background(), "ticket-1", []string{"src/handler.go", "src/new.go"})
	assert.NoError(t, err)
}
