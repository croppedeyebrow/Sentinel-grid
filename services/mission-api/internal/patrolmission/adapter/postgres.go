// Package adapter exposes Patrol Mission over HTTP and stores it in
// PostgreSQL.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

// PostgresRepository stores missions in PostgreSQL/PostGIS.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository wires the repository to a connection pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const siteQuery = `
SELECT id::text, position_stale_after_seconds, policy_version
FROM site_profiles
ORDER BY created_at, id
LIMIT 1`

const areaQuery = `
SELECT name, ST_AsGeoJSON(boundary::geometry)
FROM patrol_areas
WHERE id = $1`

const routeNameQuery = `SELECT name FROM patrol_routes WHERE id = $1`

const callsignQuery = `SELECT id::text, callsign FROM patrol_units WHERE id = ANY($1)`

const missionColumns = `
       mission.id::text, mission.patrol_area_id::text, coalesce(mission.patrol_route_id::text, ''),
       mission.name, mission.status, mission.version, mission.policy_version,
       mission.window_start, mission.window_end,
       mission.abort_reason, mission.aborted_at,
       mission.created_at, mission.approved_at, mission.started_at, mission.ended_at`

const checkpointQuery = `
SELECT mission_id::text, id::text, sequence, name,
       ST_Y(position::geometry), ST_X(position::geometry), radius_meters
FROM mission_checkpoints
WHERE ($1::uuid IS NULL OR mission_id = $1)
ORDER BY mission_id, sequence`

const assignmentQuery = `
SELECT assignment.mission_id::text, assignment.patrol_unit_id::text, unit.callsign,
       assignment.window_start, assignment.window_end
FROM mission_assignments assignment
JOIN patrol_units unit ON unit.id = assignment.patrol_unit_id
WHERE ($1::uuid IS NULL OR assignment.mission_id = $1)
ORDER BY assignment.mission_id, unit.callsign`

// commitmentQuery counts only approved plans. A draft is not a commitment yet:
// two drafts may name the same unit, and the first approval then takes it.
const commitmentQuery = `
SELECT assignment.mission_id::text, assignment.patrol_unit_id::text, unit.callsign,
       assignment.window_start, assignment.window_end
FROM mission_assignments assignment
JOIN patrol_units unit ON unit.id = assignment.patrol_unit_id
JOIN missions mission ON mission.id = assignment.mission_id
WHERE mission.status IN ('READY', 'ACTIVE')
  AND tstzrange(assignment.window_start, assignment.window_end)
      && tstzrange($1, $2)`

// observationQuery loads the observations of the units assigned to the mission
// inside its operating window. Whether an observation may credit a pass is the
// domain's decision, so the rows come back as they are stored.
const observationQuery = `
SELECT ST_Y(observation.position::geometry), ST_X(observation.position::geometry),
       observation.observed_at, observation.received_at
FROM patrol_unit_observations observation
JOIN mission_assignments assignment
  ON assignment.patrol_unit_id = observation.patrol_unit_id
 AND assignment.mission_id = $1
