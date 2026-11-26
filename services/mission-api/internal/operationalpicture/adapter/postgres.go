package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
)

// PostgresReader loads the Operational Picture projection from PostgreSQL.
// It returns stored observations untouched; freshness and unit condition are
// the domain's business.
type PostgresReader struct {
	pool *pgxpool.Pool
}

// NewPostgresReader wires the reader to a connection pool.
func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

const siteQuery = `
SELECT id::text, name,
       ST_Y(center::geometry), ST_X(center::geometry),
       position_stale_after_seconds, battery_stale_after_seconds, link_stale_after_seconds,
       battery_warning_percent, battery_critical_percent,
       link_warning_percent, link_critical_percent
FROM site_profiles
ORDER BY created_at, id
LIMIT 1`

const areaQuery = `
SELECT id::text, name, ST_AsGeoJSON(boundary::geometry)
FROM patrol_areas
WHERE site_profile_id = $1
ORDER BY name, id`

const routeQuery = `
SELECT route.id::text, route.patrol_area_id::text, route.name, ST_AsGeoJSON(route.path::geometry)
FROM patrol_routes route
JOIN patrol_areas area ON area.id = route.patrol_area_id
WHERE area.site_profile_id = $1
ORDER BY route.name, route.id`

// unitQuery keeps units without a snapshot in the result: a unit the platform
// has never heard from is part of the picture, reported as unknown.
// unitQuery reads each sensor with its own measurement and receipt time, so a
// stale position no longer ages a fresh battery, and the stream columns that
// say whether anything is arriving at all.
const unitQuery = `
SELECT unit.id::text, unit.callsign, unit.display_name,
       ST_Y(snapshot.position::geometry), ST_X(snapshot.position::geometry),
       snapshot.battery_level, snapshot.link_quality,
       snapshot.position_observed_at, snapshot.position_received_at,
       snapshot.battery_observed_at, snapshot.battery_received_at,
       snapshot.link_observed_at, snapshot.link_received_at,
       snapshot.session_id, snapshot.session_started_at,
       coalesce(snapshot.missing_frames, 0), coalesce(snapshot.rejected_frames, 0),
       snapshot.last_received_at
FROM patrol_units unit
LEFT JOIN patrol_unit_snapshots snapshot ON snapshot.patrol_unit_id = unit.id
WHERE unit.site_profile_id = $1
ORDER BY unit.callsign`

// LoadOperationalPicture reads the site, its map context and the latest unit
// snapshots.
func (r *PostgresReader) LoadOperationalPicture(ctx context.Context) (application.Projection, error) {
	site, err := r.loadSite(ctx)
	if err != nil {
		return application.Projection{}, err
	}
	areas, err := r.loadAreas(ctx, site.ID)
	if err != nil {
		return application.Projection{}, err
	}
	routes, err := r.loadRoutes(ctx, site.ID)
	if err != nil {
		return application.Projection{}, err
	}
	units, err := r.loadUnits(ctx, site.ID)
	if err != nil {
		return application.Projection{}, err
	}

	return application.Projection{Site: site, PatrolAreas: areas, Routes: routes, Units: units}, nil
}

// Ping reports whether the projection store is reachable, which is what
// readiness depends on.
func (r *PostgresReader) Ping(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping projection store: %w", err)
	}
	return nil
}

func (r *PostgresReader) loadSite(ctx context.Context) (domain.SiteProfile, error) {
	var (
		site                                      domain.SiteProfile
		positionWindow, batteryWindow, linkWindow int
		batteryWarning, batteryCritical           int16
		linkWarning, linkCritical                 int16
	)

	err := r.pool.QueryRow(ctx, siteQuery).Scan(
		&site.ID, &site.Name,
		&site.Center.Latitude, &site.Center.Longitude,
		&positionWindow, &batteryWindow, &linkWindow,
		&batteryWarning, &batteryCritical,
		&linkWarning, &linkCritical,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SiteProfile{}, application.ErrNotProvisioned
	}
	if err != nil {
		return domain.SiteProfile{}, fmt.Errorf("query site profile: %w", err)
	}

	site.Policy = domain.ObservationPolicy{
		PositionWindow:         time.Duration(positionWindow) * time.Second,
		BatteryWindow:          time.Duration(batteryWindow) * time.Second,
		LinkWindow:             time.Duration(linkWindow) * time.Second,
		BatteryWarningPercent:  int(batteryWarning),
		BatteryCriticalPercent: int(batteryCritical),
		LinkWarningPercent:     int(linkWarning),
		LinkCriticalPercent:    int(linkCritical),
	}
	return site, nil
}

func (r *PostgresReader) loadAreas(ctx context.Context, siteID string) ([]domain.PatrolArea, error) {
	rows, err := r.pool.Query(ctx, areaQuery, siteID)
	if err != nil {
		return nil, fmt.Errorf("query patrol areas: %w", err)
	}
	defer rows.Close()

	areas := make([]domain.PatrolArea, 0)
	for rows.Next() {
		var (
			area     domain.PatrolArea
			geometry string
		)
		if err := rows.Scan(&area.ID, &area.Name, &geometry); err != nil {
			return nil, fmt.Errorf("scan patrol area: %w", err)
		}
		if area.Boundary, err = decodePolygon(geometry); err != nil {
			return nil, fmt.Errorf("patrol area %s: %w", area.ID, err)
		}
		areas = append(areas, area)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read patrol areas: %w", err)
	}
	return areas, nil
}

