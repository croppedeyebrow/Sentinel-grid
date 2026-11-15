package domain

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

func testPolicy() ObservationPolicy {
	return ObservationPolicy{
		PositionWindow:         45 * time.Second,
		BatteryWindow:          180 * time.Second,
		LinkWindow:             180 * time.Second,
		BatteryWarningPercent:  40,
		BatteryCriticalPercent: 20,
		LinkWarningPercent:     50,
		LinkCriticalPercent:    20,
	}
}

func at(offset time.Duration) *time.Time {
	t := now.Add(offset)
	return &t
}

func value[T any](v T) *T { return &v }

func TestObservedFreshness(t *testing.T) {
	cases := []struct {
		name     string
		observed Observed[int]
		window   time.Duration
		want     Freshness
	}{
		{
			name:     "fresh inside the window",
			observed: Observed[int]{Value: value(80), ObservedAt: at(-10 * time.Second), ReceivedAt: at(0)},
			window:   45 * time.Second,
			want:     FreshnessFresh,
		},
		{
			name:     "exactly at the window is still fresh",
			observed: Observed[int]{Value: value(80), ObservedAt: at(-45 * time.Second), ReceivedAt: at(0)},
			window:   45 * time.Second,
			want:     FreshnessFresh,
		},
		{
			name:     "one second past the window is stale",
			observed: Observed[int]{Value: value(80), ObservedAt: at(-46 * time.Second), ReceivedAt: at(0)},
			window:   45 * time.Second,
			want:     FreshnessStale,
		},
		{
			name:     "no value is missing",
			observed: Observed[int]{Value: nil, ObservedAt: at(-1 * time.Second), ReceivedAt: at(0)},
			window:   45 * time.Second,
			want:     FreshnessMissing,
		},
		{
			name:     "value without an observation time cannot be aged",
			observed: Observed[int]{Value: value(37), ObservedAt: nil, ReceivedAt: at(-40 * time.Minute)},
			window:   45 * time.Second,
			want:     FreshnessMissing,
		},
		{
			name:     "an observation timestamped ahead of now is not stale",
			observed: Observed[int]{Value: value(80), ObservedAt: at(2 * time.Second), ReceivedAt: at(0)},
			window:   45 * time.Second,
			want:     FreshnessFresh,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.observed.Freshness(now, testCase.window); got != testCase.want {
				t.Errorf("freshness = %s, want %s", got, testCase.want)
			}
		})
	}
}

// snapshot builds a unit whose fields are all fresh, so each test can spoil
// exactly the one field it is about.
func snapshot() PatrolUnitSnapshot {
	return PatrolUnitSnapshot{
		UnitID:      "unit-1",
		Callsign:    "SG-01",
		DisplayName: "Sentinel One",
		Position:    Observed[Position]{Value: value(Position{Latitude: 37.5665, Longitude: 126.978}), ObservedAt: at(-5 * time.Second), ReceivedAt: at(0)},
		Battery:     Observed[int]{Value: value(82), ObservedAt: at(-5 * time.Second), ReceivedAt: at(0)},
		Link:        Observed[int]{Value: value(94), ObservedAt: at(-5 * time.Second), ReceivedAt: at(0)},
	}
}

