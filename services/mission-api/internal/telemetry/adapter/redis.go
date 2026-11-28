// Package adapter connects telemetry ingest to Redis Streams and PostgreSQL.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

	// status is what the reader knows about its own ingest path, which the
	// Operational Picture reports so a broken stream is not read as a silent
	// fleet.
	mu     sync.Mutex
	status StreamStatus
}

// StreamStatus is the reader's account of itself.
type StreamStatus struct {
	// Known is false until the reader has tried once. Before that it reports
	// nothing rather than claiming health.
	Known bool
	// Reachable is whether the last attempt to read the stream worked. An
	// empty read counts as reachable: the platform can receive, the units are
	// simply quiet.
	Reachable bool
	// LastEventAt is when the platform last took in an event.
	LastEventAt time.Time
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

// Status reports what the reader knows about its own ingest path.
func (r *StreamReader) Status() StreamStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *StreamReader) noteReachable(reachable bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Known = true
	r.status.Reachable = reachable
}

func (r *StreamReader) noteEvent(at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at.After(r.status.LastEventAt) {
		r.status.LastEventAt = at
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
			// Nothing arrived inside the blocking window. The stream answered,
			// so the platform can receive: this is the units being quiet.
			r.noteReachable(true)
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
				r.noteReachable(false)
			} else {
				r.noteReachable(true)
			}
			cursor = "0"
			continue
		case err != nil:
			r.noteReachable(false)
			r.logger.WarnContext(ctx, "telemetry stream read failed", slog.Any("error", err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		r.noteReachable(true)

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

	// Something reached the platform, whether or not its value could be used.
	r.noteEvent(receipt.ReceivedAt)

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
	// Sensors is kept as the gateway published it. Decoding it into a struct
	// and re-encoding would add a null for every sensor the frame did not
	// report, and the receipt log would then claim readings that never
	// arrived — it is the audit record, so it has to say exactly what came in.
	Sensors       json.RawMessage `json:"sensors"`
	MissingFrames *int64          `json:"missingFrames"`
	Rejection     *struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"rejection"`
	FrameBytes int `json:"frameBytes"`
}

// wireSensors reads the values out of the published sensors object without
// deciding how they are written back.
type wireSensors struct {
	Position *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"position"`
	Battery *int `json:"battery"`
	Link    *int `json:"link"`
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
	if len(wire.Sensors) > 0 && string(wire.Sensors) != "null" {
		var sensors wireSensors
		if err := json.Unmarshal(wire.Sensors, &sensors); err != nil {
			return application.Receipt{}, fmt.Errorf("decode sensors: %w", err)
		}
		if sensors.Position != nil {
			receipt.Position = &domain.Position{
				Latitude:  sensors.Position.Latitude,
				Longitude: sensors.Position.Longitude,
			}
		}
		receipt.Battery = sensors.Battery
		receipt.Link = sensors.Link

		// Stored byte for byte as published: a sensor the frame left out is
		// absent from the record too.
		receipt.SensorsJSON = wire.Sensors
	}

	return receipt, nil
}
