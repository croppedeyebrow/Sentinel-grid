package domain

import (
	"testing"
	"time"
)

func observedPosition() *Position {
	position := Position{Latitude: 37.5665, Longitude: 126.978}
	return &position
}

func moment(t time.Time) *time.Time { return &t }

func TestTrustObservations(t *testing.T) {
	observed := windowStart.Add(10 * time.Minute)
	allowance := 45 * time.Second

	cases := []struct {
		name        string
		observation Observation
		wantTrusted bool
	}{
		{
			name: "received immediately",
			observation: Observation{
				Position:   observedPosition(),
				ObservedAt: moment(observed),
				ReceivedAt: moment(observed),
			},
			wantTrusted: true,
		},
		{
			name: "received exactly at the allowed delay",
			observation: Observation{
				Position:   observedPosition(),
				ObservedAt: moment(observed),
				ReceivedAt: moment(observed.Add(allowance)),
			},
			wantTrusted: true,
		},
		{
			name: "received past the allowed delay",
			observation: Observation{
				Position:   observedPosition(),
				ObservedAt: moment(observed),
				ReceivedAt: moment(observed.Add(allowance + time.Second)),
			},
			wantTrusted: false,
		},
		{
			name: "no position",
			observation: Observation{
				ObservedAt: moment(observed),
				ReceivedAt: moment(observed),
			},
			wantTrusted: false,
		},
		{
			name: "no observation time",
			observation: Observation{
				Position:   observedPosition(),
				ReceivedAt: moment(observed),
			},
			wantTrusted: false,
		},
		{
			name: "received before it was measured",
			observation: Observation{
				Position:   observedPosition(),
				ObservedAt: moment(observed),
				ReceivedAt: moment(observed.Add(-time.Minute)),
			},
			wantTrusted: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			evidence := TrustObservations([]Observation{testCase.observation}, allowance)

			if len(evidence) != 1 {
				t.Fatalf("evidence = %d items, want 1: unusable rows are kept, not dropped", len(evidence))
			}
			if evidence[0].Trusted != testCase.wantTrusted {
				t.Errorf("trusted = %v, want %v", evidence[0].Trusted, testCase.wantTrusted)
			}
		})
	}
}

func TestTrustObservationsDoesNotJudgeAgainstNow(t *testing.T) {
	// A mission from last week is judged on its own observations, so age
	// relative to now must not make them unusable.
	lastWeek := windowStart.Add(-7 * 24 * time.Hour)

	evidence := TrustObservations([]Observation{
		{
			Position:   observedPosition(),
			ObservedAt: moment(lastWeek),
			ReceivedAt: moment(lastWeek.Add(time.Second)),
		},
	}, 45*time.Second)

	if !evidence[0].Trusted {
		t.Error("an old observation received promptly is still evidence")
	}
	if !evidence[0].ObservedAt.Equal(lastWeek) {
		t.Errorf("observedAt = %s, want %s", evidence[0].ObservedAt, lastWeek)
	}
}
