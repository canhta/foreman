package db

import (
	"context"
	"io"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type Database interface {
	// Tickets
	CreateTicket(ctx context.Context, t *models.Ticket) error
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
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

	// LLM calls
	RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error
	ListLlmCalls(ctx context.Context, ticketID string) ([]models.LlmCallRecord, error)
	StoreCallDetails(ctx context.Context, callID, fullPrompt, fullResponse string) error
	GetCallDetails(ctx context.Context, callID string) (fullPrompt, fullResponse string, err error)

	// Handoffs
	SetHandoff(ctx context.Context, h *models.HandoffRecord) error
	GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error)

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

	io.Closer
}
