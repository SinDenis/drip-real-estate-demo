package httpserver_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/httpserver"
)

func TestServeStopsAfterContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	if err := httpserver.Serve(ctx, server); err != nil {
		t.Fatalf("serve canceled server: %v", err)
	}
}

func TestServeReturnsListenFailure(t *testing.T) {
	t.Parallel()

	server := &http.Server{Addr: "127.0.0.1:-1", Handler: http.NewServeMux()}
	if err := httpserver.Serve(t.Context(), server); err == nil {
		t.Fatal("expected invalid listen address to fail")
	}
}
