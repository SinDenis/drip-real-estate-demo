// Package postgres adapts PostgreSQL connectivity to application probes.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Probe checks connectivity through a PostgreSQL connection pool.
type Probe struct {
	pool *pgxpool.Pool
}

// New configures a PostgreSQL readiness probe without opening a connection.
func New(ctx context.Context, databaseURL string) (*Probe, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	return &Probe{pool: pool}, nil
}

// Check verifies that PostgreSQL accepts a round trip.
func (probe *Probe) Check(ctx context.Context) error {
	if err := probe.pool.Ping(ctx); err != nil {
		return fmt.Errorf("check PostgreSQL: %w", err)
	}
	return nil
}

// Close releases the underlying PostgreSQL pool.
func (probe *Probe) Close() {
	probe.pool.Close()
}
