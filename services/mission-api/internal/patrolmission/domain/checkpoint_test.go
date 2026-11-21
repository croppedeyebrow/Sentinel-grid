package domain

import (
	"testing"
	"time"
)

// at builds an observation inside the mission window unless told otherwise.
func at(offset time.Duration) time.Time { return windowStart.Add(offset) }

// atCheckpoint returns a position a few metres from the checkpoint, well
// inside its 40 m radius.
func atCheckpoint(checkpoint Checkpoint) Position {
	return Position{
		Latitude:  checkpoint.Position.Latitude + 0.0001,
		Longitude: checkpoint.Position.Longitude,
	}
}

func states(judgements []CheckpointJudgement) []CheckpointState {
	result := make([]CheckpointState, 0, len(judgements))
	for _, judgement := range judgements {
		result = append(result, judgement.State)
	}
	return result
}

func equalStates(got, want []CheckpointState) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestDistanceTo(t *testing.T) {
	// 0.001° of latitude is about 111 m, which is the scale checkpoint radii
	// are chosen against.
	from := Position{Latitude: 37.5665, Longitude: 126.978}
	to := Position{Latitude: 37.5675, Longitude: 126.978}

	distance := from.DistanceTo(to)

	if distance < 105 || distance > 118 {
		t.Errorf("distance = %.1f m, want roughly 111 m", distance)
	}
	if same := from.DistanceTo(from); same != 0 {
		t.Errorf("distance to itself = %.3f, want 0", same)
	}
}

