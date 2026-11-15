// Package domain holds the Operational Picture rules: how an observation's
// freshness is judged and how a unit's condition is derived from it.
//
// Unit condition and data quality are deliberately separate axes. A unit can
// be healthy while its telemetry is stale, and the console has to be able to
// say so instead of collapsing both into one label.
package domain

import "time"

// Freshness is the data quality axis for a single observed field.
type Freshness string

const (
	// FreshnessFresh means the value was observed inside the site's window.
	FreshnessFresh Freshness = "FRESH"
	// FreshnessStale means a real value exists but is older than the window.
	FreshnessStale Freshness = "STALE"
	// FreshnessMissing means there is no usable observation for the field.
	FreshnessMissing Freshness = "MISSING"
)

// Observed is a telemetry value together with the two times that decide
// whether it can still be trusted: when the unit measured it, and when the
// platform received it. Both are kept so a late packet is never presented as
// a current reading.
type Observed[T any] struct {
	Value      *T
	ObservedAt *time.Time
	ReceivedAt *time.Time
}

// Freshness judges the value against the site's staleness window. A value
// without an observation time cannot be aged, so it counts as missing rather
// than fresh.
func (o Observed[T]) Freshness(now time.Time, window time.Duration) Freshness {
	if o.Value == nil || o.ObservedAt == nil {
		return FreshnessMissing
	}
	if now.Sub(*o.ObservedAt) > window {
		return FreshnessStale
	}
	return FreshnessFresh
}

// Evaluated is an observation with its freshness resolved for a point in time.
type Evaluated[T any] struct {
	Value      *T
	ObservedAt *time.Time
	ReceivedAt *time.Time
	Freshness  Freshness
}

// Evaluate resolves the freshness of the observation at now.
func (o Observed[T]) Evaluate(now time.Time, window time.Duration) Evaluated[T] {
	return Evaluated[T]{
		Value:      o.Value,
		ObservedAt: o.ObservedAt,
		ReceivedAt: o.ReceivedAt,
		Freshness:  o.Freshness(now, window),
	}
}
