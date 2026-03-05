package daemon

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestClassifyRecovery_PlanningPhase(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusPlanning,
		LastCompletedTaskSeq: 0,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryReplan, action.Action)
}

func TestClassifyRecovery_ImplementingWithProgress(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusImplementing,
		LastCompletedTaskSeq: 3,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryResume, action.Action)
	assert.Equal(t, 3, action.ResumeFromSeq)
}

func TestClassifyRecovery_ReviewingPhase(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusReviewing,
		LastCompletedTaskSeq: 5,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryResume, action.Action)
	assert.Equal(t, 5, action.ResumeFromSeq)
}

func TestClassifyRecovery_AlreadyDone(t *testing.T) {
	ticket := &models.Ticket{
		Status: models.TicketStatusDone,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoverySkip, action.Action)
}

func TestResetTasksForRecovery(t *testing.T) {
	tasks := []models.Task{
		{Sequence: 1, Status: models.TaskStatusDone},
		{Sequence: 2, Status: models.TaskStatusDone},
		{Sequence: 3, Status: models.TaskStatusImplementing}, // Was in progress
		{Sequence: 4, Status: models.TaskStatusPending},
	}

	toReset := TasksToReset(tasks, 2) // Last completed was seq 2
	assert.Len(t, toReset, 1)
	assert.Equal(t, 3, toReset[0].Sequence) // Task 3 should be reset
}

func TestResetTasksForRecovery_NoneToReset(t *testing.T) {
	tasks := []models.Task{
		{Sequence: 1, Status: models.TaskStatusDone},
		{Sequence: 2, Status: models.TaskStatusPending},
	}

	toReset := TasksToReset(tasks, 1)
	assert.Empty(t, toReset)
}

func TestClassifyRecovery_AlreadyDone_Variants(t *testing.T) {
	statuses := []models.TicketStatus{
		models.TicketStatusFailed,
		models.TicketStatusPartial,
		models.TicketStatusBlocked,
		models.TicketStatusPRCreated,
	}
	for _, status := range statuses {
		ticket := &models.Ticket{Status: status}
		action := ClassifyRecovery(ticket)
		assert.Equal(t, RecoverySkip, action.Action, "status: %s", status)
	}
}

func TestClassifyRecovery_PlanningPhaseWithProgress(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusPlanning,
		LastCompletedTaskSeq: 2,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryResume, action.Action)
	assert.Equal(t, 2, action.ResumeFromSeq)
}

func TestClassifyRecovery_DefaultStatus(t *testing.T) {
	ticket := &models.Ticket{
		Status: models.TicketStatusQueued,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryReplan, action.Action)
}

func TestClassifyRecovery_NilTicket(t *testing.T) {
	action := ClassifyRecovery(nil)
	assert.Equal(t, RecoveryReplan, action.Action)
}
