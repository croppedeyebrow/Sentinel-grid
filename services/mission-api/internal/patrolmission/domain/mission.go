package domain

import (
	"errors"
	"fmt"
	"time"
)

// Status is the mission lifecycle state.
type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusReady     Status = "READY"
	StatusActive    Status = "ACTIVE"
	StatusCompleted Status = "COMPLETED"
	StatusAborted   Status = "ABORTED"
)

// Transition is an action an operator may take on a mission.
type Transition string

const (
	TransitionApprove  Transition = "approve"
	TransitionStart    Transition = "start"
	TransitionComplete Transition = "complete"
	TransitionAbort    Transition = "abort"
)

// AbortReason records why a mission was stopped early. Safe interruption is a
// normal outcome in city patrol, so the cause is part of the record rather
// than something the incident workflow has to infer later.
type AbortReason string

const (
	AbortOperator AbortReason = "OPERATOR"
	AbortBattery  AbortReason = "BATTERY"
	AbortLink     AbortReason = "LINK"
	AbortSafety   AbortReason = "SAFETY"
	AbortPolicy   AbortReason = "POLICY"
)

// Valid reports whether the reason is one the platform knows.
func (r AbortReason) Valid() bool {
	switch r {
	case AbortOperator, AbortBattery, AbortLink, AbortSafety, AbortPolicy:
		return true
	default:
		return false
	}
}

// Abort is the recorded outcome of stopping a mission early.
type Abort struct {
	Reason AbortReason
	At     time.Time
}

// Window is the operating period a mission and its assignments are bound to.
type Window struct {
	Start time.Time
	End   time.Time
}

// Valid reports whether the window can be used at all.
func (w Window) Valid() bool { return w.End.After(w.Start) }

// Contains reports whether a moment falls inside the window, endpoints
// included, so an observation taken exactly at the start still counts.
func (w Window) Contains(moment time.Time) bool {
	return !moment.Before(w.Start) && !moment.After(w.End)
}

// Overlaps reports whether two windows share any moment.
func (w Window) Overlaps(other Window) bool {
	return w.Start.Before(other.End) && other.Start.Before(w.End)
}

// Mission is the aggregate that owns the patrol plan and its lifecycle. The
// plan is frozen at approval: area, route, checkpoints and the policy version
// are fixed to the mission so a later change cannot alter a running mission's
// judgement.
type Mission struct {
	ID      string
	Name    string
	Version int
	Status  Status

	PatrolAreaID  string
	PatrolRouteID string
	// PolicyVersion is the Site Profile revision pinned at approval.
	PolicyVersion int

	Window      Window
	Checkpoints []Checkpoint
	Assignments []Assignment

	Abort      *Abort
	CreatedAt  time.Time
	ApprovedAt *time.Time
	StartedAt  *time.Time
	EndedAt    *time.Time
}

// Errors the aggregate returns. They exist as values because the HTTP adapter
// maps each one to its own status code and error code.
var (
	ErrTransitionNotAllowed = errors.New("transition not allowed from this state")
	ErrStaleVersion         = errors.New("mission was modified by someone else")
	ErrNoCheckpoints        = errors.New("mission has no checkpoint")
	ErrNoAssignment         = errors.New("mission has no assigned unit")
	ErrInvalidWindow        = errors.New("operating window ends before it starts")
	ErrCheckpointOutside    = errors.New("checkpoint lies outside the patrol area")
	ErrAbortReasonRequired  = errors.New("abort requires a reason")
	ErrNotDraft             = errors.New("mission can only be edited while it is a draft")
	ErrUnknownAbortReason   = errors.New("abort reason is not recognised")
)

// StaleVersionError reports which revision the caller held, so the adapter can
// tell the operator what to re-read rather than only that it failed.
type StaleVersionError struct {
	Expected int
	Current  int
}

func (e StaleVersionError) Error() string {
	return fmt.Sprintf("mission was modified by someone else: expected version %d, have %d", e.Expected, e.Current)
}

func (e StaleVersionError) Is(target error) bool { return target == ErrStaleVersion }

// TransitionNotAllowedError names the transition and the state that refused it.
type TransitionNotAllowedError struct {
	Transition Transition
	Status     Status
}

func (e TransitionNotAllowedError) Error() string {
	return fmt.Sprintf("transition not allowed from this state: %s on %s", e.Transition, e.Status)
}

func (e TransitionNotAllowedError) Is(target error) bool { return target == ErrTransitionNotAllowed }

// CheckpointOutsideError names the checkpoint that lies outside the area.
type CheckpointOutsideError struct {
	Name string
}

func (e CheckpointOutsideError) Error() string {
	return fmt.Sprintf("checkpoint lies outside the patrol area: %s", e.Name)
}

func (e CheckpointOutsideError) Is(target error) bool { return target == ErrCheckpointOutside }

// NotDraftError names the state that refused the edit.
type NotDraftError struct {
	Status Status
}

func (e NotDraftError) Error() string {
	return fmt.Sprintf("mission can only be edited while it is a draft: status %s", e.Status)
}

func (e NotDraftError) Is(target error) bool { return target == ErrNotDraft }

// IsTerminal reports whether the mission has finished and accepts no further
// transition.
func (m Mission) IsTerminal() bool {
	return m.Status == StatusCompleted || m.Status == StatusAborted
}

