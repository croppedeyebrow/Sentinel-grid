// Command api serves the SentinelGrid Mission API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/adapter"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/config"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/postgres"
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

	reader := adapter.NewPostgresReader(pool)
	service := application.NewService(reader)
	handler := adapter.NewHandler(service, reader, settings.AllowedOrigins, logger)

	server := &http.Server{
		Addr:              ":" + settings.APIPort,
		Handler:           handler,
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
