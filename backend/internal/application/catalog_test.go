package application_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func TestCatalogReturnsStableRegionsAndSegments(t *testing.T) {
	t.Parallel()
	repository := &catalogRepository{regions: []application.Region{{Code: "77", Name: "Москва"}}}
	service := application.NewCatalogService(repository, &complexSource{}, fixedCatalogNow)

	regions, err := service.ListRegions(t.Context())
	if err != nil || len(regions.Items) != 1 || regions.Items[0].Code != "77" {
		t.Fatalf("regions = %#v, err = %v", regions, err)
	}
	segments := service.ListSegments()
	if len(segments) != 6 || !segments[0].EISZHSSupported || segments[1].EISZHSSupported {
		t.Fatalf("segments = %#v, want six stable support declarations", segments)
	}
}

func TestCatalogRefreshesComplexesAndFallsBackToStaleSnapshot(t *testing.T) {
	t.Parallel()
	stale := application.ComplexCatalog{
		RegionCode: "77", ObservedAt: fixedCatalogNow().Add(-25 * time.Hour),
		ExpiresAt: fixedCatalogNow().Add(-time.Hour),
		Items:     []application.ResidentialComplex{{ID: "old-1", Name: "Старый ЖК"}},
	}
	repository := &catalogRepository{
		regions: []application.Region{{Code: "77", Name: "Москва"}}, snapshot: &stale,
	}
	source := &complexSource{err: errors.New("source unavailable")}
	service := application.NewCatalogService(repository, source, fixedCatalogNow)

	catalog, err := service.ListComplexes(t.Context(), "77")
	if err != nil || catalog.Freshness != application.CatalogStale || catalog.Warning == "" {
		t.Fatalf("stale catalog = %#v, err = %v", catalog, err)
	}

	source.err = nil
	source.items = []application.ResidentialComplex{{ID: "new-1", Name: "Новый ЖК"}}
	catalog, err = service.ListComplexes(t.Context(), "77")
	if err != nil || catalog.Freshness != application.CatalogFresh || catalog.Items[0].ID != "new-1" {
		t.Fatalf("refreshed catalog = %#v, err = %v", catalog, err)
	}
	if repository.saved == nil || !repository.saved.ExpiresAt.Equal(fixedCatalogNow().Add(24*time.Hour)) {
		t.Fatalf("saved snapshot = %#v", repository.saved)
	}
}

func TestCatalogReportsUnavailableWithoutSnapshot(t *testing.T) {
	t.Parallel()
	service := application.NewCatalogService(
		&catalogRepository{regions: []application.Region{{Code: "77", Name: "Москва"}}},
		&complexSource{err: errors.New("source unavailable")}, fixedCatalogNow,
	)

	_, err := service.ListComplexes(t.Context(), "77")
	if !errors.Is(err, application.ErrCatalogUnavailable) {
		t.Fatalf("error = %v, want ErrCatalogUnavailable", err)
	}
	_, err = service.ListComplexes(t.Context(), "99")
	if !errors.Is(err, application.ErrRegionNotFound) {
		t.Fatalf("unknown region error = %v, want ErrRegionNotFound", err)
	}
}

//nolint:funlen // The concurrent callers and shared source form one observable refresh scenario.
func TestCatalogCoordinatesConcurrentRefreshesPerRegion(t *testing.T) {
	t.Parallel()
	repository := &catalogRepository{
		regions: []application.Region{{Code: "77", Name: "Москва"}},
	}
	source := &complexSource{
		items: []application.ResidentialComplex{{ID: "new-1", Name: "Новый ЖК"}},
		delay: 20 * time.Millisecond,
	}
	services := []*application.CatalogService{
		application.NewCatalogService(repository, source, fixedCatalogNow),
		application.NewCatalogService(repository, source, fixedCatalogNow),
	}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for _, service := range services {
		go func() {
			<-start
			catalog, err := service.ListComplexes(t.Context(), "77")
			if err == nil && (catalog.Freshness != application.CatalogFresh || len(catalog.Items) != 1) {
				err = errors.New("concurrent refresh returned an incomplete catalog")
			}
			results <- err
		}()
	}
	close(start)
	for range services {
		if err := <-results; err != nil {
			t.Fatalf("concurrent refresh: %v", err)
		}
	}
	if source.calls.Load() != 1 {
		t.Fatalf("source calls=%d, want one coordinated refresh", source.calls.Load())
	}
}

type catalogRepository struct {
	mu        sync.Mutex
	refreshMu sync.Mutex
	regions   []application.Region
	snapshot  *application.ComplexCatalog
	saved     *application.ComplexCatalog
}

func (repository *catalogRepository) ListRegions(context.Context) ([]application.Region, error) {
	return repository.regions, nil
}

func (repository *catalogRepository) FindRegion(_ context.Context, code string) (bool, error) {
	for _, region := range repository.regions {
		if region.Code == code {
			return true, nil
		}
	}
	return false, nil
}

func (repository *catalogRepository) LatestComplexCatalog(
	context.Context, string,
) (application.ComplexCatalog, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.snapshot == nil {
		return application.ComplexCatalog{}, false, nil
	}
	return *repository.snapshot, true, nil
}

func (repository *catalogRepository) AcquireComplexCatalogRefresh(
	context.Context,
	string,
) (func(), error) {
	repository.refreshMu.Lock()
	return repository.refreshMu.Unlock, nil
}

func (repository *catalogRepository) SaveComplexCatalog(
	_ context.Context, catalog application.ComplexCatalog,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.saved = &catalog
	repository.snapshot = &catalog
	return nil
}

type complexSource struct {
	items []application.ResidentialComplex
	err   error
	delay time.Duration
	calls atomic.Int32
}

func (source *complexSource) Complexes(context.Context, string) ([]application.ResidentialComplex, error) {
	source.calls.Add(1)
	time.Sleep(source.delay)
	return source.items, source.err
}

func fixedCatalogNow() time.Time {
	return time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
}