WHERE observation.observed_at BETWEEN $2 AND $3
ORDER BY observation.observed_at`

// Site returns the single site this deployment serves.
func (r *PostgresRepository) Site(ctx context.Context) (application.Site, error) {
	var (
		site           application.Site
		positionWindow int
	)

	err := r.pool.QueryRow(ctx, siteQuery).Scan(&site.ID, &positionWindow, &site.PolicyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Site{}, application.ErrNotProvisioned
	}
	if err != nil {
		return application.Site{}, fmt.Errorf("query site profile: %w", err)
	}

	site.ReceiptAllowance = time.Duration(positionWindow) * time.Second
	return site, nil
}

// Area returns the patrol area geometry and its name.
func (r *PostgresRepository) Area(ctx context.Context, areaID string) (domain.Area, string, error) {
	var (
		name     string
		geometry string
	)

	err := r.pool.QueryRow(ctx, areaQuery, areaID).Scan(&name, &geometry)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Area{}, "", application.ErrAreaNotFound
	}
	if err != nil {
		return domain.Area{}, "", fmt.Errorf("query patrol area: %w", err)
	}

	boundary, err := decodePolygon(geometry)
	if err != nil {
		return domain.Area{}, "", fmt.Errorf("patrol area %s: %w", areaID, err)
	}
	return domain.Area{ID: areaID, Boundary: boundary}, name, nil
}

// RouteName returns the planned route's name.
func (r *PostgresRepository) RouteName(ctx context.Context, routeID string) (string, error) {
	var name string

	err := r.pool.QueryRow(ctx, routeNameQuery, routeID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query patrol route: %w", err)
	}
	return name, nil
}

// Callsigns resolves unit ids to callsigns and refuses an unknown unit.
func (r *PostgresRepository) Callsigns(ctx context.Context, unitIDs []string) (map[string]string, error) {
	callsigns := make(map[string]string, len(unitIDs))
	if len(unitIDs) == 0 {
		return callsigns, nil
	}

	rows, err := r.pool.Query(ctx, callsignQuery, unitIDs)
	if err != nil {
		return nil, fmt.Errorf("query patrol units: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, callsign string
		if err := rows.Scan(&id, &callsign); err != nil {
			return nil, fmt.Errorf("scan patrol unit: %w", err)
		}
		callsigns[id] = callsign
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read patrol units: %w", err)
	}

	for _, unitID := range unitIDs {
		if _, found := callsigns[unitID]; !found {
			return nil, fmt.Errorf("%w: %s", application.ErrUnitNotFound, unitID)
		}
	}
	return callsigns, nil
}

// Missions returns every mission, newest plan first.
func (r *PostgresRepository) Missions(ctx context.Context) ([]domain.Mission, error) {
	return r.loadMissions(ctx, "")
}

// Mission returns one mission.
func (r *PostgresRepository) Mission(ctx context.Context, missionID string) (domain.Mission, error) {
	missions, err := r.loadMissions(ctx, missionID)
	if err != nil {
		return domain.Mission{}, err
	}
	if len(missions) == 0 {
		return domain.Mission{}, application.ErrMissionNotFound
	}
	return missions[0], nil
}

// MissionByIdempotencyKey returns the mission a previous create request with
// this key produced.
func (r *PostgresRepository) MissionByIdempotencyKey(ctx context.Context, key string) (domain.Mission, error) {
	var missionID string

	err := r.pool.QueryRow(ctx, `SELECT mission_id::text FROM mission_idempotency_keys WHERE key = $1`, key).
		Scan(&missionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Mission{}, application.ErrMissionNotFound
	}
	if err != nil {
		return domain.Mission{}, fmt.Errorf("query idempotency key: %w", err)
	}
	return r.Mission(ctx, missionID)
}

// Commitments returns the assignments that overlap the window on approved
// plans.
func (r *PostgresRepository) Commitments(ctx context.Context, window domain.Window) ([]domain.Commitment, error) {
	rows, err := r.pool.Query(ctx, commitmentQuery, window.Start, window.End)
	if err != nil {
		return nil, fmt.Errorf("query commitments: %w", err)
	}
	defer rows.Close()

	commitments := make([]domain.Commitment, 0)
	for rows.Next() {
		var commitment domain.Commitment
		if err := rows.Scan(
			&commitment.MissionID, &commitment.UnitID, &commitment.Callsign,
			&commitment.Window.Start, &commitment.Window.End,
		); err != nil {
			return nil, fmt.Errorf("scan commitment: %w", err)
		}
		commitments = append(commitments, commitment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read commitments: %w", err)
	}
	return commitments, nil
}

// Observations returns the stored observations that could bear on this
// mission's coverage.
func (r *PostgresRepository) Observations(ctx context.Context, mission domain.Mission) ([]domain.Observation, error) {
	rows, err := r.pool.Query(ctx, observationQuery, mission.ID, mission.Window.Start, mission.Window.End)
	if err != nil {
		return nil, fmt.Errorf("query observations: %w", err)
	}
	defer rows.Close()

	observations := make([]domain.Observation, 0)
	for rows.Next() {
		var (
			latitude, longitude    *float64
			observedAt, receivedAt *time.Time
		)
		if err := rows.Scan(&latitude, &longitude, &observedAt, &receivedAt); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}

		observation := domain.Observation{ObservedAt: observedAt, ReceivedAt: receivedAt}
		if latitude != nil && longitude != nil {
			observation.Position = &domain.Position{Latitude: *latitude, Longitude: *longitude}
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read observations: %w", err)
	}
	return observations, nil
}

// Create writes a new draft together with its plan and idempotency key in one
// transaction, so a retry cannot leave a half-planned mission behind.
func (r *PostgresRepository) Create(
	ctx context.Context,
	siteID string,
	mission domain.Mission,
	idempotencyKey string,
) error {
	return r.inTransaction(ctx, func(tx pgx.Tx) error {
		var routeID *string
		if mission.PatrolRouteID != "" {
			routeID = &mission.PatrolRouteID
		}

		if _, err := tx.Exec(ctx, `
