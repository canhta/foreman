package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	// Additive migrations for existing Postgres deployments (idempotent via IF NOT EXISTS).
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE llm_calls ADD COLUMN IF NOT EXISTS prompt_version TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("migrate llm_calls.prompt_version: %w", err)
	}
	// Add version and supersedes columns to handoffs for handoff versioning (ARCH-M02).
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("migrate handoffs.version: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS supersedes TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("migrate handoffs.supersedes: %w", err)
	}
	// Create dag_states table for DAG-aware crash recovery (ARCH-F03).
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS dag_states (
		    ticket_id   TEXT PRIMARY KEY,
		    state_json  TEXT NOT NULL,
		    updated_at  TIMESTAMPTZ NOT NULL
		)`); err != nil {
		return fmt.Errorf("migrate dag_states: %w", err)
	}
	return nil
}

func (p *PostgresDB) Close() error { return p.db.Close() }

// --- Tickets ---

func (p *PostgresDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.ParentTicketID, t.ChannelSenderID, t.DecomposeDepth, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create ticket: %w", err)
	}
	return nil
}

func (p *PostgresDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tickets SET status = $1, updated_at = $2 WHERE id = $3`,
		string(status), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update ticket status: %w", err)
	}
	return nil
}

func (p *PostgresDB) SetTicketPRHeadSHA(ctx context.Context, ticketID, sha string) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tickets SET pr_head_sha = $1, updated_at = $2 WHERE id = $3`,
		sha, time.Now(), ticketID,
	)
	if err != nil {
		return fmt.Errorf("set ticket pr_head_sha: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return p.scanTicket(p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, pr_number, pr_head_sha, created_at, updated_at FROM tickets WHERE id = $1`, id))
}

func (p *PostgresDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return p.scanTicket(p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, pr_number, pr_head_sha, created_at, updated_at FROM tickets WHERE external_id = $1`, externalID))
}

func (p *PostgresDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
		&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.PRNumber, &t.PRHeadSHA, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan ticket: %w", err)
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (p *PostgresDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, pr_number, pr_head_sha, created_at, updated_at FROM tickets WHERE 1=1`
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
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.PRNumber, &t.PRHeadSHA, &t.CreatedAt, &t.UpdatedAt); err != nil {
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

func (p *PostgresDB) GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, pr_number, pr_head_sha, created_at, updated_at
		 FROM tickets WHERE parent_ticket_id = $1`, parentExternalID)
	if err != nil {
		return nil, fmt.Errorf("get child tickets: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status,
			&t.ParentTicketID, &t.ChannelSenderID, &t.DecomposeDepth, &t.PRNumber, &t.PRHeadSHA, &t.CreatedAt, &t.UpdatedAt); err != nil {
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

func (p *PostgresDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tickets SET last_completed_task_seq = $1, updated_at = $2 WHERE id = $3`,
		taskSeq, time.Now(), ticketID,
	)
	if err != nil {
		return fmt.Errorf("set last completed task: %w", err)
	}
	return nil
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
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return nil
}

func (p *PostgresDB) SetTaskErrorType(ctx context.Context, id, errorType string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE tasks SET last_error_type = $1 WHERE id = $2`, errorType, id)
	if err != nil {
		return fmt.Errorf("set task error type: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetTaskContextStats(ctx context.Context, taskID string) (TaskContextStats, error) {
	var stats TaskContextStats
	err := p.db.QueryRowContext(ctx,
		`SELECT context_budget, context_used, files_selected, files_touched, context_cache_hits FROM tasks WHERE id = $1`,
		taskID,
	).Scan(&stats.Budget, &stats.Used, &stats.FilesSelected, &stats.FilesTouched, &stats.CacheHits)
	if err == sql.ErrNoRows {
		return TaskContextStats{}, fmt.Errorf("get task context stats: %w", ErrNotFound)
	}
	if err != nil {
		return TaskContextStats{}, fmt.Errorf("get task context stats: %w", err)
	}
	return stats, nil
}

func (p *PostgresDB) UpdateTaskContextStats(ctx context.Context, taskID string, stats TaskContextStats) error {
	_, err := p.db.ExecContext(ctx,
		`UPDATE tasks SET context_budget=$1, context_used=$2, files_selected=$3, files_touched=$4, context_cache_hits=$5 WHERE id=$6`,
		stats.Budget, stats.Used, stats.FilesSelected, stats.FilesTouched, stats.CacheHits, taskID,
	)
	if err != nil {
		return fmt.Errorf("update task context stats: %w", err)
	}
	return nil
}

func (p *PostgresDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	var count int
	err := p.db.QueryRowContext(ctx,
		`UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = $1 RETURNING total_llm_calls`, id).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("increment llm calls for task %q: %w", id, err)
	}
	return count, nil
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
		 cache_read_input_tokens, cache_creation_input_tokens, prompt_version, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		call.ID, call.TicketID, taskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage,
		call.CacheReadTokens, call.CacheCreationTokens, call.PromptVersion, call.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record llm call: %w", err)
	}
	return nil
}

func (p *PostgresDB) StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO llm_call_details (llm_call_id, full_prompt, full_response)
		 VALUES ($1, $2, $3)
		 ON CONFLICT(llm_call_id) DO UPDATE SET full_prompt=EXCLUDED.full_prompt, full_response=EXCLUDED.full_response`,
		callID, fullPrompt, fullResponse,
	)
	if err != nil {
		return fmt.Errorf("store call details: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetCallDetails(ctx context.Context, callID string) (string, string, error) {
	var prompt, response string
	err := p.db.QueryRowContext(ctx,
		`SELECT full_prompt, full_response FROM llm_call_details WHERE llm_call_id = $1`, callID,
	).Scan(&prompt, &response)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get call details: %w", err)
	}
	return prompt, response, nil
}

