package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type SQLiteDB struct {
	db *sql.DB
}

func NewSQLiteDB(path string) (*SQLiteDB, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err := runSQLiteMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &SQLiteDB{db: db}, nil
}

// runSQLiteMigrations applies additive, idempotent schema changes for existing DBs.
func runSQLiteMigrations(db *sql.DB) error {
	ctx := context.Background()

	// Backfill tickets where id was stored as empty string — use external_id as id.
	if _, err := db.ExecContext(ctx,
		`UPDATE tickets SET id = external_id WHERE id = '' AND external_id != ''`); err != nil {
		return err
	}

	// Add last_error_type column to tasks (ignored if already present).
	_, _ = db.ExecContext(ctx,
		`ALTER TABLE tasks ADD COLUMN last_error_type TEXT NOT NULL DEFAULT ''`)

	// Recreate llm_call_details without the FK constraint on llm_calls(id), but ONLY if
	// the old FK schema is still present. Once migrated (no REFERENCES clause), skip to
	// avoid wiping observability data on every restart.
	var tblSQL string
	_ = db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='llm_call_details'`,
	).Scan(&tblSQL)
	if strings.Contains(tblSQL, "REFERENCES") {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS llm_call_details`); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx,
			`CREATE TABLE IF NOT EXISTS llm_call_details (
			    llm_call_id TEXT PRIMARY KEY,
			    full_prompt TEXT NOT NULL DEFAULT '',
			    full_response TEXT NOT NULL DEFAULT ''
			)`); err != nil {
			return err
		}
	}

	return nil
}

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}

func (s *SQLiteDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.ParentTicketID, t.ChannelSenderID, t.DecomposeDepth, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create ticket: %w", err)
	}
	return nil
}

func (s *SQLiteDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update ticket status: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE id = ?`, id))
}

func (s *SQLiteDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE external_id = ?`, externalID))
}

func (s *SQLiteDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
		&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan ticket: %w", err)
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (s *SQLiteDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at FROM tickets WHERE 1=1`
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
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ticket row: %w", err)
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket rows: %w", err)
	}
	return tickets, nil
}

func (s *SQLiteDB) GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets WHERE parent_ticket_id = ?`, parentExternalID)
	if err != nil {
		return nil, fmt.Errorf("get child tickets: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan child ticket row: %w", err)
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate child ticket rows: %w", err)
	}
	return tickets, nil
}

func (s *SQLiteDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET last_completed_task_seq = ?, updated_at = ? WHERE id = ?`,
		taskSeq, time.Now(), ticketID,
	)
	if err != nil {
		return fmt.Errorf("set last completed task: %w", err)
	}
	return nil
}

func marshalStringSlice(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func unmarshalStringSlice(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func (s *SQLiteDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear any tasks from a previous plan attempt before inserting the new plan.
	if _, clearErr := tx.ExecContext(ctx, `DELETE FROM tasks WHERE ticket_id = ?`, ticketID); clearErr != nil {
		return fmt.Errorf("clear existing tasks: %w", clearErr)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria,
		 files_to_read, files_to_modify, test_assertions, estimated_complexity, depends_on, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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

func (s *SQLiteDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return nil
}

func (s *SQLiteDB) SetTaskErrorType(ctx context.Context, id, errorType string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET last_error_type = ? WHERE id = ?`, errorType, id)
	if err != nil {
		return fmt.Errorf("set task error type: %w", err)
	}
	return nil
}

func (s *SQLiteDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("increment llm_calls for task %q: %w", id, err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT total_llm_calls FROM tasks WHERE id = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("read llm_calls for task %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit llm_calls increment: %w", err)
	}
	return count, nil
}

