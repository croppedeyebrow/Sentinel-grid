package domain

import "time"

// Observation is a stored telemetry row as it comes out of the projection,
// before anything has been decided about it.
type Observation struct {
	Position   *Position
	ObservedAt *time.Time
	ReceivedAt *time.Time
}

// TrustObservations decides which observations may credit a checkpoint pass.
//
// Coverage asks a different question from the live operational picture. There,
// freshness means "is this value current", measured against now. Here the
// mission is being judged after the fact, so an observation from two hours ago
// is perfectly good evidence — what matters is whether it was trustworthy when
// it was taken. An observation qualifies when it carries a position, carries
// the time it was measured, and reached the platform within the site's allowed
// receipt delay. A packet that arrived long after it was measured cannot prove
// where the unit was at a given moment.
//
// Observations that do not qualify are kept with Trusted false rather than
// dropped, so a caller can still see that something was received.
func TrustObservations(observations []Observation, maxReceiptDelay time.Duration) []Evidence {
	evidence := make([]Evidence, 0, len(observations))

	for _, observation := range observations {
		item := Evidence{}
		if observation.ObservedAt != nil {
			item.ObservedAt = *observation.ObservedAt
		}
		if observation.Position != nil {
			item.Position = *observation.Position
		}
		item.Trusted = qualifies(observation, maxReceiptDelay)
		evidence = append(evidence, item)
	}

	return evidence
}

func qualifies(observation Observation, maxReceiptDelay time.Duration) bool {
	if observation.Position == nil || observation.ObservedAt == nil || observation.ReceivedAt == nil {
		return false
	}
	delay := observation.ReceivedAt.Sub(*observation.ObservedAt)
	if delay < 0 {
		// Received before it was measured: the clocks disagree, so the row
		// cannot be used as proof of a position at a moment.
		return false
	}
	return delay <= maxReceiptDelay
}