func TestEvaluateDerivesStatus(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PatrolUnitSnapshot)
		want   UnitStatus
	}{
		{
			name:   "fresh and healthy",
			mutate: func(*PatrolUnitSnapshot) {},
			want:   UnitStatusNormal,
		},
		{
			name:   "battery at the warning threshold",
			mutate: func(s *PatrolUnitSnapshot) { s.Battery.Value = value(40) },
			want:   UnitStatusWarning,
		},
		{
			name:   "link at the warning threshold",
			mutate: func(s *PatrolUnitSnapshot) { s.Link.Value = value(50) },
			want:   UnitStatusWarning,
		},
		{
			name:   "battery at the critical threshold",
			mutate: func(s *PatrolUnitSnapshot) { s.Battery.Value = value(20) },
			want:   UnitStatusCritical,
		},
		{
			name:   "lost link is critical",
			mutate: func(s *PatrolUnitSnapshot) { s.Link.Value = value(0) },
			want:   UnitStatusCritical,
		},
		{
			name:   "critical wins over warning",
			mutate: func(s *PatrolUnitSnapshot) { s.Battery.Value = value(15); s.Link.Value = value(45) },
			want:   UnitStatusCritical,
		},
		{
			name:   "stale position leaves the condition unknown",
			mutate: func(s *PatrolUnitSnapshot) { s.Position.ObservedAt = at(-20 * time.Minute) },
			want:   UnitStatusUnknown,
		},
		{
			name:   "missing battery leaves the condition unknown",
			mutate: func(s *PatrolUnitSnapshot) { s.Battery.Value = nil },
			want:   UnitStatusUnknown,
		},
		{
			name:   "missing position leaves the condition unknown",
			mutate: func(s *PatrolUnitSnapshot) { s.Position.Value = nil; s.Position.ObservedAt = nil },
			want:   UnitStatusUnknown,
		},
		{
			name: "a stale low battery is not reported as critical",
			mutate: func(s *PatrolUnitSnapshot) {
				s.Battery.Value = value(5)
				s.Battery.ObservedAt = at(-10 * time.Minute)
			},
			want: UnitStatusUnknown,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			unit := snapshot()
			testCase.mutate(&unit)

			if got := unit.Evaluate(now, testPolicy()).Status; got != testCase.want {
				t.Errorf("status = %s, want %s", got, testCase.want)
			}
		})
	}
}

func TestEvaluateKeepsBothAxesSeparate(t *testing.T) {
	unit := snapshot()
	unit.Position.ObservedAt = at(-20 * time.Minute)
	unit.Battery.ObservedAt = at(-20 * time.Minute)
	unit.Link.ObservedAt = at(-20 * time.Minute)

	view := unit.Evaluate(now, testPolicy())

	if view.Status != UnitStatusUnknown {
		t.Errorf("status = %s, want UNKNOWN", view.Status)
	}
	for name, freshness := range map[string]Freshness{
		"position": view.Position.Freshness,
		"battery":  view.Battery.Freshness,
		"link":     view.Link.Freshness,
	} {
		if freshness != FreshnessStale {
			t.Errorf("%s freshness = %s, want STALE", name, freshness)
		}
	}
	// The stale values stay available with their observation times so the
	// console can show a last known position without treating it as current.
	if view.Battery.Value == nil || *view.Battery.Value != 82 {
		t.Error("stale observations must keep their value")
	}
	if view.Position.ObservedAt == nil || view.Position.ReceivedAt == nil {
		t.Error("stale observations must keep both timestamps")
	}
}

func TestPolicyValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ObservationPolicy)
		wantErr bool
	}{
		{"valid", func(*ObservationPolicy) {}, false},
		{"zero window", func(p *ObservationPolicy) { p.PositionWindow = 0 }, true},
		{"negative window", func(p *ObservationPolicy) { p.LinkWindow = -time.Second }, true},
		{"percent above range", func(p *ObservationPolicy) { p.BatteryWarningPercent = 101 }, true},
		{"percent below range", func(p *ObservationPolicy) { p.LinkCriticalPercent = -1 }, true},
		{"battery critical above warning", func(p *ObservationPolicy) { p.BatteryCriticalPercent = 60 }, true},
		{"link critical above warning", func(p *ObservationPolicy) { p.LinkCriticalPercent = 80 }, true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := testPolicy()
			testCase.mutate(&policy)

			err := policy.Validate()
			if testCase.wantErr {
				if !errors.Is(err, ErrInvalidPolicy) {
					t.Errorf("err = %v, want ErrInvalidPolicy", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
