// Package adapter connects telemetry ingest to Redis Streams and PostgreSQL.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/domain"
)

// StreamReader consumes the telemetry stream the gateway publishes to.
//
// A consumer group is used so the service can be restarted without losing
// entries and, later, run more than one instance. An entry is acknowledged
// only after it has been applied, so a crash mid-apply means the entry is
// delivered again — which is safe, because applying a receipt twice changes
// nothing.
type StreamReader struct {
	client    *redis.Client
	stream    string
	group     string
	consumer  string
	batchSize int64
	block     time.Duration
	handler   *application.Consumer
	logger    *slog.Logger
}

// StreamConfig is what the reader needs to start.
type StreamConfig struct {
	Stream    string
	Group     string
	Consumer  string
	BatchSize int64
	Block     time.Duration
}

// NewStreamReader wires a reader to the stream and the consumer.
func NewStreamReader(
	client *redis.Client,
	config StreamConfig,
	handler *application.Consumer,
	logger *slog.Logger,
) *StreamReader {
	if logger == nil {
		logger = slog.Default()
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 64
	}
	if config.Block <= 0 {
		config.Block = 5 * time.Second
	}

	return &StreamReader{
		client:    client,
		stream:    config.Stream,
		group:     config.Group,
		consumer:  config.Consumer,
		batchSize: config.BatchSize,
		block:     config.Block,
		handler:   handler,
		logger:    logger,
	}
}

// Ping reports whether the stream can be reached, which readiness depends on.
func (r *StreamReader) Ping(ctx context.Context) error {
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping telemetry stream: %w", err)
	}
	return nil
}

// EnsureGroup creates the consumer group, and the stream with it, so the
// service can start before the gateway has published anything.
func (r *StreamReader) EnsureGroup(ctx context.Context) error {
	err := r.client.XGroupCreateMkStream(ctx, r.stream, r.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group %s: %w", r.group, err)
	}
	return nil
}