func (s *SQLiteDB) RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error {
	var taskID sql.NullString
	if call.TaskID != "" {
		taskID = sql.NullString{String: call.TaskID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_calls (id, ticket_id, task_id, role, provider, model, attempt,
		 tokens_input, tokens_output, cost_usd, duration_ms, prompt_hash, response_summary, status, error_message,
		 cache_read_input_tokens, cache_creation_input_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		call.ID, call.TicketID, taskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage,
		call.CacheReadTokens, call.CacheCreationTokens, call.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record llm call: %w", err)
	}
	return nil
}

func (s *SQLiteDB) StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_call_details (llm_call_id, full_prompt, full_response)
		 VALUES (?, ?, ?)
		 ON CONFLICT(llm_call_id) DO UPDATE SET full_prompt=excluded.full_prompt, full_response=excluded.full_response`,
		callID, fullPrompt, fullResponse,
	)
	if err != nil {
		return fmt.Errorf("store call details: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetCallDetails(ctx context.Context, callID string) (string, string, error) {
	var prompt, response string
	err := s.db.QueryRowContext(ctx,
		`SELECT full_prompt, full_response FROM llm_call_details WHERE llm_call_id = ?`, callID,
	).Scan(&prompt, &response)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get call details: %w", err)
	}
	return prompt, response, nil
}

func (s *SQLiteDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("set handoff: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, from_role, to_role, key, value, created_at FROM handoffs
		 WHERE ticket_id = ? AND (to_role = ? OR to_role IS NULL OR to_role = '')
		 ORDER BY created_at`, ticketID, forRole)
	if err != nil {
		return nil, fmt.Errorf("get handoffs: %w", err)
	}
	defer rows.Close()

	var handoffs []models.HandoffRecord
	for rows.Next() {
		var h models.HandoffRecord
		if err := rows.Scan(&h.ID, &h.TicketID, &h.FromRole, &h.ToRole, &h.Key, &h.Value, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan handoff row: %w", err)
		}
		handoffs = append(handoffs, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate handoff rows: %w", err)
	}
	return handoffs, nil
}

func (s *SQLiteDB) SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TicketID, p.PatternKey, p.PatternValue, "[]", p.DiscoveredByTask, p.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save progress pattern: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at
		 FROM progress_patterns WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("get progress patterns: %w", err)
	}
	defer rows.Close()

	var patterns []models.ProgressPattern
	for rows.Next() {
		var p models.ProgressPattern
		var dirs string
		if err := rows.Scan(&p.ID, &p.TicketID, &p.PatternKey, &p.PatternValue, &dirs, &p.DiscoveredByTask, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan progress pattern row: %w", err)
		}
		patterns = append(patterns, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate progress pattern rows: %w", err)
	}
	return patterns, nil
}

func (s *SQLiteDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, p := range paths {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO file_reservations (file_path, ticket_id, reserved_at) VALUES (?, ?, ?)`,
			p, ticketID, time.Now())
		if err != nil {
			return fmt.Errorf("reserve file %q: %w", p, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit file reservations: %w", err)
	}
	return nil
}

func (s *SQLiteDB) TryReserveFiles(ctx context.Context, ticketID string, paths []string) ([]string, error) {
	// Use a dedicated connection so we can issue BEGIN IMMEDIATE directly.
	// BEGIN IMMEDIATE acquires a write lock before reading, preventing two
	// concurrent workers from both seeing the same files as unreserved and
	// then both inserting reservations (TOCTOU race).
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	// NOTE: conn.Close() is deferred first so LIFO ordering ensures the ROLLBACK
	// defer (registered after BEGIN IMMEDIATE) fires before the connection closes.
	defer conn.Close()

	if _, beginErr := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); beginErr != nil {
		return nil, fmt.Errorf("begin immediate transaction: %w", beginErr)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	// Read current reservations within the IMMEDIATE transaction.
	rows, err := conn.QueryContext(ctx,
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
	// INSERT OR IGNORE handles the case where this ticket already holds the reservation
	// (e.g. replanning after a crash that left old reservations unreleased).
	for _, p := range paths {
		if _, err := conn.ExecContext(ctx,
			`INSERT OR IGNORE INTO file_reservations (file_path, ticket_id, reserved_at) VALUES (?, ?, ?)`,
			p, ticketID, time.Now()); err != nil {
			return nil, fmt.Errorf("insert reservation for %q: %w", p, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, fmt.Errorf("commit reservations: %w", err)
	}
	committed = true
	return nil, nil
}

func (s *SQLiteDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_reservations SET released_at = ? WHERE ticket_id = ? AND released_at IS NULL`,
		time.Now(), ticketID)
	if err != nil {
		return fmt.Errorf("release files: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_path, ticket_id FROM file_reservations WHERE released_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("get reserved files: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var path, ticketID string
		if err := rows.Scan(&path, &ticketID); err != nil {
			return nil, fmt.Errorf("scan reserved file row: %w", err)
		}
		result[path] = ticketID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reserved file rows: %w", err)
	}
	return result, nil
}

func (s *SQLiteDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM llm_calls WHERE ticket_id = ?`, ticketID).Scan(&cost)
	if err != nil {
		return 0, fmt.Errorf("get ticket cost: %w", err)
	}
	return cost, nil
}

func (s *SQLiteDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(total_usd, 0) FROM cost_daily WHERE date = ?`, date).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get daily cost: %w", err)
	}
	return cost, nil
}

