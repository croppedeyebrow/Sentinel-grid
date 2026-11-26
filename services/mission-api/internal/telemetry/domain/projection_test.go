package domain

import (
	"testing"
	"time"
)

// The cases below are the fault modes the scenario promises to exercise:
// delay, duplication, reordering, gaps and reconnects. They are the rules the
// consumer must not be able to bend.

var base = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func at(offset time.Duration) time.Time { return base.Add(offset) }

func value[T any](v T) *T { return &v }

func event(sequence int64, observed time.Duration) Event {
	return Event{
		UnitID:         "unit-1",
		SessionID:      "session-1",
		Sequence:       sequence,
		Classification: ClassificationAccepted,
		ObservedAt:     at(observed),
		ReceivedAt:     at(observed),
		Position:       value(Position{Latitude: 37.5665, Longitude: 126.978}),
		Battery:        value(82),
		Link:           value(94),
	}
}

func TestApplyStoresEverySensorOfTheFirstFrame(t *testing.T) {
	snapshot, result := UnitSnapshot{}.Apply(event(1, 0))

	if !result.Applied {
		t.Fatal("the first frame must change the snapshot")
	}
	if len(result.Sensors) != 3 {
		t.Errorf("sensors = %v, want all three", result.Sensors)
	}
	if snapshot.Battery == nil || snapshot.Battery.Value != 82 {
		t.Errorf("battery = %+v, want 82", snapshot.Battery)
	}
	if snapshot.LastSequence != 1 || snapshot.SessionID != "session-1" {
		t.Errorf("stream = seq %d session %s, want 1 and session-1", snapshot.LastSequence, snapshot.SessionID)
	}
	if result.Reconnected {
		t.Error("an opening session is not a reconnect")
	}
}

func TestApplyUpdatesOnlyTheSensorsAFrameCarries(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	// A unit that only measured its battery sends only a battery. The
	// position's age must keep running: that is the whole point of per-sensor
	// freshness.
	partial := Event{
		UnitID:         "unit-1",
		SessionID:      "session-1",
		Sequence:       2,
		Classification: ClassificationAccepted,
		ObservedAt:     at(20 * time.Minute),
		ReceivedAt:     at(20 * time.Minute),
		Battery:        value(61),
	}

	updated, result := snapshot.Apply(partial)

	if result.Sensors == nil || len(result.Sensors) != 1 || result.Sensors[0] != SensorBattery {
		t.Errorf("sensors = %v, want only the battery", result.Sensors)
	}
	if updated.Battery.Value != 61 || !updated.Battery.ObservedAt.Equal(at(20*time.Minute)) {
		t.Errorf("battery = %+v, want 61 measured 20 minutes in", updated.Battery)
	}
	if !updated.Position.ObservedAt.Equal(at(0)) {
		t.Errorf("position observedAt = %s, want it unchanged at %s", updated.Position.ObservedAt, at(0))
	}
	if updated.Position.Value.Latitude != 37.5665 {
		t.Error("an untouched sensor must keep its value")
	}
}

func TestApplyRefusesToMoveASensorBackwards(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(5, 5*time.Minute))

	// A frame that arrives late carries an older measurement. Applying it
	// would rewrite history with a worse reading.
	late := event(3, 2*time.Minute)
	late.Classification = ClassificationOutOfOrder
	late.Battery = value(40)

	updated, result := snapshot.Apply(late)

	if len(result.Sensors) != 0 {
		t.Errorf("sensors = %v, want nothing applied", result.Sensors)
	}
	if len(result.StaleSensors) != 3 {
		t.Errorf("staleSensors = %v, want all three reported as stale", result.StaleSensors)
	}
	if updated.Battery.Value != 82 {
		t.Errorf("battery = %d, want the newer 82 kept", updated.Battery.Value)
	}
	if updated.LastSequence != 5 {
		t.Errorf("lastSequence = %d, want the furthest point 5 kept", updated.LastSequence)
	}
}

