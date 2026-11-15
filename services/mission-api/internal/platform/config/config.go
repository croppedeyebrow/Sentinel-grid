// Package config reads the process configuration from the environment.
package config

import (
	"os"
	"strings"
)

// Config is everything the API needs to start.
type Config struct {
	APIPort        string
	PostgresDSN    string
	AllowedOrigins []string
}

// Defaults match the local Compose setup described in the repository README,
// so `go run ./cmd/api` works without exporting anything by hand.
const (
	defaultAPIPort     = "8080"
	defaultPostgresDSN = "postgres://sentinel:change-me@localhost:5432/sentinel_grid?sslmode=disable"
)

var defaultAllowedOrigins = []string{"http://localhost:5173", "http://localhost:4173"}

// Load reads the configuration from the environment.
func Load() Config {
	config := Config{
		APIPort:        env("API_PORT", defaultAPIPort),
		PostgresDSN:    env("POSTGRES_DSN", defaultPostgresDSN),
		AllowedOrigins: defaultAllowedOrigins,
	}
	if origins := strings.TrimSpace(os.Getenv("CONSOLE_ORIGINS")); origins != "" {
		config.AllowedOrigins = splitOrigins(origins)
	}
	return config
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}
