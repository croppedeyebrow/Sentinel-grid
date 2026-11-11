package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
}

type patrolUnit struct {
	ID           string   `json:"id"`
	Callsign     string   `json:"callsign"`
	DisplayName  string   `json:"displayName"`
	Status       string   `json:"status"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	BatteryLevel *int     `json:"batteryLevel"`
	LinkQuality  *int     `json:"linkQuality"`
	ObservedAt   *string  `json:"observedAt"`
	ReceivedAt   *string  `json:"receivedAt"`
}

type operationalPictureResponse struct {
	GeneratedAt string       `json:"generatedAt"`
	Units       []patrolUnit `json:"units"`
}

func main() {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("mission-api listening on http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "live"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
	})
	mux.HandleFunc("GET /api/v1/operational-picture", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, fixture(time.Now().UTC()))
	})

	return withCORS(mux)
}

func fixture(now time.Time) operationalPictureResponse {
	iso := func(value time.Time) *string {
		formatted := value.Format(time.RFC3339)
		return &formatted
	}
	float := func(value float64) *float64 { return &value }
	integer := func(value int) *int { return &value }

	return operationalPictureResponse{
		GeneratedAt: now.Format(time.RFC3339),
		Units: []patrolUnit{
			{ID: "sg-01", Callsign: "SG-01", DisplayName: "Sentinel One", Status: "NORMAL", Latitude: float(37.5665), Longitude: float(126.9780), BatteryLevel: integer(82), LinkQuality: integer(94), ObservedAt: iso(now.Add(-5 * time.Second)), ReceivedAt: iso(now)},
			{ID: "sg-02", Callsign: "SG-02", DisplayName: "Sentinel Two", Status: "STALE", Latitude: float(37.5685), Longitude: float(126.9820), BatteryLevel: integer(61), LinkQuality: integer(72), ObservedAt: iso(now.Add(-20 * time.Minute)), ReceivedAt: iso(now.Add(-20 * time.Minute))},
			{ID: "sg-03", Callsign: "SG-03", DisplayName: "Sentinel Three", Status: "MISSING", Latitude: float(37.5645), Longitude: float(126.9730), BatteryLevel: nil, LinkQuality: integer(88), ObservedAt: iso(now.Add(-10 * time.Second)), ReceivedAt: iso(now)},
			{ID: "sg-04", Callsign: "SG-04", DisplayName: "Sentinel Four", Status: "MISSING", Latitude: nil, Longitude: nil, BatteryLevel: integer(37), LinkQuality: integer(0), ObservedAt: nil, ReceivedAt: iso(now.Add(-40 * time.Minute))},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://localhost:4173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