func TestApplyAcceptsAnOutOfOrderFrameThatIsStillNewerPerSensor(t *testing.T) {
	// Sequence and measurement time can disagree when frames interleave: a
	// frame behind in sequence may carry a sensor measured later. The rule is
	// about the measurement, not about the sequence.
	snapshot, _ := UnitSnapshot{}.Apply(event(5, 5*time.Minute))

	interleaved := Event{
		UnitID:         "unit-1",
		SessionID:      "session-1",
		Sequence:       4,
		Classification: ClassificationOutOfOrder,
		ObservedAt:     at(9 * time.Minute),
		ReceivedAt:     at(10 * time.Minute),
		Battery:        value(70),
	}

	updated, result := snapshot.Apply(interleaved)

	if len(result.Sensors) != 1 || result.Sensors[0] != SensorBattery {
		t.Errorf("sensors = %v, want the battery applied", result.Sensors)
	}
	if updated.Battery.Value != 70 {
		t.Errorf("battery = %d, want 70", updated.Battery.Value)
	}
	if updated.LastSequence != 5 {
		t.Errorf("lastSequence = %d, want 5: a late frame is not progress", updated.LastSequence)
	}
}

func TestApplyIgnoresADuplicate(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	duplicate := event(1, 0)
	duplicate.Classification = ClassificationDuplicate
	duplicate.Battery = value(10)

	updated, result := snapshot.Apply(duplicate)

	if result.Applied {
		t.Error("a duplicate must change nothing")
	}
	if updated.Battery.Value != 82 {
		t.Errorf("battery = %d, want 82 untouched", updated.Battery.Value)
	}
}

func TestApplyRecordsAGapAsIncompleteness(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	gap := event(5, time.Minute)
	gap.Classification = ClassificationGap
	gap.MissingFrames = 3

	updated, result := snapshot.Apply(gap)

	if !result.Applied {
		t.Error("a frame past a gap is still applied")
	}
	if updated.MissingFrames != 3 {
		t.Errorf("missingFrames = %d, want 3", updated.MissingFrames)
	}
	if updated.LastSequence != 5 {
		t.Errorf("lastSequence = %d, want 5", updated.LastSequence)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	first := event(1, 0)
	gap := event(5, time.Minute)
	gap.Classification = ClassificationGap
	gap.MissingFrames = 3

	snapshot, _ := UnitSnapshot{}.Apply(first)
	snapshot, _ = snapshot.Apply(gap)

	// The consumer may re-run after a restart. Replaying the same events must
	// not count the gap twice or disturb a reading.
	replayed, result := snapshot.Apply(gap)
	replayed, _ = replayed.Apply(first)

	if result.Applied {
		t.Error("replaying an event must change nothing")
	}
	if replayed.MissingFrames != 3 {
		t.Errorf("missingFrames = %d, want 3 after a replay", replayed.MissingFrames)
	}
	if replayed.LastSequence != 5 || !replayed.LastReceivedAt.Equal(at(time.Minute)) {
		t.Errorf("stream = seq %d received %s, want 5 and %s", replayed.LastSequence, replayed.LastReceivedAt, at(time.Minute))
	}
	if replayed.Battery.Value != 82 || !replayed.Battery.ObservedAt.Equal(at(time.Minute)) {
		t.Errorf("battery = %+v, want the newest measurement kept", replayed.Battery)
	}
}

func TestApplyTreatsANewSessionAsAReconnect(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))
	gap := event(4, time.Minute)
	gap.Classification = ClassificationGap
	gap.MissingFrames = 2
	snapshot, _ = snapshot.Apply(gap)

	reconnected := event(1, 10*time.Minute)
	reconnected.SessionID = "session-2"
	reconnected.Battery = value(55)

	updated, result := snapshot.Apply(reconnected)

	if !result.Reconnected {
		t.Error("a new session for a known unit is a reconnect")
	}
	if updated.SessionID != "session-2" || updated.LastSequence != 1 {
		t.Errorf("stream = %s seq %d, want session-2 and 1", updated.SessionID, updated.LastSequence)
	}
	if updated.MissingFrames != 0 {
		t.Errorf("missingFrames = %d, want the counter reset with the session", updated.MissingFrames)
	}
	// The readings survive: the unit was there and measured them, and the last
	// known position is what the operator needs most at a reconnect.
	if updated.Battery.Value != 55 || updated.Position == nil {
		t.Errorf("readings = %+v / %+v, want them carried across the session", updated.Battery, updated.Position)
	}
}

