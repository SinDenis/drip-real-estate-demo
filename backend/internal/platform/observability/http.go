// Package observability provides structured AI-friendly process telemetry.
package observability

import (
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// NewLogger creates a JSON logger with stable service metadata.
func NewLogger(output io.Writer, service string, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler).With("service", service, "environment", environment)
}

// RequestLogger records one correlated event for every completed HTTP request.
func RequestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID, traceID := rand.Text(), rand.Text()
		writer.Header().Set("X-Request-ID", requestID)
		captured := &responseWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(captured, request)
		if isSuccessfulHealthCheck(request.URL.Path, captured.status) {
			return
		}
		logger.InfoContext(request.Context(), "HTTP request completed",
			"event", "http.request.completed", "request_id", requestID,
			"trace_id", traceID, "method", request.Method, "path", request.URL.Path,
			"status", captured.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func isSuccessfulHealthCheck(path string, status int) bool {
	return strings.HasPrefix(path, "/health/") && status < http.StatusBadRequest
}

// LogError records a safe typed error without process environment or secrets.
func LogError(logger *slog.Logger, event string, err error) {
	logger.Error("Operation failed", "event", event, slog.Group("error",
		"type", fmt.Sprintf("%T", err), "message", err.Error()))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *responseWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
