package observability_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/observability"
)

func TestRequestLoggerWritesCorrelationFields(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := observability.NewLogger(&output, "api", "test")
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Errorf("flush wrapped response: %v", err)
		}
	})
	handler := observability.RequestLogger(logger, next)
	response := httptest.NewRecorder()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/sessions", nil)
	handler.ServeHTTP(response, request)

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("decode log event: %v", err)
	}
	for _, key := range []string{"service", "event", "request_id", "trace_id"} {
		if event[key] == nil || event[key] == "" {
			t.Errorf("log field %q is missing: %#v", key, event)
		}
	}
}

func TestRequestLoggerSkipsSuccessfulHealthChecks(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := observability.NewLogger(&output, "api", "test")
	handler := observability.RequestLogger(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/live", nil)

	handler.ServeHTTP(response, request)

	if output.Len() != 0 {
		t.Fatalf("successful health check must not produce log noise: %s", output.String())
	}
}

func TestRequestLoggerRecordsFailedHealthChecks(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := observability.NewLogger(&output, "api", "test")
	handler := observability.RequestLogger(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/ready", nil)

	handler.ServeHTTP(httptest.NewRecorder(), request)

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("decode failed health check event: %v", err)
	}
	if event["status"] != float64(http.StatusServiceUnavailable) {
		t.Fatalf("failed health check status is missing: %#v", event)
	}
}

func TestLogErrorUsesStructuredSafeFields(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := observability.NewLogger(&output, "api", "test")
	observability.LogError(logger, "api.failed", errors.New("connection failed"))

	var event map[string]any
	if err := json.NewDecoder(&output).Decode(&event); err != nil {
		t.Fatalf("decode log event: %v", err)
	}
	errorFields, ok := event["error"].(map[string]any)
	if !ok || errorFields["type"] == nil || errorFields["message"] != "connection failed" {
		t.Fatalf("structured error fields missing: %#v", event)
	}
}
