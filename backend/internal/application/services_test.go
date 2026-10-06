package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func TestReadinessReturnsDependencyFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("dependency unavailable")
	service := application.NewReadinessService(staticProbe{}, staticProbe{err: expected})
	if err := service.Check(t.Context()); !errors.Is(err, expected) {
		t.Fatalf("readiness error = %v, want %v", err, expected)
	}
}

func TestReadinessAcceptsAvailableDependencies(t *testing.T) {
	t.Parallel()

	service := application.NewReadinessService(staticProbe{}, staticProbe{})
	if err := service.Check(t.Context()); err != nil {
		t.Fatalf("readiness error = %v, want nil", err)
	}
}

type staticProbe struct {
	err error
}

func (probe staticProbe) Check(context.Context) error {
	return probe.err
}
