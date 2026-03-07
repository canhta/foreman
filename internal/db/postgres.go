package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
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
		`INSERT INTO tickets (id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.ParentTicketID, t.ChannelSenderID, t.DecomposeDepth, t.CreatedAt, t.UpdatedAt,
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
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE id = $1`, id))
}

func (p *PostgresDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return p.scanTicket(p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE external_id = $1`, externalID))
}

func (p *PostgresDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
		&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (p *PostgresDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE 1=1`
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
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (p *PostgresDB) GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets WHERE parent_ticket_id = $1`, parentExternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
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
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear any tasks from a previous plan attempt before inserting the new plan.
	if _, clearErr := tx.ExecContext(ctx, `DELETE FROM tasks WHERE ticket_id = $1`, ticketID); clearErr != nil {
		return fmt.Errorf("clear existing tasks: %w", clearErr)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria,
		 files_to_read, files_to_modify, test_assertions, estimated_complexity, depends_on, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`)
	if err != nil {
		return fmt.Errorf("prepare insert tasks: %w", err)
	}
	defer stmt.Close()

	for _, t := range tasks {
		id := t.ID
		if id == "" {
			id = uuid.New().String()
		}
		_, err := stmt.ExecContext(ctx, id, ticketID, t.Sequence, t.Title, t.Description,
			marshalStringSlice(t.AcceptanceCriteria),
			marshalStringSlice(t.FilesToRead),
			marshalStringSlice(t.FilesToModify),
			marshalStringSlice(t.TestAssertions),
			t.EstimatedComplexity,
			marshalStringSlice(t.DependsOn),
			string(models.TaskStatusPending), time.Now())
		if err != nil {
			return fmt.Errorf("insert task %q: %w", t.Title, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tasks: %w", err)
	}
	return nil
}

func (p *PostgresDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tasks SET status = $1 WHERE id = $2`, string(status), id)
	return err
}

func (p *PostgresDB) SetTaskErrorType(ctx context.Context, id, errorType string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tasks SET last_error_type = $1 WHERE id = $2`, errorType, id)
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
		 tokens_input, tokens_output, cost_usd, duration_ms, prompt_hash, response_summary, status, error_message,
		 cache_read_input_tokens, cache_creation_input_tokens, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		call.ID, call.TicketID, taskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage,
		call.CacheReadTokens, call.CacheCreationTokens, call.CreatedAt,
	)
	return err
}

func (p *PostgresDB) StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO llm_call_details (llm_call_id, full_prompt, full_response)
		 VALUES ($1, $2, $3)
		 ON CONFLICT(llm_call_id) DO UPDATE SET full_prompt=EXCLUDED.full_prompt, full_response=EXCLUDED.full_response`,
		callID, fullPrompt, fullResponse,
	)
	return err
}

func (p *PostgresDB) GetCallDetails(ctx context.Context, callID string) (string, string, error) {
	var prompt, response string
	err := p.db.QueryRowContext(ctx,
		`SELECT full_prompt, full_response FROM llm_call_details WHERE llm_call_id = $1`, callID,
	).Scan(&prompt, &response)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return prompt, response, err
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

func (p *PostgresDB) TryReserveFiles(ctx context.Context, ticketID string, paths []string) ([]string, error) {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read current reservations within the transaction.
	rows, err := tx.QueryContext(ctx,
		`SELECT file_path, ticket_id FROM file_reservations WHERE released_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query reservations: %w", err)
	}
	reserved := make(map[string]string)
	for rows.Next() {
		var path, owner string
		if err := rows.Scan(&path, &owner); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan reservation row: %w", err)
		}
		reserved[path] = owner
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reservation rows: %w", err)
	}

	// Check for conflicts.
	var conflicts []string
	for _, p := range paths {
		if owner, ok := reserved[p]; ok && owner != ticketID {
			conflicts = append(conflicts, fmt.Sprintf("%s (held by %s)", p, owner))
		}
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	// No conflicts — insert reservations within the same transaction.
	for i, path := range paths {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO file_reservations (file_path, ticket_id, reserved_at) VALUES ($1, $2, $3)
			 ON CONFLICT (file_path, ticket_id) DO NOTHING`,
			path, ticketID, time.Now()); err != nil {
			return nil, fmt.Errorf("reserve file %d (%s): %w", i, path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reservations: %w", err)
	}
	return nil, nil
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
		`SELECT id, ticket_id, sequence, title, description, status, created_at,
		        acceptance_criteria, files_to_read, files_to_modify, test_assertions, depends_on
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
		var acceptanceCriteria, filesToRead, filesToModify, testAssertions, dependsOn string
		if err := rows.Scan(&t.ID, &t.TicketID, &t.Sequence, &t.Title, &t.Description, &status, &t.CreatedAt,
			&acceptanceCriteria, &filesToRead, &filesToModify, &testAssertions, &dependsOn); err != nil {
			return nil, err
		}
		t.Status = models.TaskStatus(status)
		t.AcceptanceCriteria = unmarshalStringSlice(acceptanceCriteria)
		t.FilesToRead = unmarshalStringSlice(filesToRead)
		t.FilesToModify = unmarshalStringSlice(filesToModify)
		t.TestAssertions = unmarshalStringSlice(testAssertions)
		t.DependsOn = unmarshalStringSlice(dependsOn)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (p *PostgresDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt,
		        tokens_input, tokens_output, cost_usd, duration_ms, status,
		        cache_read_input_tokens, cache_creation_input_tokens, created_at
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
			&c.TokensInput, &c.TokensOutput, &c.CostUSD, &c.DurationMs, &status,
			&c.CacheReadTokens, &c.CacheCreationTokens, &c.CreatedAt); err != nil {
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

// --- Pairing ---

func (p *PostgresDB) CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO pending_pairings (code, sender_id, channel, expires_at) VALUES ($1, $2, $3, $4)`,
		code, senderID, channel, expiresAt)
	if err != nil {
		return fmt.Errorf("create pairing: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetPairing(ctx context.Context, code string) (*models.Pairing, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE code = $1`, code)
	var pr models.Pairing
	err := row.Scan(&pr.Code, &pr.SenderID, &pr.Channel, &pr.ExpiresAt, &pr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pairing: %w", err)
	}
	return &pr, nil
}

func (p *PostgresDB) DeletePairing(ctx context.Context, code string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("delete pairing: %w", err)
	}
	return nil
}

