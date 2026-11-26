package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/domain"
)

// PostgresRepository stores what telemetry changed.
//
// Telemetry is the only writer of the unit snapshot and observation tables;
// the Operational Picture reads them. One receipt is stored as one
// transaction: the projection, the observation it contributed, and the receipt
// itself either all land or none do, so the receipt log never claims something
// was applied that was not.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository wires the repository to a connection pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const snapshotQuery = `
SELECT ST_Y(position::geometry), ST_X(position::geometry),
       battery_level, link_quality,
       position_observed_at, position_received_at,
       battery_observed_at, battery_received_at,
       link_observed_at, link_received_at,
       coalesce(session_id, ''), session_started_at,
       last_sequence, missing_frames, rejected_frames, last_received_at
FROM patrol_unit_snapshots
WHERE patrol_unit_id = $1`

// Snapshot loads a unit's projected state, or the zero value when nothing has
// been projected for it yet.
func (r *PostgresRepository) Snapshot(ctx context.Context, unitID string) (domain.UnitSnapshot, error) {
	var (
		snapshot                               domain.UnitSnapshot
		latitude, longitude                    *float64
		battery, link                          *int16
		positionObservedAt, positionReceivedAt *time.Time
		batteryObservedAt, batteryReceivedAt   *time.Time
		linkObservedAt, linkReceivedAt         *time.Time
		sessionStartedAt, lastReceivedAt       *time.Time
	)

	err := r.pool.QueryRow(ctx, snapshotQuery, unitID).Scan(
		&latitude, &longitude,
		&battery, &link,
		&positionObservedAt, &positionReceivedAt,
		&batteryObservedAt, &batteryReceivedAt,
		&linkObservedAt, &linkReceivedAt,
		&snapshot.SessionID, &sessionStartedAt,
		&snapshot.LastSequence, &snapshot.MissingFrames, &snapshot.RejectedFrames,
		&lastReceivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UnitSnapshot{UnitID: unitID}, nil
	}
	if err != nil {
		return domain.UnitSnapshot{}, fmt.Errorf("query unit snapshot: %w", err)
	}

	snapshot.UnitID = unitID
	if latitude != nil && longitude != nil && positionObservedAt != nil {
		snapshot.Position = &domain.Reading[domain.Position]{
			Value:      domain.Position{Latitude: *latitude, Longitude: *longitude},
			ObservedAt: *positionObservedAt,
			ReceivedAt: at(positionReceivedAt),
		}
	}
	if battery != nil && batteryObservedAt != nil {
		snapshot.Battery = &domain.Reading[int]{
			Value:      int(*battery),
			ObservedAt: *batteryObservedAt,
			ReceivedAt: at(batteryReceivedAt),
		}
	}
	if link != nil && linkObservedAt != nil {
		snapshot.Link = &domain.Reading[int]{
			Value:      int(*link),
			ObservedAt: *linkObservedAt,
			ReceivedAt: at(linkReceivedAt),
		}
	}
	if lastReceivedAt != nil {
		snapshot.LastReceivedAt = *lastReceivedAt
	}
	if sessionStartedAt != nil {
		snapshot.SessionStartedAt = *sessionStartedAt
	}

	return snapshot, nil
}

// KnownUnit reports whether the unit exists, so a receipt naming an unknown
// one is still stored without breaking its reference.
func (r *PostgresRepository) KnownUnit(ctx context.Context, unitID string) (bool, error) {
	var exists bool

	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM patrol_units WHERE id::text = $1)`, unitID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check patrol unit: %w", err)
	}
	return exists, nil
}

// Seen reports whether this receipt is already stored, which is what makes a
// re-delivered stream entry a no-op.
func (r *PostgresRepository) Seen(ctx context.Context, eventID string) (bool, error) {
	var exists bool

	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM telemetry_receipts WHERE event_id = $1)`, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check telemetry receipt: %w", err)
	}
	return exists, nil
}

// Apply writes one receipt and everything it changed.
func (r *PostgresRepository) Apply(ctx context.Context, update application.Update) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if update.AttachUnit {
		if err := upsertSnapshot(ctx, tx, update.Snapshot); err != nil {
			return err
		}
		if update.RecordObservation {
			if err := insertObservation(ctx, tx, update.Receipt); err != nil {
				return err
			}
		}
	}
	if err := insertReceipt(ctx, tx, update); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func upsertSnapshot(ctx context.Context, tx pgx.Tx, snapshot domain.UnitSnapshot) error {
	var (
		latitude, longitude *float64
		battery, link       *int
	)
	if snapshot.Position != nil {
		latitude = &snapshot.Position.Value.Latitude
		longitude = &snapshot.Position.Value.Longitude
	}
	if snapshot.Battery != nil {
		battery = &snapshot.Battery.Value
	}
	if snapshot.Link != nil {
		link = &snapshot.Link.Value
	}

	_, err := tx.Exec(ctx, `