func (s *SQLiteDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES (?, ?)
		 ON CONFLICT(date) DO UPDATE SET total_usd = total_usd + ?`,
		date, amount, amount)
	if err != nil {
		return fmt.Errorf("record daily cost: %w", err)
	}
	return nil
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
	if err != nil {
		return 0, fmt.Errorf("get monthly cost: %w", err)
	}
	return cost, nil
}

func (s *SQLiteDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, sequence, title, description, status, created_at,
		        acceptance_criteria, files_to_read, files_to_modify, test_assertions, depends_on
		 FROM tasks WHERE ticket_id = ? ORDER BY sequence`,
		ticketID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]models.Task, 0)
	for rows.Next() {
		var t models.Task
		var status string
		var acceptanceCriteria, filesToRead, filesToModify, testAssertions, dependsOn string
		if err := rows.Scan(&t.ID, &t.TicketID, &t.Sequence, &t.Title, &t.Description, &status, &t.CreatedAt,
			&acceptanceCriteria, &filesToRead, &filesToModify, &testAssertions, &dependsOn); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.Status = models.TaskStatus(status)
		t.AcceptanceCriteria = unmarshalStringSlice(acceptanceCriteria)
		t.FilesToRead = unmarshalStringSlice(filesToRead)
		t.FilesToModify = unmarshalStringSlice(filesToModify)
		t.TestAssertions = unmarshalStringSlice(testAssertions)
		t.DependsOn = unmarshalStringSlice(dependsOn)
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}
	return tasks, nil
}

func (s *SQLiteDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt,
		        tokens_input, tokens_output, cost_usd, duration_ms, status,
		        cache_read_input_tokens, cache_creation_input_tokens, created_at
		 FROM llm_calls WHERE ticket_id = ? ORDER BY created_at DESC`,
		ticketID)
	if err != nil {
		return nil, fmt.Errorf("list llm calls: %w", err)
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
			return nil, fmt.Errorf("scan llm call row: %w", err)
		}
		c.TaskID = taskID.String
		c.Status = status
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm call rows: %w", err)
	}
	return calls, nil
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
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	// An empty ticketID means "all events" — omit the WHERE filter.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events WHERE (? = '' OR ticket_id = ?) ORDER BY created_at DESC LIMIT ?`,
		ticketID, ticketID, limit)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var events []models.EventRecord
	for rows.Next() {
		var e models.EventRecord
		var taskID, details sql.NullString
		if err := rows.Scan(&e.ID, &e.TicketID, &taskID, &e.EventType, &e.Severity, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		e.TaskID = taskID.String
		e.Details = details.String
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}
	return events, nil
}

