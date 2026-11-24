// Package domain holds the telemetry projection rules: how a validated frame
// changes a unit's stored state.
//
// The gateway decides whether a frame may enter the platform. This package
// decides what it changes once it is in, and the governing rule is that a
// measurement never moves a sensor backwards in time. Each sensor carries its
// own measurement and receipt time, so a unit can hold a stale position next
// to a fresh battery — the per-sensor freshness that requirement TEL-01 asks
// for and that ADR 0003 recorded as missing.
package domain

import "time"

// Classification is the gateway's verdict about where a frame sat in its
// session's sequence. It travels with the frame because the projection treats
// the cases differently.
type Classification string

const (
	// ClassificationAccepted is the frame the gateway expected.
	ClassificationAccepted Classification = "ACCEPTED"
	// ClassificationDuplicate is a sequence already seen in this session.
	ClassificationDuplicate Classification = "DUPLICATE"
	// ClassificationOutOfOrder is a frame behind the session's progress.
	ClassificationOutOfOrder Classification = "OUT_OF_ORDER"
	// ClassificationGap is a frame past missing ones.
	ClassificationGap Classification = "GAP"
)

// Position is a WGS84 coordinate pair.
type Position struct {
	Latitude  float64
	Longitude float64
}

// Reading is one sensor's stored measurement, with the two times that decide
// whether it can be trusted: when the unit measured it, and when the platform
// received it.
type Reading[T any] struct {
	Value      T
	ObservedAt time.Time
	ReceivedAt time.Time
}

// UnitSnapshot is the projected state of one unit: the latest trustworthy
// measurement per sensor, plus how its stream is behaving.
type UnitSnapshot struct {
	UnitID string

	Position *Reading[Position]
	Battery  *Reading[int]
	Link     *Reading[int]

	// SessionID is the connected run the readings came from. A reconnect
	// changes it without discarding what was already measured.
	SessionID string
	// LastSequence is the furthest point the session reached.
	LastSequence int64
	// MissingFrames counts the frames the gateway reported as skipped in the
	// current session, so an incomplete stream is visible as incomplete.
	MissingFrames int64
	// RejectedFrames counts what arrived for this unit and could not be used.
	RejectedFrames int64
	// LastReceivedAt is when anything was last received for this unit, which
	// answers "is anything arriving" separately from "is anything being
	// measured".
	LastReceivedAt time.Time
}

// Event is a validated frame as the gateway published it.
type Event struct {
	UnitID         string
	SessionID      string
	Sequence       int64
	Classification Classification

	ObservedAt time.Time
	ReceivedAt time.Time

	Position *Position
	Battery  *int
	Link     *int

	// MissingFrames is how many frames the gateway found skipped, set on a gap.
	MissingFrames int64
}

// ApplyResult reports what one event changed, which is what the receipt log
// records next to the frame.
type ApplyResult struct {
	// Applied is true when the event changed anything at all.
	Applied bool
	// Sensors names the sensors whose stored measurement was replaced.
	Sensors []string
	// Reconnected is true when the event opened a new session for the unit.
	Reconnected bool
	// StaleSensors names the sensors the event carried but could not apply,
	// because the stored measurement was newer.
	StaleSensors []string
}

// Sensor names as they appear in results and in the receipt log.
const (
	SensorPosition = "position"
	SensorBattery  = "battery"
	SensorLink     = "link"
)

// Apply returns the snapshot as the event leaves it.
//
// Applying the same event twice produces the same snapshot: a sensor is only
// replaced by a strictly newer measurement, and stream counters only move when
// the session progresses. That is what makes the consumer safe to re-run after
// a restart.
func (s UnitSnapshot) Apply(event Event) (UnitSnapshot, ApplyResult) {
	result := ApplyResult{}

	// A duplicate carries nothing the projection has not already seen.
	if event.Classification == ClassificationDuplicate {
		return s, result
	}

	updated := s
	if updated.UnitID == "" {
		updated.UnitID = event.UnitID
	}

	// A reconnect starts a new session. The readings stay: the unit was there
	// and measured them, and discarding them would lose the last known
	// position exactly when the operator needs it most.
	if event.SessionID != updated.SessionID {
		updated.SessionID = event.SessionID
		updated.LastSequence = 0
		updated.MissingFrames = 0
		result.Reconnected = s.SessionID != ""
		result.Applied = true
	}

	progressed := event.Sequence > updated.LastSequence
	if progressed {
		updated.LastSequence = event.Sequence
		if event.Classification == ClassificationGap && event.MissingFrames > 0 {
			updated.MissingFrames += event.MissingFrames
		}
		result.Applied = true
	}

	if event.ReceivedAt.After(updated.LastReceivedAt) {
		updated.LastReceivedAt = event.ReceivedAt
		result.Applied = true
	}

	if event.Position != nil {
		if replaced := applyReading(&updated.Position, *event.Position, event); replaced {
			result.Sensors = append(result.Sensors, SensorPosition)
			result.Applied = true
		} else {
			result.StaleSensors = append(result.StaleSensors, SensorPosition)
		}
	}
	if event.Battery != nil {
		if replaced := applyReading(&updated.Battery, *event.Battery, event); replaced {
			result.Sensors = append(result.Sensors, SensorBattery)
			result.Applied = true
		} else {
			result.StaleSensors = append(result.StaleSensors, SensorBattery)
		}
	}
	if event.Link != nil {
		if replaced := applyReading(&updated.Link, *event.Link, event); replaced {
			result.Sensors = append(result.Sensors, SensorLink)
			result.Applied = true
		} else {
			result.StaleSensors = append(result.StaleSensors, SensorLink)
		}
	}

	return updated, result
}

// applyReading replaces a sensor only with a strictly newer measurement, which
// is what keeps a late or re-ordered frame from rewriting history.
func applyReading[T any](stored **Reading[T], value T, event Event) bool {
	if *stored != nil && !event.ObservedAt.After((*stored).ObservedAt) {
		return false
	}

	*stored = &Reading[T]{
		Value:      value,
		ObservedAt: event.ObservedAt,
		ReceivedAt: event.ReceivedAt,
	}
	return true
}

// NoteRejection records that something arrived for this unit and could not be
// used. The value is not applied, but the receipt is: a unit whose frames are
// all refused is not a healthy unit, and the operator has to be able to see
// that.
func (s UnitSnapshot) NoteRejection(unitID string, receivedAt time.Time) UnitSnapshot {
	updated := s
	if updated.UnitID == "" {
		updated.UnitID = unitID
	}
	updated.RejectedFrames++
	if receivedAt.After(updated.LastReceivedAt) {
		updated.LastReceivedAt = receivedAt
	}
	return updated
}
