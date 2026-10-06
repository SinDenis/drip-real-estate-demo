package bootstrap_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/bootstrap"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/config"
)

func TestRunRejectsIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	if err := bootstrap.Run(t.Context(), &bytes.Buffer{}, mapLookup(nil)); err == nil {
		t.Fatal("expected incomplete configuration to fail")
	}
}

func TestRunBuildsServerAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	values := runtimeEnvironment(t.TempDir())
	if err := bootstrap.Run(ctx, &bytes.Buffer{}, mapLookup(values)); err != nil {
		t.Fatalf("run canceled server: %v", err)
	}
}

func runtimeEnvironment(staticDir string) map[string]string {
	return map[string]string{
		"APP_PORT": "0", "STATIC_DIR": staticDir,
		"DATABASE_URL": "postgres://app:secret@127.0.0.1:1/app",
		"S3_ENDPOINT":  "http://127.0.0.1:1", "S3_REGION": "ru-1",
		"S3_BUCKET": "analytics", "S3_ACCESS_KEY_ID": "access",
		"S3_SECRET_ACCESS_KEY": "secret", "S3_PATH_STYLE": "true",
	}
}

func mapLookup(values map[string]string) config.LookupEnv {
	return func(key string) (string, bool) {
		value, found := values[key]
		return value, found
	}
}
