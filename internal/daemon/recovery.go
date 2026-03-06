package daemon

import (
	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// RecoveryAction describes what to do with an in-progress ticket after a crash.
type RecoveryAction string

const (
	RecoveryReplan RecoveryAction = "replan" // Start over from planning
	RecoveryResume RecoveryAction = "resume" // Resume from last completed task
	RecoverySkip   RecoveryAction = "skip"   // Already complete, do nothing
)

// RecoveryPlan describes how to recover a specific ticket.
type RecoveryPlan struct {
	Action        RecoveryAction
	ResumeFromSeq int // Only set when Action == RecoveryResume
}

// ClassifyRecovery determines how to recover an in-progress ticket.
func ClassifyRecovery(ticket *models.Ticket) RecoveryPlan {
	if ticket == nil {
		return RecoveryPlan{Action: RecoveryReplan}
	}
	switch ticket.Status {
	case models.TicketStatusDone, models.TicketStatusFailed, models.TicketStatusPartial, models.TicketStatusBlocked, models.TicketStatusPRCreated:
		return RecoveryPlan{Action: RecoverySkip}

	case models.TicketStatusPlanning, models.TicketStatusPlanValidating:
		if ticket.LastCompletedTaskSeq == 0 {
			return RecoveryPlan{Action: RecoveryReplan}
		}
		return RecoveryPlan{Action: RecoveryResume, ResumeFromSeq: ticket.LastCompletedTaskSeq}

	case models.TicketStatusImplementing, models.TicketStatusReviewing:
		return RecoveryPlan{Action: RecoveryResume, ResumeFromSeq: ticket.LastCompletedTaskSeq}

	default:
		// Queued or unknown — re-queue
		return RecoveryPlan{Action: RecoveryReplan}
	}
}

// TasksToReset returns tasks that were in progress at crash time and need resetting to pending.
func TasksToReset(tasks []models.Task, lastCompletedSeq int) []models.Task {
	// Validate bounds: clamp to [0, len(tasks)].
	if lastCompletedSeq < 0 || lastCompletedSeq > len(tasks) {
		log.Warn().
			Int("last_completed_seq", lastCompletedSeq).
			Int("task_count", len(tasks)).
			Msg("lastCompletedSeq out of bounds, treating as 0 (full restart)")
		lastCompletedSeq = 0
	}

	toReset := make([]models.Task, 0)
	for _, task := range tasks {
		if task.Sequence <= lastCompletedSeq {
			continue // Already committed, leave as done
		}
		if task.Status != models.TaskStatusPending && task.Status != models.TaskStatusDone {
			// Was in progress when crash happened — needs reset
			toReset = append(toReset, task)
		}
	}
	return toReset
}
