// Package adapter exposes the Operational Picture over HTTP and reads its
// projection from PostgreSQL.
package adapter

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
)

// PictureService builds the read model this adapter serves.
type PictureService interface {
	OperationalPicture(ctx context.Context) (domain.OperationalPicture, error)
}

// ReadinessProbe reports whether the dependencies the API needs are usable.
type ReadinessProbe interface {
	Ping(ctx context.Context) error
}

// Handler serves the Operational Picture and health endpoints.
type Handler struct {
	service PictureService
	probe   ReadinessProbe
	logger  *slog.Logger
}

// NewHandler builds the HTTP handler. Cross-origin rules are applied once for
// the whole service by platform/httpx, not per adapter.
func NewHandler(service PictureService, probe ReadinessProbe, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, probe: probe, logger: logger}
}

// Register mounts the endpoints on the service mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.ready)
	mux.HandleFunc("GET /api/v1/operational-picture", h.operationalPicture)
}

type healthResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// live reports that the process is up. It deliberately does not touch the
// database: a dependency outage must not make the process look dead.
func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "live"}, h.logger)
}

// ready reports whether the API can serve requests, which requires the
// projection store to answer.
func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.probe.Ping(ctx); err != nil {
		h.logger.WarnContext(ctx, "readiness probe failed", slog.Any("error", err))
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable", Reason: "projection store unreachable"}, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ready"}, h.logger)
}

func (h *Handler) operationalPicture(w http.ResponseWriter, r *http.Request) {
	picture, err := h.service.OperationalPicture(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "operational picture unavailable", slog.Any("error", err))
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "operational picture unavailable"}, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, newOperationalPictureDTO(picture), h.logger)
}

func writeJSON(w http.ResponseWriter, status int, value any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Error("write response", slog.Any("error", err))
	}
}
