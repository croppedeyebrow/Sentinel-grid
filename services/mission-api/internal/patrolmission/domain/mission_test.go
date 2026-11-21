package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The cases below are the refusals the mock console produced in the UI draft.
// They are the specification for this aggregate: the screens already tell the
// operator these things, so the domain has to enforce them.

var (
	windowStart = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC)
	approvedAt  = time.Date(2026, 11, 21, 8, 50, 0, 0, time.UTC)
)

func testWindow() Window { return Window{Start: windowStart, End: windowEnd} }

// testArea is the seeded city patrol area.
func testArea() Area {
	return Area{
		ID: "area-1",
		Boundary: []Position{
			{Latitude: 37.558, Longitude: 126.965},
			{Latitude: 37.558, Longitude: 126.991},
			{Latitude: 37.575, Longitude: 126.991},
			{Latitude: 37.575, Longitude: 126.965},
			{Latitude: 37.558, Longitude: 126.965},
		},
	}
}

func testCheckpoints() []Checkpoint {
	return []Checkpoint{
		{ID: "cp-1", Sequence: 1, Name: "CP-1", Position: Position{Latitude: 37.561, Longitude: 126.97}, RadiusMeters: 40},
		{ID: "cp-2", Sequence: 2, Name: "CP-2", Position: Position{Latitude: 37.566, Longitude: 126.985}, RadiusMeters: 40},
	}
}

func draft() Mission {
	return Mission{
		ID:            "mission-1",
		Name:          "하천변 정기 순찰",
		Version:       0,
		Status:        StatusDraft,
		PatrolAreaID:  "area-1",
		PatrolRouteID: "route-1",
		Window:        testWindow(),
		Checkpoints:   testCheckpoints(),
		Assignments: []Assignment{
			{UnitID: "unit-1", Callsign: "SG-01", Window: testWindow()},
		},
		CreatedAt: approvedAt.Add(-20 * time.Minute),
	}
}

func approved() Mission {
	mission := draft()
	mission.Status = StatusReady
	mission.Version = 1
	mission.ApprovedAt = &approvedAt
	return mission
}

func active() Mission {
	mission := approved()
	mission.Status = StatusActive
	mission.Version = 2
	mission.StartedAt = &windowStart
	return mission
}