func (p *PostgresDB) ListPairings(ctx context.Context, channel string) ([]models.Pairing, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE channel = $1 ORDER BY created_at`, channel)
	if err != nil {
		return nil, fmt.Errorf("list pairings: %w", err)
	}
	defer rows.Close()
	var result []models.Pairing
	for rows.Next() {
		var pr models.Pairing
		if err := rows.Scan(&pr.Code, &pr.SenderID, &pr.Channel, &pr.ExpiresAt, &pr.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pairing: %w", err)
		}
		result = append(result, pr)
	}
	return result, rows.Err()
}

func (p *PostgresDB) DeleteExpiredPairings(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("delete expired pairings: %w", err)
	}
	return nil
}

func (p *PostgresDB) FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets WHERE channel_sender_id = $1 AND status = 'clarification_needed' LIMIT 1`, senderID)
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
		&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active clarification: %w", err)
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (p *PostgresDB) DeleteTicket(ctx context.Context, id string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DELETE FROM file_reservations WHERE ticket_id = $1`,
		`DELETE FROM progress_patterns WHERE ticket_id = $1`,
		`DELETE FROM handoffs WHERE ticket_id = $1`,
		`DELETE FROM llm_calls WHERE ticket_id = $1`,
		`DELETE FROM events WHERE ticket_id = $1`,
		`DELETE FROM tasks WHERE ticket_id = $1`,
		`DELETE FROM tickets WHERE id = $1`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PostgresDB) GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT channel_sender_id,
		        COUNT(*) as ticket_count,
		        COALESCE(SUM(cost_usd), 0) as cost_usd,
		        SUM(CASE WHEN status IN ('failed', 'blocked', 'partial') THEN 1 ELSE 0 END) as failed_count
		 FROM tickets
		 WHERE channel_sender_id != '' AND created_at >= $1
		 GROUP BY channel_sender_id
		 ORDER BY ticket_count DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.TeamStat
	for rows.Next() {
		var st models.TeamStat
		if err := rows.Scan(&st.ChannelSenderID, &st.TicketCount, &st.CostUSD, &st.FailedCount); err != nil {
			return nil, err
		}
		stats = append(stats, st)
	}
	return stats, rows.Err()
}

func (p *PostgresDB) GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets
		 WHERE pr_url != ''
		 ORDER BY updated_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (p *PostgresDB) GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error) {
	query := `SELECT t.id, t.external_id, t.title, t.description, t.status,
	                 t.parent_ticket_id, t.channel_sender_id, t.decompose_depth,
	                 t.cost_usd, t.created_at, t.updated_at,
	                 COALESCE(task_counts.total, 0),
	                 COALESCE(task_counts.done, 0)
	          FROM tickets t
	          LEFT JOIN (
	              SELECT ticket_id,
	                     COUNT(*) as total,
	                     SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) as done
	              FROM tasks GROUP BY ticket_id
	          ) task_counts ON task_counts.ticket_id = t.id
	          WHERE 1=1`
	var args []interface{}
	paramIdx := 1

	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, st := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", paramIdx)
			paramIdx++
			args = append(args, st)
		}
		query += ` AND t.status IN (` + placeholders + `)`
	}
	query += ` ORDER BY t.updated_at DESC`

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.TicketSummary
	for rows.Next() {
		var ts models.TicketSummary
		var status string
		if err := rows.Scan(&ts.ID, &ts.ExternalID, &ts.Title, &ts.Description, &status,
			&ts.ParentTicketID, &ts.ChannelSenderID, &ts.DecomposeDepth,
			&ts.CostUSD, &ts.CreatedAt, &ts.UpdatedAt,
			&ts.TasksTotal, &ts.TasksDone); err != nil {
			return nil, err
		}
		ts.Status = models.TicketStatus(status)
		summaries = append(summaries, ts)
	}
	return summaries, rows.Err()
}

func (p *PostgresDB) GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
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
