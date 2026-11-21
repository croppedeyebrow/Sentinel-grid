// Package domain holds the Patrol Mission rules: the mission lifecycle, unit
// assignment, and how checkpoint coverage is judged from observations.
//
// Coverage never turns absent evidence into an outcome. A checkpoint is
// credited only by an observation the telemetry side vouched for, and when
// there is nothing trustworthy to judge with, the checkpoint stays undecided
// instead of being reported as patrolled or as missed.
package domain

import (
	"math"
	"time"
)

// Position is a WGS84 coordinate pair.
type Position struct {
	Latitude  float64
	Longitude float64
}

const earthRadiusMeters = 6_371_000.0

// DistanceTo returns the great-circle distance in metres. Checkpoint radii are
// tens of metres, so a spherical model is accurate enough and keeps the
// judgement a pure function that can be tested without a database.
func (p Position) DistanceTo(other Position) float64 {
	latitude := radians(other.Latitude - p.Latitude)
	longitude := radians(other.Longitude - p.Longitude)
	sinLat := math.Sin(latitude / 2)
	sinLon := math.Sin(longitude / 2)

	chord := sinLat*sinLat +
		math.Cos(radians(p.Latitude))*math.Cos(radians(other.Latitude))*sinLon*sinLon
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(chord)))
}

func radians(degrees float64) float64 { return degrees * math.Pi / 180 }

// Area is the patrol area a mission is planned inside.
type Area struct {
	ID       string
	Boundary []Position
}

// Contains reports whether the point lies inside the boundary ring. The
// database enforces the same rule on write; this copy keeps approval decidable
// in the domain and in tests.
func (a Area) Contains(point Position) bool {
	inside := false

	for current, previous := 0, len(a.Boundary)-1; current < len(a.Boundary); previous, current = current, current+1 {
		here, there := a.Boundary[current], a.Boundary[previous]

		if (here.Latitude > point.Latitude) == (there.Latitude > point.Latitude) {
			continue
		}
		crossing := (there.Longitude-here.Longitude)*(point.Latitude-here.Latitude)/
			(there.Latitude-here.Latitude) + here.Longitude
		if point.Longitude < crossing {
			inside = !inside
		}
	}

	return inside
}

// Checkpoint is a point the mission requires the unit to pass, within a radius.
type Checkpoint struct {
	ID           string
	Sequence     int
	Name         string
	Position     Position
	RadiusMeters float64
}

// CheckpointState is the coverage verdict for one checkpoint.
type CheckpointState string

const (
	// CheckpointPlanned means the mission has not run far enough to judge the
	// checkpoint. It is not coverage and not a miss.
	CheckpointPlanned CheckpointState = "PLANNED"
	// CheckpointVisited means a trusted observation fell inside the radius
	// during the mission window.
	CheckpointVisited CheckpointState = "VISITED"
	// CheckpointMissed means the window was observed reliably and no
	// observation passed this checkpoint.
	CheckpointMissed CheckpointState = "MISSED"
	// CheckpointUnknown means the evidence that would decide it is not
	// trustworthy, so the platform declines to judge.
	CheckpointUnknown CheckpointState = "UNKNOWN"
)

// Evidence is an observation whose trust has already been decided by the
// telemetry side. The mission domain only asks whether it may be used.
type Evidence struct {
	Position   Position
	ObservedAt time.Time
	Trusted    bool
}

// CheckpointJudgement is the verdict for one checkpoint at a point in time.
type CheckpointJudgement struct {
	CheckpointID string
	Sequence     int
	Name         string
	State        CheckpointState
	// CreditedAt is the observation time that credited the pass, set only for
	// CheckpointVisited, so the console can show which observation decided it.
	CreditedAt *time.Time
}

// Coverage counts the verdicts. Visited, missed and undecided are reported
// separately: a percentage computed from visited alone would hide the rest.
type Coverage struct {
	Visited int
	Missed  int
	Unknown int
	Planned int
	Total   int
}

// JudgeCheckpoints decides every checkpoint of the mission from the given
// observations.
//
// Only trusted observations inside the mission window can credit a pass, which
// is invariant 6 of the domain model: coverage counts observations that meet
// the quality conditions, nothing else.
func (m Mission) JudgeCheckpoints(evidence []Evidence) []CheckpointJudgement {
	judgements := make([]CheckpointJudgement, 0, len(m.Checkpoints))
	usable := m.usableEvidence(evidence)
	windowObserved := len(usable) > 0

	for _, checkpoint := range m.Checkpoints {
		judgement := CheckpointJudgement{
			CheckpointID: checkpoint.ID,
			Sequence:     checkpoint.Sequence,
			Name:         checkpoint.Name,
			State:        CheckpointPlanned,
		}

		switch {
		case !m.hasRun():
			// Nothing has happened yet; the plan is simply not judged.
		default:
			if credited := firstPass(checkpoint, usable); credited != nil {
				judgement.State = CheckpointVisited
				judgement.CreditedAt = credited
			} else if m.IsTerminal() {
				// The mission is over: absence of a pass is a miss only when
				// the window was observed with trustworthy data.
				if windowObserved {
					judgement.State = CheckpointMissed
				} else {
					judgement.State = CheckpointUnknown
				}
			}
			// While the mission is still active an unvisited checkpoint stays
			// PLANNED: the unit may still get there.
		}

		judgements = append(judgements, judgement)
	}

	return judgements
}

// usableEvidence keeps the observations that may be used at all: trusted, and
// inside the mission's operating window.
func (m Mission) usableEvidence(evidence []Evidence) []Evidence {
	usable := make([]Evidence, 0, len(evidence))
	for _, observation := range evidence {
		if !observation.Trusted {
			continue
		}
		if !m.Window.Contains(observation.ObservedAt) {
			continue
		}
		usable = append(usable, observation)
	}
	return usable
}

// firstPass returns the earliest observation time that falls inside the
// checkpoint radius, so a pass is credited to the observation that first
// proved it.
func firstPass(checkpoint Checkpoint, usable []Evidence) *time.Time {
	var earliest *time.Time

	for _, observation := range usable {
		if observation.Position.DistanceTo(checkpoint.Position) > checkpoint.RadiusMeters {
			continue
		}
		if earliest == nil || observation.ObservedAt.Before(*earliest) {
			at := observation.ObservedAt
			earliest = &at
		}
	}

	return earliest
}

// Summarize counts the judgements.
func Summarize(judgements []CheckpointJudgement) Coverage {
	coverage := Coverage{Total: len(judgements)}

	for _, judgement := range judgements {
		switch judgement.State {
		case CheckpointVisited:
			coverage.Visited++
		case CheckpointMissed:
			coverage.Missed++
		case CheckpointUnknown:
			coverage.Unknown++
		case CheckpointPlanned:
			coverage.Planned++
		}
	}

	return coverage
}
