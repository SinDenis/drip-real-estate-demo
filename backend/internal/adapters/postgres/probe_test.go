package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/postgres"
)

func TestNewRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	if _, err := postgres.New(t.Context(), "://invalid"); err == nil {
		t.Fatal("expected invalid database URL to fail")
	}
}

func TestProbeReportsUnavailableDatabase(t *testing.T) {
	t.Parallel()

	probe, err := postgres.New(t.Context(), "postgres://app:secret@127.0.0.1:1/app")
	if err != nil {
		t.Fatalf("configure probe: %v", err)
	}
	t.Cleanup(probe.Close)
	ctx, cancel := contextWithTimeout(t, 200*time.Millisecond)
	defer cancel()
	if err := probe.Check(ctx); err == nil {
		t.Fatal("expected unavailable database to fail readiness")
	}
}

func contextWithTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), timeout)
}
