package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/httpx"
)

func logged(handler http.Handler) (http.Handler, *bytes.Buffer) {
	var captured bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&captured, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return httpx.WithRequestLog(handler, logger), &captured
}

func lines(t *testing.T, captured *bytes.Buffer) []map[string]any {
	t.Helper()

	entries := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(captured.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestRequestLogStatesTheOutcome(t *testing.T) {
	handler, captured := logged(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"STALE_VERSION"}`))
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/missions/m1/approve", nil))

	entries := lines(t, captured)
	if len(entries) != 1 {
		t.Fatalf("log lines = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry["status"] != float64(http.StatusConflict) {
		t.Errorf("status = %v, want 409", entry["status"])
	}
	if entry["method"] != http.MethodPost || entry["path"] != "/api/v1/missions/m1/approve" {
		t.Errorf("entry = %v, want the request's method and path", entry)
	}
	// A refusal is an expected outcome, not an incident.
	if entry["level"] != "INFO" {
		t.Errorf("level = %v, want INFO for a refusal", entry["level"])
	}
	if entry["duration_ms"] == nil || entry["bytes"] == float64(0) {
		t.Errorf("entry = %v, want duration and written bytes", entry)
	}
}

func TestServiceFailuresAreLoggedLouder(t *testing.T) {
	handler, captured := logged(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/missions", nil))

	if entries := lines(t, captured); entries[0]["level"] != "WARN" {
		t.Errorf("level = %v, want WARN for a 5xx", entries[0]["level"])
	}
}

func TestRequestIDIsReturnedAndReadableByHandlers(t *testing.T) {
	var seen string
	handler, captured := logged(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = httpx.RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/missions", nil))

	header := response.Header().Get(httpx.RequestIDHeader)
	if header == "" {
		t.Fatal("every response must carry its request id")
	}
	if seen != header {
		t.Errorf("handler saw %q, response carries %q: they must match", seen, header)
	}
	if entries := lines(t, captured); entries[0]["request_id"] != header {
		t.Errorf("log request_id = %v, want %q", entries[0]["request_id"], header)
	}
}

func TestRequestIDsDiffer(t *testing.T) {
	handler, _ := logged(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ids := make(map[string]bool)
	for attempt := 0; attempt < 5; attempt++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/missions", nil))
		ids[response.Header().Get(httpx.RequestIDHeader)] = true
	}

	if len(ids) != 5 {
		t.Errorf("distinct ids = %d, want 5", len(ids))
	}
}

func TestStatusDefaultsToOKWhenTheHandlerNeverSetsIt(t *testing.T) {
	handler, captured := logged(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if entries := lines(t, captured); entries[0]["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", entries[0]["status"])
	}
}

func TestCORSAllowsOnlyTheConfiguredOrigins(t *testing.T) {
	handler := httpx.WithCORS(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		[]string{"http://localhost:5173"},
	)

	cases := map[string]string{
		"http://localhost:5173":  "http://localhost:5173",
		"http://localhost:51730": "",
		"http://evil.example":    "",
	}
	for origin, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/missions", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s allowed as %q, want %q", origin, got, want)
		}
	}
}

func TestCORSPreflightAnnouncesTheWriteMethodsAndIdempotencyKey(t *testing.T) {
	handler := httpx.WithCORS(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		[]string{"http://localhost:5173"},
	)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/missions", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	methods := response.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{"POST", "DELETE"} {
		if !strings.Contains(methods, method) {
			t.Errorf("methods = %q, want it to include %s", methods, method)
		}
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Error("the preflight must allow the idempotency key the console sends")
	}
}
