package domain

import (
	"testing"
	"time"
)

func TestAnUnansweredIngestQuestionIsUnknownNotHealthy(t *testing.T) {
	// The zero value must never read as "receiving": a process that does not
	// run the ingest path would otherwise vouch for a path it never touched.
	var unset Ingest

	if unset.Reported() != IngestUnknown {
		t.Errorf("reported = %q, want UNKNOWN", unset.Reported())
	}
	if unset.Trustworthy() {
		t.Error("an unknown ingest path cannot make a unit's silence trustworthy")
	}
}

func TestSilenceIsOnlyTheUnitsOwnWhileThePlatformIsReceiving(t *testing.T) {
	cases := []struct {
		state       IngestState
		trustworthy bool
	}{
		{IngestReceiving, true},
		{IngestNotReceiving, false},
		{IngestUnknown, false},
	}

	for _, current := range cases {
		ingest := Ingest{State: current.state, LastEventAt: time.Now()}
		if ingest.Trustworthy() != current.trustworthy {
			t.Errorf("%s: trustworthy = %t, want %t", current.state, ingest.Trustworthy(), current.trustworthy)
		}
	}
}
