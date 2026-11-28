package domain

import "time"

// IngestState says whether telemetry is reaching the platform at all.
//
// It exists because "no unit is reporting" and "the platform is not receiving"
// are different facts that look identical on screen. Every unit drifts to
// SILENT when the stream breaks, and an operator reading that would conclude
// the fleet was lost. The platform has to be able to say the silence is its
// own.
type IngestState string

const (
	// IngestReceiving means the ingest path is working. Units may still be
	// silent — that is then the units' own silence.
	IngestReceiving IngestState = "RECEIVING"
	// IngestNotReceiving means the platform cannot take telemetry in, so no
	// unit's silence can be held against it.
	IngestNotReceiving IngestState = "NOT_RECEIVING"
	// IngestUnknown means this process cannot tell — it is not running the
	// ingest path, or has not reached it yet. Unknown is reported rather than
	// assumed healthy.
	IngestUnknown IngestState = "UNKNOWN"
)

// Ingest is the platform's own account of whether telemetry is arriving.
type Ingest struct {
	State IngestState
	// LastEventAt is when the platform last took in a telemetry event. It is
	// zero when nothing has arrived since this process started, which is not
	// the same as nothing ever having arrived.
	LastEventAt time.Time
}

// Reported is the state as it should be published. The zero value means
// nothing answered the question, and an unanswered question is UNKNOWN — it is
// never published as health.
func (i Ingest) Reported() IngestState {
	if i.State == "" {
		return IngestUnknown
	}
	return i.State
}

// Trustworthy reports whether a unit's silence can be read as the unit's own.
// When the platform is not receiving, silence says nothing about the units.
func (i Ingest) Trustworthy() bool { return i.State == IngestReceiving }