// --- Handoffs ---

func (p *PostgresDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, version, supersedes, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 0, '', $7)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("set handoff: %w", err)
	}
	return nil
}

func (p *PostgresDB) UpdateHandoff(ctx context.Context, id string, value string, supersedes string) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE handoffs SET value = $1, version = version + 1, supersedes = $2 WHERE id = $3`,
		value, supersedes, id,
	)
	if err != nil {
		return fmt.Errorf("update handoff: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update handoff rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("update handoff %q: %w", id, ErrNotFound)
	}
	return nil
}

func (p *PostgresDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, from_role, to_role, key, value, version, supersedes, created_at FROM handoffs
		 WHERE ticket_id = $1 AND (to_role = $2 OR to_role IS NULL OR to_role = '')
		 ORDER BY created_at`, ticketID, forRole)
	if err != nil {
		return nil, fmt.Errorf("get handoffs: %w", err)
	}
	defer rows.Close()

	var handoffs []models.HandoffRecord
	for rows.Next() {
		var h models.HandoffRecord
		if err := rows.Scan(&h.ID, &h.TicketID, &h.FromRole, &h.ToRole, &h.Key, &h.Value, &h.Version, &h.Supersedes, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan handoff row: %w", err)
		}
		handoffs = append(handoffs, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate handoff rows: %w", err)
	}
	return handoffs, nil
}

// --- Progress Patterns ---

func (p *PostgresDB) SaveProgressPattern(ctx context.Context, pp *models.ProgressPattern) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		pp.ID, pp.TicketID, pp.PatternKey, pp.PatternValue, "[]", pp.DiscoveredByTask, pp.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save progress pattern: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at
		 FROM progress_patterns WHERE ticket_id = $1`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("get progress patterns: %w", err)
	}
	defer rows.Close()

	var patterns []models.ProgressPattern
	for rows.Next() {
		var pp models.ProgressPattern
		var dirs string
		if err := rows.Scan(&pp.ID, &pp.TicketID, &pp.PatternKey, &pp.PatternValue, &dirs, &pp.DiscoveredByTask, &pp.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan progress pattern row: %w", err)
		}
		patterns = append(patterns, pp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate progress pattern rows: %w", err)
	}
	return patterns, nil
}

// --- File Reservations ---

func (p *PostgresDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit file reservations: %w", err)
	}
	return nil
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
	if err != nil {
		return fmt.Errorf("release files: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx,
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

// --- Cost ---

func (p *PostgresDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM llm_calls WHERE ticket_id = $1`, ticketID).Scan(&cost)
	if err != nil {
		return 0, fmt.Errorf("get ticket cost: %w", err)
	}
	return cost, nil
}

func (p *PostgresDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := p.db.QueryRowContext(ctx, `SELECT COALESCE(total_usd, 0) FROM cost_daily WHERE date = $1`, date).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get daily cost: %w", err)
	}
	return cost, nil
}

