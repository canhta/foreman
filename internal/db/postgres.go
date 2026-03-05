package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// Compile-time check that PostgresDB satisfies the Database interface.
var _ Database = (*PostgresDB)(nil)

type PostgresDB struct {
	db *sqlx.DB
}

func NewPostgresDB(url string, maxConns int) (*PostgresDB, error) {
	db, err := sqlx.Connect("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	db.SetMaxOpenConns(maxConns)

	// Run schema migrations
	if err := runPostgresSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres schema: %w", err)
	}

	return &PostgresDB{db: db}, nil
}

func runPostgresSchema(db *sqlx.DB) error {
	_, err := db.ExecContext(context.Background(), schema)
	return err
}

func (p *PostgresDB) Close() error { return p.db.Close() }

// --- Tickets ---

func (p *PostgresDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (p *PostgresDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tickets SET status = $1, updated_at = $2 WHERE id = $3`,
		string(status), time.Now(), id,
	)
	return err
}

func (p *PostgresDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return p.scanTicket(p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE id = $1`, id))
}

func (p *PostgresDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return p.scanTicket(p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE external_id = $1`, externalID))
}

func (p *PostgresDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (p *PostgresDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE 1=1`
	var args []interface{}
	argIdx := 1

	if filter.Status != "" {
		query += fmt.Sprintf(` AND status = $%d`, argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, s := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", argIdx)
			args = append(args, s)
			argIdx++
		}
		query += ` AND status IN (` + placeholders + `)`
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (p *PostgresDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tickets SET last_completed_task_seq = $1, updated_at = $2 WHERE id = $3`,
		taskSeq, time.Now(), ticketID,
	)
	return err
}

// --- Tasks ---

func (p *PostgresDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria,
		 files_to_read, files_to_modify, test_assertions, estimated_complexity, depends_on, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tasks {
		_, err := stmt.ExecContext(ctx, t.ID, ticketID, t.Sequence, t.Title, t.Description,
			"[]", "[]", "[]", "[]", t.EstimatedComplexity, "[]",
			string(models.TaskStatusPending), time.Now())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PostgresDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tasks SET status = $1 WHERE id = $2`, string(status), id)
	return err
}

func (p *PostgresDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	var count int
	err := p.db.QueryRowContext(ctx,
		`UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = $1 RETURNING total_llm_calls`, id).Scan(&count)
	return count, err
}

// --- LLM Calls ---

func (p *PostgresDB) RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error {
	var taskID sql.NullString
	if call.TaskID != "" {
		taskID = sql.NullString{String: call.TaskID, Valid: true}
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO llm_calls (id, ticket_id, task_id, role, provider, model, attempt,
		 tokens_input, tokens_output, cost_usd, duration_ms, prompt_hash, response_summary, status, error_message, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		call.ID, call.TicketID, taskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage, call.CreatedAt,
	)
	return err
}

// --- Handoffs ---

func (p *PostgresDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt,
	)
	return err
}

func (p *PostgresDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, from_role, to_role, key, value, created_at FROM handoffs
		 WHERE ticket_id = $1 AND (to_role = $2 OR to_role IS NULL OR to_role = '')
		 ORDER BY created_at`, ticketID, forRole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handoffs []models.HandoffRecord
	for rows.Next() {
		var h models.HandoffRecord
		if err := rows.Scan(&h.ID, &h.TicketID, &h.FromRole, &h.ToRole, &h.Key, &h.Value, &h.CreatedAt); err != nil {
			return nil, err
		}
		handoffs = append(handoffs, h)
	}
	return handoffs, rows.Err()
}

// --- Progress Patterns ---

func (p *PostgresDB) SaveProgressPattern(ctx context.Context, pp *models.ProgressPattern) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		pp.ID, pp.TicketID, pp.PatternKey, pp.PatternValue, "[]", pp.DiscoveredByTask, pp.CreatedAt,
	)
	return err
}

func (p *PostgresDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at
		 FROM progress_patterns WHERE ticket_id = $1`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []models.ProgressPattern
	for rows.Next() {
		var pp models.ProgressPattern
		var dirs string
		if err := rows.Scan(&pp.ID, &pp.TicketID, &pp.PatternKey, &pp.PatternValue, &dirs, &pp.DiscoveredByTask, &pp.CreatedAt); err != nil {
			return nil, err
		}
		patterns = append(patterns, pp)
	}
	return patterns, rows.Err()
}

// --- File Reservations ---

func (p *PostgresDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for i, path := range paths {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO file_reservations (file_path, ticket_id, reserved_at) VALUES ($1, $2, $3)
			 ON CONFLICT (file_path, ticket_id) DO NOTHING`,
			path, ticketID, time.Now())
		if err != nil {
			return fmt.Errorf("reserve file %d (%s): %w", i, path, err)
		}
	}
	return tx.Commit()
}

func (p *PostgresDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE file_reservations SET released_at = $1 WHERE ticket_id = $2 AND released_at IS NULL`,
		time.Now(), ticketID)
	return err
}

func (p *PostgresDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT file_path, ticket_id FROM file_reservations WHERE released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var path, ticketID string
		if err := rows.Scan(&path, &ticketID); err != nil {
			return nil, err
		}
		result[path] = ticketID
	}
	return result, rows.Err()
}

// --- Cost ---

func (p *PostgresDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM llm_calls WHERE ticket_id = $1`, ticketID).Scan(&cost)
	return cost, err
}

func (p *PostgresDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := p.db.QueryRowContext(ctx, `SELECT COALESCE(total_usd, 0) FROM cost_daily WHERE date = $1`, date).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (p *PostgresDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES ($1, $2)
		 ON CONFLICT (date) DO UPDATE SET total_usd = cost_daily.total_usd + EXCLUDED.total_usd`,
		date, amount)
	return err
}

