package objectstorage_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/objectstorage"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/config"
)

func TestProbeChecksConfiguredBucket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead || request.URL.Path != "/analytics" {
			t.Errorf("request = %s %s, want HEAD /analytics", request.Method, request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	probe, err := objectstorage.New(t.Context(), s3Settings(server.URL))
	if err != nil {
		t.Fatalf("configure probe: %v", err)
	}
	if err := probe.Check(t.Context()); err != nil {
		t.Fatalf("check bucket: %v", err)
	}
}

//nolint:funlen,gocyclo // The fake S3 handler verifies conditional create and idempotent retry in one seam.
func TestStoragePutsAnImmutablePrivateRawArtifact(t *testing.T) {
	t.Parallel()
	var stored []byte
	putCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/analytics/eiszhs/sessions/session-id/objects/2001/extended.json" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Method == http.MethodHead {
			writer.Header().Set("Content-Length", "7")
			writer.Header().Set("X-Amz-Meta-Sha256", "abc123")
			writer.WriteHeader(http.StatusOK)
			return
		}
		if request.Method != http.MethodPut || request.Header.Get("If-None-Match") != "*" {
			t.Errorf("request = %s If-None-Match %q", request.Method, request.Header.Get("If-None-Match"))
		}
		if request.Header.Get("Content-Type") != "application/json" ||
			request.Header.Get("X-Amz-Meta-Sha256") != "abc123" {
			t.Errorf("headers = %#v", request.Header)
		}
		putCalls++
		if putCalls == 2 {
			writer.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		stored, _ = io.ReadAll(request.Body)
		writer.Header().Set("ETag", `"fixture"`)
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	storage, err := objectstorage.New(t.Context(), s3Settings(server.URL))
	if err != nil {
		t.Fatalf("configure storage: %v", err)
	}
	err = storage.PutRaw(t.Context(), application.RawArtifact{
		Key:    "eiszhs/sessions/session-id/objects/2001/extended.json",
		SHA256: "abc123", Size: 7, ContentType: "application/json",
		ObservedAt: time.Date(2026, time.August, 31, 13, 6, 0, 0, time.UTC),
	}, []byte(`{"a":1}`))
	if err != nil || string(stored) != `{"a":1}` {
		t.Fatalf("PutRaw error=%v body=%q", err, stored)
	}
	if err := storage.PutRaw(t.Context(), application.RawArtifact{
		Key:    "eiszhs/sessions/session-id/objects/2001/extended.json",
		SHA256: "abc123", Size: 7, ContentType: "application/json",
		ObservedAt: time.Date(2026, time.August, 31, 13, 6, 0, 0, time.UTC),
	}, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("idempotent PutRaw: %v", err)
	}
}

func TestProbeReportsRejectedBucket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	probe, err := objectstorage.New(t.Context(), s3Settings(server.URL))
	if err != nil {
		t.Fatalf("configure probe: %v", err)
	}
	if err := probe.Check(t.Context()); err == nil {
		t.Fatal("expected rejected bucket to fail readiness")
	}
}

func s3Settings(endpoint string) config.S3Settings {
	return config.S3Settings{
		Endpoint: endpoint, Region: "ru-1", Bucket: "analytics",
		AccessKeyID: "access", SecretAccessKey: "secret", PathStyle: true,
	}
}
