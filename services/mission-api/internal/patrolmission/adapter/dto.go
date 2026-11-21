package adapter

import (
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

type missionListDTO struct {
	GeneratedAt string       `json:"generatedAt"`
	Missions    []missionDTO `json:"missions"`
}

type namedReferenceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type windowDTO struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type positionDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type checkpointDTO struct {
	ID           string      `json:"id"`
	Sequence     int         `json:"sequence"`
	Name         string      `json:"name"`
	Position     positionDTO `json:"position"`
	RadiusMeters float64     `json:"radiusMeters"`
	State        string      `json:"state"`
	// CreditedObservedAt is the observation that credited a pass, so the
	// operator can see which evidence decided it.
	CreditedObservedAt *string `json:"creditedObservedAt"`
}

type assignmentDTO struct {
	UnitID   string    `json:"unitId"`
	Callsign string    `json:"callsign"`
	Window   windowDTO `json:"window"`
}

type coverageDTO struct {
	Visited int `json:"visited"`
	Missed  int `json:"missed"`
	Unknown int `json:"unknown"`
	Planned int `json:"planned"`
	Total   int `json:"total"`
}

type abortDTO struct {
	Reason string `json:"reason"`
	At     string `json:"at"`
}

type missionDTO struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Version            int                `json:"version"`
	Status             string             `json:"status"`
	PatrolArea         namedReferenceDTO  `json:"patrolArea"`
	PatrolRoute        *namedReferenceDTO `json:"patrolRoute"`
	Window             windowDTO          `json:"window"`
	PolicyVersion      int                `json:"policyVersion"`
	Checkpoints        []checkpointDTO    `json:"checkpoints"`
	Assignments        []assignmentDTO    `json:"assignments"`
	Coverage           coverageDTO        `json:"coverage"`
	Abort              *abortDTO          `json:"abort"`
	AllowedTransitions []string           `json:"allowedTransitions"`
	CreatedAt          string             `json:"createdAt"`
	ApprovedAt         *string            `json:"approvedAt"`
	StartedAt          *string            `json:"startedAt"`
	EndedAt            *string            `json:"endedAt"`
}

func newMissionDTO(view application.MissionView) missionDTO {
	mission := view.Mission

	// The stored plan carries no verdict; the judgements decide each
	// checkpoint's state, so they are matched back by id.
	judgements := make(map[string]domain.CheckpointJudgement, len(view.Judgements))
	for _, judgement := range view.Judgements {
		judgements[judgement.CheckpointID] = judgement
	}

	checkpoints := make([]checkpointDTO, 0, len(mission.Checkpoints))
	for _, checkpoint := range mission.Checkpoints {
		judgement := judgements[checkpoint.ID]
		state := judgement.State
		if state == "" {
			state = domain.CheckpointPlanned
		}
		checkpoints = append(checkpoints, checkpointDTO{
			ID:                 checkpoint.ID,
			Sequence:           checkpoint.Sequence,
			Name:               checkpoint.Name,
			Position:           positionDTO{Latitude: checkpoint.Position.Latitude, Longitude: checkpoint.Position.Longitude},
			RadiusMeters:       checkpoint.RadiusMeters,
			State:              string(state),
			CreditedObservedAt: iso(judgement.CreditedAt),
		})
	}

	assignments := make([]assignmentDTO, 0, len(mission.Assignments))
	for _, assignment := range mission.Assignments {
		assignments = append(assignments, assignmentDTO{
			UnitID:   assignment.UnitID,
			Callsign: assignment.Callsign,
			Window:   window(assignment.Window),
		})
	}

	transitions := make([]string, 0, 2)
	for _, transition := range mission.AllowedTransitions() {
		transitions = append(transitions, string(transition))
	}

	dto := missionDTO{
		ID:            mission.ID,
		Name:          mission.Name,
		Version:       mission.Version,
		Status:        string(mission.Status),
		PatrolArea:    namedReferenceDTO{ID: mission.PatrolAreaID, Name: view.AreaName},
		Window:        window(mission.Window),
		PolicyVersion: mission.PolicyVersion,
		Checkpoints:   checkpoints,
		Assignments:   assignments,
		Coverage: coverageDTO{
			Visited: view.Coverage.Visited,
			Missed:  view.Coverage.Missed,
			Unknown: view.Coverage.Unknown,
			Planned: view.Coverage.Planned,
			Total:   view.Coverage.Total,
		},
		AllowedTransitions: transitions,
		CreatedAt:          mission.CreatedAt.UTC().Format(time.RFC3339),
		ApprovedAt:         iso(mission.ApprovedAt),
		StartedAt:          iso(mission.StartedAt),
		EndedAt:            iso(mission.EndedAt),
	}

	if mission.PatrolRouteID != "" {
		dto.PatrolRoute = &namedReferenceDTO{ID: mission.PatrolRouteID, Name: view.RouteName}
	}
	if mission.Abort != nil {
		dto.Abort = &abortDTO{
			Reason: string(mission.Abort.Reason),
			At:     mission.Abort.At.UTC().Format(time.RFC3339),
		}
	}
	return dto
}

func window(value domain.Window) windowDTO {
	return windowDTO{
		Start: value.Start.UTC().Format(time.RFC3339),
		End:   value.End.UTC().Format(time.RFC3339),
	}
}

func iso(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

// Request bodies.

type positionRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type windowRequest struct {
	Start *time.Time `json:"start"`
	End   *time.Time `json:"end"`
}

type checkpointRequest struct {
	Name         string          `json:"name"`
	Position     positionRequest `json:"position"`
	RadiusMeters *float64        `json:"radiusMeters"`
}

type assignmentRequest struct {
	UnitID string `json:"unitId"`
}

type createMissionRequest struct {
	Name          string              `json:"name"`
	PatrolAreaID  string              `json:"patrolAreaId"`
	PatrolRouteID *string             `json:"patrolRouteId"`
	Window        windowRequest       `json:"window"`
	Checkpoints   []checkpointRequest `json:"checkpoints"`
	Assignments   []assignmentRequest `json:"assignments"`
}

type transitionRequest struct {
	Version *int   `json:"version"`
	Reason  string `json:"reason"`
}
