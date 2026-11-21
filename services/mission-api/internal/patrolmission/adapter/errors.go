package adapter

import (
	"errors"
	"net/http"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

// errorDetails carries the facts behind a refusal, as published in
// contracts/schemas/api-error.schema.json. The operator has to be able to act
// on a refusal, which means knowing which unit, checkpoint or version caused
// it.
type errorDetails struct {
	Callsigns       []string `json:"callsigns,omitempty"`
	CheckpointName  string   `json:"checkpointName,omitempty"`
	ExpectedVersion *int     `json:"expectedVersion,omitempty"`
	CurrentVersion  *int     `json:"currentVersion,omitempty"`
	Status          string   `json:"status,omitempty"`
	Transition      string   `json:"transition,omitempty"`
}

type errorResponse struct {
	Code    string        `json:"code"`
	Error   string        `json:"error"`
	Details *errorDetails `json:"details,omitempty"`
}

// refusal maps an error to the published contract: an HTTP status, a code the
// client keys off, and the facts behind it.
func refusal(err error) (int, errorResponse) {
	response := errorResponse{Error: err.Error()}

	var conflict application.AssignmentConflictError
	if errors.As(err, &conflict) {
		response.Code = "ASSIGNMENT_CONFLICT"
		response.Details = &errorDetails{Callsigns: conflict.Callsigns}
		return http.StatusConflict, response
	}

	var stale domain.StaleVersionError
	if errors.As(err, &stale) {
		response.Code = "STALE_VERSION"
		expected, current := stale.Expected, stale.Current
		response.Details = &errorDetails{ExpectedVersion: &expected, CurrentVersion: &current}
		return http.StatusConflict, response
	}

	var notAllowed domain.TransitionNotAllowedError
	if errors.As(err, &notAllowed) {
		response.Code = "TRANSITION_NOT_ALLOWED"
		response.Details = &errorDetails{
			Status:     string(notAllowed.Status),
			Transition: string(notAllowed.Transition),
		}
		return http.StatusConflict, response
	}

	var outside domain.CheckpointOutsideError
	if errors.As(err, &outside) {
		response.Code = "CHECKPOINT_OUTSIDE_AREA"
		response.Details = &errorDetails{CheckpointName: outside.Name}
		return http.StatusUnprocessableEntity, response
	}

	var notDraft domain.NotDraftError
	if errors.As(err, &notDraft) {
		response.Code = "NOT_DRAFT"
		response.Details = &errorDetails{Status: string(notDraft.Status)}
		return http.StatusConflict, response
	}

	for _, mapping := range []struct {
		sentinel error
		status   int
		code     string
	}{
		{application.ErrMissionNotFound, http.StatusNotFound, "MISSION_NOT_FOUND"},
		{application.ErrCheckpointNotFound, http.StatusNotFound, "CHECKPOINT_NOT_FOUND"},
		{application.ErrAreaNotFound, http.StatusNotFound, "PATROL_AREA_NOT_FOUND"},
		{application.ErrUnitNotFound, http.StatusNotFound, "UNIT_NOT_FOUND"},
		{application.ErrIdempotencyKeyRequired, http.StatusBadRequest, "INVALID_REQUEST"},
		{application.ErrNotProvisioned, http.StatusServiceUnavailable, "NOT_PROVISIONED"},
		{domain.ErrNoCheckpoints, http.StatusUnprocessableEntity, "NO_CHECKPOINTS"},
		{domain.ErrNoAssignment, http.StatusUnprocessableEntity, "NO_ASSIGNMENT"},
		{domain.ErrInvalidWindow, http.StatusUnprocessableEntity, "INVALID_WINDOW"},
		{domain.ErrAbortReasonRequired, http.StatusUnprocessableEntity, "ABORT_REASON_REQUIRED"},
		{domain.ErrUnknownAbortReason, http.StatusUnprocessableEntity, "UNKNOWN_ABORT_REASON"},
	} {
		if errors.Is(err, mapping.sentinel) {
			response.Code = mapping.code
			return mapping.status, response
		}
	}

	// Anything left is a failure of the store or of this service, not of the
	// request: the operator is told the picture is unavailable, and the detail
	// goes to the log.
	return http.StatusServiceUnavailable, errorResponse{
		Code:  "PROJECTION_UNAVAILABLE",
		Error: "mission store unavailable",
	}
}

// invalidRequest is returned for a body that does not match the published
// request schema.
func invalidRequest(message string) (int, errorResponse) {
	return http.StatusBadRequest, errorResponse{Code: "INVALID_REQUEST", Error: message}
}
