package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func TestReadyReportsAvailableDependencies(t *testing.T) {
	t.Parallel()

	handler := newHandler(staticProbe{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	assertStatus(t, response, "ready")
}

func TestReadyReportsUnavailableDependency(t *testing.T) {
	t.Parallel()

	handler := newHandler(staticProbe{err: errors.New("storage unavailable")})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	assertStatus(t, response, "not_ready")
}

func TestLivenessDoesNotDependOnInfrastructure(t *testing.T) {
	t.Parallel()

	handler := newHandler(staticProbe{err: errors.New("database unavailable")})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	assertStatus(t, response, "live")
}

func TestMalformedRequestUsesThePublicErrorEnvelope(t *testing.T) {
	t.Parallel()

	handler := newHandler(staticProbe{})
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader("{"),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode malformed request response: %v", err)
	}
	if body.Error.Code != "validation_failed" {
		t.Fatalf("error code = %q, want validation_failed", body.Error.Code)
	}
}

func TestCreateSessionRequiresAnIdempotencyKey(t *testing.T) {
	t.Parallel()

	handler := newHandler(staticProbe{})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/sessions",
		strings.NewReader(`{"regionCodes":["77"],"residentialComplexIds":["1001"],"segmentCodes":["residential"]}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "validation_failed" {
		t.Fatalf("error code = %q, want validation_failed", body.Error.Code)
	}
}

type staticProbe struct {
	err error
}

func (probe staticProbe) Check(context.Context) error {
	return probe.err
}

func newHandler(probe staticProbe) http.Handler {
	return httpapi.NewHandler(httpapi.Dependencies{
		Readiness: application.NewReadinessService(probe),
	})
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != expected {
		t.Errorf("status body = %q, want %q", body.Status, expected)
	}
}
