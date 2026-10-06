//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/postgres"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func TestPostgreSQLCoordinatesEISZHSRequestsAcrossStores(t *testing.T) {
	requireAuthenticatedPhase(t)
	first, second := openCatalogStores(t)
	database := openDatabase(t)
	defer database.Close(t.Context())
	if _, err := database.Exec(t.Context(), `
		UPDATE source_request_gate SET next_allowed_at = clock_timestamp()
		WHERE source_key = 'eiszhs'`); err != nil {
		t.Fatalf("reset ЕИСЖС request gate: %v", err)
	}
	start := make(chan struct{})
	completed := make(chan time.Time, 2)
	for _, store := range []*postgres.Store{first, second} {
		go func() {
			<-start
			if err := store.WaitForEISZHSRequest(t.Context()); err != nil {
				t.Errorf("wait for ЕИСЖС request gate: %v", err)
			}
			completed <- time.Now()
		}()
	}
	close(start)
	times := []time.Time{<-completed, <-completed}
	sort.Slice(times, func(left, right int) bool { return times[left].Before(times[right]) })
	if gap := times[1].Sub(times[0]); gap < 5500*time.Millisecond {
		t.Fatalf("cross-store request gap=%s, want at least 5.5s", gap)
	}
}

func TestPostgreSQLLeasePreventsDuplicateSameRegionRefresh(t *testing.T) {
	requireAuthenticatedPhase(t)
	first, second := openCatalogStores(t)
	database := openDatabase(t)
	defer database.Close(t.Context())
	if _, err := database.Exec(t.Context(), `
		DELETE FROM residential_complex_catalog_item
		WHERE snapshot_id IN (
			SELECT id FROM residential_complex_catalog_snapshot WHERE region_code = '78'
		);
		DELETE FROM residential_complex_catalog_snapshot WHERE region_code = '78';
		DELETE FROM residential_complex_catalog_page_checkpoint WHERE region_code = '78'`); err != nil {
		t.Fatalf("clear region refresh fixtures: %v", err)
	}
	source := &countedComplexSource{}
	services := []*application.CatalogService{
		application.NewCatalogService(first, source, time.Now),
		application.NewCatalogService(second, source, time.Now),
	}
	start := make(chan struct{})
	results := make(chan error, len(services))
	for _, service := range services {
		go func() {
			<-start
			catalog, err := service.ListComplexes(t.Context(), "78")
			if err == nil && (catalog.Freshness != application.CatalogFresh || len(catalog.Items) != 1) {
				t.Errorf("concurrent catalog=%#v, want one fresh item", catalog)
			}
			results <- err
		}()
	}
	close(start)
	for range services {
		if err := <-results; err != nil {
			t.Fatalf("concurrent catalog refresh: %v", err)
		}
	}
	if source.calls.Load() != 1 {
		t.Fatalf("source calls=%d, want one PostgreSQL-coordinated refresh", source.calls.Load())
	}
}

func TestPostgreSQLKeepsPartialPagesWithoutPublishingFreshSnapshot(t *testing.T) {
	requireAuthenticatedPhase(t)
	first, second := openCatalogStores(t)
	database := openDatabase(t)
	defer database.Close(t.Context())
	if _, err := database.Exec(t.Context(), `
		DELETE FROM residential_complex_catalog_item
		WHERE snapshot_id IN (
			SELECT id FROM residential_complex_catalog_snapshot WHERE region_code = '80'
		);
		DELETE FROM residential_complex_catalog_snapshot WHERE region_code = '80';
		DELETE FROM residential_complex_catalog_page_checkpoint WHERE region_code = '80'`); err != nil {
		t.Fatalf("clear page checkpoint fixtures: %v", err)
	}
	page := application.ComplexCatalogPage{
		RegionCode: "80", Page: 0, PageSize: 50, Total: 51,
		Items: []application.ResidentialComplex{{ID: "partial-1", Name: "Частичный ЖК"}},
	}
	if err := first.SaveComplexCatalogPage(t.Context(), page); err != nil {
		t.Fatalf("save partial page: %v", err)
	}
	pages, err := second.LoadComplexCatalogPages(t.Context(), "80")
	if err != nil || len(pages) != 1 || pages[0].Items[0].ID != "partial-1" {
		t.Fatalf("durable pages=%#v err=%v", pages, err)
	}
	if catalog, published, err := second.LatestComplexCatalog(t.Context(), "80"); err != nil || published {
		t.Fatalf("partial page published=%t catalog=%#v err=%v", published, catalog, err)
	}
}

func openCatalogStores(t *testing.T) (*postgres.Store, *postgres.Store) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	first, err := postgres.NewStore(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open first catalog store: %v", err)
	}
	t.Cleanup(first.Close)
	second, err := postgres.NewStore(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open second catalog store: %v", err)
	}
	t.Cleanup(second.Close)
	return first, second
}

type countedComplexSource struct {
	calls atomic.Int32
}

func (source *countedComplexSource) Complexes(
	ctx context.Context,
	_ string,
) ([]application.ResidentialComplex, error) {
	source.calls.Add(1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return []application.ResidentialComplex{{ID: "shared-1", Name: "Общий ЖК"}}, nil
	}
}