func (r *PostgresReader) loadRoutes(ctx context.Context, siteID string) ([]domain.PatrolRoute, error) {
	rows, err := r.pool.Query(ctx, routeQuery, siteID)
	if err != nil {
		return nil, fmt.Errorf("query patrol routes: %w", err)
	}
	defer rows.Close()

	routes := make([]domain.PatrolRoute, 0)
	for rows.Next() {
		var (
			route    domain.PatrolRoute
			geometry string
		)
		if err := rows.Scan(&route.ID, &route.PatrolAreaID, &route.Name, &geometry); err != nil {
			return nil, fmt.Errorf("scan patrol route: %w", err)
		}
		if route.Path, err = decodeLineString(geometry); err != nil {
			return nil, fmt.Errorf("patrol route %s: %w", route.ID, err)
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read patrol routes: %w", err)
	}
	return routes, nil
}

func (r *PostgresReader) loadUnits(ctx context.Context, siteID string) ([]domain.PatrolUnitSnapshot, error) {
	rows, err := r.pool.Query(ctx, unitQuery, siteID)
	if err != nil {
		return nil, fmt.Errorf("query patrol units: %w", err)
	}
	defer rows.Close()

	units := make([]domain.PatrolUnitSnapshot, 0)
	for rows.Next() {
		var (
			unit                                   domain.PatrolUnitSnapshot
			latitude, longitude                    *float64
			battery, link                          *int16
			positionObservedAt, positionReceivedAt *time.Time
			batteryObservedAt, batteryReceivedAt   *time.Time
			linkObservedAt, linkReceivedAt         *time.Time
			sessionID                              *string
			sessionStartedAt, lastReceivedAt       *time.Time
			missingFrames, rejectedFrames          int64
		)
		if err := rows.Scan(
			&unit.UnitID, &unit.Callsign, &unit.DisplayName,
			&latitude, &longitude,
			&battery, &link,
			&positionObservedAt, &positionReceivedAt,
			&batteryObservedAt, &batteryReceivedAt,
			&linkObservedAt, &linkReceivedAt,
			&sessionID, &sessionStartedAt,
			&missingFrames, &rejectedFrames,
			&lastReceivedAt,
		); err != nil {
			return nil, fmt.Errorf("scan patrol unit: %w", err)
		}

		// Each sensor is read with its own pair of times, which is what lets
		// one sensor go stale while another stays current.
		unit.Position = domain.Observed[domain.Position]{
			ObservedAt: positionObservedAt,
			ReceivedAt: positionReceivedAt,
		}
		if latitude != nil && longitude != nil {
			unit.Position.Value = &domain.Position{Latitude: *latitude, Longitude: *longitude}
		}
		unit.Battery = domain.Observed[int]{
			Value:      percent(battery),
			ObservedAt: batteryObservedAt,
			ReceivedAt: batteryReceivedAt,
		}
		unit.Link = domain.Observed[int]{
			Value:      percent(link),
			ObservedAt: linkObservedAt,
			ReceivedAt: linkReceivedAt,
		}
		unit.Stream = domain.UnitStream{
			SessionStartedAt: sessionStartedAt,
			LastReceivedAt:   lastReceivedAt,
			MissingFrames:    missingFrames,
			RejectedFrames:   rejectedFrames,
		}
		if sessionID != nil {
			unit.Stream.SessionID = *sessionID
		}

		units = append(units, unit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read patrol units: %w", err)
	}
	return units, nil
}

func percent(value *int16) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

type geoJSONGeometry struct {
	Type string `json:"type"`
}

type geoJSONPolygon struct {
	geoJSONGeometry
	Coordinates [][][]float64 `json:"coordinates"`
}

type geoJSONLineString struct {
	geoJSONGeometry
	Coordinates [][]float64 `json:"coordinates"`
}

// decodePolygon keeps the exterior ring only. Holes in a patrol area are not
// part of this slice, and silently flattening them would misstate coverage.
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
	return positions(polygon.Coordinates[0])
}

func decodeLineString(raw string) ([]domain.Position, error) {
	var line geoJSONLineString
	if err := json.Unmarshal([]byte(raw), &line); err != nil {
		return nil, fmt.Errorf("decode line string: %w", err)
	}
	return positions(line.Coordinates)
}

func positions(coordinates [][]float64) ([]domain.Position, error) {
	result := make([]domain.Position, 0, len(coordinates))
	for _, pair := range coordinates {
		if len(pair) < 2 {
			return nil, errors.New("coordinate pair is incomplete")
		}
		result = append(result, domain.Position{Longitude: pair[0], Latitude: pair[1]})
	}
	return result, nil
}
