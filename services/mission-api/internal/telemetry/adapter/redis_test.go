package adapter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

// The receipt log is the audit record of what arrived, so decoding has to
// preserve the published event rather than reconstruct it. Re-encoding a
// parsed struct is what put a null position into receipts for frames that
// never reported one.

func entry(payload string) redis.XMessage {
	return redis.XMessage{ID: "1-0", Values: map[string]any{"event": payload}}
}

func TestDecodeKeepsTheSensorsExactlyAsPublished(t *testing.T) {
	receipt, err := decodeReceipt(entry(`{
      "eventId": "e1",
      "outcome": "ACCEPTED",
      "unitId": "unit-1",
      "sessionId": "session-1",
      "sequence": 7,
      "observedAt": "2026-11-28T09:00:00Z",
      "receivedAt": "2026-11-28T09:00:02Z",
      "sensors": {"battery": 63, "link": 83},
      "frameBytes": 140
    }`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if strings.Contains(string(receipt.SensorsJSON), "position") {
		t.Errorf("sensors = %s: a sensor the frame left out must be absent from the record",
			receipt.SensorsJSON)
	}
	if receipt.Position != nil {
		t.Error("an absent position must not become a value")
	}
	if receipt.Battery == nil || *receipt.Battery != 63 {
		t.Errorf("battery = %v, want 63", receipt.Battery)
	}

	// And what is stored has to stay readable as the published object.
	var stored map[string]any
	if err := json.Unmarshal(receipt.SensorsJSON, &stored); err != nil {
		t.Fatalf("stored sensors are not an object: %v", err)
	}
	if len(stored) != 2 {
		t.Errorf("stored sensors = %v, want only the two that arrived", stored)
	}
}

func TestDecodeReadsAFullFrame(t *testing.T) {
	receipt, err := decodeReceipt(entry(`{
      "eventId": "e2",
      "outcome": "GAP",
      "unitId": "unit-1",
      "sessionId": "session-1",
      "sequence": 9,
      "observedAt": "2026-11-28T09:00:10Z",
      "receivedAt": "2026-11-28T09:00:11Z",
      "sensors": {"position": {"latitude": 37.5665, "longitude": 126.978}, "battery": 60, "link": 80},
      "missingFrames": 2,
      "frameBytes": 190
    }`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if receipt.Position == nil || receipt.Position.Latitude != 37.5665 {
		t.Errorf("position = %+v, want the published coordinate", receipt.Position)
	}
	if receipt.MissingFrames != 2 {
		t.Errorf("missingFrames = %d, want 2", receipt.MissingFrames)
	}
	if receipt.Rejected {
		t.Error("a gap is not a rejection")
	}
}

func TestDecodeKeepsARefusalWithoutSensors(t *testing.T) {
	receipt, err := decodeReceipt(entry(`{
      "eventId": "e3",
      "outcome": "REJECTED",
      "unitId": null,
      "sessionId": null,
      "sequence": null,
      "observedAt": null,
      "receivedAt": "2026-11-28T09:00:20Z",
      "rejection": {"code": "FRAME_TOO_LARGE", "detail": "frame is 8300 bytes, limit is 4096"},
      "frameBytes": 8300
    }`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !receipt.Rejected || receipt.Rejection == nil || receipt.Rejection.Code != "FRAME_TOO_LARGE" {
		t.Errorf("rejection = %+v, want FRAME_TOO_LARGE recorded", receipt.Rejection)
	}
	if receipt.SensorsJSON != nil {
		t.Errorf("sensors = %s, want none: nothing was parsed out of the frame", receipt.SensorsJSON)
	}
	if receipt.UnitID != "" {
		t.Errorf("unitId = %q, want empty: the frame could not be attributed", receipt.UnitID)
	}
}

func TestDecodeRefusesAnEntryWithoutAnEvent(t *testing.T) {
	if _, err := decodeReceipt(redis.XMessage{ID: "1-0", Values: map[string]any{}}); err == nil {
		t.Error("an entry with no event field cannot be decoded")
	}
}
