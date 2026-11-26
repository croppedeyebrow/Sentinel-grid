package adapter

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/ids"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/domain"
)

// Integration tests for the telemetry projection. The round trip through
// PostgreSQL is where a counter can quietly fail to persist, which no fake
// repository would catch.

const defaultTestDSN = "postgres://sentinel:change-me@localhost:5432/sentinel_grid?sslmode=disable"

// The seeded site every test unit belongs to.
const testSite = "10000000-0000-0000-0000-000000000001"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("no database available: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// testRepository returns a repository and a patrol unit of its own.
//
// Each test gets a fresh unit rather than sharing a seeded one: the projection
// is a single row per unit, so tests that shared one would read each other's
// state, and the fixture the console shows would be left disturbed.
func testRepository(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()

	unitID, err := ids.NewUUID()
	if err != nil {
		t.Fatalf("new id: %v", err)
	}
	callsign := "IT-" + unitID[:8]

	if _, err := pool.Exec(ctx, `
INSERT INTO patrol_units (id, site_profile_id, callsign, display_name)
VALUES ($1, $2, $3, $3)`, unitID, testSite, callsign); err != nil {
		t.Fatalf("create test unit: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM telemetry_receipts WHERE patrol_unit_id = $1`,
			`DELETE FROM patrol_unit_observations WHERE patrol_unit_id = $1`,
			`DELETE FROM patrol_unit_snapshots WHERE patrol_unit_id = $1`,
			`DELETE FROM patrol_units WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, statement, unitID); err != nil {
				t.Logf("cleanup %s: %v", statement, err)
			}
		}
	})

	return NewPostgresRepository(pool), unitID
}

func eventID(t *testing.T, suffix string) string {
	t.Helper()
	return "itest-" + t.Name() + "-" + suffix
}

func receipt(t *testing.T, unitID, suffix string, sequence int64, outcome domain.Classification, missing int64, observed time.Time) application.Receipt {
	t.Helper()

	battery := 70
	link := 90
	return application.Receipt{
		EventID:       eventID(t, suffix),
		Outcome:       outcome,
		UnitID:        unitID,
		SessionID:     "itest-session",
		Sequence:      sequence,
		ObservedAt:    &observed,
		ReceivedAt:    observed,
		Position:      &domain.Position{Latitude: 37.5665, Longitude: 126.978},
		Battery:       &battery,
		Link:          &link,
		MissingFrames: missing,
		FrameBytes:    180,
		SensorsJSON:   []byte(`{"battery":70,"link":90}`),
	}
}

func TestEveryGapOfASessionSurvivesTheRoundTrip(t *testing.T) {
	repository, unitID := testRepository(t)
	ctx := context.Background()
	consumer := application.NewConsumer(repository)
	start := time.Now().UTC().Add(-time.Minute)

	// The arrival order a real run produced: three refused frames left three
	// holes, and each hole was reported as its own gap.
	steps := []struct {
		suffix   string
		sequence int64
		outcome  domain.Classification
		missing  int64
	}{
		{"1", 1, domain.ClassificationAccepted, 0},
		{"2", 2, domain.ClassificationAccepted, 0},
		{"3", 3, domain.ClassificationAccepted, 0},
		{"5", 5, domain.ClassificationGap, 1},
		{"6", 6, domain.ClassificationAccepted, 0},
		{"8", 8, domain.ClassificationGap, 1},
		{"10", 10, domain.ClassificationGap, 1},
		{"11", 11, domain.ClassificationAccepted, 0},
	}

	for index, step := range steps {
		entry := receipt(t, unitID, step.suffix, step.sequence, step.outcome, step.missing,
			start.Add(time.Duration(index)*time.Second))
		if err := consumer.Handle(ctx, entry); err != nil {
			t.Fatalf("handle %s: %v", step.suffix, err)
		}
	}

	stored, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if stored.MissingFrames != 3 {
		t.Errorf("missingFrames = %d, want 3: every gap of the session counts", stored.MissingFrames)
	}
	if stored.LastSequence != 11 {
		t.Errorf("lastSequence = %d, want 11", stored.LastSequence)
	}
	if stored.SessionID != "itest-session" {
		t.Errorf("sessionId = %q, want the session carried through", stored.SessionID)
	}
}

func TestARefusalIsCountedAndTheReadingIsLeftAlone(t *testing.T) {
	repository, unitID := testRepository(t)
	ctx := context.Background()
	consumer := application.NewConsumer(repository)
	observed := time.Now().UTC().Add(-30 * time.Second)

	if err := consumer.Handle(ctx, receipt(t, unitID, "1", 1, domain.ClassificationAccepted, 0, observed)); err != nil {
		t.Fatalf("handle accepted: %v", err)
	}
	before, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	refused := application.Receipt{
		EventID:    eventID(t, "refused"),
		Rejected:   true,
		Rejection:  &application.Rejection{Code: "VALUE_OUT_OF_RANGE", Detail: "battery is out of range: 150"},
		UnitID:     unitID,
		SessionID:  "itest-session",
		Sequence:   2,
		ReceivedAt: observed.Add(time.Second),
		FrameBytes: 166,
	}
	if err := consumer.Handle(ctx, refused); err != nil {
		t.Fatalf("handle refusal: %v", err)
	}

	after, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if after.RejectedFrames != before.RejectedFrames+1 {
		t.Errorf("rejectedFrames = %d, want %d", after.RejectedFrames, before.RejectedFrames+1)
	}
	if after.Battery == nil || before.Battery == nil || after.Battery.Value != before.Battery.Value {
		t.Error("a refused frame must not disturb a stored reading")
	}
	if !after.LastReceivedAt.After(before.LastReceivedAt) {
		t.Error("a refused frame still proves something arrived")
	}
}

func TestPerSensorTimesSurviveTheRoundTrip(t *testing.T) {
	repository, unitID := testRepository(t)
	ctx := context.Background()
	consumer := application.NewConsumer(repository)
	now := time.Now().UTC()

	// A full frame, then a battery-only frame measured later: the position's
	// age has to keep running while the battery is refreshed.
	full := receipt(t, unitID, "full", 1, domain.ClassificationAccepted, 0, now.Add(-20*time.Minute))
	if err := consumer.Handle(ctx, full); err != nil {
		t.Fatalf("handle full: %v", err)
	}

	battery := 55
	partial := application.Receipt{
		EventID:     eventID(t, "partial"),
		Outcome:     domain.ClassificationAccepted,
		UnitID:      unitID,
		SessionID:   "itest-session",
		Sequence:    2,
		ObservedAt:  timePointer(now.Add(-5 * time.Second)),
		ReceivedAt:  now,
		Battery:     &battery,
		FrameBytes:  120,
		SensorsJSON: []byte(`{"battery":55}`),
	}
	if err := consumer.Handle(ctx, partial); err != nil {
		t.Fatalf("handle partial: %v", err)
	}

	stored, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if stored.Battery == nil || stored.Battery.Value != 55 {
		t.Fatalf("battery = %+v, want 55", stored.Battery)
	}
	if stored.Position == nil {
		t.Fatal("the position must survive a frame that did not report it")
	}
	// This is TEL-01: one sensor stale, another current, in the same row.
	if !stored.Battery.ObservedAt.After(stored.Position.ObservedAt.Add(10 * time.Minute)) {
		t.Errorf("battery observed %s, position observed %s: the two must differ",
			stored.Battery.ObservedAt, stored.Position.ObservedAt)
	}
}

func TestAReDeliveredReceiptChangesNothing(t *testing.T) {
	repository, unitID := testRepository(t)
	ctx := context.Background()
	consumer := application.NewConsumer(repository)
	observed := time.Now().UTC().Add(-time.Minute)

	gap := receipt(t, unitID, "gap", 5, domain.ClassificationGap, 2, observed)
	if err := consumer.Handle(ctx, gap); err != nil {
		t.Fatalf("handle gap: %v", err)
	}
	first, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	// Re-delivery is normal with a consumer group.
	if err := consumer.Handle(ctx, gap); err != nil {
		t.Fatalf("handle re-delivery: %v", err)
	}
	second, err := repository.Snapshot(ctx, unitID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if second.MissingFrames != first.MissingFrames {
		t.Errorf("missingFrames moved from %d to %d on a re-delivery",
			first.MissingFrames, second.MissingFrames)
	}
	if second.LastSequence != first.LastSequence {
		t.Errorf("lastSequence moved from %d to %d on a re-delivery",
			first.LastSequence, second.LastSequence)
	}
}