func (s *SQLiteDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES (?, ?, ?)`,
		tokenHash, name, time.Now())
	if err != nil {
		return fmt.Errorf("create auth token: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var revoked bool
	err := s.db.QueryRowContext(ctx,
		`SELECT revoked FROM auth_tokens WHERE token_hash = ?`, tokenHash).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate auth token: %w", err)
	}
	if !revoked {
		_, _ = s.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = ? WHERE token_hash = ?`, time.Now(), tokenHash)
	}
	return !revoked, nil
}

// --- Pairing ---

func (s *SQLiteDB) CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_pairings (code, sender_id, channel, expires_at) VALUES (?, ?, ?, ?)`,
		code, senderID, channel, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("create pairing: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetPairing(ctx context.Context, code string) (*models.Pairing, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE code = ?`, code)
	var p models.Pairing
	err := row.Scan(&p.Code, &p.SenderID, &p.Channel, &p.ExpiresAt, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pairing: %w", err)
	}
	return &p, nil
}

func (s *SQLiteDB) DeletePairing(ctx context.Context, code string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE code = ?`, code)
	if err != nil {
		return fmt.Errorf("delete pairing: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ListPairings(ctx context.Context, channel string) ([]models.Pairing, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE channel = ? ORDER BY created_at`, channel)
	if err != nil {
		return nil, fmt.Errorf("list pairings: %w", err)
	}
	defer rows.Close()
	var result []models.Pairing
	for rows.Next() {
		var p models.Pairing
		if err := rows.Scan(&p.Code, &p.SenderID, &p.Channel, &p.ExpiresAt, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pairing: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *SQLiteDB) DeleteExpiredPairings(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE expires_at < datetime('now')`)
	if err != nil {
		return fmt.Errorf("delete expired pairings: %w", err)
	}
	return nil
}

func (s *SQLiteDB) FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets WHERE channel_sender_id = ? AND status = 'clarification_needed' LIMIT 1`, senderID)
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

func (s *SQLiteDB) DeleteTicket(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DELETE FROM file_reservations WHERE ticket_id = ?`,
		`DELETE FROM progress_patterns WHERE ticket_id = ?`,
		`DELETE FROM handoffs WHERE ticket_id = ?`,
		`DELETE FROM llm_calls WHERE ticket_id = ?`,
		`DELETE FROM events WHERE ticket_id = ?`,
		`DELETE FROM tasks WHERE ticket_id = ?`,
		`DELETE FROM tickets WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("delete ticket %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete ticket: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT channel_sender_id,
		        COUNT(*) as ticket_count,
		        COALESCE(SUM(cost_usd), 0) as cost_usd,
		        SUM(CASE WHEN status IN ('failed', 'blocked', 'partial') THEN 1 ELSE 0 END) as failed_count
		 FROM tickets
		 WHERE channel_sender_id != '' AND created_at >= ?
		 GROUP BY channel_sender_id
		 ORDER BY ticket_count DESC`, since)
	if err != nil {
		return nil, fmt.Errorf("get team stats: %w", err)
	}
	defer rows.Close()

	var stats []models.TeamStat
	for rows.Next() {
		var st models.TeamStat
		if err := rows.Scan(&st.ChannelSenderID, &st.TicketCount, &st.CostUSD, &st.FailedCount); err != nil {
			return nil, fmt.Errorf("scan team stat row: %w", err)
		}
		stats = append(stats, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team stat rows: %w", err)
	}
	return stats, nil
}

func (s *SQLiteDB) GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets
		 WHERE pr_url != ''
		 ORDER BY updated_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent prs: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan recent pr row: %w", err)
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent pr rows: %w", err)
	}
	return tickets, nil
}

func (s *SQLiteDB) GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error) {
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

	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, st := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, st)
		}
		query += ` AND t.status IN (` + placeholders + `)`
	}
	query += ` ORDER BY t.updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get ticket summaries: %w", err)
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
			return nil, fmt.Errorf("scan ticket summary row: %w", err)
		}
		ts.Status = models.TicketStatus(status)
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket summary rows: %w", err)
	}
	return summaries, nil
}

