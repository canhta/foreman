package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDB implements the minimal DB interface needed for scheduler tests.
type mockDB struct {
	reservations map[string]string
	reserved     map[string][]string
	mu           sync.Mutex
}

func newMockDB() *mockDB {
	return &mockDB{
		reservations: make(map[string]string),
		reserved:     make(map[string][]string),
	}
}

func (m *mockDB) GetReservedFiles(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reservations, nil
}

func (m *mockDB) ReserveFiles(_ context.Context, ticketID string, paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range paths {
		m.reservations[p] = ticketID
	}
	m.reserved[ticketID] = paths
	return nil
}

func (m *mockDB) ReleaseFiles(_ context.Context, ticketID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.reserved[ticketID] {
		delete(m.reservations, p)
	}
	delete(m.reserved, ticketID)
	return nil
}

func (m *mockDB) TryReserveFiles(_ context.Context, ticketID string, paths []string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var conflicts []string
	for _, p := range paths {
		if owner, ok := m.reservations[p]; ok && owner != ticketID {
			conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", p, owner))
		}
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}
	for _, p := range paths {
		m.reservations[p] = ticketID
	}
	m.reserved[ticketID] = paths
	return nil, nil
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
	require.NoError(t, sched.Release(context.Background(), "ticket-1"))

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

func TestScheduler_TryReserve_ConcurrentOnlyOneWins(t *testing.T) {
	db := newMockDB()
	sched := NewScheduler(db)

	const goroutines = 10
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ticketID := fmt.Sprintf("ticket-%d", idx)
			errs[idx] = sched.TryReserve(context.Background(), ticketID, []string{"shared/file.go"})
		}(i)
	}
	wg.Wait()

	successCount := 0
	conflictCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		} else {
			var conflictErr *FileConflictError
			if assert.ErrorAs(t, err, &conflictErr) {
				conflictCount++
			}
		}
	}

	assert.Equal(t, 1, successCount, "exactly one goroutine should succeed")
	assert.Equal(t, goroutines-1, conflictCount, "all others should get conflict errors")
}
