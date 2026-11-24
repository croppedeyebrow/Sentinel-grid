package adapter

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/ids"
)

// Integration tests against the database the service actually runs on. The
// spatial queries, the conditional update that implements optimistic locking,
// and the constraints that back the aggregate's invariants cannot be proven
// with a fake repository, which is why the scenario asks for these.
//
// They skip when no database is reachable, so `go test ./...` still works
// without Docker running.

const defaultTestDSN = "postgres://sentinel:change-me@localhost:5432/sentinel_grid?sslmode=disable"

// Identifiers from database/seed.
const (
	seededArea  = "20000000-0000-0000-0000-000000000001"
	seededRoute = "40000000-0000-0000-0000-000000000001"
	unitSG01    = "30000000-0000-0000-0000-000000000001"
	unitSG02    = "30000000-0000-0000-0000-000000000002"
)

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

// testRepository returns a repository and a tracker. Every mission id handed
// to the tracker is deleted when the test ends, so runs never leak into each
// other or into the fixture the console shows.
func testRepository(t *testing.T) (*PostgresRepository, func(missionID string)) {
	t.Helper()
	pool := testPool(t)

	missionIDs := make([]string, 0)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, missionID := range missionIDs {
			if _, err := pool.Exec(ctx, `DELETE FROM missions WHERE id = $1`, missionID); err != nil {
				t.Logf("cleanup mission %s: %v", missionID, err)
			}
		}
	})

	return NewPostgresRepository(pool), func(missionID string) {
		missionIDs = append(missionIDs, missionID)
	}
}

func newID(t *testing.T) string {
	t.Helper()
	id, err := ids.NewUUID()
	if err != nil {
		t.Fatalf("new id: %v", err)
	}
	return id
}

func testWindow(offsetStart, offsetEnd time.Duration) domain.Window {
	now := time.Now().UTC()
	return domain.Window{Start: now.Add(offsetStart), End: now.Add(offsetEnd)}
}

var siteCentre = domain.Position{Latitude: 37.5665, Longitude: 126.978}

// storedMission plans a mission, writes it, and registers it for cleanup.
func storedMission(
	t *testing.T,
	repository *PostgresRepository,
	track func(string),
	window domain.Window,
	unitIDs ...string,
) domain.Mission {
	t.Helper()
	ctx := context.Background()

	site, err := repository.Site(ctx)
	if err != nil {
		t.Fatalf("site: %v", err)
	}

	mission := domain.Mission{
		ID:            newID(t),
		Name:          "itest " + t.Name(),
		Status:        domain.StatusDraft,
		PatrolAreaID:  seededArea,
		PatrolRouteID: seededRoute,
		Window:        window,
		CreatedAt:     time.Now().UTC(),
	}
	if err := mission.AddCheckpoint(domain.Checkpoint{
		ID:           newID(t),
		Name:         "CP-1",
		Position:     siteCentre,
		RadiusMeters: 40,
	}); err != nil {
		t.Fatalf("add checkpoint: %v", err)
	}
	for index, unitID := range unitIDs {
		if err := mission.Assign(unitID, []string{"SG-01", "SG-02"}[index]); err != nil {
			t.Fatalf("assign: %v", err)
		}
	}

	track(mission.ID)
	if err := repository.Create(ctx, site.ID, mission, "itest-key-"+mission.ID); err != nil {
		t.Fatalf("create: %v", err)
	}
	return mission
}

