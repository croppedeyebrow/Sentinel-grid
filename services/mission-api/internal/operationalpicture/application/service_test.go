package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
)

var fixedNow = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

type fakeReader struct {
	projection Projection
	err        error
	calls      int
}

func (f *fakeReader) LoadOperationalPicture(context.Context) (Projection, error) {
	f.calls++
	if f.err != nil {
		return Projection{}, f.err
	}
	return f.projection, nil
}

func policy() domain.ObservationPolicy {
	return domain.ObservationPolicy{
		PositionWindow:         45 * time.Second,
		BatteryWindow:          180 * time.Second,
		LinkWindow:             180 * time.Second,
		BatteryWarningPercent:  40,
		BatteryCriticalPercent: 20,
		LinkWarningPercent:     50,
		LinkCriticalPercent:    20,
	}
}

func observedAt(offset time.Duration) *time.Time {
	t := fixedNow.Add(offset)
	return &t
}

func value[T any](v T) *T { return &v }

func projectionWith(units ...domain.PatrolUnitSnapshot) Projection {
	return Projection{
		Site: domain.SiteProfile{
			ID:     "site-1",
			Name:   "Sentinel Test Site",
			Center: domain.Position{Latitude: 37.5665, Longitude: 126.978},
			Policy: policy(),
		},
		PatrolAreas: []domain.PatrolArea{{ID: "area-1", Name: "Primary", Boundary: []domain.Position{{Latitude: 37.558, Longitude: 126.965}}}},
		Routes:      []domain.PatrolRoute{{ID: "route-1", PatrolAreaID: "area-1", Name: "Loop", Path: []domain.Position{{Latitude: 37.56, Longitude: 126.969}}}},
		Units:       units,
	}
}

func freshUnit(id string) domain.PatrolUnitSnapshot {
	return domain.PatrolUnitSnapshot{
		UnitID:      id,
		Callsign:    id,
		DisplayName: id,
		Position:    domain.Observed[domain.Position]{Value: value(domain.Position{Latitude: 37.5665, Longitude: 126.978}), ObservedAt: observedAt(-5 * time.Second), ReceivedAt: observedAt(0)},
		Battery:     domain.Observed[int]{Value: value(82), ObservedAt: observedAt(-5 * time.Second), ReceivedAt: observedAt(0)},
		Link:        domain.Observed[int]{Value: value(94), ObservedAt: observedAt(-5 * time.Second), ReceivedAt: observedAt(0)},
	}
}

func service(reader SnapshotReader) *Service {
	return NewService(reader, WithClock(func() time.Time { return fixedNow }))
}

func TestOperationalPictureEvaluatesUnits(t *testing.T) {
	stale := freshUnit("SG-02")
	stale.Position.ObservedAt = observedAt(-20 * time.Minute)
	reader := &fakeReader{projection: projectionWith(freshUnit("SG-01"), stale)}

	picture, err := service(reader).OperationalPicture(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !picture.GeneratedAt.Equal(fixedNow) {
		t.Errorf("generatedAt = %s, want %s", picture.GeneratedAt, fixedNow)
	}
	if len(picture.Units) != 2 {
		t.Fatalf("units = %d, want 2", len(picture.Units))
	}
	if picture.Units[0].Status != domain.UnitStatusNormal {
		t.Errorf("first unit status = %s, want NORMAL", picture.Units[0].Status)
	}
	if picture.Units[1].Status != domain.UnitStatusUnknown {
		t.Errorf("second unit status = %s, want UNKNOWN", picture.Units[1].Status)
	}
	if picture.Units[1].Position.Freshness != domain.FreshnessStale {
		t.Errorf("second unit position freshness = %s, want STALE", picture.Units[1].Position.Freshness)
	}
	if len(picture.PatrolAreas) != 1 || len(picture.Routes) != 1 {
		t.Error("map context must be carried through to the read model")
	}
}

func TestOperationalPictureJudgesEveryUnitAgainstOneMoment(t *testing.T) {
	reader := &fakeReader{projection: projectionWith(freshUnit("SG-01"), freshUnit("SG-02"))}

	picture, err := service(reader).OperationalPicture(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, unit := range picture.Units {
		if unit.Position.Freshness != domain.FreshnessFresh {
			t.Errorf("unit %s judged against a different moment", unit.Callsign)
		}
	}
	if reader.calls != 1 {
		t.Errorf("reader calls = %d, want 1", reader.calls)
	}
}

func TestOperationalPictureAllowsASiteWithNoUnits(t *testing.T) {
	reader := &fakeReader{projection: projectionWith()}

	picture, err := service(reader).OperationalPicture(context.Background())
	if err != nil {
		t.Fatalf("an empty site is a valid picture, got error: %v", err)
	}
	if len(picture.Units) != 0 {
		t.Errorf("units = %d, want 0", len(picture.Units))
	}
	if picture.Site.ID != "site-1" {
		t.Error("site profile must still be reported")
	}
}

func TestOperationalPictureRejectsAnUnusablePolicy(t *testing.T) {
	broken := projectionWith(freshUnit("SG-01"))
	broken.Site.Policy.PositionWindow = 0
	reader := &fakeReader{projection: broken}

	_, err := service(reader).OperationalPicture(context.Background())
	if !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestOperationalPictureWrapsReaderFailure(t *testing.T) {
	failure := errors.New("connection refused")
	reader := &fakeReader{err: failure}

	_, err := service(reader).OperationalPicture(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want it to wrap the reader failure", err)
	}
}
