// Package application drives the Patrol Mission use cases: it loads a mission,
// lets the aggregate decide, persists the outcome, and judges coverage from the
// observations the projection holds.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

// Site is the operating context missions are planned in.
type Site struct {
	ID string
	// ReceiptAllowance is how late an observation may arrive and still prove a
	// position. It comes from the Site Profile's position freshness window.
	ReceiptAllowance time.Duration
	// PolicyVersion is the Site Profile's current revision, which approval
	// freezes onto the mission.
	PolicyVersion int
}

// NewCheckpoint is a checkpoint as the planner places it; the sequence is the
// server's to assign.
type NewCheckpoint struct {
	Name         string
	Position     domain.Position
	RadiusMeters float64
}

// CreateMission opens a draft.
type CreateMission struct {
	Name          string
	PatrolAreaID  string
	PatrolRouteID string
	Window        domain.Window
	Checkpoints   []NewCheckpoint
	UnitIDs       []string
}

// MissionView is a mission with its coverage judged, which is what the API
// serves.
type MissionView struct {
	Mission    domain.Mission
	AreaName   string
	RouteName  string
	Judgements []domain.CheckpointJudgement
	Coverage   domain.Coverage
}

// Repository is the port to stored state. It lives here, on the consumer side,
// so the adapter depends on the application rather than the other way round.
type Repository interface {
	Site(ctx context.Context) (Site, error)
	Area(ctx context.Context, areaID string) (domain.Area, string, error)
	RouteName(ctx context.Context, routeID string) (string, error)
	Callsigns(ctx context.Context, unitIDs []string) (map[string]string, error)

	Missions(ctx context.Context) ([]domain.Mission, error)
	Mission(ctx context.Context, missionID string) (domain.Mission, error)
	MissionByIdempotencyKey(ctx context.Context, key string) (domain.Mission, error)
	Commitments(ctx context.Context, window domain.Window) ([]domain.Commitment, error)
	Observations(ctx context.Context, mission domain.Mission) ([]domain.Observation, error)

	Create(ctx context.Context, siteID string, mission domain.Mission, idempotencyKey string) error
	Save(ctx context.Context, mission domain.Mission, expectedVersion int) error
}

// IDFactory produces identifiers for new missions and checkpoints.
type IDFactory func() (string, error)

// Errors the application returns on top of the aggregate's own.
var (
	ErrNotProvisioned         = errors.New("no site profile is configured")
	ErrMissionNotFound        = errors.New("mission not found")
	ErrAreaNotFound           = errors.New("patrol area not found")
	ErrUnitNotFound           = errors.New("patrol unit not found")
	ErrCheckpointNotFound     = errors.New("checkpoint not found")
	ErrAssignmentConflict     = errors.New("unit is already committed to an overlapping window")
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
)

// AssignmentConflictError names the units that are already committed, because
// the planner needs to know which ones to free.
type AssignmentConflictError struct {
	Callsigns []string
}

func (e AssignmentConflictError) Error() string {
	return fmt.Sprintf("unit is already committed to an overlapping window: %s", strings.Join(e.Callsigns, ", "))
}

func (e AssignmentConflictError) Is(target error) bool { return target == ErrAssignmentConflict }

// Service implements the Patrol Mission use cases.
type Service struct {
	repository Repository
	now        func() time.Time
	newID      IDFactory
}

// Option customises a Service.
type Option func(*Service)

// WithClock replaces the clock, which keeps tests independent of wall time.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithIDFactory replaces the identifier source so tests can predict ids.
func WithIDFactory(factory IDFactory) Option {
	return func(s *Service) { s.newID = factory }
}