func TestCreateAndLoadRoundTrip(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	mission := storedMission(t, repository, track, testWindow(-time.Hour, time.Hour), unitSG01)

	loaded, err := repository.Mission(ctx, mission.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Name != mission.Name || loaded.Status != domain.StatusDraft {
		t.Errorf("loaded = %s/%s, want %s/DRAFT", loaded.Name, loaded.Status, mission.Name)
	}
	if len(loaded.Checkpoints) != 1 {
		t.Fatalf("checkpoints = %d, want 1", len(loaded.Checkpoints))
	}
	// The position has to survive the geography round trip, or coverage would
	// be judged against a different point than the planner placed.
	if moved := loaded.Checkpoints[0].Position.DistanceTo(siteCentre); moved > 0.01 {
		t.Errorf("checkpoint moved by %.4f m in the round trip", moved)
	}
	if loaded.Checkpoints[0].Sequence != 1 || loaded.Checkpoints[0].RadiusMeters != 40 {
		t.Errorf("checkpoint = %+v, want sequence 1 and radius 40", loaded.Checkpoints[0])
	}
	if len(loaded.Assignments) != 1 || loaded.Assignments[0].Callsign != "SG-01" {
		t.Errorf("assignments = %+v, want the callsign resolved from the unit row", loaded.Assignments)
	}
	if loaded.Window.Start.Sub(mission.Window.Start).Abs() > time.Millisecond {
		t.Errorf("window start = %s, want %s", loaded.Window.Start, mission.Window.Start)
	}
}

func TestAreaGeometryDecidesContainment(t *testing.T) {
	repository, _ := testRepository(t)

	area, name, err := repository.Area(context.Background(), seededArea)
	if err != nil {
		t.Fatalf("area: %v", err)
	}

	if name == "" {
		t.Error("the area name must come back for the console")
	}
	if len(area.Boundary) < 4 {
		t.Fatalf("boundary = %d points, want a closed ring", len(area.Boundary))
	}
	if !area.Contains(siteCentre) {
		t.Error("the site centre must be inside its own patrol area")
	}
	if area.Contains(domain.Position{Latitude: 37.62, Longitude: 127.08}) {
		t.Error("a point well outside the ring must not be contained")
	}
}

func TestAreaReportsAnUnknownArea(t *testing.T) {
	repository, _ := testRepository(t)

	_, _, err := repository.Area(context.Background(), "20000000-0000-0000-0000-0000000000ff")

	if !errors.Is(err, application.ErrAreaNotFound) {
		t.Fatalf("err = %v, want ErrAreaNotFound", err)
	}
}

func TestCommitmentsCountApprovedPlansOnly(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	window := testWindow(2*time.Hour, 4*time.Hour)
	mission := storedMission(t, repository, track, window, unitSG02)

	// A draft is not a commitment: two plans may name the same unit until one
	// of them is approved.
	commitments, err := repository.Commitments(ctx, window)
	if err != nil {
		t.Fatalf("commitments: %v", err)
	}
	if containsMission(commitments, mission.ID) {
		t.Error("a draft must not count as a commitment")
	}

	site, err := repository.Site(ctx)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	area, _, err := repository.Area(ctx, seededArea)
	if err != nil {
		t.Fatalf("area: %v", err)
	}
	if err := mission.Approve(area, site.PolicyVersion, mission.Version, time.Now().UTC()); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := repository.Save(ctx, mission, 0); err != nil {
		t.Fatalf("save: %v", err)
	}

	if commitments, err = repository.Commitments(ctx, window); err != nil {
		t.Fatalf("commitments: %v", err)
	}
	if !containsMission(commitments, mission.ID) {
		t.Error("an approved plan must count as a commitment")
	}

	// A window that only touches the edge does not overlap.
	later, err := repository.Commitments(ctx, domain.Window{Start: window.End, End: window.End.Add(time.Hour)})
	if err != nil {
		t.Fatalf("commitments: %v", err)
	}
	if containsMission(later, mission.ID) {
		t.Error("a window starting where the other ends must not overlap")
	}
}

func containsMission(commitments []domain.Commitment, missionID string) bool {
	for _, commitment := range commitments {
		if commitment.MissionID == missionID {
			return true
		}
	}
	return false
}

func TestObservationsAreScopedToTheWindowAndTheAssignedUnits(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	site, err := repository.Site(ctx)
	if err != nil {
		t.Fatalf("site: %v", err)
	}

	// SG-01's seeded observations sit on the site centre, seconds old.
	mission := storedMission(t, repository, track, testWindow(-time.Hour, time.Hour), unitSG01)

	observations, err := repository.Observations(ctx, mission)
	if err != nil {
		t.Fatalf("observations: %v", err)
	}
	if len(observations) == 0 {
		t.Skip("the fixture has no recent observation for SG-01; run database/tools/refresh_fixture.sql")
	}

	// The slice's completion gate: a stored observation credits a pass.
	mission.Status = domain.StatusActive
	judgements := mission.JudgeCheckpoints(domain.TrustObservations(observations, site.ReceiptAllowance))
	if judgements[0].State != domain.CheckpointVisited {
		t.Errorf("state = %s, want VISITED from the stored observation", judgements[0].State)
	}
	if judgements[0].CreditedAt == nil {
		t.Error("the crediting observation time must be reported")
	}

	// Nothing outside the operating window is even loaded.
	past := mission
	past.Window = testWindow(-48*time.Hour, -47*time.Hour)
	stale, err := repository.Observations(ctx, past)
	if err != nil {
		t.Fatalf("observations: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("observations = %d, want none outside the window", len(stale))
	}
}

func TestSaveRefusesAStaleVersion(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	mission := storedMission(t, repository, track, testWindow(time.Hour, 3*time.Hour), unitSG01)

	// Two operators read version 0 and both act. The first write wins.
	first := mission
	first.Version = 1
	first.Name += " (first)"
	if err := repository.Save(ctx, first, 0); err != nil {
		t.Fatalf("first save: %v", err)
	}

	second := mission
	second.Version = 1
	second.Name += " (second)"
	err := repository.Save(ctx, second, 0)

	var stale domain.StaleVersionError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v, want StaleVersionError", err)
	}
	if stale.Expected != 0 || stale.Current != 1 {
		t.Errorf("stale = %+v, want expected 0 and current 1", stale)
	}

	loaded, err := repository.Mission(ctx, mission.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Name != first.Name {
		t.Errorf("stored name = %q, want the first writer's %q", loaded.Name, first.Name)
	}
}

func TestSaveReportsAMissingMission(t *testing.T) {
	repository, _ := testRepository(t)

	mission := domain.Mission{
		ID:           newID(t),
		Name:         "itest missing",
		Status:       domain.StatusDraft,
		PatrolAreaID: seededArea,
		Window:       testWindow(time.Hour, 2*time.Hour),
		CreatedAt:    time.Now().UTC(),
	}

	err := repository.Save(context.Background(), mission, 0)

	if !errors.Is(err, application.ErrMissionNotFound) {
		t.Fatalf("err = %v, want ErrMissionNotFound", err)
	}
}

func TestIdempotencyKeyIsEnforcedByTheDatabase(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	site, err := repository.Site(ctx)
	if err != nil {
		t.Fatalf("site: %v", err)
	}

	first := storedMission(t, repository, track, testWindow(5*time.Hour, 6*time.Hour), unitSG01)
	key := "itest-key-" + first.ID

	second := first
	second.ID = newID(t)
	track(second.ID)

	// The service checks the key before creating, but the database is the
	// backstop: a racing retry must not produce a second mission.
	if err := repository.Create(ctx, site.ID, second, key); err == nil {
		t.Fatal("a second create with the same key must be refused")
	}
	if _, err := repository.Mission(ctx, second.ID); !errors.Is(err, application.ErrMissionNotFound) {
		t.Error("the refused create must not leave a mission behind")
	}

	found, err := repository.MissionByIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("lookup by key: %v", err)
	}
	if found.ID != first.ID {
		t.Errorf("key resolved to %s, want the first mission %s", found.ID, first.ID)
	}
}

func TestDatabaseRefusesAnAbortWithoutAReason(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	mission := storedMission(t, repository, track, testWindow(time.Hour, 2*time.Hour), unitSG01)

	// Bypassing the aggregate must still fail: the constraint is the last line
	// of defence for "an abort without a reason is not an abort".
	_, err := repository.pool.Exec(ctx, `UPDATE missions SET status = 'ABORTED' WHERE id = $1`, mission.ID)

	if err == nil {
		t.Fatal("the database must refuse an aborted mission with no reason")
	}
}

func TestDatabaseRefusesRunningWithoutApproval(t *testing.T) {
	repository, track := testRepository(t)
	ctx := context.Background()

	mission := storedMission(t, repository, track, testWindow(time.Hour, 2*time.Hour), unitSG01)

	_, err := repository.pool.Exec(ctx, `UPDATE missions SET status = 'ACTIVE' WHERE id = $1`, mission.ID)

	if err == nil {
		t.Fatal("the database must refuse a running mission that was never approved")
	}
}
