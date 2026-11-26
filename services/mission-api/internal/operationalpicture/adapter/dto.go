package adapter

import (
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
)

type operationalPictureDTO struct {
	GeneratedAt  string           `json:"generatedAt"`
	Site         siteDTO          `json:"site"`
	PatrolAreas  []patrolAreaDTO  `json:"patrolAreas"`
	PatrolRoutes []patrolRouteDTO `json:"patrolRoutes"`
	Units        []patrolUnitDTO  `json:"units"`
}

type siteDTO struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Center positionDTO `json:"center"`
	Policy policyDTO   `json:"policy"`
}

// policyDTO is published so the console can explain why a value counts as
// stale or as a warning instead of hard-coding the same thresholds again.
type policyDTO struct {
	PositionStaleAfterSeconds int `json:"positionStaleAfterSeconds"`
	BatteryStaleAfterSeconds  int `json:"batteryStaleAfterSeconds"`
	LinkStaleAfterSeconds     int `json:"linkStaleAfterSeconds"`
	BatteryWarningPercent     int `json:"batteryWarningPercent"`
	BatteryCriticalPercent    int `json:"batteryCriticalPercent"`
	LinkWarningPercent        int `json:"linkWarningPercent"`
	LinkCriticalPercent       int `json:"linkCriticalPercent"`
}

type positionDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type polygonDTO struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

type lineStringDTO struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

type patrolAreaDTO struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Boundary polygonDTO `json:"boundary"`
}

type patrolRouteDTO struct {
	ID           string        `json:"id"`
	PatrolAreaID string        `json:"patrolAreaId"`
	Name         string        `json:"name"`
	Path         lineStringDTO `json:"path"`
}

// observedDTO keeps the value, both timestamps and the freshness verdict
// together, so a client cannot read a value without seeing how old it is.
type observedDTO[T any] struct {
	Value      *T      `json:"value"`
	ObservedAt *string `json:"observedAt"`
	ReceivedAt *string `json:"receivedAt"`
	Freshness  string  `json:"freshness"`
}

type patrolUnitDTO struct {
	ID          string                   `json:"id"`
	Callsign    string                   `json:"callsign"`
	DisplayName string                   `json:"displayName"`
	Status      string                   `json:"status"`
	Position    observedDTO[positionDTO] `json:"position"`
	Battery     observedDTO[int]         `json:"battery"`
	Link        observedDTO[int]         `json:"link"`
	Stream      streamDTO                `json:"stream"`
}

func newOperationalPictureDTO(picture domain.OperationalPicture) operationalPictureDTO {
	areas := make([]patrolAreaDTO, 0, len(picture.PatrolAreas))
	for _, area := range picture.PatrolAreas {
		areas = append(areas, patrolAreaDTO{
			ID:   area.ID,
			Name: area.Name,
			Boundary: polygonDTO{
				Type:        "Polygon",
				Coordinates: [][][]float64{coordinates(area.Boundary)},
			},
		})
	}

	routes := make([]patrolRouteDTO, 0, len(picture.Routes))
	for _, route := range picture.Routes {
		routes = append(routes, patrolRouteDTO{
			ID:           route.ID,
			PatrolAreaID: route.PatrolAreaID,
			Name:         route.Name,
			Path:         lineStringDTO{Type: "LineString", Coordinates: coordinates(route.Path)},
		})
	}

	units := make([]patrolUnitDTO, 0, len(picture.Units))
	for _, unit := range picture.Units {
		units = append(units, patrolUnitDTO{
			ID:          unit.UnitID,
			Callsign:    unit.Callsign,
			DisplayName: unit.DisplayName,
			Status:      string(unit.Status),
			Position:    observedPosition(unit.Position),
			Battery:     observedPercent(unit.Battery),
			Link:        observedPercent(unit.Link),
			Stream:      newStreamDTO(unit),
		})
	}

	return operationalPictureDTO{
		GeneratedAt: picture.GeneratedAt.UTC().Format(time.RFC3339),
		Site: siteDTO{
			ID:     picture.Site.ID,
			Name:   picture.Site.Name,
			Center: positionDTO{Latitude: picture.Site.Center.Latitude, Longitude: picture.Site.Center.Longitude},
			Policy: policyDTO{
				PositionStaleAfterSeconds: int(picture.Site.Policy.PositionWindow.Seconds()),
				BatteryStaleAfterSeconds:  int(picture.Site.Policy.BatteryWindow.Seconds()),
				LinkStaleAfterSeconds:     int(picture.Site.Policy.LinkWindow.Seconds()),
				BatteryWarningPercent:     picture.Site.Policy.BatteryWarningPercent,
				BatteryCriticalPercent:    picture.Site.Policy.BatteryCriticalPercent,
				LinkWarningPercent:        picture.Site.Policy.LinkWarningPercent,
				LinkCriticalPercent:       picture.Site.Policy.LinkCriticalPercent,
			},
		},
		PatrolAreas:  areas,
		PatrolRoutes: routes,
		Units:        units,
	}
}

// coordinates emits GeoJSON order, longitude first, so the payload can be fed
// to the map without a second conversion.
func coordinates(positions []domain.Position) [][]float64 {
	result := make([][]float64, 0, len(positions))
	for _, position := range positions {
		result = append(result, []float64{position.Longitude, position.Latitude})
	}
	return result
}

func observedPosition(observed domain.Evaluated[domain.Position]) observedDTO[positionDTO] {
	dto := observedDTO[positionDTO]{
		ObservedAt: iso(observed.ObservedAt),
		ReceivedAt: iso(observed.ReceivedAt),
		Freshness:  string(observed.Freshness),
	}
	if observed.Value != nil {
		dto.Value = &positionDTO{Latitude: observed.Value.Latitude, Longitude: observed.Value.Longitude}
	}
	return dto
}

func observedPercent(observed domain.Evaluated[int]) observedDTO[int] {
	return observedDTO[int]{
		Value:      observed.Value,
		ObservedAt: iso(observed.ObservedAt),
		ReceivedAt: iso(observed.ReceivedAt),
		Freshness:  string(observed.Freshness),
	}
}

func iso(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

// streamDTO is how a unit's telemetry is behaving, as opposed to what it
// measured. These readings belong to the data quality axis: a silent unit is
// reported as unknown, never as a failure.
type streamDTO struct {
	State            string  `json:"state"`
	SessionID        *string `json:"sessionId"`
	SessionStartedAt *string `json:"sessionStartedAt"`
	LastReceivedAt   *string `json:"lastReceivedAt"`
	// Reconnected is true when the current session began inside the site's
	// window, which is what lets the console say a unit just came back.
	Reconnected bool `json:"reconnected"`
	// MissingFrames and RejectedFrames make an incomplete or refused stream
	// visible instead of letting it look like silence.
	MissingFrames  int64 `json:"missingFrames"`
	RejectedFrames int64 `json:"rejectedFrames"`
}

func newStreamDTO(unit domain.PatrolUnitView) streamDTO {
	dto := streamDTO{
		State:          string(unit.Stream.State),
		LastReceivedAt: iso(unit.Stream.LastReceivedAt),
		Reconnected:    unit.Reconnected,
		MissingFrames:  unit.Stream.MissingFrames,
		RejectedFrames: unit.Stream.RejectedFrames,
	}
	if unit.Stream.SessionID != "" {
		session := unit.Stream.SessionID
		dto.SessionID = &session
	}
	dto.SessionStartedAt = iso(unit.Stream.SessionStartedAt)
	return dto
}
