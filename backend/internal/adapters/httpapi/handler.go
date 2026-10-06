// Package httpapi adapts application operations to the strict OpenAPI contract.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi/generated"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/google/uuid"
)

// Dependencies contains the application services exposed over HTTP.
type Dependencies struct {
	Readiness         application.ReadinessService
	Access            *application.AccessService
	Catalog           *application.CatalogService
	Sessions          *application.AnalyticalSessionService
	CurrentCollection *application.CurrentCollectionService
	SecureCookies     bool
	Logger            *slog.Logger
}

func (service server) Login(
	ctx context.Context,
	request generated.LoginRequestObject,
) (generated.LoginResponseObject, error) {
	if request.Body == nil {
		return loginUnauthorized(), nil
	}
	session, err := service.dependencies.Access.Login(
		ctx, request.Body.Login, request.Body.Password, clientIP(ctx),
	)
	if response := loginAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	user, err := currentUser(session.Account)
	if err != nil {
		return nil, err
	}
	cookie := service.sessionCookie(session.RawToken, 12*time.Hour)
	return generated.Login200JSONResponse{
		Body:    user,
		Headers: generated.Login200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (service server) ChangePassword(
	ctx context.Context,
	request generated.ChangePasswordRequestObject,
) (generated.ChangePasswordResponseObject, error) {
	if request.Body == nil {
		return passwordValidationFailed(), nil
	}
	session, err := service.dependencies.Access.ChangeTemporaryPassword(
		ctx,
		cookieValue(request.Params.AdSession),
		request.Body.Password,
	)
	if response := passwordChangeAccessError(err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	user, err := currentUser(session.Account)
	if err != nil {
		return nil, err
	}
	cookie := service.sessionCookie(session.RawToken, 12*time.Hour)
	return generated.ChangePassword200JSONResponse{
		Body:    user,
		Headers: generated.ChangePassword200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (service server) Logout(
	ctx context.Context,
	request generated.LogoutRequestObject,
) (generated.LogoutResponseObject, error) {
	if err := service.dependencies.Access.Logout(ctx, cookieValue(request.Params.AdSession)); err != nil {
		return nil, err
	}
	cookie := service.sessionCookie("", -time.Hour)
	return generated.Logout204Response{
		Headers: generated.Logout204ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (service server) GetCurrentUser(
	ctx context.Context,
	request generated.GetCurrentUserRequestObject,
) (generated.GetCurrentUserResponseObject, error) {
	account, err := service.dependencies.Access.CurrentAccount(ctx, cookieValue(request.Params.AdSession))
	if errors.Is(err, application.ErrUnauthenticated) {
		return generated.GetCurrentUser401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	user, err := currentUser(account)
	if err != nil {
		return nil, err
	}
	return generated.GetCurrentUser200JSONResponse(user), nil
}

func (service server) ListAnalyticalSessions(
	ctx context.Context,
	request generated.ListAnalyticalSessionsRequestObject,
) (generated.ListAnalyticalSessionsResponseObject, error) {
	cursor, limit := analyticalSessionListParameters(request.Params)
	page, err := service.dependencies.Access.ListAnalyticalSessions(
		ctx,
		cookieValue(request.Params.AdSession),
		cursor,
		limit,
	)
	if errors.Is(err, application.ErrUnauthenticated) ||
		errors.Is(err, application.ErrPasswordChangeNeeded) {
		return generated.ListAnalyticalSessions401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}, nil
	}
	if errors.Is(err, application.ErrValidation) {
		return generated.ListAnalyticalSessions422JSONResponse{
			ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(
				errorResponse("invalid_cursor", "Курсор списка сессий некорректен"),
			),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return analyticalSessionPageResponse(page)
}

type server struct {
	dependencies Dependencies
}

// NewHandler returns a strict OpenAPI HTTP handler.
func NewHandler(dependencies Dependencies) http.Handler {
	implementation := server{dependencies: dependencies}
	strict := generated.NewStrictHandlerWithOptions(
		implementation, nil, strictHandlerOptions(dependencies),
	)
	contract := generated.HandlerWithOptions(strict, generated.StdHTTPServerOptions{
		BaseRouter:       http.NewServeMux(),
		ErrorHandlerFunc: contractRequestError,
	})
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		address := request.RemoteAddr
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
			address = host
		}
		contract.ServeHTTP(response, request.WithContext(
			context.WithValue(request.Context(), clientIPContextKey{}, address),
		))
	})
}

func strictHandlerOptions(dependencies Dependencies) generated.StrictHTTPServerOptions {
	return generated.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: contractRequestError,
		ResponseErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, err error) {
			if dependencies.Logger != nil {
				dependencies.Logger.Error(
					"HTTP handler failed", "event", "http.handler.failed", "error", err,
				)
			}
			writeErrorResponse(
				response, http.StatusInternalServerError,
				errorResponse("internal_error", "Сервис временно недоступен"),
			)
		},
	}
}

func contractRequestError(response http.ResponseWriter, _ *http.Request, _ error) {
	writeErrorResponse(
		response, http.StatusBadRequest,
		errorResponse("validation_failed", "Запрос не соответствует API-контракту"),
	)
}

func writeErrorResponse(
	response http.ResponseWriter,
	status int,
	body generated.ErrorResponse,
) {
	payload, err := json.Marshal(body)
	if err != nil {
		http.Error(response, "internal server error", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_, _ = response.Write(append(payload, '\n'))
}

func (server) GetLiveness(
	context.Context,
	generated.GetLivenessRequestObject,
) (generated.GetLivenessResponseObject, error) {
	return generated.GetLiveness200JSONResponse{Status: generated.Live}, nil
}

func (service server) GetReadiness(
	ctx context.Context,
	_ generated.GetReadinessRequestObject,
) (generated.GetReadinessResponseObject, error) {
	if err := service.dependencies.Readiness.Check(ctx); err != nil {
		return generated.GetReadiness503JSONResponse{Status: generated.NotReady}, nil
	}
	return generated.GetReadiness200JSONResponse{Status: generated.Ready}, nil
}

func (service server) sessionCookie(raw string, lifetime time.Duration) string {
	cookie := http.Cookie{
		Name: "ad_session", Value: raw, Path: "/", HttpOnly: true,
		Secure: service.dependencies.SecureCookies, SameSite: http.SameSiteLaxMode,
	}
	if lifetime < 0 {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	} else {
		cookie.MaxAge = int(lifetime.Seconds())
		cookie.Expires = time.Now().Add(lifetime).UTC()
	}
	return cookie.String()
}

func currentUser(account application.Account) (generated.CurrentUser, error) {
	id, err := uuid.Parse(account.ID)
	if err != nil {
		return generated.CurrentUser{}, err
	}
	return generated.CurrentUser{
		Id: id, Login: account.Login, DisplayName: account.DisplayName,
		Role: generated.Role(account.Role), State: generated.AccountState(account.State),
		RequiresPasswordChange: account.State == application.AccountPendingActivation,
	}, nil
}

func cookieValue(cookie *generated.SessionCookie) string {
	if cookie == nil {
		return ""
	}
	return string(*cookie)
}

func loginUnauthorized() generated.Login401JSONResponse {
	return generated.Login401JSONResponse{
		UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
	}
}

func unauthorizedError() generated.ErrorResponse {
	return errorResponse("unauthenticated", "Имя входа или пароль неверны")
}

func passwordValidationFailed() generated.ChangePassword422JSONResponse {
	return generated.ChangePassword422JSONResponse{
		ValidationFailedJSONResponse: generated.ValidationFailedJSONResponse(
			errorResponse("password_invalid", "Пароль не соответствует требованиям"),
		),
	}
}

func loginAccessError(err error) generated.LoginResponseObject {
	if errors.Is(err, application.ErrUnauthenticated) {
		return loginUnauthorized()
	}
	if errors.Is(err, application.ErrRateLimited) {
		retryAfter := 2
		return generated.Login429JSONResponse{RateLimitedJSONResponse: generated.RateLimitedJSONResponse{
			Body:    errorResponse("rate_limited", "Слишком много попыток входа. Повторите позже"),
			Headers: generated.RateLimitedResponseHeaders{RetryAfter: &retryAfter},
		}}
	}
	return nil
}

func passwordChangeAccessError(err error) generated.ChangePasswordResponseObject {
	if errors.Is(err, application.ErrPasswordInvalid) {
		return passwordValidationFailed()
	}
	if errors.Is(err, application.ErrUnauthenticated) {
		return generated.ChangePassword401JSONResponse{
			UnauthorizedJSONResponse: generated.UnauthorizedJSONResponse(unauthorizedError()),
		}
	}
	return nil
}

func analyticalSessionListParameters(params generated.ListAnalyticalSessionsParams) (string, int) {
	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	return cursor, limit
}

func analyticalSessionPageResponse(
	page application.AnalyticalSessionPage,
) (generated.ListAnalyticalSessionsResponseObject, error) {
	items := make([]generated.AnalyticalSessionSummary, 0, len(page.Items))
	for _, session := range page.Items {
		item, err := analyticalSessionSummary(session)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	response := generated.ListAnalyticalSessions200JSONResponse{Items: items}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	return response, nil
}

func errorResponse(code string, message string) generated.ErrorResponse {
	return generated.ErrorResponse{Error: generated.ErrorDetail{Code: code, Message: message}}
}

type clientIPContextKey struct{}

func clientIP(ctx context.Context) string {
	value, _ := ctx.Value(clientIPContextKey{}).(string)
	return value
}