// hasRun reports whether the mission ever started, which is what makes
// checkpoint judgement meaningful.
func (m Mission) hasRun() bool {
	return m.Status == StatusActive || m.IsTerminal()
}

// AllowedTransitions lists what may be attempted from the current state. An
// approved mission that has not started can still be called off.
func (m Mission) AllowedTransitions() []Transition {
	switch m.Status {
	case StatusDraft:
		return []Transition{TransitionApprove, TransitionAbort}
	case StatusReady:
		return []Transition{TransitionStart, TransitionAbort}
	case StatusActive:
		return []Transition{TransitionComplete, TransitionAbort}
	default:
		return nil
	}
}

func (m Mission) allows(transition Transition) bool {
	for _, allowed := range m.AllowedTransitions() {
		if allowed == transition {
			return true
		}
	}
	return false
}

// guard checks the two things every transition shares: that the caller saw the
// current version, and that the transition is legal from this state.
func (m Mission) guard(transition Transition, expectedVersion int) error {
	if expectedVersion != m.Version {
		return StaleVersionError{Expected: expectedVersion, Current: m.Version}
	}
	if !m.allows(transition) {
		return TransitionNotAllowedError{Transition: transition, Status: m.Status}
	}
	return nil
}

// AddCheckpoint appends a checkpoint to a draft. Checkpoints are owned by the
// mission so that approval can freeze them.
func (m *Mission) AddCheckpoint(checkpoint Checkpoint) error {
	if m.Status != StatusDraft {
		return NotDraftError{Status: m.Status}
	}
	checkpoint.Sequence = len(m.Checkpoints) + 1
	m.Checkpoints = append(m.Checkpoints, checkpoint)
	return nil
}

// RemoveCheckpoint drops a checkpoint from a draft and renumbers the rest, so
// the sequence always reflects the planned order.
func (m *Mission) RemoveCheckpoint(id string) error {
	if m.Status != StatusDraft {
		return NotDraftError{Status: m.Status}
	}

	remaining := make([]Checkpoint, 0, len(m.Checkpoints))
	for _, checkpoint := range m.Checkpoints {
		if checkpoint.ID == id {
			continue
		}
		checkpoint.Sequence = len(remaining) + 1
		remaining = append(remaining, checkpoint)
	}
	m.Checkpoints = remaining
	return nil
}

// Assign adds a unit to a draft mission for the mission's window. Conflicts
// with other missions are checked by the application, which can see them.
func (m *Mission) Assign(unitID, callsign string) error {
	if m.Status != StatusDraft {
		return NotDraftError{Status: m.Status}
	}
	for _, assignment := range m.Assignments {
		if assignment.UnitID == unitID {
			return nil
		}
	}
	m.Assignments = append(m.Assignments, Assignment{
		UnitID:   unitID,
		Callsign: callsign,
		Window:   m.Window,
	})
	return nil
}

// Unassign removes a unit from a draft mission.
func (m *Mission) Unassign(unitID string) error {
	if m.Status != StatusDraft {
		return NotDraftError{Status: m.Status}
	}

	remaining := make([]Assignment, 0, len(m.Assignments))
	for _, assignment := range m.Assignments {
		if assignment.UnitID != unitID {
			remaining = append(remaining, assignment)
		}
	}
	m.Assignments = remaining
	return nil
}

// Approve freezes the plan. A mission without checkpoints, without an assigned
// unit, with an unusable window, or with a checkpoint outside its area cannot
// be approved, which is what keeps an unapprovable plan from ever starting.
func (m *Mission) Approve(area Area, policyVersion, expectedVersion int, now time.Time) error {
	if err := m.guard(TransitionApprove, expectedVersion); err != nil {
		return err
	}
	if len(m.Checkpoints) == 0 {
		return ErrNoCheckpoints
	}
	if len(m.Assignments) == 0 {
		return ErrNoAssignment
	}
	if !m.Window.Valid() {
		return ErrInvalidWindow
	}
	for _, checkpoint := range m.Checkpoints {
		if !area.Contains(checkpoint.Position) {
			return CheckpointOutsideError{Name: checkpoint.Name}
		}
	}

	m.Status = StatusReady
	m.PolicyVersion = policyVersion
	m.ApprovedAt = &now
	m.Version++
	return nil
}

// Start moves an approved mission into execution.
func (m *Mission) Start(expectedVersion int, now time.Time) error {
	if err := m.guard(TransitionStart, expectedVersion); err != nil {
		return err
	}

	m.Status = StatusActive
	m.StartedAt = &now
	m.Version++
	return nil
}

// Complete closes a mission that ran to its end.
func (m *Mission) Complete(expectedVersion int, now time.Time) error {
	if err := m.guard(TransitionComplete, expectedVersion); err != nil {
		return err
	}

	m.Status = StatusCompleted
	m.EndedAt = &now
	m.Version++
	return nil
}

// AbortWith stops a mission early and records why.
func (m *Mission) AbortWith(reason AbortReason, expectedVersion int, now time.Time) error {
	if err := m.guard(TransitionAbort, expectedVersion); err != nil {
		return err
	}
	if reason == "" {
		return ErrAbortReasonRequired
	}
	if !reason.Valid() {
		return fmt.Errorf("%w: %s", ErrUnknownAbortReason, reason)
	}

	m.Status = StatusAborted
	m.Abort = &Abort{Reason: reason, At: now}
	m.EndedAt = &now
	m.Version++
	return nil
}
