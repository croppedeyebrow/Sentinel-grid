package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthEndpoints(t *testing.T) {
	for _, path := range []string{"/health/live", "/health/ready"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		newHandler().ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("%s returned status %d", path, response.Code)
		}
	}
}

func TestOperationalPictureFixture(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operational-picture", nil)
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}

	var payload operationalPictureResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Units) != 4 {
		t.Fatalf("expected 4 units, got %d", len(payload.Units))
	}

	seen := map[string]bool{}
	for _, unit := range payload.Units {
		seen[unit.Status] = true
	}
	for _, status := range []string{"NORMAL", "STALE", "MISSING"} {
		if !seen[status] {
			t.Errorf("fixture does not contain %s", status)
		}
	}
}

func TestFixtureUsesObservationAndReceiptTimes(t *testing.T) {
	payload := fixture(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC))
	if payload.Units[0].ObservedAt == nil || payload.Units[0].ReceivedAt == nil {
		t.Fatal("normal unit must contain observation and receipt times")
	}
	if payload.Units[3].ObservedAt != nil {
		t.Fatal("disconnected unit should have a missing observation time")
	}
}
