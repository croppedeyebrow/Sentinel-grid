package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/telemetry/domain"
)

var received = time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)

func value[T any](v T) *T { return &v }

type fakeRepository struct {
	snapshots map[string]domain.UnitSnapshot
	known     map[string]bool
	seen      map[string]bool
	applied   []Update
	err       error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		snapshots: map[string]domain.UnitSnapshot{},
		known:     map[string]bool{"unit-1": true},
		seen:      map[string]bool{},
	}
}

func (f *fakeRepository) Snapshot(_ context.Context, unitID string) (domain.UnitSnapshot, error) {
	if f.err != nil {
		return domain.UnitSnapshot{}, f.err
	}
	return f.snapshots[unitID], nil
}

func (f *fakeRepository) Apply(_ context.Context, update Update) error {
	if f.err != nil {
		return f.err
	}
	f.applied = append(f.applied, update)
	f.seen[update.Receipt.EventID] = true
	if update.Receipt.UnitID != "" {
		f.snapshots[update.Receipt.UnitID] = update.Snapshot
	}
	return nil
}

func (f *fakeRepository) KnownUnit(_ context.Context, unitID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.known[unitID], nil
}

func (f *fakeRepository) Seen(_ context.Context, eventID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.seen[eventID], nil
}

func accepted(eventID string, sequence int64, observed time.Duration) Receipt {
	at := received.Add(observed)
	return Receipt{
		EventID:    eventID,
		Outcome:    domain.ClassificationAccepted,
		UnitID:     "unit-1",
		SessionID:  "session-1",
		Sequence:   sequence,
		ObservedAt: &at,
		ReceivedAt: received.Add(observed),
		Position:   value(domain.Position{Latitude: 37.5665, Longitude: 126.978}),
		Battery:    value(82),
		Link:       value(94),
		FrameBytes: 180,
	}
}

func TestHandleProjectsAnAcceptedFrame(t *testing.T) {
	repository := newFakeRepository()

	if err := NewConsumer(repository).Handle(context.Background(), accepted("event-1", 1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repository.applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(repository.applied))
	}
	update := repository.applied[0]
	if !update.AttachUnit {
		t.Error("a known unit must be referenced by its receipt")
	}
	if !update.RecordObservation {
		t.Error("a frame that moved sensors forward belongs in the observation history")
	}
	if update.Snapshot.Battery == nil || update.Snapshot.Battery.Value != 82 {
		t.Errorf("battery = %+v, want 82", update.Snapshot.Battery)
	}
}

