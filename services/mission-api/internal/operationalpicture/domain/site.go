package domain

import (
	"errors"
	"fmt"
	"time"
)

// Position is a WGS84 coordinate pair.
type Position struct {
	Latitude  float64
	Longitude float64
}

// ObservationPolicy carries the thresholds that turn raw observations into
// freshness and condition. City and industrial sites do not share one policy,
// so the values belong to the Site Profile rather than to the code.
type ObservationPolicy struct {
	PositionWindow time.Duration
	BatteryWindow  time.Duration
	LinkWindow     time.Duration

	BatteryWarningPercent  int
	BatteryCriticalPercent int
	LinkWarningPercent     int
	LinkCriticalPercent    int
}

// ErrInvalidPolicy reports a Site Profile whose thresholds cannot be applied.
var ErrInvalidPolicy = errors.New("invalid observation policy")

// Validate rejects a policy that would make freshness or condition
// undecidable, so a misconfigured site fails loudly instead of silently
// reporting everything as healthy.
func (p ObservationPolicy) Validate() error {
	for name, window := range map[string]time.Duration{
		"position": p.PositionWindow,
		"battery":  p.BatteryWindow,
		"link":     p.LinkWindow,
	} {
		if window <= 0 {
			return fmt.Errorf("%w: %s window must be positive", ErrInvalidPolicy, name)
		}
	}
	for name, percent := range map[string]int{
		"battery warning":  p.BatteryWarningPercent,
		"battery critical": p.BatteryCriticalPercent,
		"link warning":     p.LinkWarningPercent,
		"link critical":    p.LinkCriticalPercent,
	} {
		if percent < 0 || percent > 100 {
			return fmt.Errorf("%w: %s percent out of range", ErrInvalidPolicy, name)
		}
	}
	if p.BatteryCriticalPercent > p.BatteryWarningPercent {
		return fmt.Errorf("%w: battery critical above warning", ErrInvalidPolicy)
	}
	if p.LinkCriticalPercent > p.LinkWarningPercent {
		return fmt.Errorf("%w: link critical above warning", ErrInvalidPolicy)
	}
	return nil
}

// SiteProfile is the operating context a patrol unit belongs to.
type SiteProfile struct {
	ID     string
	Name   string
	Center Position
	Policy ObservationPolicy
}

// PatrolArea is the boundary a site patrols.
type PatrolArea struct {
	ID       string
	Name     string
	Boundary []Position
}

// PatrolRoute is a planned path drawn on the operational picture. Mission
// planning and coverage rules belong to the Patrol Mission slice; here the
// route is map context only.
type PatrolRoute struct {
	ID           string
	PatrolAreaID string
	Name         string
	Path         []Position
}
