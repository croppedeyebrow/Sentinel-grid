// Command api serves the SentinelGrid Mission API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	pictureadapter "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/adapter"
	pictureapp "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/application"
	missionadapter "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/adapter"
	missionapp "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/config"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/httpx"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/ids"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/postgres"
	telemetryadapter "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/adapter"
	telemetryapp "github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/application"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("mission-api stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	settings := config.Load()

	pool, err := postgres.NewPool(ctx, settings.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	mux := http.NewServeMux()

	pictureReader := pictureadapter.NewPostgresReader(pool)
	pictureadapter.
		NewHandler(pictureapp.NewService(pictureReader), pictureReader, logger).
		Register(mux)

	missionRepository := missionadapter.NewPostgresRepository(pool)
	missionadapter.
		NewHandler(missionapp.NewService(missionRepository, ids.NewUUID), logger).
		Register(mux)

	// Telemetry ingest runs alongside the HTTP server: it reads what the
	// gateway published and projects it. The API keeps serving reads if the
	// stream is unreachable, so this failure does not take the service down.
	telemetryClient, err := redis.ParseURL(settings.RedisURL)
	if err != nil {
		return fmt.Errorf("parse redis url: %w", err)
	}
	streamClient := redis.NewClient(telemetryClient)
	defer func() { _ = streamClient.Close() }()

	streamReader := telemetryadapter.NewStreamReader(
		streamClient,
		telemetryadapter.StreamConfig{
			Stream:   settings.TelemetryStream,
			Group:    settings.TelemetryGroup,
			Consumer: settings.TelemetryConsumer,
		},
		telemetryapp.NewConsumer(telemetryadapter.NewPostgresRepository(pool)),
		logger,
	)

	go func() {
		logger.Info("telemetry consumer starting",
			slog.String("stream", settings.TelemetryStream),
			slog.String("group", settings.TelemetryGroup))
		if err := streamReader.Run(ctx); err != nil {
			logger.Error("telemetry consumer stopped", slog.Any("error", err))
		}
	}()

	server := &http.Server{
		Addr: ":" + settings.APIPort,
		// Every request gets an id and one log line; the cross-origin rules
		// are applied once for the whole service.
		Handler:           httpx.WithRequestLog(httpx.WithCORS(mux, settings.AllowedOrigins), logger),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	serverFailed := make(chan error, 1)
	go func() {
		logger.Info("mission-api listening", slog.String("address", "http://localhost:"+settings.APIPort))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverFailed <- err
		}
	}()

	select {
	case err := <-serverFailed:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
