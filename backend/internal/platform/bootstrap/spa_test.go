package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAHandlerServesAssetsAndFallsBackWithoutLeavingItsRoot(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	staticDir := filepath.Join(parent, "dist")
	if err := os.Mkdir(staticDir, 0o700); err != nil {
		t.Fatalf("create static directory: %v", err)
	}
	writeFixture(t, filepath.Join(staticDir, "index.html"), "application shell")
	writeFixture(t, filepath.Join(staticDir, "app.js"), "application asset")
	writeFixture(t, filepath.Join(parent, "secret.txt"), "outside secret")
	handler := spaHandler(staticDir)

	assertBody(t, handler, "/app.js", "application asset")
	assertBody(t, handler, "/sessions/session-id", "application shell")
	assertBody(t, handler, "/../secret.txt", "application shell")
}

func writeFixture(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertBody(t *testing.T, handler http.Handler, path string, expected string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != expected {
		t.Fatalf("GET %s = %d %q, want 200 %q", path, response.Code, response.Body.String(), expected)
	}
}
