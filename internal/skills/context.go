// internal/skills/context.go
package skills

import (
	"context"

	"github.com/canhta/foreman/internal/models"
)

// HandoffAccessor provides read/write access to handoff records in the database.
// It is a subset of db.Database, allowing injection of a minimal stub in tests.
type HandoffAccessor interface {
	SetHandoff(ctx context.Context, h *models.HandoffRecord) error
	GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error)
}

// ProgressAccessor provides write access to progress patterns in the database.
// It is a subset of db.Database, allowing injection of a minimal stub in tests.
type ProgressAccessor interface {
	SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error
}

// SkillEventEmitter can emit structured events during skill execution.
// It mirrors the signature of telemetry.EventEmitter.Emit so that *telemetry.EventEmitter
// satisfies this interface without an adapter.
type SkillEventEmitter interface {
	Emit(ctx context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string)
}