func (p *PostgresDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES ($1, $2)
		 ON CONFLICT (date) DO UPDATE SET total_usd = cost_daily.total_usd + EXCLUDED.total_usd`,
		date, amount)
	if err != nil {
		return fmt.Errorf("record daily cost: %w", err)
	}
	return nil
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
	if err != nil {
		return 0, fmt.Errorf("get monthly cost: %w", err)
	}
	return cost, nil
}

func (p *PostgresDB) ListTasks(ctx context.Context, ticketID string) ([]models.Task, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, sequence, title, description, status, created_at,
		        acceptance_criteria, files_to_read, files_to_modify, test_assertions, depends_on
		 FROM tasks WHERE ticket_id = $1 ORDER BY sequence`,
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

func (p *PostgresDB) ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, role, provider, model, attempt,
		        tokens_input, tokens_output, cost_usd, duration_ms, status,
		        cache_read_input_tokens, cache_creation_input_tokens, prompt_version, created_at
		 FROM llm_calls WHERE ticket_id = $1 ORDER BY created_at DESC`,
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
			&c.CacheReadTokens, &c.CacheCreationTokens, &c.PromptVersion, &c.CreatedAt); err != nil {
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
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	// An empty ticketID means "all events" — omit the WHERE filter.
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events WHERE ($1 = '' OR ticket_id = $1) ORDER BY created_at DESC LIMIT $2`,
		ticketID, limit)
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

// --- Auth ---

func (p *PostgresDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES ($1, $2, $3)`,
		tokenHash, name, time.Now())
	if err != nil {
		return fmt.Errorf("create auth token: %w", err)
	}
	return nil
}

func (p *PostgresDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var revoked bool
	err := p.db.QueryRowContext(ctx,
		`SELECT revoked FROM auth_tokens WHERE token_hash = $1`, tokenHash).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate auth token: %w", err)
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
		return fmt.Errorf("begin transaction: %w", err)
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
			return fmt.Errorf("delete ticket %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete ticket: %w", err)
	}
	return nil
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

func (p *PostgresDB) GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, external_id, title, description, status, parent_ticket_id, channel_sender_id, decompose_depth, created_at, updated_at
		 FROM tickets
		 WHERE pr_url != ''
		 ORDER BY updated_at DESC
		 LIMIT $1`, limit)
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

func (p *PostgresDB) GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
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
// It first cleans up any expired locks, then atomically inserts using ON CONFLICT DO NOTHING.
// Returns acquired=true if this caller now holds the lock.
func (p *PostgresDB) AcquireLock(ctx context.Context, lockName string, ttlSeconds int) (bool, error) {
	// Clean up expired locks first.
	if _, err := p.db.ExecContext(ctx,
		`DELETE FROM distributed_locks WHERE expires_at < NOW()`); err != nil {
		return false, fmt.Errorf("acquire lock cleanup: %w", err)
	}

	// Attempt atomic insert; ON CONFLICT DO NOTHING skips if row already exists.
	res, err := p.db.ExecContext(ctx,
		`INSERT INTO distributed_locks (lock_name, expires_at, holder_id)
		 VALUES ($1, NOW() + ($2 || ' seconds')::interval, $3)
		 ON CONFLICT (lock_name) DO NOTHING`,
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
func (p *PostgresDB) ReleaseLock(ctx context.Context, lockName string) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM distributed_locks WHERE lock_name = $1 AND holder_id = $2`,
		lockName, holderID)
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// --- Embedding Store (REQ-INFRA-002) ---

// UpsertEmbedding inserts or updates an embedding record, serializing the vector as BYTEA.
func (p *PostgresDB) UpsertEmbedding(ctx context.Context, e EmbeddingRecord) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO embeddings (repo_path, head_sha, file_path, start_line, end_line, chunk_text, vector)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (repo_path, head_sha, file_path, start_line) DO UPDATE SET
		     end_line = EXCLUDED.end_line,
		     chunk_text = EXCLUDED.chunk_text,
		     vector = EXCLUDED.vector`,
		e.RepoPath, e.HeadSHA, e.FilePath, e.StartLine, e.EndLine, e.ChunkText, SerializeVector(e.Vector),
	)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

// GetEmbeddingsByRepoSHA retrieves all embedding records for a given repo and commit SHA.
func (p *PostgresDB) GetEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) ([]EmbeddingRecord, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT repo_path, head_sha, file_path, start_line, end_line, chunk_text, vector
		 FROM embeddings WHERE repo_path = $1 AND head_sha = $2`,
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
func (p *PostgresDB) DeleteEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM embeddings WHERE repo_path = $1 AND head_sha = $2`,
		repoPath, headSHA,
	)
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	return nil
}

