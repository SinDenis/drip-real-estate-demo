package application

import "context"

// Probe represents one required external dependency.
type Probe interface {
	Check(context.Context) error
}

// ReadinessService evaluates all required dependency probes.
type ReadinessService struct {
	probes []Probe
}

// NewReadinessService creates a readiness service for the supplied probes.
func NewReadinessService(probes ...Probe) ReadinessService {
	return ReadinessService{probes: probes}
}

// Check returns the first dependency failure or nil when all probes pass.
func (service ReadinessService) Check(ctx context.Context) error {
	for _, probe := range service.probes {
		if err := probe.Check(ctx); err != nil {
			return err
		}
	}
	return nil
}
