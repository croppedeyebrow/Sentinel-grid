package domain

import (
	"testing"
	"time"
)

func moment(offset time.Duration) *time.Time {
	t := now.Add(offset)
	return &t
}

func TestStreamStateSeparatesSilenceFromStillness(t *testing.T) {
	window := 45 * time.Second

	cases := []struct {
		name   string
		stream UnitStream
		want   StreamState
	}{
		{
			name:   "nothing has ever arrived",
			stream: UnitStream{},
			want:   StreamNeverSeen,
		},
		{
			name:   "something arrived just now",
			stream: UnitStream{LastReceivedAt: moment(-5 * time.Second)},
			want:   StreamStreaming,
		},
		{
			name:   "arrival exactly at the window still counts",
			stream: UnitStream{LastReceivedAt: moment(-45 * time.Second)},
			want:   StreamStreaming,
		},
		{
			name:   "nothing since longer than the window",
			stream: UnitStream{LastReceivedAt: moment(-20 * time.Minute)},
			want:   StreamSilent,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.stream.Evaluate(now, window).State; got != testCase.want {
				t.Errorf("state = %s, want %s", got, testCase.want)
			}
		})
	}
}

func TestSilentStreamReportsHowLong(t *testing.T) {
	stream := UnitStream{LastReceivedAt: moment(-20 * time.Minute)}

	evaluated := stream.Evaluate(now, 45*time.Second)

	if evaluated.SilentFor != 20*time.Minute {
		t.Errorf("silentFor = %s, want 20m", evaluated.SilentFor)
	}
}

func TestStreamToleratesAReceiptStampedAhead(t *testing.T) {
	// Clocks drift. A receipt a second ahead of ours is not a negative
	// silence.
	stream := UnitStream{LastReceivedAt: moment(time.Second)}

	evaluated := stream.Evaluate(now, 45*time.Second)

	if evaluated.State != StreamStreaming || evaluated.SilentFor != 0 {
		t.Errorf("evaluated = %s / %s, want STREAMING and no silence", evaluated.State, evaluated.SilentFor)
	}
}

func TestReconnectedOnlyWhileTheSessionIsNew(t *testing.T) {
	window := 45 * time.Second

	cases := map[string]struct {
		stream UnitStream
		want   bool
	}{
		"session opened seconds ago":  {UnitStream{SessionStartedAt: moment(-10 * time.Second)}, true},
		"session running for an hour": {UnitStream{SessionStartedAt: moment(-time.Hour)}, false},
		"no session at all":           {UnitStream{}, false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := testCase.stream.Reconnected(now, window); got != testCase.want {
				t.Errorf("reconnected = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestASilentUnitKeepsItsReadingsAndStaysUnknown(t *testing.T) {
	// The two axes again: silence is a data quality reading. The unit is not
	// reported as failing, and the values it did send are not discarded.
	unit := snapshot()
	unit.Position.ObservedAt = at(-40 * time.Minute)
	unit.Battery.ObservedAt = at(-40 * time.Minute)
	unit.Link.ObservedAt = at(-40 * time.Minute)
	unit.Stream = UnitStream{
		SessionID:        "session-1",
		SessionStartedAt: at(-70 * time.Minute),
		LastReceivedAt:   at(-40 * time.Minute),
		MissingFrames:    7,
	}

	view := unit.Evaluate(now, testPolicy())

	if view.Status != UnitStatusUnknown {
		t.Errorf("status = %s, want UNKNOWN", view.Status)
	}
	if view.Stream.State != StreamSilent {
		t.Errorf("stream = %s, want SILENT", view.Stream.State)
	}
	if view.Stream.MissingFrames != 7 {
		t.Errorf("missingFrames = %d, want 7 carried through", view.Stream.MissingFrames)
	}
	if view.Battery.Value == nil || *view.Battery.Value != 82 {
		t.Error("a silent unit keeps the values it did report")
	}
}

func TestAStreamingUnitCanHoldAStalePositionAndAFreshBattery(t *testing.T) {
	// The case TEL-01 exists for, and the one a single shared timestamp could
	// not express.
	unit := snapshot()
	unit.Position.ObservedAt = at(-20 * time.Minute)
	unit.Stream = UnitStream{SessionID: "session-1", LastReceivedAt: at(-2 * time.Second)}

	view := unit.Evaluate(now, testPolicy())

	if view.Position.Freshness != FreshnessStale {
		t.Errorf("position freshness = %s, want STALE", view.Position.Freshness)
	}
	if view.Battery.Freshness != FreshnessFresh {
		t.Errorf("battery freshness = %s, want FRESH", view.Battery.Freshness)
	}
	if view.Stream.State != StreamStreaming {
		t.Errorf("stream = %s, want STREAMING: frames are still arriving", view.Stream.State)
	}
	if view.Status != UnitStatusUnknown {
		t.Errorf("status = %s, want UNKNOWN while a sensor is stale", view.Status)
	}
}