func TestAllowedTransitions(t *testing.T) {
	cases := map[Status][]Transition{
		StatusDraft:     {TransitionApprove, TransitionAbort},
		StatusReady:     {TransitionStart, TransitionAbort},
		StatusActive:    {TransitionComplete, TransitionAbort},
		StatusCompleted: nil,
		StatusAborted:   nil,
	}

	for status, want := range cases {
		t.Run(string(status), func(t *testing.T) {
			mission := draft()
			mission.Status = status

			got := mission.AllowedTransitions()
			if len(got) != len(want) {
				t.Fatalf("transitions = %v, want %v", got, want)
			}
			for index, transition := range want {
				if got[index] != transition {
					t.Errorf("transitions = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestApproveRefusesAnIncompletePlan(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Mission)
		wantErr error
	}{
		{
			name:    "no checkpoint",
			mutate:  func(m *Mission) { m.Checkpoints = nil },
			wantErr: ErrNoCheckpoints,
		},
		{
			name:    "no assigned unit",
			mutate:  func(m *Mission) { m.Assignments = nil },
			wantErr: ErrNoAssignment,
		},
		{
			name:    "window ends before it starts",
			mutate:  func(m *Mission) { m.Window = Window{Start: windowEnd, End: windowStart} },
			wantErr: ErrInvalidWindow,
		},
		{
			name: "checkpoint outside the patrol area",
			mutate: func(m *Mission) {
				m.Checkpoints[1].Position = Position{Latitude: 37.6, Longitude: 127.05}
			},
			wantErr: ErrCheckpointOutside,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mission := draft()
			testCase.mutate(&mission)

			err := mission.Approve(testArea(), mission.Version, approvedAt)

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
			if mission.Status != StatusDraft {
				t.Errorf("status = %s, want it to stay DRAFT", mission.Status)
			}
			if mission.Version != 0 {
				t.Errorf("version = %d, want it unchanged", mission.Version)
			}
		})
	}
}

func TestApproveNamesTheOffendingCheckpoint(t *testing.T) {
	mission := draft()
	mission.Checkpoints[1].Name = "CP-구역밖"
	mission.Checkpoints[1].Position = Position{Latitude: 37.6, Longitude: 127.05}

	err := mission.Approve(testArea(), mission.Version, approvedAt)

	if err == nil || !errors.Is(err, ErrCheckpointOutside) {
		t.Fatalf("err = %v, want ErrCheckpointOutside", err)
	}
	if got := err.Error(); !strings.Contains(got, "CP-구역밖") {
		t.Errorf("err = %q, want it to name the checkpoint", got)
	}
}

func TestApproveFreezesThePlan(t *testing.T) {
	mission := draft()

	if err := mission.Approve(testArea(), mission.Version, approvedAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mission.Status != StatusReady {
		t.Errorf("status = %s, want READY", mission.Status)
	}
	if mission.Version != 1 {
		t.Errorf("version = %d, want 1", mission.Version)
	}
	if mission.ApprovedAt == nil || !mission.ApprovedAt.Equal(approvedAt) {
		t.Error("approval moment must be recorded")
	}
}

func TestTransitionsRequireTheVersionTheCallerSaw(t *testing.T) {
	mission := active()

	err := mission.Complete(mission.Version-1, windowEnd)

	if !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("err = %v, want ErrStaleVersion", err)
	}
	if mission.Status != StatusActive {
		t.Errorf("status = %s, want it unchanged", mission.Status)
	}
}

func TestStartRequiresApproval(t *testing.T) {
	mission := draft()

	err := mission.Start(mission.Version, windowStart)

	if !errors.Is(err, ErrTransitionNotAllowed) {
		t.Fatalf("err = %v, want ErrTransitionNotAllowed", err)
	}
	if mission.Status != StatusDraft {
		t.Errorf("status = %s, want DRAFT", mission.Status)
	}
}

func TestCompleteClosesAnActiveMission(t *testing.T) {
	mission := active()

	if err := mission.Complete(mission.Version, windowEnd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mission.Status != StatusCompleted {
		t.Errorf("status = %s, want COMPLETED", mission.Status)
	}
	if mission.EndedAt == nil {
		t.Error("end moment must be recorded")
	}
	if mission.Version != 3 {
		t.Errorf("version = %d, want 3", mission.Version)
	}
}

func TestAbortRequiresARecognisedReason(t *testing.T) {
	cases := []struct {
		name    string
		reason  AbortReason
		wantErr error
	}{
		{"empty reason", "", ErrAbortReasonRequired},
		{"unrecognised reason", AbortReason("BECAUSE"), ErrUnknownAbortReason},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mission := active()

			err := mission.AbortWith(testCase.reason, mission.Version, windowEnd)

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
			if mission.Status != StatusActive {
				t.Errorf("status = %s, want it unchanged", mission.Status)
			}
			if mission.Abort != nil {
				t.Error("no abort may be recorded for a refused abort")
			}
		})
	}
}

func TestAbortRecordsTheCause(t *testing.T) {
	mission := active()
	at := windowStart.Add(40 * time.Minute)

	if err := mission.AbortWith(AbortBattery, mission.Version, at); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mission.Status != StatusAborted {
		t.Errorf("status = %s, want ABORTED", mission.Status)
	}
	if mission.Abort == nil || mission.Abort.Reason != AbortBattery || !mission.Abort.At.Equal(at) {
		t.Errorf("abort = %+v, want BATTERY at %s", mission.Abort, at)
	}
}

func TestAnApprovedMissionCanStillBeCalledOff(t *testing.T) {
	mission := approved()

	if err := mission.AbortWith(AbortPolicy, mission.Version, approvedAt); err != nil {
		t.Fatalf("an approved mission must be abortable: %v", err)
	}
	if mission.Status != StatusAborted {
		t.Errorf("status = %s, want ABORTED", mission.Status)
	}
}

func TestTerminalMissionsAcceptNothing(t *testing.T) {
	for _, status := range []Status{StatusCompleted, StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			mission := active()
			mission.Status = status
			version := mission.Version

			errs := []error{
				mission.Approve(testArea(), version, windowEnd),
				mission.Start(version, windowEnd),
				mission.Complete(version, windowEnd),
				mission.AbortWith(AbortOperator, version, windowEnd),
			}

			for _, err := range errs {
				if !errors.Is(err, ErrTransitionNotAllowed) {
					t.Errorf("err = %v, want ErrTransitionNotAllowed", err)
				}
			}
			if mission.Version != version {
				t.Errorf("version = %d, want it unchanged", mission.Version)
			}
		})
	}
}

func TestDraftEditing(t *testing.T) {
	t.Run("checkpoints are numbered in placement order", func(t *testing.T) {
		mission := draft()
		mission.Checkpoints = nil

		for _, id := range []string{"a", "b", "c"} {
			if err := mission.AddCheckpoint(Checkpoint{ID: id, Name: id, RadiusMeters: 40}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}

		for index, checkpoint := range mission.Checkpoints {
			if checkpoint.Sequence != index+1 {
				t.Errorf("checkpoint %s sequence = %d, want %d", checkpoint.ID, checkpoint.Sequence, index+1)
			}
		}
	})

	t.Run("removing a checkpoint renumbers the rest", func(t *testing.T) {
		mission := draft()

		if err := mission.RemoveCheckpoint("cp-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mission.Checkpoints) != 1 {
			t.Fatalf("checkpoints = %d, want 1", len(mission.Checkpoints))
		}
		if mission.Checkpoints[0].Sequence != 1 {
			t.Errorf("sequence = %d, want 1", mission.Checkpoints[0].Sequence)
		}
	})

	t.Run("assigning the same unit twice changes nothing", func(t *testing.T) {
		mission := draft()

		if err := mission.Assign("unit-1", "SG-01"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mission.Assignments) != 1 {
			t.Errorf("assignments = %d, want 1", len(mission.Assignments))
		}
	})

	t.Run("a unit can be removed from a draft", func(t *testing.T) {
		mission := draft()

		if err := mission.Unassign("unit-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mission.Assignments) != 0 {
			t.Errorf("assignments = %d, want 0", len(mission.Assignments))
		}
	})

	t.Run("an approved plan can no longer be edited", func(t *testing.T) {
		mission := approved()

		errs := []error{
			mission.AddCheckpoint(Checkpoint{ID: "x"}),
			mission.RemoveCheckpoint("cp-1"),
			mission.Assign("unit-2", "SG-02"),
			mission.Unassign("unit-1"),
		}

		for _, err := range errs {
			if !errors.Is(err, ErrNotDraft) {
				t.Errorf("err = %v, want ErrNotDraft", err)
			}
		}
	})
}
