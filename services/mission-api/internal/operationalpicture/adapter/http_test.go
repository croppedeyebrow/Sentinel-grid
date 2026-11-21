package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/operationalpicture/domain"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/contracts"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/httpx"
)

var generatedAt = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

type stubService struct {
	picture domain.OperationalPicture
	err     error
}

func (s stubService) OperationalPicture(context.Context) (domain.OperationalPicture, error) {
	if s.err != nil {
		return domain.OperationalPicture{}, s.err
	}
	return s.picture, nil
}

type stubProbe struct{ err error }

func (p stubProbe) Ping(context.Context) error { return p.err }

func at(offset time.Duration) *time.Time {
	t := generatedAt.Add(offset)
	return &t
}

func value[T any](v T) *T { return &v }

func picture() domain.OperationalPicture {
	fresh := func(status domain.UnitStatus, callsign string, battery, link int) domain.PatrolUnitView {
		return domain.PatrolUnitView{
			UnitID:      callsign,
			Callsign:    callsign,
			DisplayName: callsign,
			Status:      status,
			Position: domain.Evaluated[domain.Position]{
				Value:      value(domain.Position{Latitude: 37.5665, Longitude: 126.978}),
				ObservedAt: at(-5 * time.Second), ReceivedAt: at(0), Freshness: domain.FreshnessFresh,
			},
			Battery: domain.Evaluated[int]{Value: value(battery), ObservedAt: at(-5 * time.Second), ReceivedAt: at(0), Freshness: domain.FreshnessFresh},
			Link:    domain.Evaluated[int]{Value: value(link), ObservedAt: at(-5 * time.Second), ReceivedAt: at(0), Freshness: domain.FreshnessFresh},
		}
	}

	stale := fresh(domain.UnitStatusUnknown, "SG-02", 61, 72)
	stale.Position.Freshness = domain.FreshnessStale
	stale.Battery.Freshness = domain.FreshnessStale
	stale.Link.Freshness = domain.FreshnessStale

	disconnected := domain.PatrolUnitView{
		UnitID: "SG-04", Callsign: "SG-04", DisplayName: "Sentinel Four", Status: domain.UnitStatusUnknown,
		Position: domain.Evaluated[domain.Position]{ReceivedAt: at(-40 * time.Minute), Freshness: domain.FreshnessMissing},
		Battery:  domain.Evaluated[int]{Value: value(37), ReceivedAt: at(-40 * time.Minute), Freshness: domain.FreshnessMissing},
		Link:     domain.Evaluated[int]{Value: value(0), ReceivedAt: at(-40 * time.Minute), Freshness: domain.FreshnessMissing},
	}

	return domain.OperationalPicture{
		GeneratedAt: generatedAt,
		Site: domain.SiteProfile{
			ID: "site-1", Name: "Sentinel Test Site",
			Center: domain.Position{Latitude: 37.5665, Longitude: 126.978},
			Policy: domain.ObservationPolicy{
				PositionWindow: 45 * time.Second, BatteryWindow: 180 * time.Second, LinkWindow: 180 * time.Second,
				BatteryWarningPercent: 40, BatteryCriticalPercent: 20,
				LinkWarningPercent: 50, LinkCriticalPercent: 20,
			},
		},
		PatrolAreas: []domain.PatrolArea{{
			ID: "area-1", Name: "Primary Patrol Area",
			Boundary: []domain.Position{
				{Latitude: 37.558, Longitude: 126.965},
				{Latitude: 37.558, Longitude: 126.991},
				{Latitude: 37.575, Longitude: 126.991},
				{Latitude: 37.558, Longitude: 126.965},
			},
		}},
		Routes: []domain.PatrolRoute{{
			ID: "route-1", PatrolAreaID: "area-1", Name: "Riverside Loop",
			Path: []domain.Position{{Latitude: 37.56, Longitude: 126.969}, {Latitude: 37.566, Longitude: 126.985}},
		}},
		Units: []domain.PatrolUnitView{
			fresh(domain.UnitStatusNormal, "SG-01", 82, 94),
			stale,
			fresh(domain.UnitStatusWarning, "SG-05", 35, 88),
			fresh(domain.UnitStatusCritical, "SG-06", 14, 62),
			disconnected,
		},
	}
}