// NewService wires a Service to its repository.
func NewService(repository Repository, newID IDFactory, options ...Option) *Service {
	service := &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
		newID:      newID,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// List returns every mission of the site with its coverage judged.
func (s *Service) List(ctx context.Context) ([]MissionView, error) {
	missions, err := s.repository.Missions(ctx)
	if err != nil {
		return nil, err
	}

	site, err := s.repository.Site(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]MissionView, 0, len(missions))
	for _, mission := range missions {
		view, err := s.judge(ctx, mission, site)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// Get returns one mission with its coverage judged.
func (s *Service) Get(ctx context.Context, missionID string) (MissionView, error) {
	mission, err := s.repository.Mission(ctx, missionID)
	if err != nil {
		return MissionView{}, err
	}
	return s.viewOf(ctx, mission)
}

// Create opens a draft mission. The idempotency key makes a retry return the
// mission planned the first time instead of planning a second one.
func (s *Service) Create(ctx context.Context, command CreateMission, idempotencyKey string) (MissionView, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return MissionView{}, ErrIdempotencyKeyRequired
	}

	if existing, err := s.repository.MissionByIdempotencyKey(ctx, idempotencyKey); err == nil {
		return s.viewOf(ctx, existing)
	} else if !errors.Is(err, ErrMissionNotFound) {
		return MissionView{}, err
	}

	site, err := s.repository.Site(ctx)
	if err != nil {
		return MissionView{}, err
	}
	if _, _, err := s.repository.Area(ctx, command.PatrolAreaID); err != nil {
		return MissionView{}, err
	}

	callsigns, err := s.repository.Callsigns(ctx, command.UnitIDs)
	if err != nil {
		return MissionView{}, err
	}

	missionID, err := s.newID()
	if err != nil {
		return MissionView{}, err
	}

	mission := domain.Mission{
		ID:            missionID,
		Name:          strings.TrimSpace(command.Name),
		Status:        domain.StatusDraft,
		PatrolAreaID:  command.PatrolAreaID,
		PatrolRouteID: command.PatrolRouteID,
		Window:        command.Window,
		CreatedAt:     s.now(),
	}
	if !mission.Window.Valid() {
		return MissionView{}, domain.ErrInvalidWindow
	}

	for _, checkpoint := range command.Checkpoints {
		checkpointID, err := s.newID()
		if err != nil {
			return MissionView{}, err
		}
		if err := mission.AddCheckpoint(domain.Checkpoint{
			ID:           checkpointID,
			Name:         strings.TrimSpace(checkpoint.Name),
			Position:     checkpoint.Position,
			RadiusMeters: checkpoint.RadiusMeters,
		}); err != nil {
			return MissionView{}, err
		}
	}
	for _, unitID := range command.UnitIDs {
		if err := mission.Assign(unitID, callsigns[unitID]); err != nil {
			return MissionView{}, err
		}
	}

	if err := s.repository.Create(ctx, site.ID, mission, idempotencyKey); err != nil {
		return MissionView{}, err
	}
	return s.judge(ctx, mission, site)
}

// AddCheckpoint appends a checkpoint to a draft.
func (s *Service) AddCheckpoint(ctx context.Context, missionID string, checkpoint NewCheckpoint) (MissionView, error) {
	return s.edit(ctx, missionID, func(mission *domain.Mission) error {
		checkpointID, err := s.newID()
		if err != nil {
			return err
		}
		return mission.AddCheckpoint(domain.Checkpoint{
			ID:           checkpointID,
			Name:         strings.TrimSpace(checkpoint.Name),
			Position:     checkpoint.Position,
			RadiusMeters: checkpoint.RadiusMeters,
		})
	})
}

// RemoveCheckpoint drops a checkpoint from a draft.
func (s *Service) RemoveCheckpoint(ctx context.Context, missionID, checkpointID string) (MissionView, error) {
	return s.edit(ctx, missionID, func(mission *domain.Mission) error {
		found := false
		for _, checkpoint := range mission.Checkpoints {
			if checkpoint.ID == checkpointID {
				found = true
			}
		}
		if !found {
			return ErrCheckpointNotFound
		}
		return mission.RemoveCheckpoint(checkpointID)
	})
}

// Assign adds a unit to a draft.
func (s *Service) Assign(ctx context.Context, missionID, unitID string) (MissionView, error) {
	return s.edit(ctx, missionID, func(mission *domain.Mission) error {
		callsigns, err := s.repository.Callsigns(ctx, []string{unitID})
		if err != nil {
			return err
		}
		return mission.Assign(unitID, callsigns[unitID])
	})
}

// Unassign removes a unit from a draft.
func (s *Service) Unassign(ctx context.Context, missionID, unitID string) (MissionView, error) {
	return s.edit(ctx, missionID, func(mission *domain.Mission) error {
		return mission.Unassign(unitID)
	})
}

// Transition runs one lifecycle transition. The caller's version is passed
// through to the aggregate, which refuses a stale one.
func (s *Service) Transition(
	ctx context.Context,
	missionID string,
	transition domain.Transition,
	expectedVersion int,
	reason domain.AbortReason,
) (MissionView, error) {
	mission, err := s.repository.Mission(ctx, missionID)
	if err != nil {
		return MissionView{}, err
	}

	now := s.now()
	switch transition {
	case domain.TransitionApprove:
		area, _, err := s.repository.Area(ctx, mission.PatrolAreaID)
		if err != nil {
			return MissionView{}, err
		}
		// Approval pins the policy the judgements will be made under, so the
		// current Site Profile revision is read here and frozen onto the
		// mission.
		site, err := s.repository.Site(ctx)
		if err != nil {
			return MissionView{}, err
		}
		if err := s.refuseConflicts(ctx, mission); err != nil {
			return MissionView{}, err
		}
		if err := mission.Approve(area, site.PolicyVersion, expectedVersion, now); err != nil {
			return MissionView{}, err
		}
	case domain.TransitionStart:
		if err := s.refuseConflicts(ctx, mission); err != nil {
			return MissionView{}, err
		}
		if err := mission.Start(expectedVersion, now); err != nil {
			return MissionView{}, err
		}
	case domain.TransitionComplete:
		if err := mission.Complete(expectedVersion, now); err != nil {
			return MissionView{}, err
		}
	case domain.TransitionAbort:
		if err := mission.AbortWith(reason, expectedVersion, now); err != nil {
			return MissionView{}, err
		}
	default:
		return MissionView{}, domain.TransitionNotAllowedError{Transition: transition, Status: mission.Status}
	}

	if err := s.repository.Save(ctx, mission, expectedVersion); err != nil {
		return MissionView{}, err
	}
	return s.viewOf(ctx, mission)
}

// refuseConflicts rejects a mission whose units are committed elsewhere in an
// overlapping window. The commitments span missions, so no single aggregate
// can see them.
func (s *Service) refuseConflicts(ctx context.Context, mission domain.Mission) error {
	commitments, err := s.repository.Commitments(ctx, mission.Window)
	if err != nil {
		return err
	}
	if conflicts := domain.ConflictingCallsigns(mission, commitments); len(conflicts) > 0 {
		return AssignmentConflictError{Callsigns: conflicts}
	}
	return nil
}

// edit applies a draft-only change and persists it.
func (s *Service) edit(
	ctx context.Context,
	missionID string,
	change func(*domain.Mission) error,
) (MissionView, error) {
	mission, err := s.repository.Mission(ctx, missionID)
	if err != nil {
		return MissionView{}, err
	}

	expectedVersion := mission.Version
	if err := change(&mission); err != nil {
		return MissionView{}, err
	}
	if err := s.repository.Save(ctx, mission, expectedVersion); err != nil {
		return MissionView{}, err
	}
	return s.viewOf(ctx, mission)
}

func (s *Service) viewOf(ctx context.Context, mission domain.Mission) (MissionView, error) {
	site, err := s.repository.Site(ctx)
	if err != nil {
		return MissionView{}, err
	}
	return s.judge(ctx, mission, site)
}

// judge builds the read model: coverage comes from the observations the
// projection holds, and only observations that qualify may credit a pass.
func (s *Service) judge(ctx context.Context, mission domain.Mission, site Site) (MissionView, error) {
	_, areaName, err := s.repository.Area(ctx, mission.PatrolAreaID)
	if err != nil {
		return MissionView{}, err
	}

	routeName := ""
	if mission.PatrolRouteID != "" {
		routeName, err = s.repository.RouteName(ctx, mission.PatrolRouteID)
		if err != nil {
			return MissionView{}, err
		}
	}

	observations, err := s.repository.Observations(ctx, mission)
	if err != nil {
		return MissionView{}, err
	}
	judgements := mission.JudgeCheckpoints(
		domain.TrustObservations(observations, site.ReceiptAllowance),
	)

	return MissionView{
		Mission:    mission,
		AreaName:   areaName,
		RouteName:  routeName,
		Judgements: judgements,
		Coverage:   domain.Summarize(judgements),
	}, nil
}