func (p *PostgresDB) GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error) {
	var cost float64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_usd), 0) FROM cost_daily WHERE date LIKE $1`,
		yearMonth+"%",
	).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (p *PostgresDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, sequence, title, description, status, created_at
		 FROM tasks WHERE ticket_id = $1 ORDER BY sequence`,
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]models.Task, 0)
	for rows.Next() {
		var t models.Task
		var status string
		if err := rows.Scan(&t.ID, &t.TicketID, &t.Sequence, &t.Title, &t.Description, &status, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TaskStatus(status)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (p *PostgresDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt,
		        tokens_input, tokens_output, cost_usd, duration_ms, status, created_at
		 FROM llm_calls WHERE ticket_id = $1 ORDER BY created_at DESC`,
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	calls := make([]models.LlmCallRecord, 0)
	for rows.Next() {
		var c models.LlmCallRecord
		var taskID sql.NullString
		var status string
		if err := rows.Scan(&c.ID, &c.TicketID, &taskID, &c.Role, &c.Provider, &c.Model, &c.Attempt,
			&c.TokensInput, &c.TokensOutput, &c.CostUSD, &c.DurationMs, &status, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.TaskID = taskID.String
		c.Status = status
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

// --- Events ---

func (p *PostgresDB) RecordEvent(ctx context.Context, e *models.EventRecord) error {
	var taskID sql.NullString
	if e.TaskID != "" {
		taskID = sql.NullString{String: e.TaskID, Valid: true}
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO events (id, ticket_id, task_id, event_type, severity, message, details, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.TicketID, taskID, e.EventType, e.Severity, e.Message, e.Details, e.CreatedAt,
	)
	return err
}

func (p *PostgresDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	// An empty ticketID means "all events" — omit the WHERE filter.
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events WHERE ($1 = '' OR ticket_id = $1) ORDER BY created_at DESC LIMIT $2`,
		ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.EventRecord
	for rows.Next() {
		var e models.EventRecord
		var taskID, details sql.NullString
		if err := rows.Scan(&e.ID, &e.TicketID, &taskID, &e.EventType, &e.Severity, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.TaskID = taskID.String
		e.Details = details.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Auth ---

func (p *PostgresDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES ($1, $2, $3)`,
		tokenHash, name, time.Now())
	return err
}

func (p *PostgresDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var revoked bool
	err := p.db.QueryRowContext(ctx,
		`SELECT revoked FROM auth_tokens WHERE token_hash = $1`, tokenHash).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !revoked {
		_, _ = p.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = $1 WHERE token_hash = $2`, time.Now(), tokenHash)
	}
	return !revoked, nil
}