// handlerFor assembles the service the way main does: endpoints on one mux,
// with the cross-origin rules applied once around it.
func handlerFor(service PictureService, probe ReadinessProbe) http.Handler {
	mux := http.NewServeMux()
	NewHandler(service, probe, nil).Register(mux)
	return httpx.WithCORS(mux, []string{"http://localhost:5173"})
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestLivenessIgnoresDependencies(t *testing.T) {
	handler := handlerFor(stubService{picture: picture()}, stubProbe{err: errors.New("database down")})

	response := get(t, handler, "/health/live")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a dependency outage must not look like a dead process", response.Code)
	}
}

func TestReadinessFollowsTheProjectionStore(t *testing.T) {
	cases := []struct {
		name  string
		probe stubProbe
		want  int
	}{
		{"store reachable", stubProbe{}, http.StatusOK},
		{"store unreachable", stubProbe{err: errors.New("connection refused")}, http.StatusServiceUnavailable},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := get(t, handlerFor(stubService{picture: picture()}, testCase.probe), "/health/ready")

			if response.Code != testCase.want {
				t.Errorf("status = %d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestOperationalPictureKeepsStatusAndFreshnessSeparate(t *testing.T) {
	response := get(t, handlerFor(stubService{picture: picture()}, stubProbe{}), "/api/v1/operational-picture")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var payload struct {
		Units []struct {
			Callsign string `json:"callsign"`
			Status   string `json:"status"`
			Position struct {
				Value     *struct{} `json:"value"`
				Freshness string    `json:"freshness"`
			} `json:"position"`
			Battery struct {
				Value     *int   `json:"value"`
				Freshness string `json:"freshness"`
			} `json:"battery"`
		} `json:"units"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byCallsign := map[string]string{}
	for _, unit := range payload.Units {
		byCallsign[unit.Callsign] = unit.Status + "/" + unit.Position.Freshness
	}
	for callsign, want := range map[string]string{
		"SG-01": "NORMAL/FRESH",
		"SG-02": "UNKNOWN/STALE",
		"SG-05": "WARNING/FRESH",
		"SG-06": "CRITICAL/FRESH",
		"SG-04": "UNKNOWN/MISSING",
	} {
		if got := byCallsign[callsign]; got != want {
			t.Errorf("%s status/freshness = %q, want %q", callsign, got, want)
		}
	}

	// A disconnected unit keeps its last received value while reporting that
	// the value can no longer be trusted.
	for _, unit := range payload.Units {
		if unit.Callsign != "SG-04" {
			continue
		}
		if unit.Battery.Value == nil || *unit.Battery.Value != 37 {
			t.Error("last known battery must survive in the payload")
		}
		if unit.Battery.Freshness != "MISSING" {
			t.Errorf("battery freshness = %s, want MISSING", unit.Battery.Freshness)
		}
	}
}

func TestOperationalPictureFailureIsReportedAsUnavailable(t *testing.T) {
	handler := handlerFor(stubService{err: errors.New("projection unavailable")}, stubProbe{})

	response := get(t, handler, "/api/v1/operational-picture")

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	handler := handlerFor(stubService{picture: picture()}, stubProbe{})

	cases := map[string]string{
		"http://localhost:5173": "http://localhost:5173",
		"http://evil.example":   "",
	}
	for origin, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/operational-picture", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s allowed as %q, want %q", origin, got, want)
		}
	}
}

// contractSchema compiles the published contract. The response and the
// committed example are both checked against it, so the schema cannot drift
// away from what the API actually serves.
func contractSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	schema, err := contracts.Compile("operational-picture")
	if err != nil {
		t.Fatalf("compile contract: %v", err)
	}
	return schema
}

func TestResponseMatchesPublishedContract(t *testing.T) {
	response := get(t, handlerFor(stubService{picture: picture()}, stubProbe{}), "/api/v1/operational-picture")

	instance, err := jsonschema.UnmarshalJSON(response.Body)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if err := contractSchema(t).Validate(instance); err != nil {
		t.Fatalf("response violates the published contract: %v", err)
	}
}

func TestCommittedExampleMatchesPublishedContract(t *testing.T) {
	instance, err := contracts.Example("operational-picture")
	if err != nil {
		t.Fatalf("read example: %v", err)
	}

	if err := contractSchema(t).Validate(instance); err != nil {
		t.Fatalf("example violates the published contract: %v", err)
	}
}
