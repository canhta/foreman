package db

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// ErrNotFound is returned by database methods when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// PromptSnapshot records the SHA256 hash of a prompt template file at a point in time.
type PromptSnapshot struct {
	RecordedAt   time.Time `json:"recorded_at"`
	ID           string    `json:"id"`
	TemplateName string    `json:"template_name"`
	SHA256       string    `json:"sha256"`
}

// ContextFeedbackRow represents a recorded observation of files selected vs files touched
// for a completed or failed task. It is used to boost context file scores for similar tasks.
//
//nolint:govet // fieldalignment: all orderings of (3×string + 2×[]string + time.Time) produce 120 bytes
type ContextFeedbackRow struct {
	CreatedAt     time.Time
	FilesSelected []string
	FilesTouched  []string
	ID            string
	TicketID      string
	TaskID        string
}

// TaskContextStats holds token budget utilization data for a task.
type TaskContextStats struct {
	Budget        int
	Used          int
	FilesSelected int
	FilesTouched  int
	CacheHits     int
}

type Database interface {
	// Tickets
	CreateTicket(ctx context.Context, t *models.Ticket) error
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	SetTicketPRHeadSHA(ctx context.Context, ticketID, sha string) error
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	GetChildTickets(ctx context.Context, parentExternalID string) ([]models.Ticket, error)
	SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error

	// Tasks
	CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	SetTaskErrorType(ctx context.Context, id, errorType string) error
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)
	ListTasks(ctx context.Context, ticketID string) ([]models.Task, error)
	GetTaskContextStats(ctx context.Context, taskID string) (TaskContextStats, error)
	UpdateTaskContextStats(ctx context.Context, taskID string, stats TaskContextStats) error

	// LLM calls
	RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error
	GetCallDetails(ctx context.Context, callID string) (fullPrompt, fullResponse string, err error)

	// Handoffs
	SetHandoff(ctx context.Context, h *models.HandoffRecord) error
	GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error)
	UpdateHandoff(ctx context.Context, id string, value string, supersedes string) error

	// Progress patterns
	SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error
	GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error)

	// File reservations
	ReserveFiles(ctx context.Context, ticketID string, paths []string) error
	ReleaseFiles(ctx context.Context, ticketID string) error
	GetReservedFiles(ctx context.Context) (map[string]string, error)
	TryReserveFiles(ctx context.Context, ticketID string, paths []string) ([]string, error)

	// Cost
	GetTicketCost(ctx context.Context, ticketID string) (float64, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	GetMonthlyCost(ctx context.Context, yearMonth string) (float64, error)
	RecordDailyCost(ctx context.Context, date string, amount float64) error

	// Events
	RecordEvent(ctx context.Context, e *models.EventRecord) error
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)

	// Auth
	CreateAuthToken(ctx context.Context, tokenHash, name string) error
	ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error)

	// Pairing
	CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error
	GetPairing(ctx context.Context, code string) (*models.Pairing, error)
	DeletePairing(ctx context.Context, code string) error
	ListPairings(ctx context.Context, channel string) ([]models.Pairing, error)
	DeleteExpiredPairings(ctx context.Context) error

	// Channel queries
	FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error)

	// Dashboard mutations
	DeleteTicket(ctx context.Context, id string) error

	// Dashboard aggregates
	GetTeamStats(ctx context.Context, since time.Time) ([]models.TeamStat, error)
	GetRecentPRs(ctx context.Context, limit int) ([]models.Ticket, error)
	GetTicketSummaries(ctx context.Context, filter models.TicketFilter) ([]models.TicketSummary, error)
	GetGlobalEvents(ctx context.Context, limit, offset int) ([]models.EventRecord, error)

	// Distributed Locks
	AcquireLock(ctx context.Context, lockName string, ttlSeconds int) (acquired bool, err error)
	ReleaseLock(ctx context.Context, lockName string) error

	// Embedding store
	UpsertEmbedding(ctx context.Context, e EmbeddingRecord) error
	GetEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) ([]EmbeddingRecord, error)
	DeleteEmbeddingsByRepoSHA(ctx context.Context, repoPath, headSHA string) error
	// DeleteEmbeddingsByRepoExceptSHA deletes all embedding records for a repo_path
	// whose head_sha does NOT match the given headSHA. Used to evict stale indices.
	DeleteEmbeddingsByRepoExceptSHA(ctx context.Context, repoPath, headSHA string) error

	// Context feedback
	// WriteContextFeedback records files selected vs files touched for a task.
	WriteContextFeedback(ctx context.Context, row ContextFeedbackRow) error
	// QueryContextFeedback returns prior feedback rows whose files_selected set has
	// Jaccard similarity >= minJaccard with the provided candidates set.
	QueryContextFeedback(ctx context.Context, candidates []string, minJaccard float64) ([]ContextFeedbackRow, error)

	// Prompt version snapshots (REQ-OBS-001)
	// UpsertPromptSnapshot stores (or updates) the SHA256 hash for a named prompt template.
	UpsertPromptSnapshot(ctx context.Context, name, sha256 string) error
	// GetPromptSnapshots returns all recorded prompt template snapshots.
	GetPromptSnapshots(ctx context.Context) ([]PromptSnapshot, error)

	io.Closer
}