INSERT INTO patrol_unit_snapshots (
    patrol_unit_id, position, battery_level, link_quality,
    position_observed_at, position_received_at,
    battery_observed_at, battery_received_at,
    link_observed_at, link_received_at,
    session_id, session_started_at, last_sequence, missing_frames, rejected_frames,
    last_received_at, updated_at
) VALUES (
    $1,
    CASE WHEN $2::double precision IS NULL THEN NULL
         ELSE ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography END,
    $4, $5,
    $6, $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17, now()
)
ON CONFLICT (patrol_unit_id) DO UPDATE SET
    position = excluded.position,
    battery_level = excluded.battery_level,
    link_quality = excluded.link_quality,
    position_observed_at = excluded.position_observed_at,
    position_received_at = excluded.position_received_at,
    battery_observed_at = excluded.battery_observed_at,
    battery_received_at = excluded.battery_received_at,
    link_observed_at = excluded.link_observed_at,
    link_received_at = excluded.link_received_at,
    session_id = excluded.session_id,
    session_started_at = excluded.session_started_at,
    last_sequence = excluded.last_sequence,
    missing_frames = excluded.missing_frames,
    rejected_frames = excluded.rejected_frames,
    last_received_at = excluded.last_received_at,
    updated_at = now()`,
		snapshot.UnitID,
		latitude, longitude,
		battery, link,
		observedAt(snapshot.Position), receivedAt(snapshot.Position),
		observedAt(snapshot.Battery), receivedAt(snapshot.Battery),
		observedAt(snapshot.Link), receivedAt(snapshot.Link),
		nullableText(snapshot.SessionID), nullableTime(timePointer(snapshot.SessionStartedAt)),
		snapshot.LastSequence, snapshot.MissingFrames, snapshot.RejectedFrames,
		nullableTime(timePointer(snapshot.LastReceivedAt)),
	)
	if err != nil {
		return fmt.Errorf("upsert unit snapshot: %w", err)
	}
	return nil
}

// insertObservation records the measurement in the history mission coverage is
// judged from, with the frame it came from so a verdict can be traced back.
func insertObservation(ctx context.Context, tx pgx.Tx, receipt application.Receipt) error {
	var latitude, longitude *float64
	if receipt.Position != nil {
		latitude = &receipt.Position.Latitude
		longitude = &receipt.Position.Longitude
	}

	_, err := tx.Exec(ctx, `
INSERT INTO patrol_unit_observations (
    patrol_unit_id, position, battery_level, link_quality,
    observed_at, received_at, session_id, sequence
) VALUES (
    $1,
    CASE WHEN $2::double precision IS NULL THEN NULL
         ELSE ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography END,
    $4, $5, $6, $7, $8, $9
)`,
		receipt.UnitID,
		latitude, longitude,
		receipt.Battery, receipt.Link,
		receipt.ObservedAt, receipt.ReceivedAt,
		nullableText(receipt.SessionID), nullableSequence(receipt.Sequence),
	)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

func insertReceipt(ctx context.Context, tx pgx.Tx, update application.Update) error {
	receipt := update.Receipt

	var unitID *string
	if update.AttachUnit {
		id := receipt.UnitID
		unitID = &id
	}

	var missing *int64
	if receipt.Outcome == domain.ClassificationGap && receipt.MissingFrames > 0 {
		value := receipt.MissingFrames
		missing = &value
	}

	var code, detail *string
	outcome := string(receipt.Outcome)
	if receipt.Rejected {
		outcome = "REJECTED"
		if receipt.Rejection != nil {
			code = &receipt.Rejection.Code
			detail = &receipt.Rejection.Detail
		}
	}

	var sensors any
	if len(receipt.SensorsJSON) > 0 {
		sensors = string(receipt.SensorsJSON)
	}

	_, err := tx.Exec(ctx, `
INSERT INTO telemetry_receipts (
    event_id, patrol_unit_id, session_id, sequence, outcome,
    observed_at, received_at, sensors, missing_frames,
    rejection_code, rejection_detail, frame_bytes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (event_id) DO NOTHING`,
		receipt.EventID, unitID, nullableText(receipt.SessionID),
		nullableSequence(receipt.Sequence), outcome,
		receipt.ObservedAt, receipt.ReceivedAt, sensors, missing,
		code, detail, receipt.FrameBytes,
	)
	if err != nil {
		return fmt.Errorf("insert telemetry receipt: %w", err)
	}
	return nil
}

func observedAt[T any](reading *domain.Reading[T]) *time.Time {
	if reading == nil {
		return nil
	}
	at := reading.ObservedAt
	return &at
}

func receivedAt[T any](reading *domain.Reading[T]) *time.Time {
	if reading == nil {
		return nil
	}
	at := reading.ReceivedAt
	return &at
}

func at(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func nullableTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return value
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableSequence(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