// Run consumes until the context is cancelled.
//
// Entries this consumer already owns but never acknowledged are replayed
// first: a restart in the middle of a batch must not drop them.
func (r *StreamReader) Run(ctx context.Context) error {
	if err := r.EnsureGroup(ctx); err != nil {
		return err
	}

	// "0" replays this consumer's pending entries; ">" waits for new ones.
	cursor := "0"
	for {
		if ctx.Err() != nil {
			return nil
		}

		streams, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    r.group,
			Consumer: r.consumer,
			Streams:  []string{r.stream, cursor},
			Count:    r.batchSize,
			Block:    r.block,
		}).Result()

		switch {
		case errors.Is(err, redis.Nil):
			// Nothing arrived inside the blocking window.
			cursor = ">"
			continue
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil
		case err != nil && strings.Contains(err.Error(), "NOGROUP"):
			// The stream or the group is gone: Redis carries no durable state
			// here, so a restart or a trimmed stream means starting the group
			// again rather than failing. Nothing is lost that mattered — the
			// projection and the receipt log live in PostgreSQL.
			r.logger.WarnContext(ctx, "telemetry consumer group missing, recreating",
				slog.String("stream", r.stream), slog.String("group", r.group))
			if err := r.EnsureGroup(ctx); err != nil {
				r.logger.ErrorContext(ctx, "cannot recreate consumer group", slog.Any("error", err))
			}
			cursor = "0"
			continue
		case err != nil:
			r.logger.WarnContext(ctx, "telemetry stream read failed", slog.Any("error", err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		delivered := 0
		for _, stream := range streams {
			delivered += len(stream.Messages)
			for _, message := range stream.Messages {
				r.consume(ctx, message)
			}
		}

		// Once the replay is drained, follow the live stream.
		if cursor == "0" && delivered == 0 {
			cursor = ">"
		}
	}
}

func (r *StreamReader) consume(ctx context.Context, message redis.XMessage) {
	receipt, err := decodeReceipt(message)
	if err != nil {
		// An entry nobody can read will never become readable, so it is
		// acknowledged and logged rather than retried forever.
		r.logger.ErrorContext(ctx, "telemetry stream entry is unreadable",
			slog.String("entry", message.ID), slog.Any("error", err))
		r.acknowledge(ctx, message.ID)
		return
	}

	if err := r.handler.Handle(ctx, receipt); err != nil {
		if errors.Is(err, application.ErrUnusableReceipt) {
			r.logger.ErrorContext(ctx, "telemetry receipt cannot be applied",
				slog.String("entry", message.ID), slog.Any("error", err))
			r.acknowledge(ctx, message.ID)
			return
		}
		// A store failure is temporary: leave the entry pending so it is
		// delivered again instead of losing it.
		r.logger.ErrorContext(ctx, "telemetry receipt not applied",
			slog.String("entry", message.ID), slog.Any("error", err))
		return
	}

	r.acknowledge(ctx, message.ID)
}

func (r *StreamReader) acknowledge(ctx context.Context, id string) {
	if err := r.client.XAck(ctx, r.stream, r.group, id).Err(); err != nil {
		r.logger.WarnContext(ctx, "cannot acknowledge telemetry entry",
			slog.String("entry", id), slog.Any("error", err))
	}
}

// wireEvent mirrors contracts/schemas/telemetry-event.schema.json.
type wireEvent struct {
	EventID    string     `json:"eventId"`
	Outcome    string     `json:"outcome"`
	UnitID     *string    `json:"unitId"`
	SessionID  *string    `json:"sessionId"`
	Sequence   *int64     `json:"sequence"`
	ObservedAt *time.Time `json:"observedAt"`
	ReceivedAt time.Time  `json:"receivedAt"`
	Sensors    *struct {
		Position *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"position"`
		Battery *int `json:"battery"`
		Link    *int `json:"link"`
	} `json:"sensors"`
	MissingFrames *int64 `json:"missingFrames"`
	Rejection     *struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"rejection"`
	FrameBytes int `json:"frameBytes"`
}

func decodeReceipt(message redis.XMessage) (application.Receipt, error) {
	payload, ok := message.Values["event"].(string)
	if !ok {
		return application.Receipt{}, errors.New("entry has no event field")
	}

	var wire wireEvent
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return application.Receipt{}, fmt.Errorf("decode telemetry event: %w", err)
	}

	receipt := application.Receipt{
		EventID:    wire.EventID,
		Outcome:    domain.Classification(wire.Outcome),
		Rejected:   wire.Outcome == "REJECTED",
		ObservedAt: wire.ObservedAt,
		ReceivedAt: wire.ReceivedAt,
		FrameBytes: wire.FrameBytes,
	}
	if wire.UnitID != nil {
		receipt.UnitID = *wire.UnitID
	}
	if wire.SessionID != nil {
		receipt.SessionID = *wire.SessionID
	}
	if wire.Sequence != nil {
		receipt.Sequence = *wire.Sequence
	}
	if wire.MissingFrames != nil {
		receipt.MissingFrames = *wire.MissingFrames
	}
	if wire.Rejection != nil {
		receipt.Rejection = &application.Rejection{
			Code:   wire.Rejection.Code,
			Detail: wire.Rejection.Detail,
		}
	}
	if wire.Sensors != nil {
		if wire.Sensors.Position != nil {
			receipt.Position = &domain.Position{
				Latitude:  wire.Sensors.Position.Latitude,
				Longitude: wire.Sensors.Position.Longitude,
			}
		}
		receipt.Battery = wire.Sensors.Battery
		receipt.Link = wire.Sensors.Link

		// The sensors are kept as published so the receipt log can be read
		// back without reconstructing them.
		sensors, err := json.Marshal(wire.Sensors)
		if err != nil {
			return application.Receipt{}, fmt.Errorf("re-encode sensors: %w", err)
		}
		receipt.SensorsJSON = sensors
	}

	return receipt, nil
}
