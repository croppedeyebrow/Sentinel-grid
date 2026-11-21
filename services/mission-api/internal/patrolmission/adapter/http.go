package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

// MissionService is the use case surface this adapter serves.
type MissionService interface {
	List(ctx context.Context) ([]application.MissionView, error)
	Get(ctx context.Context, missionID string) (application.MissionView, error)
	Create(ctx context.Context, command application.CreateMission, idempotencyKey string) (application.MissionView, error)
	AddCheckpoint(ctx context.Context, missionID string, checkpoint application.NewCheckpoint) (application.MissionView, error)
	RemoveCheckpoint(ctx context.Context, missionID, checkpointID string) (application.MissionView, error)
	Assign(ctx context.Context, missionID, unitID string) (application.MissionView, error)
	Unassign(ctx context.Context, missionID, unitID string) (application.MissionView, error)
	Transition(ctx context.Context, missionID string, transition domain.Transition, expectedVersion int, reason domain.AbortReason) (application.MissionView, error)
}

// Handler serves the Patrol Mission endpoints.
type Handler struct {
	service MissionService
	now     func() time.Time
	logger  *slog.Logger
}

// NewHandler builds the mission handler.
func NewHandler(service MissionService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		now:     func() time.Time { return time.Now().UTC() },
		logger:  logger,
	}
}

// Register mounts the endpoints published in contracts/README.md.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/missions", h.list)
	mux.HandleFunc("POST /api/v1/missions", h.create)
	mux.HandleFunc("GET /api/v1/missions/{id}", h.get)
	mux.HandleFunc("POST /api/v1/missions/{id}/checkpoints", h.addCheckpoint)
	mux.HandleFunc("DELETE /api/v1/missions/{id}/checkpoints/{checkpointId}", h.removeCheckpoint)
	mux.HandleFunc("POST /api/v1/missions/{id}/assignments", h.assign)
	mux.HandleFunc("DELETE /api/v1/missions/{id}/assignments/{unitId}", h.unassign)
	mux.HandleFunc("POST /api/v1/missions/{id}/approve", h.transition(domain.TransitionApprove))
	mux.HandleFunc("POST /api/v1/missions/{id}/start", h.transition(domain.TransitionStart))
	mux.HandleFunc("POST /api/v1/missions/{id}/complete", h.transition(domain.TransitionComplete))
	mux.HandleFunc("POST /api/v1/missions/{id}/abort", h.transition(domain.TransitionAbort))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	views, err := h.service.List(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	missions := make([]missionDTO, 0, len(views))
	for _, view := range views {
		missions = append(missions, newMissionDTO(view))
	}
	writeJSON(w, http.StatusOK, missionListDTO{
		GeneratedAt: h.now().Format(time.RFC3339),
		Missions:    missions,
	}, h.logger)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var body createMissionRequest
	if message, ok := decode(r, &body); !ok {
		h.invalid(w, message)
		return
	}

	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.PatrolAreaID) == "" {
		h.invalid(w, "name and patrolAreaId are required")
		return
	}
	if body.Window.Start == nil || body.Window.End == nil {
		h.invalid(w, "window.start and window.end are required")
		return
	}

	command := application.CreateMission{
		Name:         body.Name,
		PatrolAreaID: body.PatrolAreaID,
		Window:       domain.Window{Start: *body.Window.Start, End: *body.Window.End},
	}
	if body.PatrolRouteID != nil {
		command.PatrolRouteID = *body.PatrolRouteID
	}
	for _, checkpoint := range body.Checkpoints {
		parsed, message, ok := newCheckpoint(checkpoint)
		if !ok {
			h.invalid(w, message)
			return
		}
		command.Checkpoints = append(command.Checkpoints, parsed)
	}
	for _, assignment := range body.Assignments {
		if strings.TrimSpace(assignment.UnitID) == "" {
			h.invalid(w, "assignments[].unitId is required")
			return
		}
		command.UnitIDs = append(command.UnitIDs, assignment.UnitID)
	}

	view, err := h.service.Create(r.Context(), command, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newMissionDTO(view), h.logger)
}

func (h *Handler) addCheckpoint(w http.ResponseWriter, r *http.Request) {
	var body checkpointRequest
	if message, ok := decode(r, &body); !ok {
		h.invalid(w, message)
		return
	}

	checkpoint, message, ok := newCheckpoint(body)
	if !ok {
		h.invalid(w, message)
		return
	}

	view, err := h.service.AddCheckpoint(r.Context(), r.PathValue("id"), checkpoint)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
}

func (h *Handler) removeCheckpoint(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.RemoveCheckpoint(r.Context(), r.PathValue("id"), r.PathValue("checkpointId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	var body assignmentRequest
	if message, ok := decode(r, &body); !ok {
		h.invalid(w, message)
		return
	}
	if strings.TrimSpace(body.UnitID) == "" {
		h.invalid(w, "unitId is required")
		return
	}

	view, err := h.service.Assign(r.Context(), r.PathValue("id"), body.UnitID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
}

func (h *Handler) unassign(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.Unassign(r.Context(), r.PathValue("id"), r.PathValue("unitId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
}

// transition builds the handler for one lifecycle transition. The body carries
// the version the caller last read, which the aggregate checks.
func (h *Handler) transition(transition domain.Transition) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body transitionRequest
		if message, ok := decode(r, &body); !ok {
			h.invalid(w, message)
			return
		}
		if body.Version == nil {
			h.invalid(w, "version is required so a stale transition can be refused")
			return
		}

		view, err := h.service.Transition(
			r.Context(),
			r.PathValue("id"),
			transition,
			*body.Version,
			domain.AbortReason(body.Reason),
		)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, newMissionDTO(view), h.logger)
	}
}

func newCheckpoint(body checkpointRequest) (application.NewCheckpoint, string, bool) {
	if strings.TrimSpace(body.Name) == "" {
		return application.NewCheckpoint{}, "checkpoint name is required", false
	}
	if body.Position.Latitude == nil || body.Position.Longitude == nil {
		return application.NewCheckpoint{}, "checkpoint position is required", false
	}
	if body.RadiusMeters == nil || *body.RadiusMeters <= 0 {
		return application.NewCheckpoint{}, "checkpoint radiusMeters must be positive", false
	}

	return application.NewCheckpoint{
		Name: body.Name,
		Position: domain.Position{
			Latitude:  *body.Position.Latitude,
			Longitude: *body.Position.Longitude,
		},
		RadiusMeters: *body.RadiusMeters,
	}, "", true
}

// decode reads a JSON body strictly: an undeclared field is a bad request
// rather than something silently ignored.
func decode(r *http.Request, target any) (string, bool) {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return "request body is required", false
		}
		return "request body does not match the published schema: " + err.Error(), false
	}
	return "", true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, response := refusal(err)
	if status >= http.StatusInternalServerError {
		h.logger.ErrorContext(r.Context(), "mission request failed", slog.Any("error", err))
	} else {
		h.logger.InfoContext(r.Context(), "mission request refused",
			slog.String("code", response.Code), slog.Any("error", err))
	}
	h.write(w, status, response)
}

func (h *Handler) write(w http.ResponseWriter, status int, response errorResponse) {
	writeJSON(w, status, response, h.logger)
}

func writeJSON(w http.ResponseWriter, status int, value any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Error("write response", slog.Any("error", err))
	}
}

// invalid reports a body that does not match the published request schema.
func (h *Handler) invalid(w http.ResponseWriter, message string) {
	status, response := invalidRequest(message)
	h.write(w, status, response)
}
