package contracts_test

import (
	"testing"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/contracts"
)

// Every published example is validated against its schema from the Go side as
// well, so a contract change cannot land with only the console updated.
func TestPublishedExamplesMatchTheirSchemas(t *testing.T) {
	cases := []struct {
		example string
		schema  string
		refs    []string
	}{
		{example: "operational-picture", schema: "operational-picture"},
		{
			example: "patrol-mission-list",
			schema:  "patrol-mission-list",
			refs:    []string{"patrol-mission"},
		},
		{example: "patrol-mission-create-request", schema: "patrol-mission-create-request"},
		{example: "patrol-mission-transition-request", schema: "patrol-mission-transition-request"},
		{example: "api-error", schema: "api-error"},
		{example: "telemetry-frame", schema: "telemetry-frame"},
		{example: "telemetry-frame-partial", schema: "telemetry-frame"},
		{example: "telemetry-receipt", schema: "telemetry-receipt"},
		{example: "telemetry-event", schema: "telemetry-event"},
		{example: "telemetry-event-rejected", schema: "telemetry-event"},
		{example: "telemetry-error", schema: "telemetry-error"},
		{example: "telemetry-resume", schema: "telemetry-resume"},
	}

	for _, testCase := range cases {
		t.Run(testCase.example, func(t *testing.T) {
			schema, err := contracts.Compile(testCase.schema, testCase.refs...)
			if err != nil {
				t.Fatalf("compile schema: %v", err)
			}
			example, err := contracts.Example(testCase.example)
			if err != nil {
				t.Fatalf("read example: %v", err)
			}

			if err := schema.Validate(example); err != nil {
				t.Fatalf("example violates its schema: %v", err)
			}
		})
	}
}

func TestSchemasRejectAnUndeclaredField(t *testing.T) {
	schema, err := contracts.Compile("patrol-mission-transition-request")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if err := schema.Validate(map[string]any{"version": 3.0, "note": "why not"}); err == nil {
		t.Fatal("a request with an undeclared field must be refused")
	}
}

func TestTransitionRequestRequiresTheVersion(t *testing.T) {
	schema, err := contracts.Compile("patrol-mission-transition-request")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if err := schema.Validate(map[string]any{"reason": "BATTERY"}); err == nil {
		t.Fatal("a transition without the caller's version must be refused")
	}
}

// The telemetry contracts carry rules a type system cannot: a frame must
// report at least one sensor, a gap must say how much is missing, and a
// refusal must say why. These check the schema actually enforces them, because
// the gateway and the consumer both rely on that.
func TestTelemetryFrameSchemaRequiresASensor(t *testing.T) {
	schema, err := contracts.Compile("telemetry-frame")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	frame := map[string]any{
		"unitId":     "unit-1",
		"sessionId":  "session-1",
		"sequence":   1.0,
		"observedAt": "2026-11-26T09:00:00Z",
		"sensors":    map[string]any{},
	}

	if err := schema.Validate(frame); err == nil {
		t.Fatal("a frame reporting no sensor must be refused")
	}
}

func TestTelemetryFrameSchemaRefusesImpossibleValues(t *testing.T) {
	schema, err := contracts.Compile("telemetry-frame")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	cases := map[string]any{
		"battery above 100":    map[string]any{"battery": 150.0},
		"negative battery":     map[string]any{"battery": -1.0},
		"link above 100":       map[string]any{"link": 101.0},
		"latitude beyond pole": map[string]any{"position": map[string]any{"latitude": 95.0, "longitude": 126.9}},
		"undeclared sensor":    map[string]any{"battery": 80.0, "altitude": 120.0},
	}

	for name, sensors := range cases {
		t.Run(name, func(t *testing.T) {
			frame := map[string]any{
				"unitId":     "unit-1",
				"sessionId":  "session-1",
				"sequence":   1.0,
				"observedAt": "2026-11-26T09:00:00Z",
				"sensors":    sensors,
			}

			if err := schema.Validate(frame); err == nil {
				t.Fatalf("the schema must refuse %s", name)
			}
		})
	}
}

func TestTelemetryReceiptTiesMissingFramesToAGap(t *testing.T) {
	schema, err := contracts.Compile("telemetry-receipt")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	receipt := func(outcome string, missing any) map[string]any {
		body := map[string]any{
			"outcome":              outcome,
			"unitId":               "unit-1",
			"sessionId":            "session-1",
			"sequence":             7.0,
			"receivedAt":           "2026-11-26T09:00:00Z",
			"lastAcceptedSequence": 7.0,
		}
		if missing != nil {
			body["missingFrames"] = missing
		}
		return body
	}

	if err := schema.Validate(receipt("GAP", nil)); err == nil {
		t.Error("a gap must say how many frames are missing")
	}
	if err := schema.Validate(receipt("ACCEPTED", 2.0)); err == nil {
		t.Error("only a gap may report missing frames")
	}
	if err := schema.Validate(receipt("GAP", 2.0)); err != nil {
		t.Errorf("a gap with its missing count must be valid: %v", err)
	}
	if err := schema.Validate(receipt("ACCEPTED", nil)); err != nil {
		t.Errorf("an accepted frame without a missing count must be valid: %v", err)
	}
}

func TestTelemetryEventRequiresAReasonForARefusal(t *testing.T) {
	schema, err := contracts.Compile("telemetry-event")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	event := map[string]any{
		"eventId":    "event-1",
		"outcome":    "REJECTED",
		"receivedAt": "2026-11-26T09:00:00Z",
		"frameBytes": 120.0,
	}

	if err := schema.Validate(event); err == nil {
		t.Fatal("a refusal must carry the reason it was refused")
	}

	event["rejection"] = map[string]any{"code": "MALFORMED_FRAME", "detail": "bad json"}
	if err := schema.Validate(event); err != nil {
		t.Fatalf("a refusal with its reason must be valid: %v", err)
	}
}

func TestTelemetryEventRequiresTheFrameFieldsWhenItWasApplied(t *testing.T) {
	schema, err := contracts.Compile("telemetry-event")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	// An accepted event is what the projection acts on, so it cannot leave the
	// unit, session, sequence or measurement unknown.
	event := map[string]any{
		"eventId":    "event-1",
		"outcome":    "ACCEPTED",
		"receivedAt": "2026-11-26T09:00:00Z",
		"frameBytes": 120.0,
	}

	if err := schema.Validate(event); err == nil {
		t.Fatal("an accepted event must identify what it applies to")
	}
}
