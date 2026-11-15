// Package postgres owns the connection pool shared by the API's adapters.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool limits are stated explicitly rather than left to defaults, so the
// database load of one API instance is predictable.
const (
	maxConnections  = 10
	minConnections  = 1
	connectTimeout  = 5 * time.Second
	maxConnLifetime = time.Hour
	maxConnIdleTime = 15 * time.Minute
)

// NewPool prepares a connection pool. Connections are established lazily, so
// the API still starts while the database is down and reports it through the
// readiness probe instead of crash-looping.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	config.MaxConns = maxConnections
	config.MinConns = minConnections
	config.MaxConnLifetime = maxConnLifetime
	config.MaxConnIdleTime = maxConnIdleTime
	config.ConnConfig.ConnectTimeout = connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	return pool, nil
}