func TestHandleIgnoresAReDeliveredReceipt(t *testing.T) {
	repository := newFakeRepository()
	consumer := NewConsumer(repository)
	receipt := accepted("event-1", 1, 0)

	if err := consumer.Handle(context.Background(), receipt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Re-delivery is normal with a consumer group.
	if err := consumer.Handle(context.Background(), receipt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repository.applied) != 1 {
		t.Errorf("applied = %d, want the second delivery ignored", len(repository.applied))
	}
}

func TestHandleKeepsADuplicateOutOfTheObservationHistory(t *testing.T) {
	repository := newFakeRepository()
	consumer := NewConsumer(repository)

	if err := consumer.Handle(context.Background(), accepted("event-1", 1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	duplicate := accepted("event-2", 1, 0)
	duplicate.Outcome = domain.ClassificationDuplicate
	if err := consumer.Handle(context.Background(), duplicate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second := repository.applied[1]
	if second.RecordObservation {
		t.Error("a duplicate carries no new measurement, so it is not observation evidence")
	}
	// The receipt is still stored: the frame arrived.
	if second.Receipt.EventID != "event-2" {
		t.Error("a duplicate must still be recorded as received")
	}
}

func TestHandleKeepsALateFrameOutOfTheObservationHistory(t *testing.T) {
	repository := newFakeRepository()
	consumer := NewConsumer(repository)

	if err := consumer.Handle(context.Background(), accepted("event-1", 5, 5*time.Minute)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	late := accepted("event-2", 3, 2*time.Minute)
	late.Outcome = domain.ClassificationOutOfOrder
	late.Battery = value(40)
	if err := consumer.Handle(context.Background(), late); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second := repository.applied[1]
	if second.RecordObservation {
		t.Error("a measurement older than the stored one is not coverage evidence")
	}
	if second.Snapshot.Battery.Value != 82 {
		t.Errorf("battery = %d, want the newer 82 kept", second.Snapshot.Battery.Value)
	}
}

func TestHandleRecordsARefusalWithoutApplyingAnyValue(t *testing.T) {
	repository := newFakeRepository()
	consumer := NewConsumer(repository)

	if err := consumer.Handle(context.Background(), accepted("event-1", 1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	refused := Receipt{
		EventID:    "event-2",
		Rejected:   true,
		Rejection:  &Rejection{Code: "VALUE_OUT_OF_RANGE", Detail: "battery is out of range: 150"},
		UnitID:     "unit-1",
		SessionID:  "session-1",
		Sequence:   2,
		ReceivedAt: received.Add(time.Minute),
		FrameBytes: 166,
	}
	if err := consumer.Handle(context.Background(), refused); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	update := repository.applied[1]
	if update.Snapshot.RejectedFrames != 1 {
		t.Errorf("rejectedFrames = %d, want 1", update.Snapshot.RejectedFrames)
	}
	if update.Snapshot.Battery.Value != 82 {
		t.Error("a refused frame must not disturb a stored reading")
	}
	if update.RecordObservation {
		t.Error("a refused frame contributes no observation")
	}
	if !update.Snapshot.LastReceivedAt.Equal(received.Add(time.Minute)) {
		t.Error("a refused frame still proves something arrived")
	}
}

func TestHandleStillRecordsAReceiptForAnUnknownUnit(t *testing.T) {
	repository := newFakeRepository()
	receipt := accepted("event-1", 1, 0)
	receipt.UnitID = "unit-unknown"

	if err := NewConsumer(repository).Handle(context.Background(), receipt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	update := repository.applied[0]
	if update.AttachUnit {
		t.Error("an unknown unit cannot be referenced")
	}
	if update.RecordObservation {
		t.Error("there is nothing to attach an observation to")
	}
	// The frame is not lost: it arrived, and the log says so.
	if update.Receipt.EventID != "event-1" {
		t.Error("the receipt must be stored anyway")
	}
}

func TestHandleRefusesAReceiptItCannotActOn(t *testing.T) {
	cases := map[string]Receipt{
		"no event id":     {Outcome: domain.ClassificationAccepted, ReceivedAt: received},
		"no receipt time": {EventID: "event-1", Outcome: domain.ClassificationAccepted},
	}

	for name, receipt := range cases {
		t.Run(name, func(t *testing.T) {
			err := NewConsumer(newFakeRepository()).Handle(context.Background(), receipt)

			if !errors.Is(err, ErrUnusableReceipt) {
				t.Fatalf("err = %v, want ErrUnusableReceipt", err)
			}
		})
	}
}

func TestHandlePassesStoreFailuresUp(t *testing.T) {
	repository := newFakeRepository()
	failure := errors.New("connection refused")
	repository.err = failure

	err := NewConsumer(repository).Handle(context.Background(), accepted("event-1", 1, 0))

	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the store failure", err)
	}
}

func TestHandleRecordsAGapAsIncompleteness(t *testing.T) {
	repository := newFakeRepository()
	consumer := NewConsumer(repository)

	if err := consumer.Handle(context.Background(), accepted("event-1", 1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gap := accepted("event-2", 5, time.Minute)
	gap.Outcome = domain.ClassificationGap
	gap.MissingFrames = 3
	if err := consumer.Handle(context.Background(), gap); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	update := repository.applied[1]
	if update.Snapshot.MissingFrames != 3 {
		t.Errorf("missingFrames = %d, want 3", update.Snapshot.MissingFrames)
	}
	if !update.RecordObservation {
		t.Error("a frame past a gap still carries measurements worth keeping")
	}
}
