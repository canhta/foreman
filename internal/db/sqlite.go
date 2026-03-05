package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

type SQLiteDB struct {
	db *sql.DB
}

func NewSQLiteDB(path string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &SQLiteDB{db: db}, nil
}

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}

func (s *SQLiteDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *SQLiteDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now(), id,
	)
	return err
}

func (s *SQLiteDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE id = ?`, id))
}

func (s *SQLiteDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE external_id = ?`, externalID))
}

func (s *SQLiteDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (s *SQLiteDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE 1=1`
	var args []interface{}

	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, s := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, s)
		}
		query += ` AND status IN (` + placeholders + `)`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
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

func (s *SQLiteDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET last_completed_task_seq = ?, updated_at = ? WHERE id = ?`,
		taskSeq, time.Now(), ticketID,
	)
	return err
}

func (s *SQLiteDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria,
		 files_to_read, files_to_modify, test_assertions, estimated_complexity, depends_on, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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

func (s *SQLiteDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, string(status), id)
	return err
}

func (s *SQLiteDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT total_llm_calls FROM tasks WHERE id = ?`, id).Scan(&count)
	return count, err
}

func (s *SQLiteDB) RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error {
	var taskID sql.NullString
	if call.TaskID != "" {
		taskID = sql.NullString{String: call.TaskID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_calls (id, ticket_id, task_id, role, provider, model, attempt,
		 tokens_input, tokens_output, cost_usd, duration_ms, prompt_hash, response_summary, status, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		call.ID, call.TicketID, taskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage, call.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, from_role, to_role, key, value, created_at FROM handoffs
		 WHERE ticket_id = ? AND (to_role = ? OR to_role IS NULL OR to_role = '')
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

func (s *SQLiteDB) SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TicketID, p.PatternKey, p.PatternValue, "[]", p.DiscoveredByTask, p.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at
		 FROM progress_patterns WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []models.ProgressPattern
	for rows.Next() {
		var p models.ProgressPattern
		var dirs string
		if err := rows.Scan(&p.ID, &p.TicketID, &p.PatternKey, &p.PatternValue, &dirs, &p.DiscoveredByTask, &p.CreatedAt); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range paths {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO file_reservations (file_path, ticket_id, reserved_at) VALUES (?, ?, ?)`,
			p, ticketID, time.Now())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_reservations SET released_at = ? WHERE ticket_id = ? AND released_at IS NULL`,
		time.Now(), ticketID)
	return err
}

func (s *SQLiteDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
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

func (s *SQLiteDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT cost_usd FROM tickets WHERE id = ?`, ticketID).Scan(&cost)
	return cost, err
}

func (s *SQLiteDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(total_usd, 0) FROM cost_daily WHERE date = ?`, date).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (s *SQLiteDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES (?, ?)
		 ON CONFLICT(date) DO UPDATE SET total_usd = total_usd + ?`,
		date, amount, amount)
	return err
}

func (s *SQLiteDB) GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_usd), 0) FROM cost_daily WHERE date LIKE ?`,
		yearMonth+"%",
	).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (s *SQLiteDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, sequence, title, description, status, created_at
		 FROM tasks WHERE ticket_id = ? ORDER BY sequence`,
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

func (s *SQLiteDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt,
		        tokens_input, tokens_output, cost_usd, duration_ms, status, created_at
		 FROM llm_calls WHERE ticket_id = ? ORDER BY created_at DESC`,
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

func (s *SQLiteDB) RecordEvent(ctx context.Context, e *models.EventRecord) error {
	var taskID sql.NullString
	if e.TaskID != "" {
		taskID = sql.NullString{String: e.TaskID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, ticket_id, task_id, event_type, severity, message, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TicketID, taskID, e.EventType, e.Severity, e.Message, e.Details, e.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	// An empty ticketID means "all events" — omit the WHERE filter.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events WHERE (? = '' OR ticket_id = ?) ORDER BY created_at DESC LIMIT ?`,
		ticketID, ticketID, limit)
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

func (s *SQLiteDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES (?, ?, ?)`,
		tokenHash, name, time.Now())
	return err
}

func (s *SQLiteDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var revoked bool
	err := s.db.QueryRowContext(ctx,
		`SELECT revoked FROM auth_tokens WHERE token_hash = ?`, tokenHash).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !revoked {
		s.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = ? WHERE token_hash = ?`, time.Now(), tokenHash)
	}
	return !revoked, nil
}
