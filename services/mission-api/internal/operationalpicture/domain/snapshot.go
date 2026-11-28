package domain

import "time"

// UnitStatus is the unit condition axis. It answers "is this unit all right",
// never "is this data recent" — that is what Freshness is for.
type UnitStatus string

const (
	// UnitStatusNormal means every required observation is fresh and within
	// the site's safe thresholds.
	UnitStatusNormal UnitStatus = "NORMAL"
	// UnitStatusWarning means a fresh observation crossed a warning threshold.
	UnitStatusWarning UnitStatus = "WARNING"
	// UnitStatusCritical means a fresh observation crossed a critical threshold.
	UnitStatusCritical UnitStatus = "CRITICAL"
	// UnitStatusUnknown means the evidence is insufficient to judge the unit.
	// The platform does not guess in either direction.
	UnitStatusUnknown UnitStatus = "UNKNOWN"
)

// PatrolUnitSnapshot is the latest projection of one unit: the observed values
// as stored, with no condition baked in.
type PatrolUnitSnapshot struct {
	UnitID      string
	Callsign    string
	DisplayName string

	Position Observed[Position]
	Battery  Observed[int]
	Link     Observed[int]

	// Stream is how this unit's telemetry is behaving, which is separate
	// from what it measured.
	Stream UnitStream
}

// PatrolUnitView is a snapshot resolved against a policy at a point in time:
// per-field freshness on one axis, unit condition on the other.
type PatrolUnitView struct {
	UnitID      string
	Callsign    string
	DisplayName string
	Status      UnitStatus

	Position Evaluated[Position]
	Battery  Evaluated[int]
	Link     Evaluated[int]

	// Stream carries the telemetry health of the unit, resolved against the
	// same moment as the readings.
	Stream EvaluatedStream
	// Reconnected is true when the current session began inside the window.
	Reconnected bool
}

// Evaluate resolves freshness per field and derives the unit condition.
func (s PatrolUnitSnapshot) Evaluate(now time.Time, policy ObservationPolicy) PatrolUnitView {
	view := PatrolUnitView{
		UnitID:      s.UnitID,
		Callsign:    s.Callsign,
		DisplayName: s.DisplayName,
		Position:    s.Position.Evaluate(now, policy.PositionWindow),
		Battery:     s.Battery.Evaluate(now, policy.BatteryWindow),
		Link:        s.Link.Evaluate(now, policy.LinkWindow),
		Stream:      s.Stream.Evaluate(now, policy.PositionWindow),
		Reconnected: s.Stream.Reconnected(now, policy.PositionWindow),
	}
	view.Status = deriveStatus(view, policy)
	return view
}

// deriveStatus applies the condition rules in order of severity.
//
// Insufficient evidence wins over everything else: if any required field is
// not fresh, the unit is UNKNOWN rather than normal or at risk. That keeps a
// stale battery reading from being presented as a healthy unit.
func deriveStatus(view PatrolUnitView, policy ObservationPolicy) UnitStatus {
	if view.Position.Freshness != FreshnessFresh ||
		view.Battery.Freshness != FreshnessFresh ||
		view.Link.Freshness != FreshnessFresh {
		return UnitStatusUnknown
	}

	battery, link := *view.Battery.Value, *view.Link.Value
	if battery <= policy.BatteryCriticalPercent || link <= policy.LinkCriticalPercent {
		return UnitStatusCritical
	}
	if battery <= policy.BatteryWarningPercent || link <= policy.LinkWarningPercent {
		return UnitStatusWarning
	}
	return UnitStatusNormal
}

// OperationalPicture is the whole read model the console renders.
type OperationalPicture struct {
	GeneratedAt time.Time
	Site        SiteProfile
	PatrolAreas []PatrolArea
	Routes      []PatrolRoute
	Units       []PatrolUnitView
	// Ingest is the platform's account of whether telemetry is arriving at
	// all, which the units cannot answer for themselves.
	Ingest Ingest
}