// DeleteEmbeddingsByRepoExceptSHA deletes all embedding records for the given repo_path
// whose head_sha does NOT match headSHA (i.e. stale indices from previous commits).
func (p *PostgresDB) DeleteEmbeddingsByRepoExceptSHA(ctx context.Context, repoPath, headSHA string) error {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM embeddings WHERE repo_path = $1 AND head_sha != $2`,
		repoPath, headSHA,
	)
	if err != nil {
		return fmt.Errorf("delete stale embeddings: %w", err)
	}
	return nil
}

// WriteContextFeedback inserts a context feedback row recording which files were
// selected vs touched for a completed or failed task.
func (p *PostgresDB) WriteContextFeedback(ctx context.Context, row ContextFeedbackRow) error {
	id := uuid.New().String()
	selected := strings.Join(row.FilesSelected, ",")
	touched := strings.Join(row.FilesTouched, ",")
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO context_feedback (id, ticket_id, task_id, files_selected, files_touched, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, row.TicketID, row.TaskID, selected, touched, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("write context feedback: %w", err)
	}
	return nil
}

// QueryContextFeedback returns prior feedback rows whose files_selected set has
// Jaccard similarity >= minJaccard with the provided candidates set.
// The Jaccard comparison is computed in Go after loading rows.
func (p *PostgresDB) QueryContextFeedback(ctx context.Context, candidates []string, minJaccard float64) ([]ContextFeedbackRow, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, files_selected, files_touched, created_at FROM context_feedback ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("query context feedback: %w", err)
	}
	defer rows.Close()

	candidateSet := toStringSet(candidates)
	var results []ContextFeedbackRow
	for rows.Next() {
		var r ContextFeedbackRow
		var sel, touched string
		if err := rows.Scan(&r.ID, &r.TicketID, &r.TaskID, &sel, &touched, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan context feedback row: %w", err)
		}
		r.FilesSelected = splitFiles(sel)
		r.FilesTouched = splitFiles(touched)

		if jaccardSimilarity(candidateSet, toStringSet(r.FilesSelected)) >= minJaccard {
			results = append(results, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate context feedback rows: %w", err)
	}
	return results, nil
}

// --- Prompt Snapshots (REQ-OBS-001) ---

// UpsertPromptSnapshot inserts or updates the SHA256 hash for a named prompt template.
func (p *PostgresDB) UpsertPromptSnapshot(ctx context.Context, name, sha256 string) error {
	id := uuid.New().String()
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO prompt_snapshots (id, template_name, sha256, recorded_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(template_name) DO UPDATE SET sha256 = EXCLUDED.sha256, recorded_at = EXCLUDED.recorded_at`,
		id, name, sha256, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert prompt snapshot: %w", err)
	}
	return nil
}

// GetPromptSnapshots returns all recorded prompt template snapshots.
func (p *PostgresDB) GetPromptSnapshots(ctx context.Context) ([]PromptSnapshot, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, template_name, sha256, recorded_at FROM prompt_snapshots ORDER BY template_name`)
	if err != nil {
		return nil, fmt.Errorf("get prompt snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []PromptSnapshot
	for rows.Next() {
		var ps PromptSnapshot
		if err := rows.Scan(&ps.ID, &ps.TemplateName, &ps.SHA256, &ps.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan prompt snapshot row: %w", err)
		}
		snapshots = append(snapshots, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prompt snapshot rows: %w", err)
	}
	return snapshots, nil
}

// --- DAG State (ARCH-F03) ---

// SaveDAGState persists or replaces the DAG execution state for a ticket.
func (p *PostgresDB) SaveDAGState(ctx context.Context, ticketID string, state DAGState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal dag state: %w", err)
	}
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO dag_states (ticket_id, state_json, updated_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (ticket_id) DO UPDATE SET state_json = EXCLUDED.state_json, updated_at = EXCLUDED.updated_at`,
		ticketID, string(b), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("save dag state: %w", err)
	}
	return nil
}

// GetDAGState returns the persisted DAG execution state for a ticket.
// Returns (nil, nil) when no state has been saved yet.
func (p *PostgresDB) GetDAGState(ctx context.Context, ticketID string) (*DAGState, error) {
	var stateJSON string
	err := p.db.QueryRowContext(ctx,
		`SELECT state_json FROM dag_states WHERE ticket_id = $1`, ticketID,
	).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dag state: %w", err)
	}
	var state DAGState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("unmarshal dag state: %w", err)
	}
	return &state, nil
}

// DeleteDAGState removes the DAG execution state for a ticket once it has reached a
// terminal state, preventing unbounded growth of the dag_states table.
func (p *PostgresDB) DeleteDAGState(ctx context.Context, ticketID string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM dag_states WHERE ticket_id = $1`, ticketID)
	if err != nil {
		return fmt.Errorf("delete dag state: %w", err)
	}
	return nil
}
