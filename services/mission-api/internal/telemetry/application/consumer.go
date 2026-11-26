// Package application drives telemetry ingest: it reads what the gateway
// published, lets the domain decide what each event changes, and persists the
// outcome together with the receipt.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/domain"
)

// Receipt is one entry of the telemetry stream: what arrived, and what the
// gateway made of it. Everything received becomes a receipt, including a
// refused frame, because the fact of receipt has to survive even when the
// value is not applied.
type Receipt struct {
	EventID   string
	Outcome   domain.Classification
	Rejected  bool
	Rejection *Rejection

	UnitID     string
	SessionID  string
	Sequence   int64
	ObservedAt *time.Time
	ReceivedAt time.Time

	Position *domain.Position
	Battery  *int
	Link     *int

	MissingFrames int64
	FrameBytes    int
	// SensorsJSON is the sensors exactly as published, stored so the receipt
	// log can be read back without reconstructing it.
	SensorsJSON []byte
}

// Rejection is why a frame was refused.
type Rejection struct {
	Code   string
	Detail string
}

// Repository is the port to stored state. It lives here, on the consumer side,
// so the adapter depends on the application.
type Repository interface {
	// Snapshot loads the projected state of a unit, or the zero value when
	// the unit has no snapshot yet.
	Snapshot(ctx context.Context, unitID string) (domain.UnitSnapshot, error)
	// Apply persists one receipt: the snapshot it produced, the observation it
	// contributed, and the receipt itself, in one transaction.
	Apply(ctx context.Context, update Update) error
	// KnownUnit reports whether the unit exists, so a receipt for an unknown
	// unit is still logged without breaking its foreign key.
	KnownUnit(ctx context.Context, unitID string) (bool, error)
	// Seen reports whether this receipt was already stored, which is what
	// makes a re-delivered stream entry a no-op.
	Seen(ctx context.Context, eventID string) (bool, error)
}

// Update is everything one receipt changes.
type Update struct {
	Receipt  Receipt
	Snapshot domain.UnitSnapshot
	// RecordObservation is true when the event carried values worth keeping in
	// the observation history that mission coverage is judged from.
	RecordObservation bool
	// AttachUnit is false when the receipt names a unit the platform does not
	// know; the receipt is still stored, without the reference.
	AttachUnit bool
}

// ErrUnusableReceipt reports a receipt the consumer cannot act on at all.
var ErrUnusableReceipt = errors.New("receipt cannot be applied")

// Consumer applies receipts to the projection.
type Consumer struct {
	repository Repository
}

// NewConsumer wires a consumer to its repository.
func NewConsumer(repository Repository) *Consumer {
	return &Consumer{repository: repository}
}

// Handle applies one receipt.
//
// Re-delivery is normal with a consumer group, so the same receipt may arrive
// twice: it is stored once, and the projection rules make a second application
// a no-op anyway.
func (c *Consumer) Handle(ctx context.Context, receipt Receipt) error {
	if receipt.EventID == "" {
		return fmt.Errorf("%w: no event id", ErrUnusableReceipt)
	}
	if receipt.ReceivedAt.IsZero() {
		return fmt.Errorf("%w: no receipt time", ErrUnusableReceipt)
	}

	seen, err := c.repository.Seen(ctx, receipt.EventID)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}

	known := false
	if receipt.UnitID != "" {
		if known, err = c.repository.KnownUnit(ctx, receipt.UnitID); err != nil {
			return err
		}
	}

	update := Update{Receipt: receipt, AttachUnit: known}

	// A refused frame changes no value. It still counts as received, because a
	// unit whose frames are all refused is not a healthy unit and the operator
	// has to be able to see that.
	if receipt.Rejected {
		if known {
			snapshot, err := c.repository.Snapshot(ctx, receipt.UnitID)
			if err != nil {
				return err
			}
			update.Snapshot = snapshot.NoteRejection(receipt.UnitID, receipt.ReceivedAt)
		}
		return c.repository.Apply(ctx, update)
	}

	if !known {
		// Nothing to project onto, but the receipt is kept so the frame is not
		// silently lost.
		return c.repository.Apply(ctx, update)
	}

	snapshot, err := c.repository.Snapshot(ctx, receipt.UnitID)
	if err != nil {
		return err
	}

	updated, result := snapshot.Apply(receipt.event())
	update.Snapshot = updated
	// Only measurements that actually moved a sensor forward belong in the
	// observation history: a duplicate or a late frame would otherwise distort
	// the coverage evidence.
	update.RecordObservation = len(result.Sensors) > 0

	return c.repository.Apply(ctx, update)
}

// event converts a receipt into the domain event the projection applies.
func (r Receipt) event() domain.Event {
	observedAt := time.Time{}
	if r.ObservedAt != nil {
		observedAt = *r.ObservedAt
	}

	return domain.Event{
		UnitID:         r.UnitID,
		SessionID:      r.SessionID,
		Sequence:       r.Sequence,
		Classification: r.Outcome,
		ObservedAt:     observedAt,
		ReceivedAt:     r.ReceivedAt,
		Position:       r.Position,
		Battery:        r.Battery,
		Link:           r.Link,
		MissingFrames:  r.MissingFrames,
	}
}
