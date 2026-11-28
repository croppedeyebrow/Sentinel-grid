package domain

import (
	"errors"
	"time"
)

// The live feed is the rule set behind pushing the picture to a connected
// operator. It is written without any transport: a socket is how a message
// travels, not what may be sent. Everything here is about what the platform is
// allowed to tell a client and in what order, which is the part that has to be
// right whether the connection is a WebSocket, a test, or something later.

// MessageKind is what a message carries. One shape per kind: a client never
// has to guess which fields are present.
type MessageKind string

const (
	// MessageSnapshot carries the whole picture. A feed always starts with one,
	// so a client never renders from a partial view.
	MessageSnapshot MessageKind = "SNAPSHOT"
	// MessageUnits carries only the units that changed, whole.
	MessageUnits MessageKind = "UNITS"
	// MessageIngest carries the platform's own intake state.
	MessageIngest MessageKind = "INGEST"
)

// CloseReason says why a feed ended, because a client that was dropped should
// be able to tell that from a client that closed.
type CloseReason string

const (
	// CloseTooSlow means the client could not keep up. Its messages were not
	// trimmed to keep it attached: a screen built on silently skipped changes
	// is one nobody can describe, so the connection ends and the client comes
	// back to a fresh snapshot.
	CloseTooSlow CloseReason = "TOO_SLOW"
	// CloseGoingAway means the server is shutting down.
	CloseGoingAway CloseReason = "GOING_AWAY"
)

// ErrNotStarted reports a push before the first snapshot. A delta without a
// base is not a picture.
var ErrNotStarted = errors.New("live feed has not sent its snapshot yet")

// ErrClosed reports a push to a feed that has ended.
var ErrClosed = errors.New("live feed is closed")

// Message is one thing sent to one client.
type Message struct {
	Kind MessageKind
	// Revision increases by one per message on this feed. It answers "did I
	// miss anything", not "which version of the world is this", so it is per
	// connection and starts again for every client.
	Revision int64
	// SentAt is when the platform sent it. Ages on screen are still measured
	// against the picture's own judgement time, not against this.
	SentAt time.Time

	// Picture is set on a snapshot and nil otherwise.
	Picture *OperationalPicture
	// Units is set on a units message, and never empty.
	Units []PatrolUnitView
	// Ingest is set on an ingest message.
	Ingest *Ingest
}

// Change is what one applied event left different. It is the only input to the
// feed: the feed does not read the projection, because the thing that applied
// the event already knows what moved.
type Change struct {
	// Units are the changed units, already evaluated.
	Units []PatrolUnitView
	// Ingest is set only when the platform's intake changed state.
	Ingest *Ingest
}

// Empty reports that nothing changed, which is the common case: a duplicate
// frame, or a measurement older than the one on file. Nothing is pushed for it.
func (c Change) Empty() bool { return len(c.Units) == 0 && c.Ingest == nil }

// IngestChanged reports whether an intake difference is worth telling a client
// about. Only the state counts: the moment of the last event moves with every
// frame, and pushing that would mean a message per frame saying nothing an
// operator can act on. It travels with the next snapshot or state change.
func IngestChanged(previous, current Ingest) bool {
	return previous.Reported() != current.Reported()
}

// Feed is one client's place in the stream of changes.
//
// It is deliberately not a queue. Buffering belongs to whatever carries the
// messages; the rules here are the ones that must hold regardless of how they
// are carried: a feed starts whole, revisions have no holes, nothing is sent
// for a change that was not a change, and a feed that ended stays ended.
type Feed struct {
	revision int64
	started  bool
	closed   bool
	reason   CloseReason
}

// NewFeed opens a feed that has not sent anything yet.
func NewFeed() *Feed { return &Feed{} }

// Revision is the last revision sent, zero before the snapshot.
func (f *Feed) Revision() int64 { return f.revision }

// Started reports whether the snapshot has been sent.
func (f *Feed) Started() bool { return f.started }

// Closed reports whether the feed has ended, and why.
func (f *Feed) Closed() (bool, CloseReason) { return f.closed, f.reason }

// Snapshot produces the whole picture as the next message.
//
// It serves both the first message and a resync. A resync does not rewind the
// revision: the client takes the snapshot's revision as its new expectation,
// so one missed message cannot leave a client permanently suspicious.
func (f *Feed) Snapshot(picture OperationalPicture, now time.Time) (Message, error) {
	if f.closed {
		return Message{}, ErrClosed
	}

	f.revision++
	f.started = true
	return Message{
		Kind:     MessageSnapshot,
		Revision: f.revision,
		SentAt:   now,
		Picture:  &picture,
	}, nil
}

// Push turns one change into the messages a client should receive.
//
// The intake state goes first. It decides how every unit's silence is to be
// read, so a client that learns "we are not receiving" before it learns "these
// units went quiet" never has to interpret the second without the first.
func (f *Feed) Push(change Change, now time.Time) ([]Message, error) {
	if f.closed {
		return nil, ErrClosed
	}
	if !f.started {
		return nil, ErrNotStarted
	}
	if change.Empty() {
		// No revision is spent: a client's expectation only moves when
		// something was actually sent.
		return nil, nil
	}

	messages := make([]Message, 0, 2)
	if change.Ingest != nil {
		ingest := *change.Ingest
		f.revision++
		messages = append(messages, Message{
			Kind:     MessageIngest,
			Revision: f.revision,
			SentAt:   now,
			Ingest:   &ingest,
		})
	}
	if len(change.Units) > 0 {
		f.revision++
		messages = append(messages, Message{
			Kind:     MessageUnits,
			Revision: f.revision,
			SentAt:   now,
			Units:    change.Units,
		})
	}

	return messages, nil
}

// Close ends the feed with a reason. Closing twice keeps the first reason: why
// a client was dropped is the interesting fact, and a later shutdown should not
// overwrite it.
func (f *Feed) Close(reason CloseReason) {
	if f.closed {
		return
	}
	f.closed = true
	f.reason = reason
}