func TestLastReceivedAtAnswersWhetherAnythingIsArriving(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	// A frame measured long ago but received now still proves the link is
	// alive, which is a different question from whether the value is current.
	delayed := Event{
		UnitID:         "unit-1",
		SessionID:      "session-1",
		Sequence:       2,
		Classification: ClassificationAccepted,
		ObservedAt:     at(-30 * time.Minute),
		ReceivedAt:     at(time.Minute),
		Battery:        value(50),
	}

	updated, _ := snapshot.Apply(delayed)

	if !updated.LastReceivedAt.Equal(at(time.Minute)) {
		t.Errorf("lastReceivedAt = %s, want %s", updated.LastReceivedAt, at(time.Minute))
	}
	if updated.Battery.Value != 82 {
		t.Error("a delayed measurement must not replace a newer one")
	}
}

func TestNoteRejectionKeepsTheReceipt(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	updated := snapshot.NoteRejection("unit-1", at(time.Minute))
	updated = updated.NoteRejection("unit-1", at(2*time.Minute))

	if updated.RejectedFrames != 2 {
		t.Errorf("rejectedFrames = %d, want 2", updated.RejectedFrames)
	}
	if !updated.LastReceivedAt.Equal(at(2 * time.Minute)) {
		t.Errorf("lastReceivedAt = %s, want %s: a refused frame still arrived", updated.LastReceivedAt, at(2*time.Minute))
	}
	if updated.Battery.Value != 82 {
		t.Error("a refused frame must not disturb a stored reading")
	}
}

func TestApplyAdoptsTheUnitOnAnEmptySnapshot(t *testing.T) {
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	if snapshot.UnitID != "unit-1" {
		t.Errorf("unitId = %q, want unit-1", snapshot.UnitID)
	}
}

func TestApplyDoesNotReopenASessionThatAlreadyEnded(t *testing.T) {
	// A delayed frame from the previous session arrives after the reconnect.
	// Treating it as a session change would wind the stream state back to a
	// session that is over.
	snapshot, _ := UnitSnapshot{}.Apply(event(9, 0))

	reconnect := event(1, 5*time.Minute)
	reconnect.SessionID = "session-2"
	snapshot, _ = snapshot.Apply(reconnect)

	late := event(10, time.Minute)
	late.SessionID = "session-1"
	late.ReceivedAt = at(6 * time.Minute)
	late.Classification = ClassificationOutOfOrder

	updated, result := snapshot.Apply(late)

	if updated.SessionID != "session-2" {
		t.Errorf("session = %s, want the current session-2 kept", updated.SessionID)
	}
	if updated.LastSequence != 1 {
		t.Errorf("lastSequence = %d, want 1: a late frame of an old session is not progress", updated.LastSequence)
	}
	if result.Reconnected {
		t.Error("a frame from a session that ended is not a reconnect")
	}
	// It still proves something arrived.
	if !updated.LastReceivedAt.Equal(at(6 * time.Minute)) {
		t.Errorf("lastReceivedAt = %s, want the late arrival recorded", updated.LastReceivedAt)
	}
	// And its older measurements still lose to the newer ones.
	if !updated.Battery.ObservedAt.Equal(at(5 * time.Minute)) {
		t.Error("the newer measurement must survive a late frame")
	}
}

func TestApplyStillCountsAGapWithinTheCurrentSession(t *testing.T) {
	// The guard above must not stop a genuine gap from being recorded.
	snapshot, _ := UnitSnapshot{}.Apply(event(1, 0))

	gap := event(4, time.Minute)
	gap.Classification = ClassificationGap
	gap.MissingFrames = 2

	updated, _ := snapshot.Apply(gap)

	if updated.MissingFrames != 2 || updated.LastSequence != 4 {
		t.Errorf("stream = missing %d seq %d, want 2 and 4", updated.MissingFrames, updated.LastSequence)
	}
}

func TestApplyAccumulatesEveryGapOfASession(t *testing.T) {
	// The arrival order a real run produced: refused frames leave holes, and
	// each hole is reported as its own gap. The counter has to add them up.
	type step struct {
		sequence       int64
		classification Classification
		missing        int64
	}
	steps := []step{
		{1, ClassificationAccepted, 0},
		{2, ClassificationAccepted, 0},
		{3, ClassificationAccepted, 0},
		{5, ClassificationGap, 1},
		{6, ClassificationAccepted, 0},
		{8, ClassificationGap, 1},
		{10, ClassificationGap, 1},
		{11, ClassificationAccepted, 0},
	}

	snapshot := UnitSnapshot{}
	for index, current := range steps {
		next := event(current.sequence, time.Duration(index)*time.Second)
		next.Classification = current.classification
		next.MissingFrames = current.missing
		snapshot, _ = snapshot.Apply(next)
	}

	if snapshot.MissingFrames != 3 {
		t.Errorf("missingFrames = %d, want 3: every gap of the session counts", snapshot.MissingFrames)
	}
	if snapshot.LastSequence != 11 {
		t.Errorf("lastSequence = %d, want 11", snapshot.LastSequence)
	}
}

