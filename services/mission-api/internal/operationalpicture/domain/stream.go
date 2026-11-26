package domain

import "time"

// StreamState answers a question the sensor values cannot: is anything
// arriving from this unit at all. A unit sitting still and a unit that stopped
// transmitting look identical in their readings, and the operator has to be
// able to tell them apart.
type StreamState string

const (
	// StreamStreaming means something arrived inside the site's window.
	StreamStreaming StreamState = "STREAMING"
	// StreamSilent means nothing has arrived for longer than that window.
	StreamSilent StreamState = "SILENT"
	// StreamNeverSeen means the platform has never received a frame from this
	// unit, which is different from a unit that went quiet.
	StreamNeverSeen StreamState = "NEVER_SEEN"
)

// UnitStream is how a unit's telemetry is behaving, as opposed to what it
// measured. These readings are data quality, not unit condition: a silent unit
// is UNKNOWN, never a failure.
type UnitStream struct {
	// SessionID is the connected run the readings came from. A reconnect
	// changes it without discarding what was already measured.
	SessionID        string
	SessionStartedAt *time.Time
	LastReceivedAt   *time.Time
	// MissingFrames counts frames the gateway reported as skipped in the
	// current session, so an incomplete stream is visible as incomplete.
	MissingFrames int64
	// RejectedFrames counts what arrived and could not be used. A unit whose
	// frames are all refused is not a healthy unit.
	RejectedFrames int64
}

// EvaluatedStream is a stream resolved against a moment.
type EvaluatedStream struct {
	UnitStream
	State StreamState
	// SilentFor is how long nothing has arrived, zero while streaming.
	SilentFor time.Duration
}

// Evaluate decides the stream state. The silence threshold is the site's
// position freshness window: if a position could no longer be trusted as
// current, neither can the claim that the unit is still reporting.
func (s UnitStream) Evaluate(now time.Time, window time.Duration) EvaluatedStream {
	evaluated := EvaluatedStream{UnitStream: s, State: StreamNeverSeen}

	if s.LastReceivedAt == nil {
		return evaluated
	}

	silence := now.Sub(*s.LastReceivedAt)
	if silence < 0 {
		silence = 0
	}
	if silence > window {
		evaluated.State = StreamSilent
		evaluated.SilentFor = silence
		return evaluated
	}

	evaluated.State = StreamStreaming
	return evaluated
}

// Reconnected reports whether the current session began inside the window,
// which is what lets the console say a unit just came back rather than only
// that it is streaming.
func (s UnitStream) Reconnected(now time.Time, window time.Duration) bool {
	if s.SessionStartedAt == nil {
		return false
	}
	age := now.Sub(*s.SessionStartedAt)
	return age >= 0 && age <= window
}
