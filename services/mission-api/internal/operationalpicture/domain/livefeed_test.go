package domain

import (
	"errors"
	"testing"
	"time"
)

// These are the rules a connected operator's screen depends on. They are
// tested without a socket because none of them is about sockets.

var sentAt = time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)

func pictureWith(units ...PatrolUnitView) OperationalPicture {
	return OperationalPicture{
		GeneratedAt: sentAt,
		Site:        SiteProfile{ID: "site-1", Name: "Sentinel Test Site"},
		Units:       units,
		Ingest:      Ingest{State: IngestReceiving, LastEventAt: sentAt},
	}
}

func unitView(callsign string) PatrolUnitView {
	return PatrolUnitView{UnitID: callsign, Callsign: callsign, Status: UnitStatusNormal}
}

func TestAFeedStartsWithTheWholePicture(t *testing.T) {
	feed := NewFeed()

	// A delta before a snapshot would be a change with nothing to change.
	if _, err := feed.Push(Change{Units: []PatrolUnitView{unitView("SG-01")}}, sentAt); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("push before snapshot = %v, want ErrNotStarted", err)
	}

	message, err := feed.Snapshot(pictureWith(unitView("SG-01")), sentAt)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if message.Kind != MessageSnapshot || message.Picture == nil {
		t.Errorf("message = %+v, want a snapshot carrying the picture", message)
	}
	if message.Revision != 1 {
		t.Errorf("revision = %d, want 1", message.Revision)
	}
}

func TestEveryMessageTakesTheNextRevision(t *testing.T) {
	// The client's only way to know it missed something is that the numbers
	// have no holes, so this holds across kinds.
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	ingest := Ingest{State: IngestNotReceiving}
	messages, err := feed.Push(Change{
		Units:  []PatrolUnitView{unitView("SG-01")},
		Ingest: &ingest,
	}, sentAt)
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	revisions := make([]int64, 0, len(messages))
	for _, message := range messages {
		revisions = append(revisions, message.Revision)
	}
	if len(revisions) != 2 || revisions[0] != 2 || revisions[1] != 3 {
		t.Errorf("revisions = %v, want [2 3]", revisions)
	}
	if feed.Revision() != 3 {
		t.Errorf("feed revision = %d, want 3", feed.Revision())
	}
}

func TestTheIntakeStateIsSentBeforeTheUnitsItExplains(t *testing.T) {
	// A client that learns "these units went quiet" before "we are not
	// receiving" would have to interpret the silence without the fact that
	// explains it.
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	ingest := Ingest{State: IngestNotReceiving}
	messages, err := feed.Push(Change{
		Units:  []PatrolUnitView{unitView("SG-01")},
		Ingest: &ingest,
	}, sentAt)
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Kind != MessageIngest || messages[1].Kind != MessageUnits {
		t.Errorf("order = %s then %s, want INGEST then UNITS", messages[0].Kind, messages[1].Kind)
	}
}

func TestNothingIsSentForAChangeThatChangedNothing(t *testing.T) {
	// Duplicates and measurements older than what is stored apply nothing, and
	// they are the common case. A message for them would move the client's
	// revision and shake the screen for no reason.
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	messages, err := feed.Push(Change{}, sentAt)
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("messages = %+v, want none", messages)
	}
	if feed.Revision() != 1 {
		t.Errorf("revision = %d, want 1: an empty change spends no revision", feed.Revision())
	}
}

func TestAUnitsMessageCarriesOnlyWhatChanged(t *testing.T) {
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(unitView("SG-01"), unitView("SG-02"), unitView("SG-03")), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	messages, err := feed.Push(Change{Units: []PatrolUnitView{unitView("SG-02")}}, sentAt)
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	if len(messages) != 1 || len(messages[0].Units) != 1 || messages[0].Units[0].Callsign != "SG-02" {
		t.Errorf("messages = %+v, want only SG-02", messages)
	}
	if messages[0].Picture != nil {
		t.Error("a delta must not carry the whole picture")
	}
}

func TestAResyncSendsTheWholePictureWithoutRewinding(t *testing.T) {
	// The client takes the snapshot's revision as its new expectation. Rewinding
	// the server's counter instead would leave a client that missed one message
	// permanently out of step.
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := feed.Push(Change{Units: []PatrolUnitView{unitView("SG-01")}}, sentAt); err != nil {
		t.Fatalf("push: %v", err)
	}

	resync, err := feed.Snapshot(pictureWith(unitView("SG-01")), sentAt)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	if resync.Kind != MessageSnapshot || resync.Revision != 3 {
		t.Errorf("resync = %s at revision %d, want a snapshot at 3", resync.Kind, resync.Revision)
	}
}

func TestAClientThatCannotKeepUpIsDroppedAndStaysDropped(t *testing.T) {
	// Trimming its messages to keep it attached would leave it showing a
	// picture built on changes it never saw.
	feed := NewFeed()
	if _, err := feed.Snapshot(pictureWith(), sentAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	feed.Close(CloseTooSlow)

	if _, err := feed.Push(Change{Units: []PatrolUnitView{unitView("SG-01")}}, sentAt); !errors.Is(err, ErrClosed) {
		t.Errorf("push after close = %v, want ErrClosed", err)
	}
	if _, err := feed.Snapshot(pictureWith(), sentAt); !errors.Is(err, ErrClosed) {
		t.Errorf("snapshot after close = %v, want ErrClosed", err)
	}

	closed, reason := feed.Closed()
	if !closed || reason != CloseTooSlow {
		t.Errorf("closed = %t reason = %s, want true and TOO_SLOW", closed, reason)
	}
}

func TestClosingTwiceKeepsWhyTheClientWasDropped(t *testing.T) {
	feed := NewFeed()
	feed.Close(CloseTooSlow)
	feed.Close(CloseGoingAway)

	if _, reason := feed.Closed(); reason != CloseTooSlow {
		t.Errorf("reason = %s, want TOO_SLOW kept: a later shutdown must not hide it", reason)
	}
}

func TestOnlyAnIntakeStateChangeIsWorthAMessage(t *testing.T) {
	// The moment of the last event moves with every frame. Pushing that would
	// be a message per frame telling the operator nothing they can act on.
	receiving := Ingest{State: IngestReceiving, LastEventAt: sentAt}
	later := Ingest{State: IngestReceiving, LastEventAt: sentAt.Add(time.Second)}
	broken := Ingest{State: IngestNotReceiving, LastEventAt: sentAt}

	if IngestChanged(receiving, later) {
		t.Error("a newer last event is not a state change")
	}
	if !IngestChanged(receiving, broken) {
		t.Error("receiving to not receiving is a state change")
	}
	// An instance that cannot tell must announce that too, rather than letting
	// the client keep believing the path is healthy.
	if !IngestChanged(receiving, Ingest{State: IngestUnknown}) {
		t.Error("receiving to unknown is a state change")
	}
	if IngestChanged(Ingest{}, Ingest{State: IngestUnknown}) {
		t.Error("an unset state is already unknown, so this is no change")
	}
}
