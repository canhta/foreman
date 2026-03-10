package channel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// RouterDB is the subset of db.Database needed by ChannelRouter.
type RouterDB interface {
	PairingDB
	CreateTicket(ctx context.Context, t *models.Ticket) error
	FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	AppendTicketDescription(ctx context.Context, id, text string) error
	CreateChatMessage(ctx context.Context, msg *models.ChatMessage) error
}

// ChannelRouter implements InboundHandler and routes messages to the right action.
type ChannelRouter struct {
	channel    Channel
	db         RouterDB
	classifier *Classifier
	allowlist  *Allowlist
	pairing    *PairingManager
	commands   CommandHandler
	logger     zerolog.Logger
}

// NewRouter creates a ChannelRouter.
func NewRouter(
	channel Channel,
	db RouterDB,
	classifier *Classifier,
	allowlist *Allowlist,
	pairing *PairingManager,
	commands CommandHandler,
	logger zerolog.Logger,
) *ChannelRouter {
	return &ChannelRouter{
		channel:    channel,
		db:         db,
		classifier: classifier,
		allowlist:  allowlist,
		pairing:    pairing,
		commands:   commands,
		logger:     logger.With().Str("component", "channel-router").Logger(),
	}
}

// HandleMessage processes an inbound message from the channel.
func (r *ChannelRouter) HandleMessage(ctx context.Context, msg InboundMessage) error {
	// 1. Check allowlist
	if !r.allowlist.IsAllowed(msg.SenderID) {
		return r.handleUnknownSender(ctx, msg)
	}

	// 2. Check for active clarification (before classifier — context makes intent obvious)
	if r.db != nil {
		ticket, err := r.db.FindActiveClarification(ctx, msg.SenderID)
		if err != nil {
			r.logger.Error().Err(err).Msg("failed to check clarification")
		} else if ticket != nil {
			return r.handleClarificationReply(ctx, msg, ticket)
		}
	}

	// 3. Classify message
	kind := r.classifier.Classify(ctx, msg.Body)

	switch kind.Kind {
	case "command":
		return r.handleCommand(ctx, msg, kind.Command)
	default:
		return r.handleNewTicket(ctx, msg)
	}
}

func (r *ChannelRouter) handleUnknownSender(ctx context.Context, msg InboundMessage) error {
	if r.pairing == nil {
		r.logger.Warn().Str("sender", msg.SenderID).Msg("rejected message from unknown sender")
		return nil
	}

	code, err := r.pairing.Challenge(ctx, msg.SenderID)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to create pairing challenge")
		return nil
	}

	reply := fmt.Sprintf("Pairing code: %s\nRun: foreman pairing approve %s", code, code)
	if err := r.channel.Send(ctx, msg.SenderID, reply); err != nil {
		r.logger.Error().Err(err).Msg("failed to send pairing challenge")
	}
	return nil
}

func (r *ChannelRouter) handleCommand(ctx context.Context, msg InboundMessage, command string) error {
	if r.commands == nil {
		return nil
	}

	var reply string
	var err error
	switch command {
	case "status":
		reply, err = r.commands.Status(ctx)
	case "pause":
		reply, err = r.commands.Pause(ctx)
	case "resume":
		reply, err = r.commands.Resume(ctx)
	case "cost":
		reply, err = r.commands.Cost(ctx)
	default:
		reply = fmt.Sprintf("Unknown command: %s", command)
	}

	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}

	return r.channel.Send(ctx, msg.SenderID, reply)
}

func (r *ChannelRouter) handleClarificationReply(ctx context.Context, msg InboundMessage, ticket *models.Ticket) error {
	if err := r.db.AppendTicketDescription(ctx, ticket.ID, msg.Body); err != nil {
		r.logger.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to append clarification reply")
		return err
	}

	chatMsg := &models.ChatMessage{
		ID:          fmt.Sprintf("chat-%s-%d", ticket.ID, time.Now().UnixNano()),
		TicketID:    ticket.ID,
		Sender:      "user",
		MessageType: "reply",
		Content:     msg.Body,
		CreatedAt:   time.Now(),
	}
	if err := r.db.CreateChatMessage(ctx, chatMsg); err != nil {
		r.logger.Warn().Err(err).Str("ticket", ticket.ID).Msg("failed to write clarification reply chat message")
	}

	if err := r.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); err != nil {
		r.logger.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to requeue after clarification")
		return err
	}

	reply := fmt.Sprintf("Updated ticket #%s, resuming...", ticket.ID)
	if err := r.channel.Send(ctx, msg.SenderID, reply); err != nil {
		r.logger.Error().Err(err).Msg("failed to send clarification confirmation")
	}
	return nil
}

func (r *ChannelRouter) handleNewTicket(ctx context.Context, msg InboundMessage) error {
	if r.db == nil {
		r.logger.Error().Str("sender", msg.SenderID).Msg("cannot create ticket: router has no DB")
		_ = r.channel.Send(ctx, msg.SenderID, "System not ready — ticket could not be created.")
		return nil
	}

	title := msg.Body
	if i := strings.IndexAny(title, "\n\r"); i > 0 {
		title = title[:i]
	}
	if len(title) > 120 {
		title = title[:120]
	}

	now := time.Now()
	ticket := &models.Ticket{
		ID:              uuid.New().String(),
		Title:           title,
		Description:     msg.Body,
		Status:          models.TicketStatusQueued,
		ChannelSenderID: msg.SenderID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := r.db.CreateTicket(ctx, ticket); err != nil {
		r.logger.Error().Err(err).Str("sender", msg.SenderID).Msg("failed to create ticket from channel message")
		_ = r.channel.Send(ctx, msg.SenderID, "Sorry, failed to create ticket. Please try again.")
		return err
	}

	r.logger.Info().Str("sender", msg.SenderID).Str("ticket", ticket.ID).Msg("ticket created from channel message")
	reply := fmt.Sprintf("Ticket #%s created — I'll get to work on it.", ticket.ID)
	if err := r.channel.Send(ctx, msg.SenderID, reply); err != nil {
		r.logger.Error().Err(err).Msg("failed to send ticket confirmation")
	}
	return nil
}
