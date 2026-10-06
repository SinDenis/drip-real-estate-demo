package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/config"
)

func TestLoadReturnsRuntimeConfiguration(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	settings, err := config.Load(func(key string) (string, bool) {
		value, found := values[key]
		return value, found
	})

	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if settings.Port != "9090" || settings.S3.Bucket != "analytics" {
		t.Fatalf("configuration = %#v, want selected environment values", settings)
	}
	if settings.Bootstrap.Login != "admin" ||
		settings.Bootstrap.Password != "Bootstrap-Admin-2026!" ||
		settings.Bootstrap.DisplayName != "Аналитик-администратор" {
		t.Fatalf("bootstrap administrator = %#v, want environment values", settings.Bootstrap)
	}
	if !settings.S3.PathStyle {
		t.Error("S3 path-style access should be enabled")
	}
	if settings.CurrentCollection.Lease != 600*time.Millisecond ||
		settings.CurrentCollection.RenewEvery != 100*time.Millisecond {
		t.Fatalf("current collection timing = %#v", settings.CurrentCollection)
	}
}

func TestLoadRejectsCurrentCollectionRenewalAtOrAfterLease(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	values["CURRENT_COLLECTION_RENEW_EVERY"] = values["CURRENT_COLLECTION_LEASE"]
	_, err := config.Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "CURRENT_COLLECTION_RENEW_EVERY") {
		t.Fatalf("error = %v, want invalid current collection renewal", err)
	}
}

func TestLoadUsesSafeDefaults(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	delete(values, "APP_PORT")
	delete(values, "S3_PATH_STYLE")
	delete(values, "CURRENT_COLLECTION_LEASE")
	delete(values, "CURRENT_COLLECTION_RENEW_EVERY")
	settings, err := config.Load(mapLookup(values))

	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if settings.Port != "8080" || settings.S3.PathStyle ||
		settings.CurrentCollection.Lease != 2*time.Minute ||
		settings.CurrentCollection.RenewEvery != 30*time.Second {
		t.Fatalf("defaults not applied: %#v", settings)
	}
}

func TestLoadRejectsMissingSettingsWithoutSecretValues(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	delete(values, "S3_BUCKET")
	_, err := config.Load(mapLookup(values))

	if err == nil || !strings.Contains(err.Error(), "S3_BUCKET") {
		t.Fatalf("error = %v, want missing S3_BUCKET", err)
	}
	if strings.Contains(err.Error(), values["S3_SECRET_ACCESS_KEY"]) {
		t.Fatal("configuration error contains a secret")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	delete(values, "DATABASE_URL")
	if _, err := config.Load(mapLookup(values)); err == nil {
		t.Fatal("expected missing DATABASE_URL to fail")
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	t.Parallel()

	values := validEnvironment()
	values["S3_PATH_STYLE"] = "sometimes"
	if _, err := config.Load(mapLookup(values)); err == nil {
		t.Fatal("expected invalid S3_PATH_STYLE to fail")
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		"APP_PORT":                       "9090",
		"BOOTSTRAP_ADMIN_LOGIN":          "admin",
		"BOOTSTRAP_ADMIN_PASSWORD":       "Bootstrap-Admin-2026!",
		"BOOTSTRAP_ADMIN_DISPLAY_NAME":   "Аналитик-администратор",
		"DATABASE_URL":                   "postgres://app:secret@postgres/app",
		"S3_ENDPOINT":                    "http://s3mock:9090",
		"S3_REGION":                      "ru-1",
		"S3_BUCKET":                      "analytics",
		"S3_ACCESS_KEY_ID":               "access",
		"S3_SECRET_ACCESS_KEY":           "secret",
		"S3_PATH_STYLE":                  "true",
		"CURRENT_COLLECTION_LEASE":       "600ms",
		"CURRENT_COLLECTION_RENEW_EVERY": "100ms",
	}
}

func mapLookup(values map[string]string) config.LookupEnv {
	return func(key string) (string, bool) {
		value, found := values[key]
		return value, found
	}
}
