package httpapi

import (
	"context"
	"errors"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi/generated"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

//nolint:funlen // Request translation keeps the canonical identifier arrays visible together.
func (service server) CreateAnalyticalSession(
	ctx context.Context,
	request generated.CreateAnalyticalSessionRequestObject,
) (generated.CreateAnalyticalSessionResponseObject, error) {
	if request.Body == nil {
		return createSessionValidationResponse(), nil
	}
	segmentCodes := make([]string, 0, len(request.Body.SegmentCodes))
	for _, code := range request.Body.SegmentCodes {
		segmentCodes = append(segmentCodes, string(code))
	}
	session, _, err := service.dependencies.Sessions.Create(
		ctx,
		cookieValue(request.Params.AdSession),
		request.Params.IdempotencyKey,
		application.CreateSessionInput{
			RegionCodes:           request.Body.RegionCodes,
			ResidentialComplexIDs: request.Body.ResidentialComplexIds,
			SegmentCodes:          segmentCodes,
		},
	)
	if response := createSessionErrorResponse(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	detail, err := analyticalSessionDetail(session)
	if err != nil {
		return nil, err
	}
	return generated.CreateAnalyticalSession201JSONResponse{
		Body: detail,
		Headers: generated.CreateAnalyticalSession201ResponseHeaders{
			Location: "/api/v1/sessions/" + session.ID,
		},
	}, nil
}

func createSessionErrorResponse(err error) generated.CreateAnalyticalSessionResponseObject {
	if errors.Is(err, application.ErrUnauthenticated) ||
		errors.Is(err, application.ErrPasswordChangeNeeded) {
		return generated.CreateAnalyticalSession401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}
	}
	if errors.Is(err, application.ErrIdempotencyConflict) {
		return generated.CreateAnalyticalSession409JSONResponse{
			ConflictJSONResponse: generated.ConflictJSONResponse(
				errorResponse("idempotency_conflict", "Этот ключ уже использован с другими фильтрами"),
			),
		}
	}
	if errors.Is(err, application.ErrInvalidIdempotencyKey) ||
		errors.Is(err, application.ErrInvalidSessionSelection) {
		return createSessionValidationResponse()
	}
	return nil
}

func createSessionValidationResponse() generated.CreateAnalyticalSession422JSONResponse {
	return generated.CreateAnalyticalSession422JSONResponse{
		ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(
			errorResponse("invalid_session_filters", "Выберите допустимые регионы, ЖК и сегменты"),
		),
	}
}

func (service server) GetAnalyticalSession(
	ctx context.Context,
	request generated.GetAnalyticalSessionRequestObject,
) (generated.GetAnalyticalSessionResponseObject, error) {
	session, err := service.dependencies.Access.GetAnalyticalSession(
		ctx, cookieValue(request.Params.AdSession), request.SessionId.String(),
	)
	if errors.Is(err, application.ErrUnauthenticated) ||
		errors.Is(err, application.ErrPasswordChangeNeeded) {
		return generated.GetAnalyticalSession401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}, nil
	}
	if errors.Is(err, application.ErrNotFound) {
		return generated.GetAnalyticalSession404JSONResponse{
			NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сессия не найдена")),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	detail, err := analyticalSessionDetail(session)
	if err != nil {
		return nil, err
	}
	return generated.GetAnalyticalSession200JSONResponse(detail), nil
}

func (service server) CollectCurrentSnapshot(
	ctx context.Context,
	request generated.CollectCurrentSnapshotRequestObject,
) (generated.CollectCurrentSnapshotResponseObject, error) {
	session, _, err := service.dependencies.CurrentCollection.Collect(
		ctx, cookieValue(request.Params.AdSession), request.SessionId.String(),
	)
	if response := collectCurrentSnapshotError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	detail, err := analyticalSessionDetail(session)
	if err != nil {
		return nil, err
	}
	return generated.CollectCurrentSnapshot200JSONResponse(detail), nil
}

func (service server) ListEmployees(
	ctx context.Context,
	request generated.ListEmployeesRequestObject,
) (generated.ListEmployeesResponseObject, error) {
	accounts, err := service.dependencies.Access.ListEmployees(ctx, cookieValue(request.Params.AdSession))
	if response := listEmployeesAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.Employee, 0, len(accounts))
	for _, account := range accounts {
		employee, mapErr := employeeResponse(account)
		if mapErr != nil {
			return nil, mapErr
		}
		items = append(items, employee)
	}
	return generated.ListEmployees200JSONResponse{Items: items}, nil
}

func (service server) CreateEmployee(
	ctx context.Context,
	request generated.CreateEmployeeRequestObject,
) (generated.CreateEmployeeResponseObject, error) {
	if request.Body == nil {
		return generated.CreateEmployee422JSONResponse{
			ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(validationError()),
		}, nil
	}
	credential, err := service.dependencies.Access.CreateEmployee(
		ctx, cookieValue(request.Params.AdSession), request.Body.Login,
		request.Body.DisplayName, application.Role(request.Body.Role),
	)
	if response := createEmployeeAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := temporaryCredentialResponse(credential)
	if err != nil {
		return nil, err
	}
	cacheControl := "no-store"
	return generated.CreateEmployee201JSONResponse{
		Body: body, Headers: generated.CreateEmployee201ResponseHeaders{CacheControl: &cacheControl},
	}, nil
}

func (service server) UpdateEmployee(
	ctx context.Context,
	request generated.UpdateEmployeeRequestObject,
) (generated.UpdateEmployeeResponseObject, error) {
	if request.Body == nil {
		return generated.UpdateEmployee422JSONResponse{
			ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(validationError()),
		}, nil
	}
	changes := application.EmployeeChanges{DisplayName: request.Body.DisplayName}
	if request.Body.Role != nil {
		role := application.Role(*request.Body.Role)
		changes.Role = &role
	}
	account, err := service.dependencies.Access.UpdateEmployee(
		ctx, cookieValue(request.Params.AdSession), request.EmployeeId.String(), changes,
	)
	if response := updateEmployeeAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	employee, err := employeeResponse(account)
	if err != nil {
		return nil, err
	}
	return generated.UpdateEmployee200JSONResponse(employee), nil
}

func (service server) DeactivateEmployee(
	ctx context.Context,
	request generated.DeactivateEmployeeRequestObject,
) (generated.DeactivateEmployeeResponseObject, error) {
	account, err := service.dependencies.Access.DeactivateEmployee(
		ctx, cookieValue(request.Params.AdSession), request.EmployeeId.String(),
	)
	if response := deactivateEmployeeAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	employee, err := employeeResponse(account)
	if err != nil {
		return nil, err
	}
	return generated.DeactivateEmployee200JSONResponse(employee), nil
}

func (service server) RestoreEmployee(
	ctx context.Context,
	request generated.RestoreEmployeeRequestObject,
) (generated.RestoreEmployeeResponseObject, error) {
	credential, err := service.dependencies.Access.RestoreEmployee(
		ctx, cookieValue(request.Params.AdSession), request.EmployeeId.String(),
	)
	if response := restoreEmployeeAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := temporaryCredentialResponse(credential)
	if err != nil {
		return nil, err
	}
	cacheControl := "no-store"
	return generated.RestoreEmployee200JSONResponse{
		Body: body, Headers: generated.RestoreEmployee200ResponseHeaders{CacheControl: &cacheControl},
	}, nil
}

func (service server) ResetEmployeePassword(
	ctx context.Context,
	request generated.ResetEmployeePasswordRequestObject,
) (generated.ResetEmployeePasswordResponseObject, error) {
	credential, err := service.dependencies.Access.ResetEmployeePassword(
		ctx, cookieValue(request.Params.AdSession), request.EmployeeId.String(),
	)
	if response := resetEmployeePasswordAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := temporaryCredentialResponse(credential)
	if err != nil {
		return nil, err
	}
	cacheControl := "no-store"
	return generated.ResetEmployeePassword200JSONResponse{
		Body:    body,
		Headers: generated.ResetEmployeePassword200ResponseHeaders{CacheControl: &cacheControl},
	}, nil
}

func (service server) RevokeEmployeeSessions(
	ctx context.Context,
	request generated.RevokeEmployeeSessionsRequestObject,
) (generated.RevokeEmployeeSessionsResponseObject, error) {
	err := service.dependencies.Access.RevokeEmployeeSessions(
		ctx, cookieValue(request.Params.AdSession), request.EmployeeId.String(),
	)
	if response := revokeEmployeeSessionsAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.RevokeEmployeeSessions204Response{}, nil
}

func analyticalSessionSummary(session application.AnalyticalSession) (generated.AnalyticalSessionSummary, error) {
	sessionID, err := uuid.Parse(session.ID)
	if err != nil {
		return generated.AnalyticalSessionSummary{}, err
	}
	creatorID, err := uuid.Parse(session.CreatedByID)
	if err != nil {
		return generated.AnalyticalSessionSummary{}, err
	}
	return generated.AnalyticalSessionSummary{
		Id: sessionID, Name: session.Name, Subtitle: application.AnalyticalSessionSubtitle(session),
		Status: generated.AnalyticalSessionStatus(session.Status), CreatedAt: session.CreatedAt,
		CreatedBy: generated.EmployeeSummary{Id: creatorID, DisplayName: session.CreatedByDisplayName},
	}, nil
}

func analyticalSessionDetail(session application.AnalyticalSession) (generated.AnalyticalSessionDetail, error) {
	summary, err := analyticalSessionSummary(session)
	if err != nil {
		return generated.AnalyticalSessionDetail{}, err
	}
	detail := generated.AnalyticalSessionDetail{
		Id: summary.Id, Name: summary.Name, Subtitle: summary.Subtitle, Status: summary.Status,
		CreatedAt: summary.CreatedAt, CreatedBy: summary.CreatedBy,
		Filters: analyticalSessionFilters(session),
	}
	addAnalyticalSessionCurrentState(&detail, session)
	return detail, nil
}

func analyticalSessionFilters(session application.AnalyticalSession) generated.AnalyticalSessionFilters {
	segmentCodes := make([]generated.SegmentCode, 0, len(session.SegmentCodes))
	for _, code := range session.SegmentCodes {
		segmentCodes = append(segmentCodes, generated.SegmentCode(code))
	}
	filters := generated.AnalyticalSessionFilters{
		Regions: session.Regions, ResidentialComplexes: session.ResidentialComplexes,
		Segments: session.Segments,
	}
	if session.PeriodFrom != nil && session.PeriodTo != nil {
		filters.Period = &generated.AnalyticalPeriod{
			From: openapi_types.Date{Time: *session.PeriodFrom},
			To:   openapi_types.Date{Time: *session.PeriodTo},
		}
	}
	if len(session.RegionCodes) > 0 {
		filters.RegionCodes = &session.RegionCodes
	}
	if len(session.ResidentialComplexIDs) > 0 {
		filters.ResidentialComplexIds = &session.ResidentialComplexIDs
	}
	if len(segmentCodes) > 0 {
		filters.SegmentCodes = &segmentCodes
	}
	return filters
}

func addAnalyticalSessionCurrentState(
	detail *generated.AnalyticalSessionDetail,
	session application.AnalyticalSession,
) {
	if session.ObservedFrom != nil && session.ObservedTo != nil {
		detail.ObservedWindow = &generated.ObservationWindow{
			From: *session.ObservedFrom, To: *session.ObservedTo,
		}
	}
	if session.CollectionProgress != nil {
		progress := collectionProgressResource(*session.CollectionProgress)
		detail.CollectionProgress = &progress
	}
	if session.CurrentDashboard != nil {
		detail.CurrentEiszhsDashboard = &generated.CurrentEISZHSDashboard{
			SourceLabel: generated.CurrentEISZHSDashboardSourceLabel(
				session.CurrentDashboard.SourceLabel,
			),
			CumulativeDduCount: session.CurrentDashboard.CumulativeDDUCount,
		}
	}
}

func collectionProgressResource(progress application.CollectionProgress) generated.CollectionProgress {
	return generated.CollectionProgress{
		Phase:             generated.CollectionProgressPhase(progress.Phase),
		CompletedRequests: progress.CompletedRequests, TotalRequests: progress.TotalRequests,
		DiscoveredTotal: progress.DiscoveredTotal, Cached: progress.Cached,
		Fetched: progress.Fetched, Failed: progress.Failed, Remaining: progress.Remaining,
		CurrentComplexId: progress.CurrentComplexID, CurrentObjectId: progress.CurrentObjectID,
		NextActionAt: progress.NextActionAt, Percent: progress.Percent,
	}
}

func employeeResponse(account application.Account) (generated.Employee, error) {
	id, err := uuid.Parse(account.ID)
	if err != nil {
		return generated.Employee{}, err
	}
	return generated.Employee{
		Id: id, Login: account.Login, DisplayName: account.DisplayName,
		Role: generated.Role(account.Role), State: generated.AccountState(account.State),
		CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
	}, nil
}

func temporaryCredentialResponse(
	credential application.TemporaryCredential,
) (generated.TemporaryCredential, error) {
	employee, err := employeeResponse(credential.Account)
	if err != nil {
		return generated.TemporaryCredential{}, err
	}
	return generated.TemporaryCredential{
		Employee: employee, TemporaryPassword: credential.TemporaryPassword,
		ExpiresAt: credential.ExpiresAt,
	}, nil
}

func notFoundError(message string) generated.ErrorResponse {
	return errorResponse("not_found", message)
}

func forbiddenError() generated.ErrorResponse {
	return errorResponse("forbidden", "Недостаточно прав для этого действия")
}

func conflictError() generated.ErrorResponse {
	return errorResponse("conflict", "Изменение конфликтует с текущим состоянием учетной записи")
}

func validationError() generated.ErrorResponse {
	return errorResponse("validation_failed", "Проверьте заполненные поля")
}

func isUnauthorized(err error) bool {
	return errors.Is(err, application.ErrUnauthenticated) ||
		errors.Is(err, application.ErrPasswordChangeNeeded)
}

func listEmployeesAccessError(err error) generated.ListEmployeesResponseObject {
	if isUnauthorized(err) {
		return generated.ListEmployees401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.ListEmployees403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	return nil
}

func collectCurrentSnapshotError(err error) generated.CollectCurrentSnapshotResponseObject {
	if isUnauthorized(err) {
		return generated.CollectCurrentSnapshot401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}
	}
	if errors.Is(err, application.ErrNotFound) {
		return generated.CollectCurrentSnapshot404JSONResponse{
			NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сессия не найдена")),
		}
	}
	if errors.Is(err, application.ErrConflict) {
		return generated.CollectCurrentSnapshot409JSONResponse{
			ConflictJSONResponse: generated.ConflictJSONResponse(
				errorResponse("collection_conflict", "Состояние сбора уже изменилось"),
			),
		}
	}
	return nil
}

func createEmployeeAccessError(err error) generated.CreateEmployeeResponseObject {
	if isUnauthorized(err) {
		return generated.CreateEmployee401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.CreateEmployee403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrConflict) {
		return generated.CreateEmployee409JSONResponse{ConflictJSONResponse: generated.ConflictJSONResponse(conflictError())}
	}
	if errors.Is(err, application.ErrValidation) {
		return generated.CreateEmployee422JSONResponse{ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(validationError())}
	}
	return nil
}

func updateEmployeeAccessError(err error) generated.UpdateEmployeeResponseObject {
	if isUnauthorized(err) {
		return generated.UpdateEmployee401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.UpdateEmployee403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrNotFound) {
		return generated.UpdateEmployee404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сотрудник не найден"))}
	}
	if errors.Is(err, application.ErrConflict) {
		return generated.UpdateEmployee409JSONResponse{ConflictJSONResponse: generated.ConflictJSONResponse(conflictError())}
	}
	if errors.Is(err, application.ErrValidation) {
		return generated.UpdateEmployee422JSONResponse{ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(validationError())}
	}
	return nil
}

func deactivateEmployeeAccessError(err error) generated.DeactivateEmployeeResponseObject {
	if isUnauthorized(err) {
		return generated.DeactivateEmployee401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.DeactivateEmployee403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrNotFound) {
		return generated.DeactivateEmployee404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сотрудник не найден"))}
	}
	if errors.Is(err, application.ErrConflict) {
		return generated.DeactivateEmployee409JSONResponse{ConflictJSONResponse: generated.ConflictJSONResponse(conflictError())}
	}
	return nil
}

func restoreEmployeeAccessError(err error) generated.RestoreEmployeeResponseObject {
	if isUnauthorized(err) {
		return generated.RestoreEmployee401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.RestoreEmployee403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrNotFound) || errors.Is(err, application.ErrConflict) {
		return generated.RestoreEmployee404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сотрудник не найден"))}
	}
	return nil
}

func resetEmployeePasswordAccessError(err error) generated.ResetEmployeePasswordResponseObject {
	if isUnauthorized(err) {
		return generated.ResetEmployeePassword401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.ResetEmployeePassword403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrNotFound) || errors.Is(err, application.ErrConflict) {
		return generated.ResetEmployeePassword404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сотрудник не найден"))}
	}
	return nil
}

func revokeEmployeeSessionsAccessError(err error) generated.RevokeEmployeeSessionsResponseObject {
	if isUnauthorized(err) {
		return generated.RevokeEmployeeSessions401JSONResponse{UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError())}
	}
	if errors.Is(err, application.ErrForbidden) {
		return generated.RevokeEmployeeSessions403JSONResponse{ForbiddenJSONResponse: generated.ForbiddenJSONResponse(forbiddenError())}
	}
	if errors.Is(err, application.ErrNotFound) {
		return generated.RevokeEmployeeSessions404JSONResponse{NotFoundJSONResponse: generated.NotFoundJSONResponse(notFoundError("Сотрудник не найден"))}
	}
	return nil
}
