package httpapi

import (
	"context"
	"errors"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi/generated"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func (service server) ListRegions(
	ctx context.Context,
	request generated.ListRegionsRequestObject,
) (generated.ListRegionsResponseObject, error) {
	if err := service.requireCatalogAccess(ctx, request.Params.AdSession); err != nil {
		if isCatalogUnauthorized(err) {
			return generated.ListRegions401JSONResponse{
				UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
			}, nil
		}
		return nil, err
	}
	catalog, err := service.dependencies.Catalog.ListRegions(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]generated.Region, 0, len(catalog.Items))
	for _, region := range catalog.Items {
		items = append(items, generated.Region{Code: region.Code, Name: region.Name})
	}
	return generated.ListRegions200JSONResponse{
		Version: catalog.Version, Source: catalog.Source, Items: items,
	}, nil
}

func (service server) ListSegments(
	ctx context.Context,
	request generated.ListSegmentsRequestObject,
) (generated.ListSegmentsResponseObject, error) {
	if err := service.requireCatalogAccess(ctx, request.Params.AdSession); err != nil {
		if isCatalogUnauthorized(err) {
			return generated.ListSegments401JSONResponse{
				UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
			}, nil
		}
		return nil, err
	}
	segments := service.dependencies.Catalog.ListSegments()
	items := make([]generated.Segment, 0, len(segments))
	for _, segment := range segments {
		item := generated.Segment{
			Code: generated.SegmentCode(segment.Code), Name: segment.Name,
			EiszhsSupported: segment.EISZHSSupported,
		}
		if segment.EISZHSCategory != "" {
			category := generated.SegmentEiszhsCategory(segment.EISZHSCategory)
			item.EiszhsCategory = &category
		}
		items = append(items, item)
	}
	return generated.ListSegments200JSONResponse{Items: items}, nil
}

//nolint:funlen // Public fresh, stale, missing-region, and retry responses remain auditable together.
func (service server) ListResidentialComplexes(
	ctx context.Context,
	request generated.ListResidentialComplexesRequestObject,
) (generated.ListResidentialComplexesResponseObject, error) {
	if err := service.requireCatalogAccess(ctx, request.Params.AdSession); err != nil {
		if isCatalogUnauthorized(err) {
			return generated.ListResidentialComplexes401JSONResponse{
				UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
			}, nil
		}
		return nil, err
	}
	catalog, err := service.dependencies.Catalog.ListComplexes(ctx, request.RegionCode)
	if errors.Is(err, application.ErrRegionNotFound) {
		return generated.ListResidentialComplexes404JSONResponse{
			NotFoundJSONResponse: generated.NotFoundJSONResponse(
				errorResponse("region_not_found", "Регион не найден"),
			),
		}, nil
	}
	if errors.Is(err, application.ErrCatalogUnavailable) {
		retryAfter := 6
		return generated.ListResidentialComplexes503JSONResponse{
			Body: errorResponse(
				"complex_catalog_unavailable",
				"Справочник ЖК временно недоступен. Повторите запрос",
			),
			Headers: generated.ListResidentialComplexes503ResponseHeaders{RetryAfter: &retryAfter},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.ResidentialComplex, 0, len(catalog.Items))
	for _, item := range catalog.Items {
		items = append(items, generated.ResidentialComplex{ComplexId: item.ID, Name: item.Name})
	}
	response := generated.ListResidentialComplexes200JSONResponse{
		Freshness:  generated.ResidentialComplexCatalogFreshness(catalog.Freshness),
		ObservedAt: catalog.ObservedAt, ExpiresAt: catalog.ExpiresAt, Items: items,
	}
	if catalog.Warning != "" {
		response.Warning = &catalog.Warning
	}
	return response, nil
}

func (service server) requireCatalogAccess(
	ctx context.Context,
	cookie *generated.SessionCookie,
) error {
	if service.dependencies.Access == nil || service.dependencies.Catalog == nil {
		return application.ErrUnauthenticated
	}
	account, err := service.dependencies.Access.CurrentAccount(ctx, cookieValue(cookie))
	if err != nil {
		return err
	}
	if account.State == application.AccountPendingActivation {
		return application.ErrPasswordChangeNeeded
	}
	return nil
}

func isCatalogUnauthorized(err error) bool {
	return errors.Is(err, application.ErrUnauthenticated) ||
		errors.Is(err, application.ErrPasswordChangeNeeded)
}