func (s *SQLiteDB) GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get global events: %w", err)
	}
	defer rows.Close()

	var events []models.EventRecord
	for rows.Next() {
		var e models.EventRecord
		var taskID, details sql.NullString
		if err := rows.Scan(&e.ID, &e.TicketID, &taskID, &e.EventType, &e.Severity, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan global event row: %w", err)
		}
		e.TaskID = taskID.String
		e.Details = details.String
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate global event rows: %w", err)
	}
	return events, nil
}

// --- Distributed Locks (BUG-M15) ---

// AcquireLock attempts to acquire an advisory lock named lockName with the given TTL.
// It first cleans up any expired locks, then atomically inserts the lock row.
// Returns acquired=true if this caller now holds the lock.
func (s *SQLiteDB) AcquireLock(ctx context.Context, lockName string, ttlSeconds int) (bool, error) {
	// Clean up expired locks first.
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM distributed_locks WHERE expires_at < datetime('now')`); err != nil {
		return false, fmt.Errorf("acquire lock cleanup: %w", err)
	}

	// Attempt atomic insert; INSERT OR IGNORE silently skips if the row already exists.
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO distributed_locks (lock_name, expires_at, holder_id)
		 VALUES (?, datetime('now', '+' || ? || ' seconds'), ?)`,
		lockName, ttlSeconds, holderID)
	if err != nil {
		return false, fmt.Errorf("acquire lock insert: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("acquire lock rows affected: %w", err)
	}
	return rows == 1, nil
}

// ReleaseLock releases the named lock only if this process holds it.
func (s *SQLiteDB) ReleaseLock(ctx context.Context, lockName string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM distributed_locks WHERE lock_name = ? AND holder_id = ?`,
		lockName, holderID)
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// --- Embedding Store (REQ-INFRA-002) ---

// UpsertEmbedding inserts or updates an embedding record, serializing the vector as BLOB.
func (s *SQLiteDB) UpsertEmbedding(ctx context.Context, e EmbeddingRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO embeddings (repo_path, head_sha, file_path, start_line, end_line, chunk_text, vector)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo_path, head_sha, file_path, start_line) DO UPDATE SET
		     end_line = excluded.end_line,
		     chunk_text = excluded.chunk_text,
		     vector = excluded.vector`,
		e.RepoPath, e.HeadSHA, e.FilePath, e.StartLine, e.EndLine, e.ChunkText, SerializeVector(e.Vector),
	)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

// GetEmbeddingsByRepoSHA retrieves all embedding records for a given repo and commit SHA.
func (s *SQLiteDB) GetEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) ([]EmbeddingRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT repo_path, head_sha, file_path, start_line, end_line, chunk_text, vector
		 FROM embeddings WHERE repo_path = ? AND head_sha = ?`,
		repoPath, headSHA,
	)
	if err != nil {
		return nil, fmt.Errorf("get embeddings: %w", err)
	}
	defer rows.Close()

	var results []EmbeddingRecord
	for rows.Next() {
		var rec EmbeddingRecord
		var blob []byte
		if err := rows.Scan(&rec.RepoPath, &rec.HeadSHA, &rec.FilePath,
			&rec.StartLine, &rec.EndLine, &rec.ChunkText, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		rec.Vector = DeserializeVector(blob)
		results = append(results, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding rows: %w", err)
	}
	return results, nil
}

// DeleteEmbeddingsByRepoSHA deletes all embedding records for a given repo and commit SHA.
func (s *SQLiteDB) DeleteEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM embeddings WHERE repo_path = ? AND head_sha = ?`,
		repoPath, headSHA,
	)
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	return nil
}
