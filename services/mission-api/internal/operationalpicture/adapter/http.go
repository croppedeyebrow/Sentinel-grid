// Package adapter exposes the Operational Picture over HTTP and reads its
// projection from PostgreSQL.
package adapter

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
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

// Handler serves the Operational Picture endpoints.
type Handler struct {
	service        PictureService
	probe          ReadinessProbe
	allowedOrigins []string
	logger         *slog.Logger
}

// NewHandler builds the HTTP handler. allowedOrigins is the exact list of
// browser origins permitted to read the API.
func NewHandler(service PictureService, probe ReadinessProbe, allowedOrigins []string, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	handler := &Handler{service: service, probe: probe, allowedOrigins: allowedOrigins, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", handler.live)
	mux.HandleFunc("GET /health/ready", handler.ready)
	mux.HandleFunc("GET /api/v1/operational-picture", handler.operationalPicture)

	return handler.withCORS(mux)
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

func (h *Handler) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && slices.Contains(h.allowedOrigins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Error("write response", slog.Any("error", err))
	}
}