func TestAreaContains(t *testing.T) {
	area := testArea()
	cases := []struct {
		name  string
		point Position
		want  bool
	}{
		{"centre", Position{Latitude: 37.5665, Longitude: 126.978}, true},
		{"near the south-west corner", Position{Latitude: 37.5585, Longitude: 126.9655}, true},
		{"north of the area", Position{Latitude: 37.6, Longitude: 126.978}, false},
		{"east of the area", Position{Latitude: 37.5665, Longitude: 127.05}, false},
		{"south of the area", Position{Latitude: 37.55, Longitude: 126.978}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := area.Contains(testCase.point); got != testCase.want {
				t.Errorf("contains = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestJudgeCheckpointsBeforeTheMissionRuns(t *testing.T) {
	for _, mission := range []Mission{draft(), approved()} {
		t.Run(string(mission.Status), func(t *testing.T) {
			judgements := mission.JudgeCheckpoints([]Evidence{
				{Position: atCheckpoint(testCheckpoints()[0]), ObservedAt: at(5 * time.Minute), Trusted: true},
			})

			if !equalStates(states(judgements), []CheckpointState{CheckpointPlanned, CheckpointPlanned}) {
				t.Errorf("states = %v, want all PLANNED before the mission runs", states(judgements))
			}
		})
	}
}

func TestJudgeCheckpointsCreditsOnlyTrustedObservations(t *testing.T) {
	mission := active()
	checkpoints := mission.Checkpoints

	cases := []struct {
		name     string
		evidence []Evidence
		want     []CheckpointState
	}{
		{
			name: "a trusted observation inside the radius credits the pass",
			evidence: []Evidence{
				{Position: atCheckpoint(checkpoints[0]), ObservedAt: at(5 * time.Minute), Trusted: true},
			},
			want: []CheckpointState{CheckpointVisited, CheckpointPlanned},
		},
		{
			name: "an untrusted observation credits nothing",
			evidence: []Evidence{
				{Position: atCheckpoint(checkpoints[0]), ObservedAt: at(5 * time.Minute), Trusted: false},
			},
			want: []CheckpointState{CheckpointPlanned, CheckpointPlanned},
		},
		{
			name: "an observation taken outside the window credits nothing",
			evidence: []Evidence{
				{Position: atCheckpoint(checkpoints[0]), ObservedAt: windowStart.Add(-time.Hour), Trusted: true},
			},
			want: []CheckpointState{CheckpointPlanned, CheckpointPlanned},
		},
		{
			name: "an observation beyond the radius credits nothing",
			evidence: []Evidence{
				{
					Position:   Position{Latitude: checkpoints[0].Position.Latitude + 0.01, Longitude: checkpoints[0].Position.Longitude},
					ObservedAt: at(5 * time.Minute),
					Trusted:    true,
				},
			},
			want: []CheckpointState{CheckpointPlanned, CheckpointPlanned},
		},
		{
			name: "both checkpoints passed",
			evidence: []Evidence{
				{Position: atCheckpoint(checkpoints[0]), ObservedAt: at(5 * time.Minute), Trusted: true},
				{Position: atCheckpoint(checkpoints[1]), ObservedAt: at(20 * time.Minute), Trusted: true},
			},
			want: []CheckpointState{CheckpointVisited, CheckpointVisited},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := states(mission.JudgeCheckpoints(testCase.evidence))

			if !equalStates(got, testCase.want) {
				t.Errorf("states = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestJudgeCheckpointsCreditsTheEarliestPass(t *testing.T) {
	mission := active()
	checkpoint := mission.Checkpoints[0]
	first := at(5 * time.Minute)

	judgements := mission.JudgeCheckpoints([]Evidence{
		{Position: atCheckpoint(checkpoint), ObservedAt: at(9 * time.Minute), Trusted: true},
		{Position: atCheckpoint(checkpoint), ObservedAt: first, Trusted: true},
	})

	if judgements[0].CreditedAt == nil || !judgements[0].CreditedAt.Equal(first) {
		t.Errorf("creditedAt = %v, want %s", judgements[0].CreditedAt, first)
	}
}

func TestJudgeCheckpointsOnAClosedMission(t *testing.T) {
	passedFirst := []Evidence{
		{Position: atCheckpoint(testCheckpoints()[0]), ObservedAt: at(5 * time.Minute), Trusted: true},
	}

	cases := []struct {
		name     string
		status   Status
		evidence []Evidence
		want     []CheckpointState
	}{
		{
			name:     "a completed mission with reliable observations reports the gap as missed",
			status:   StatusCompleted,
			evidence: passedFirst,
			want:     []CheckpointState{CheckpointVisited, CheckpointMissed},
		},
		{
			name:     "an aborted mission with reliable observations also reports the gap",
			status:   StatusAborted,
			evidence: passedFirst,
			want:     []CheckpointState{CheckpointVisited, CheckpointMissed},
		},
		{
			name:   "with no trustworthy observation nothing is called missed",
			status: StatusCompleted,
			evidence: []Evidence{
				{Position: atCheckpoint(testCheckpoints()[0]), ObservedAt: at(5 * time.Minute), Trusted: false},
			},
			want: []CheckpointState{CheckpointUnknown, CheckpointUnknown},
		},
		{
			name:     "with no observation at all nothing is called missed",
			status:   StatusAborted,
			evidence: nil,
			want:     []CheckpointState{CheckpointUnknown, CheckpointUnknown},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mission := active()
			mission.Status = testCase.status

			got := states(mission.JudgeCheckpoints(testCase.evidence))

			if !equalStates(got, testCase.want) {
				t.Errorf("states = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestSummarizeKeepsTheVerdictsApart(t *testing.T) {
	coverage := Summarize([]CheckpointJudgement{
		{State: CheckpointVisited},
		{State: CheckpointVisited},
		{State: CheckpointMissed},
		{State: CheckpointUnknown},
		{State: CheckpointPlanned},
	})

	want := Coverage{Visited: 2, Missed: 1, Unknown: 1, Planned: 1, Total: 5}
	if coverage != want {
		t.Errorf("coverage = %+v, want %+v", coverage, want)
	}
	// Undecided checkpoints are not coverage and not a miss, so the two
	// reported sides deliberately do not add up to the total.
	if coverage.Visited+coverage.Missed == coverage.Total {
		t.Error("undecided checkpoints must not be folded into either side")
	}
}
