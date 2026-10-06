// Package httpserver runs the HTTP process with graceful shutdown.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Serve runs an HTTP server and shuts it down when the context is canceled.
func Serve(ctx context.Context, server *http.Server) error {
	result := make(chan error, 1)
	go func() {
		result <- server.ListenAndServe()
	}()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