func TestApplyOpensANewSessionEvenWhenItsFramesAreOlderThanWhatIsStored(t *testing.T) {
	// The live case this rule was written for. A unit that has been quiet
	// holds readings newer than the first frames of its next connection: the
	// stored battery was measured seconds before the unit dropped out, while
	// the new run replays a window that starts minutes back. Judging the
	// session by "is this the newest measurement" would call the whole new
	// session a late frame of the old one, and the stream counters would stand
	// still for as long as the replay lasted.
	stored := UnitSnapshot{
		UnitID:           "unit-1",
		Battery:          &Reading[int]{Value: 90, ObservedAt: at(0), ReceivedAt: at(0)},
		SessionID:        "session-0",
		SessionStartedAt: at(-30 * time.Minute),
		LastSequence:     88,
	}

	first := event(1, -10*time.Minute)
	first.SessionID = "session-1"
	updated, result := stored.Apply(first)

	if !result.Reconnected || updated.SessionID != "session-1" {
		t.Fatalf("session = %s reconnected = %t, want session-1 adopted as a reconnect",
			updated.SessionID, result.Reconnected)
	}
	if updated.LastSequence != 1 {
		t.Errorf("lastSequence = %d, want 1: the new session numbers from the start", updated.LastSequence)
	}

	gap := event(3, -9*time.Minute)
	gap.SessionID = "session-1"
	gap.Classification = ClassificationGap
	gap.MissingFrames = 1
	updated, _ = updated.Apply(gap)

	if updated.MissingFrames != 1 || updated.LastSequence != 3 {
		t.Errorf("stream = missing %d seq %d, want 1 and 3: the new session's gaps count",
			updated.MissingFrames, updated.LastSequence)
	}
	// The older replayed measurement still loses to the newer stored one: a
	// reconnect changes the session, not the history.
	if updated.Battery.Value != 90 {
		t.Errorf("battery = %d, want the newer stored measurement kept", updated.Battery.Value)
	}
}

func TestApplyJudgesASessionByMeasurementTimeNotArrivalTime(t *testing.T) {
	// A live run's shape: the frames were measured across a window in the
	// past and all arrive within the same second. Judging a session change by
	// arrival time would mean every frame of the new session looks older than
	// the session already on file, and the stream would never move on.
	arrival := at(time.Hour)

	frame := func(session string, sequence int64, observed time.Duration) Event {
		next := event(sequence, observed)
		next.SessionID = session
		next.ReceivedAt = arrival
		return next
	}

	snapshot := UnitSnapshot{}
	snapshot, _ = snapshot.Apply(frame("session-1", 1, 0))
	snapshot, _ = snapshot.Apply(frame("session-1", 2, 10*time.Second))

	// The link drops and the unit comes back: a later measurement, same
	// arrival second.
	reconnect := frame("session-2", 1, 40*time.Second)
	snapshot, result := snapshot.Apply(reconnect)

	if !result.Reconnected || snapshot.SessionID != "session-2" || snapshot.LastSequence != 1 {
		t.Fatalf("session = %s seq %d reconnected = %t, want session-2 at 1",
			snapshot.SessionID, snapshot.LastSequence, result.Reconnected)
	}

	// And the delayed frame of the session that ended, which the simulator
	// sends last of all, must not drag the stream back to it.
	late := frame("session-1", 3, 20*time.Second)
	late.Classification = ClassificationOutOfOrder
	snapshot, lateResult := snapshot.Apply(late)

	if lateResult.Reconnected || snapshot.SessionID != "session-2" || snapshot.LastSequence != 1 {
		t.Errorf("session = %s seq %d, want session-2 at 1: a delayed frame of an ended session is not progress",
			snapshot.SessionID, snapshot.LastSequence)
	}
}