INSERT INTO missions (
    id, site_profile_id, patrol_area_id, patrol_route_id,
    name, status, version, policy_version, window_start, window_end, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			mission.ID, siteID, mission.PatrolAreaID, routeID,
			mission.Name, string(mission.Status), mission.Version, mission.PolicyVersion,
			mission.Window.Start, mission.Window.End, mission.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert mission: %w", err)
		}

		if err := writePlan(ctx, tx, mission); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO mission_idempotency_keys (key, mission_id) VALUES ($1, $2)`,
			idempotencyKey, mission.ID,
		); err != nil {
			return fmt.Errorf("insert idempotency key: %w", err)
		}
		return nil
	})
}

// Save persists a mission that the aggregate has already decided on. The
// update is conditional on the version the caller read, so a transition built
// on stale state is refused instead of overwriting the other operator's
// outcome.
func (r *PostgresRepository) Save(ctx context.Context, mission domain.Mission, expectedVersion int) error {
	return r.inTransaction(ctx, func(tx pgx.Tx) error {
		var (
			abortReason *string
			abortedAt   *time.Time
		)
		if mission.Abort != nil {
			reason := string(mission.Abort.Reason)
			abortReason = &reason
			at := mission.Abort.At
			abortedAt = &at
		}

		tag, err := tx.Exec(ctx, `
UPDATE missions SET
    name = $3,
    status = $4,
    version = $5,
    policy_version = $6,
    window_start = $7,
    window_end = $8,
    abort_reason = $9,
    aborted_at = $10,
    approved_at = $11,
    started_at = $12,
    ended_at = $13
WHERE id = $1 AND version = $2`,
			mission.ID, expectedVersion,
			mission.Name, string(mission.Status), mission.Version, mission.PolicyVersion,
			mission.Window.Start, mission.Window.End,
			abortReason, abortedAt,
			mission.ApprovedAt, mission.StartedAt, mission.EndedAt,
		)
		if err != nil {
			return fmt.Errorf("update mission: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return r.explainFailedUpdate(ctx, tx, mission.ID, expectedVersion)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM mission_checkpoints WHERE mission_id = $1`, mission.ID); err != nil {
			return fmt.Errorf("clear checkpoints: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM mission_assignments WHERE mission_id = $1`, mission.ID); err != nil {
			return fmt.Errorf("clear assignments: %w", err)
		}
		return writePlan(ctx, tx, mission)
	})
}

// explainFailedUpdate turns a no-op update into the reason for it: either the
// mission is gone, or someone else moved it on.
func (r *PostgresRepository) explainFailedUpdate(
	ctx context.Context,
	tx pgx.Tx,
	missionID string,
	expectedVersion int,
) error {
	var current int

	err := tx.QueryRow(ctx, `SELECT version FROM missions WHERE id = $1`, missionID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrMissionNotFound
	}
	if err != nil {
		return fmt.Errorf("read mission version: %w", err)
	}
	return domain.StaleVersionError{Expected: expectedVersion, Current: current}
}

func writePlan(ctx context.Context, tx pgx.Tx, mission domain.Mission) error {
	for _, checkpoint := range mission.Checkpoints {
		if _, err := tx.Exec(ctx, `
INSERT INTO mission_checkpoints (id, mission_id, sequence, name, position, radius_meters)
VALUES ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, $7)`,
			checkpoint.ID, mission.ID, checkpoint.Sequence, checkpoint.Name,
			checkpoint.Position.Longitude, checkpoint.Position.Latitude, checkpoint.RadiusMeters,
		); err != nil {
			return fmt.Errorf("insert checkpoint %s: %w", checkpoint.Name, err)
		}
	}

	for _, assignment := range mission.Assignments {
		if _, err := tx.Exec(ctx, `
INSERT INTO mission_assignments (mission_id, patrol_unit_id, window_start, window_end)
VALUES ($1, $2, $3, $4)`,
			mission.ID, assignment.UnitID, assignment.Window.Start, assignment.Window.End,
		); err != nil {
			return fmt.Errorf("insert assignment %s: %w", assignment.Callsign, err)
		}
	}
	return nil
}

