package daemon

import (
	"testing"

	"github.com/canhta/foreman/internal/db"
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

// TestClassifyRecovery_NegativeLastCompletedTaskSeq verifies BUG-M04:
// If the database has a corrupted negative LastCompletedTaskSeq, ClassifyRecovery
// must treat the ticket as RecoveryReplan (not RecoveryResume from an invalid index).
func TestClassifyRecovery_NegativeLastCompletedTaskSeq(t *testing.T) {
	ticket := &models.Ticket{
		Status:               models.TicketStatusImplementing,
		LastCompletedTaskSeq: -1,
	}
	action := ClassifyRecovery(ticket)
	assert.Equal(t, RecoveryReplan, action.Action,
		"negative LastCompletedTaskSeq must trigger RecoveryReplan, not RecoveryResume")
}

// TestClassifyRecovery_PlanningWithNegativeSeq verifies that the Planning/PlanValidating
// branch also guards against negative LastCompletedTaskSeq, consistent with the M04 fix
// applied to the Implementing/Reviewing branch.
func TestClassifyRecovery_PlanningWithNegativeSeq(t *testing.T) {
	for _, status := range []models.TicketStatus{
		models.TicketStatusPlanning,
		models.TicketStatusPlanValidating,
	} {
		ticket := &models.Ticket{
			Status:               status,
			LastCompletedTaskSeq: -1,
		}
		action := ClassifyRecovery(ticket)
		assert.Equal(t, RecoveryReplan, action.Action,
			"negative LastCompletedTaskSeq in %s must trigger RecoveryReplan, not RecoveryResume", status)
	}
}

// TestClassifyRecovery_AwaitingMerge verifies that awaiting_merge tickets are skipped
// on daemon restart rather than being replanned (which would create duplicate PRs).
func TestClassifyRecovery_AwaitingMerge(t *testing.T) {
	for _, status := range []models.TicketStatus{
		models.TicketStatusAwaitingMerge,
		models.TicketStatusMerged,
		models.TicketStatusPRClosed,
	} {
		ticket := &models.Ticket{Status: status}
		action := ClassifyRecovery(ticket)
		assert.Equal(t, RecoverySkip, action.Action,
			"status %s should be RecoverySkip to avoid duplicate PRs", status)
	}
}

// TestRecovery_SkipsCompletedDAGTasks verifies that TasksForDAGRecovery filters out
// tasks that are already recorded as completed in the DAGState. Given tasks A, B, C
// where A is in the DAGState.CompletedTasks list, only B and C are returned for
// re-execution.
func TestRecovery_SkipsCompletedDAGTasks(t *testing.T) {
	dagState := &db.DAGState{
		TicketID:       "ticket-1",
		CompletedTasks: []string{"task-A"},
	}

	allTasks := []DAGTask{
		{ID: "task-A"},
		{ID: "task-B"},
		{ID: "task-C"},
	}

	pending := TasksForDAGRecovery(allTasks, dagState)

	ids := make([]string, 0, len(pending))
	for _, t := range pending {
		ids = append(ids, t.ID)
	}
	assert.ElementsMatch(t, []string{"task-B", "task-C"}, ids,
		"completed task-A must be skipped; task-B and task-C must be scheduled")
}

// TestRecovery_NilDAGState_ReturnsAllTasks verifies that when no DAGState exists
// (first run or state lost), all tasks are returned for execution.
func TestRecovery_NilDAGState_ReturnsAllTasks(t *testing.T) {
	allTasks := []DAGTask{
		{ID: "task-A"},
		{ID: "task-B"},
	}

	pending := TasksForDAGRecovery(allTasks, nil)
	assert.Len(t, pending, 2, "nil DAGState must return all tasks unchanged")
}
