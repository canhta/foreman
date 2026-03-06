package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDBContractSuite runs the same operations against any db.Database implementation.
func runDBContractSuite(t *testing.T, database db.Database) {
	t.Helper()
	ctx := context.Background()

	t.Run("ticket_roundtrip", func(t *testing.T) {
		ticket := &models.Ticket{
			ID: "contract-t1", ExternalID: "CONTRACT-1",
			Title: "Contract test", Description: "desc",
			Status:    models.TicketStatusQueued,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		require.NoError(t, database.CreateTicket(ctx, ticket))

		got, err := database.GetTicket(ctx, "contract-t1")
		require.NoError(t, err)
		assert.Equal(t, "Contract test", got.Title)
		assert.Equal(t, models.TicketStatusQueued, got.Status)

		require.NoError(t, database.UpdateTicketStatus(ctx, "contract-t1", models.TicketStatusImplementing))
		got, err = database.GetTicket(ctx, "contract-t1")
		require.NoError(t, err)
		assert.Equal(t, models.TicketStatusImplementing, got.Status)
	})

	t.Run("task_roundtrip", func(t *testing.T) {
		require.NoError(t, database.CreateTicket(ctx, &models.Ticket{
			ID: "contract-t2", ExternalID: "CONTRACT-2",
			Title: "t", Description: "d", Status: models.TicketStatusQueued,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
		tasks := []models.Task{
			{ID: "contract-task-1", TicketID: "contract-t2", Sequence: 1, Title: "step 1", Description: "do it"},
		}
		require.NoError(t, database.CreateTasks(ctx, "contract-t2", tasks))

		list, err := database.ListTasks(ctx, "contract-t2")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, "step 1", list[0].Title)

		require.NoError(t, database.UpdateTaskStatus(ctx, "contract-task-1", models.TaskStatusDone))
	})

	t.Run("file_reservations", func(t *testing.T) {
		require.NoError(t, database.CreateTicket(ctx, &models.Ticket{
			ID: "contract-t3", ExternalID: "CONTRACT-3",
			Title: "t", Description: "d", Status: models.TicketStatusQueued,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}))
		require.NoError(t, database.ReserveFiles(ctx, "contract-t3", []string{"a.go", "b.go"}))

		reserved, err := database.GetReservedFiles(ctx)
		require.NoError(t, err)
		assert.Equal(t, "contract-t3", reserved["a.go"])

		require.NoError(t, database.ReleaseFiles(ctx, "contract-t3"))
		reserved, err = database.GetReservedFiles(ctx)
		require.NoError(t, err)
		_, stillReserved := reserved["a.go"]
		assert.False(t, stillReserved)
	})

	t.Run("cost_tracking", func(t *testing.T) {
		date := fmt.Sprintf("2026-03-%02d", time.Now().Day())
		require.NoError(t, database.RecordDailyCost(ctx, date, 5.0))
		cost, err := database.GetDailyCost(ctx, date)
		require.NoError(t, err)
		assert.InDelta(t, 5.0, cost, 0.01)
	})
}

func TestDBContract_SQLite(t *testing.T) {
	f, err := os.CreateTemp("", "foreman-contract-*.db")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	database, err := db.NewSQLiteDB(f.Name())
	require.NoError(t, err)
	defer database.Close()

	runDBContractSuite(t, database)
}

func TestDBContract_Postgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set; skipping Postgres contract tests")
	}

	database, err := db.NewPostgresDB(dsn, 5)
	require.NoError(t, err)
	defer database.Close()

	runDBContractSuite(t, database)
}