func (r *PostgresRepository) inTransaction(ctx context.Context, work func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// loadMissions reads missions with their plan. missionID empty loads all.
func (r *PostgresRepository) loadMissions(ctx context.Context, missionID string) ([]domain.Mission, error) {
	var filter *string
	if missionID != "" {
		filter = &missionID
	}

	query := `SELECT` + missionColumns + `
FROM missions mission
WHERE ($1::uuid IS NULL OR mission.id = $1)
ORDER BY mission.created_at DESC, mission.id`

	rows, err := r.pool.Query(ctx, query, filter)
	if err != nil {
		return nil, fmt.Errorf("query missions: %w", err)
	}
	defer rows.Close()

	missions := make([]domain.Mission, 0)
	index := make(map[string]int)
	for rows.Next() {
		var (
			mission     domain.Mission
			status      string
			abortReason *string
			abortedAt   *time.Time
		)
		if err := rows.Scan(
			&mission.ID, &mission.PatrolAreaID, &mission.PatrolRouteID,
			&mission.Name, &status, &mission.Version, &mission.PolicyVersion,
			&mission.Window.Start, &mission.Window.End,
			&abortReason, &abortedAt,
			&mission.CreatedAt, &mission.ApprovedAt, &mission.StartedAt, &mission.EndedAt,
		); err != nil {
			return nil, fmt.Errorf("scan mission: %w", err)
		}

		mission.Status = domain.Status(status)
		if abortReason != nil && abortedAt != nil {
			mission.Abort = &domain.Abort{Reason: domain.AbortReason(*abortReason), At: *abortedAt}
		}
		mission.Checkpoints = make([]domain.Checkpoint, 0)
		mission.Assignments = make([]domain.Assignment, 0)

		index[mission.ID] = len(missions)
		missions = append(missions, mission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read missions: %w", err)
	}
	if len(missions) == 0 {
		return missions, nil
	}

	if err := r.attachCheckpoints(ctx, filter, missions, index); err != nil {
		return nil, err
	}
	if err := r.attachAssignments(ctx, filter, missions, index); err != nil {
		return nil, err
	}
	return missions, nil
}

func (r *PostgresRepository) attachCheckpoints(
	ctx context.Context,
	filter *string,
	missions []domain.Mission,
	index map[string]int,
) error {
	rows, err := r.pool.Query(ctx, checkpointQuery, filter)
	if err != nil {
		return fmt.Errorf("query checkpoints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			missionID  string
			checkpoint domain.Checkpoint
			latitude   float64
			longitude  float64
		)
		if err := rows.Scan(
			&missionID, &checkpoint.ID, &checkpoint.Sequence, &checkpoint.Name,
			&latitude, &longitude, &checkpoint.RadiusMeters,
		); err != nil {
			return fmt.Errorf("scan checkpoint: %w", err)
		}

		position, found := index[missionID]
		if !found {
			continue
		}
		checkpoint.Position = domain.Position{Latitude: latitude, Longitude: longitude}
		missions[position].Checkpoints = append(missions[position].Checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read checkpoints: %w", err)
	}
	return nil
}

func (r *PostgresRepository) attachAssignments(
	ctx context.Context,
	filter *string,
	missions []domain.Mission,
	index map[string]int,
) error {
	rows, err := r.pool.Query(ctx, assignmentQuery, filter)
	if err != nil {
		return fmt.Errorf("query assignments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			missionID  string
			assignment domain.Assignment
		)
		if err := rows.Scan(
			&missionID, &assignment.UnitID, &assignment.Callsign,
			&assignment.Window.Start, &assignment.Window.End,
		); err != nil {
			return fmt.Errorf("scan assignment: %w", err)
		}

		position, found := index[missionID]
		if !found {
			continue
		}
		missions[position].Assignments = append(missions[position].Assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read assignments: %w", err)
	}
	return nil
}

type geoJSONPolygon struct {
	Coordinates [][][]float64 `json:"coordinates"`
}

// decodePolygon keeps the exterior ring. A polygon with holes is refused
// rather than flattened, because flattening would misstate the area.
func decodePolygon(raw string) ([]domain.Position, error) {
	var polygon geoJSONPolygon
	if err := json.Unmarshal([]byte(raw), &polygon); err != nil {
		return nil, fmt.Errorf("decode polygon: %w", err)
	}
	if len(polygon.Coordinates) == 0 {
		return nil, errors.New("polygon has no ring")
	}
	if len(polygon.Coordinates) > 1 {
		return nil, errors.New("polygon with holes is not supported yet")
	}

	ring := make([]domain.Position, 0, len(polygon.Coordinates[0]))
	for _, pair := range polygon.Coordinates[0] {
		if len(pair) < 2 {
			return nil, errors.New("coordinate pair is incomplete")
		}
		ring = append(ring, domain.Position{Longitude: pair[0], Latitude: pair[1]})
	}
	return ring, nil
}
