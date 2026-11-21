package domain

import (
	"testing"
	"time"
)

func commitment(missionID, unitID string, start, end time.Time) Commitment {
	return Commitment{
		MissionID: missionID,
		UnitID:    unitID,
		Callsign:  "SG-01",
		Window:    Window{Start: start, End: end},
	}
}

func TestConflictingCallsigns(t *testing.T) {
	cases := []struct {
		name      string
		committed []Commitment
		want      []string
	}{
		{
			name:      "no other commitment",
			committed: nil,
			want:      nil,
		},
		{
			name: "the same unit in an overlapping window conflicts",
			committed: []Commitment{
				commitment("other", "unit-1", windowStart.Add(time.Hour), windowEnd.Add(time.Hour)),
			},
			want: []string{"SG-01"},
		},
		{
			name: "a window that only touches the edge does not overlap",
			committed: []Commitment{
				commitment("other", "unit-1", windowEnd, windowEnd.Add(time.Hour)),
			},
			want: nil,
		},
		{
			name: "a different unit never conflicts",
			committed: []Commitment{
				commitment("other", "unit-2", windowStart, windowEnd),
			},
			want: nil,
		},
		{
			name: "the mission's own commitment is not a conflict with itself",
			committed: []Commitment{
				commitment("mission-1", "unit-1", windowStart, windowEnd),
			},
			want: nil,
		},
		{
			name: "a unit committed twice is reported once",
			committed: []Commitment{
				commitment("other-a", "unit-1", windowStart, windowEnd),
				commitment("other-b", "unit-1", windowStart, windowEnd),
			},
			want: []string{"SG-01"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ConflictingCallsigns(draft(), testCase.committed)

			if len(got) != len(testCase.want) {
				t.Fatalf("conflicts = %v, want %v", got, testCase.want)
			}
			for index, callsign := range testCase.want {
				if got[index] != callsign {
					t.Errorf("conflicts = %v, want %v", got, testCase.want)
				}
			}
		})
	}
}

func TestWindowOverlaps(t *testing.T) {
	window := testWindow()
	cases := []struct {
		name  string
		other Window
		want  bool
	}{
		{"identical", window, true},
		{"contained", Window{Start: windowStart.Add(time.Minute), End: windowEnd.Add(-time.Minute)}, true},
		{"starts before and ends inside", Window{Start: windowStart.Add(-time.Hour), End: windowStart.Add(time.Minute)}, true},
		{"entirely before", Window{Start: windowStart.Add(-2 * time.Hour), End: windowStart.Add(-time.Hour)}, false},
		{"entirely after", Window{Start: windowEnd.Add(time.Hour), End: windowEnd.Add(2 * time.Hour)}, false},
		{"touching the start", Window{Start: windowStart.Add(-time.Hour), End: windowStart}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := window.Overlaps(testCase.other); got != testCase.want {
				t.Errorf("overlaps = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestWindowContains(t *testing.T) {
	window := testWindow()

	cases := map[string]struct {
		moment time.Time
		want   bool
	}{
		"before the start": {windowStart.Add(-time.Second), false},
		"exactly at start": {windowStart, true},
		"inside":           {windowStart.Add(time.Hour), true},
		"exactly at end":   {windowEnd, true},
		"after the end":    {windowEnd.Add(time.Second), false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := window.Contains(testCase.moment); got != testCase.want {
				t.Errorf("contains = %v, want %v", got, testCase.want)
			}
		})
	}
}
