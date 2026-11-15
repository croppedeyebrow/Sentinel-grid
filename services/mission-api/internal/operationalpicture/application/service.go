// Package application assembles the Operational Picture read model: it loads
// the stored projection through a port and lets the domain decide freshness
// and unit condition.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
)

// Projection is the stored state the read model is built from: observations
// exactly as persisted, with no condition derived yet.
type Projection struct {
	Site        domain.SiteProfile
	PatrolAreas []domain.PatrolArea
	Routes      []domain.PatrolRoute
	Units       []domain.PatrolUnitSnapshot
}

// SnapshotReader loads the stored projection. The interface lives here, on the
// consumer side, so the adapter depends on the application rather than the
// other way round.
type SnapshotReader interface {
	LoadOperationalPicture(ctx context.Context) (Projection, error)
}

// ErrNotProvisioned reports that no Site Profile is configured, which means
// the platform cannot say anything about the operating area yet. It is not the
// same as a site with no units, which is a legitimate empty picture.
var ErrNotProvisioned = errors.New("operational picture is not provisioned")

// Service builds the Operational Picture read model.
type Service struct {
	reader SnapshotReader
	now    func() time.Time
}

// Option customises a Service.
type Option func(*Service)

// WithClock replaces the clock used to judge freshness, which keeps tests
// independent of wall time.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// NewService wires a Service to its projection reader.
func NewService(reader SnapshotReader, options ...Option) *Service {
	service := &Service{
		reader: reader,
		now:    func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// OperationalPicture returns the current read model. Freshness is resolved
// once, against a single timestamp, so every unit in one response is judged
// against the same moment.
func (s *Service) OperationalPicture(ctx context.Context) (domain.OperationalPicture, error) {
	projection, err := s.reader.LoadOperationalPicture(ctx)
	if err != nil {
		return domain.OperationalPicture{}, fmt.Errorf("load projection: %w", err)
	}
	if err := projection.Site.Policy.Validate(); err != nil {
		return domain.OperationalPicture{}, fmt.Errorf("site %q: %w", projection.Site.ID, err)
	}

	now := s.now()
	units := make([]domain.PatrolUnitView, 0, len(projection.Units))
	for _, unit := range projection.Units {
		units = append(units, unit.Evaluate(now, projection.Site.Policy))
	}

	return domain.OperationalPicture{
		GeneratedAt: now,
		Site:        projection.Site,
		PatrolAreas: projection.PatrolAreas,
		Routes:      projection.Routes,
		Units:       units,
	}, nil
}
