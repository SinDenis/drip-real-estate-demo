// Package application contains infrastructure-independent use cases.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	regionCatalogVersion = "fns-ssrf-2025-10-01"
	regionCatalogSource  = "https://www.nalog.gov.ru/rn77/program/5961292/"
	complexCatalogTTL    = 24 * time.Hour
)

// CatalogFreshness tells the UI whether a complex catalog is inside its TTL.
type CatalogFreshness string

const (
	// CatalogFresh means the shared snapshot is inside its 24-hour TTL.
	CatalogFresh CatalogFreshness = "fresh"
	// CatalogStale means refresh failed after the shared snapshot expired.
	CatalogStale CatalogFreshness = "stale"
)

// ErrCatalogUnavailable means neither ЕИСЖС nor a cached snapshot can answer.
var ErrCatalogUnavailable = errors.New("residential complex catalog unavailable")

// ErrRegionNotFound means the code is not in the versioned region catalog.
var ErrRegionNotFound = errors.New("region not found")

// Region is a selectable Russian region with the code accepted by ЕИСЖС.
type Region struct {
	Code string
	Name string
}

// RegionCatalog is the versioned selectable region list and its official provenance.
type RegionCatalog struct {
	Version string
	Source  string
	Items   []Region
}

// Segment is a stable selectable real-estate category.
type Segment struct {
	Code            string
	Name            string
	EISZHSSupported bool
	EISZHSCategory  string
}

// ResidentialComplex is the stable ЕИСЖС identifier and full display name.
type ResidentialComplex struct {
	ID   string
	Name string
}

// ComplexCatalogPage is one durably reusable quota-bearing source page.
type ComplexCatalogPage struct {
	RegionCode string
	Page       int
	PageSize   int
	Total      int64
	Items      []ResidentialComplex
}

// ComplexCatalog is one immutable observation of a region's residential complexes.
type ComplexCatalog struct {
	RegionCode string
	Freshness  CatalogFreshness
	ObservedAt time.Time
	ExpiresAt  time.Time
	Warning    string
	Items      []ResidentialComplex
}

// CatalogRepository persists versioned reference and immutable complex snapshots.
type CatalogRepository interface {
	ListRegions(context.Context) ([]Region, error)
	FindRegion(context.Context, string) (bool, error)
	LatestComplexCatalog(context.Context, string) (ComplexCatalog, bool, error)
	AcquireComplexCatalogRefresh(context.Context, string) (func(), error)
	SaveComplexCatalog(context.Context, ComplexCatalog) error
}

// ComplexSource returns all residential complexes for one region.
type ComplexSource interface {
	Complexes(context.Context, string) ([]ResidentialComplex, error)
}

// CatalogService coordinates reference data and the bounded ЕИСЖС cache.
type CatalogService struct {
	repository CatalogRepository
	source     ComplexSource
	now        func() time.Time
}

// NewCatalogService creates the current-market catalog use case.
func NewCatalogService(
	repository CatalogRepository,
	source ComplexSource,
	now func() time.Time,
) *CatalogService {
	return &CatalogService{repository: repository, source: source, now: now}
}

// ListRegions returns the versioned selectable region catalog.
func (service *CatalogService) ListRegions(ctx context.Context) (RegionCatalog, error) {
	items, err := service.repository.ListRegions(ctx)
	if err != nil {
		return RegionCatalog{}, fmt.Errorf("list regions: %w", err)
	}
	return RegionCatalog{Version: regionCatalogVersion, Source: regionCatalogSource, Items: items}, nil
}

// ListSegments returns the six stable MVP segment codes and source support.
func (*CatalogService) ListSegments() []Segment {
	return []Segment{
		{Code: "residential", Name: "Жилые помещения", EISZHSSupported: true, EISZHSCategory: "living"},
		{Code: "apartments", Name: "Апартаменты"},
		{Code: "non_residential", Name: "Нежилые помещения", EISZHSSupported: true, EISZHSCategory: "nonliving"},
		{Code: "storage", Name: "Кладовые"},
		{Code: "parking", Name: "Машино-места", EISZHSSupported: true, EISZHSCategory: "parking"},
		{Code: "industrial_warehouse", Name: "Производственно-складская недвижимость"},
	}
}

// ListComplexes returns a fresh catalog, or a clearly marked stale fallback.
//
//nolint:funlen,gocyclo // Fresh, coordinated refresh, stale, and no-cache outcomes expose one policy.
func (service *CatalogService) ListComplexes(
	ctx context.Context,
	regionCode string,
) (ComplexCatalog, error) {
	found, err := service.repository.FindRegion(ctx, regionCode)
	if err != nil {
		return ComplexCatalog{}, fmt.Errorf("find region: %w", err)
	}
	if !found {
		return ComplexCatalog{}, ErrRegionNotFound
	}
	now := service.now().UTC()
	cached, hasCache, err := service.repository.LatestComplexCatalog(ctx, regionCode)
	if err != nil {
		return ComplexCatalog{}, fmt.Errorf("load residential complex catalog: %w", err)
	}
	if hasCache && now.Before(cached.ExpiresAt) {
		cached.Freshness = CatalogFresh
		return cached, nil
	}
	release, err := service.repository.AcquireComplexCatalogRefresh(ctx, regionCode)
	if err != nil {
		return ComplexCatalog{}, fmt.Errorf("acquire residential complex catalog refresh: %w", err)
	}
	defer release()
	cached, hasCache, err = service.repository.LatestComplexCatalog(ctx, regionCode)
	if err != nil {
		return ComplexCatalog{}, fmt.Errorf("reload residential complex catalog: %w", err)
	}
	if hasCache && now.Before(cached.ExpiresAt) {
		cached.Freshness = CatalogFresh
		return cached, nil
	}
	items, sourceErr := service.source.Complexes(ctx, regionCode)
	if sourceErr == nil {
		catalog := ComplexCatalog{
			RegionCode: regionCode, Freshness: CatalogFresh, Items: items,
			ObservedAt: now, ExpiresAt: now.Add(complexCatalogTTL),
		}
		if err := service.repository.SaveComplexCatalog(ctx, catalog); err != nil {
			return ComplexCatalog{}, fmt.Errorf("save residential complex catalog: %w", err)
		}
		return catalog, nil
	}
	if hasCache {
		cached.Freshness = CatalogStale
		cached.Warning = "ЕИСЖС недоступна. Показан устаревший справочник ЖК"
		return cached, nil
	}
	return ComplexCatalog{}, fmt.Errorf("%w: %w", ErrCatalogUnavailable, sourceErr)
}
