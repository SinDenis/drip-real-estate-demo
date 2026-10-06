// Package config validates runtime settings from neutral environment variables.
package config

import (
	"fmt"
	"strconv"
	"time"
)

// LookupEnv retrieves one environment variable without exposing all process state.
type LookupEnv func(string) (string, bool)

// Settings contains validated application runtime configuration.
type Settings struct {
	Port              string
	Environment       string
	StaticDir         string
	DatabaseURL       string
	Bootstrap         BootstrapSettings
	S3                S3Settings
	EISZHS            EISZHSSettings
	CurrentCollection CurrentCollectionSettings
}

// CurrentCollectionSettings controls the duration and renewal cadence of durable step claims.
type CurrentCollectionSettings struct {
	Lease      time.Duration
	RenewEvery time.Duration
}

// EISZHSSettings contains the licensed read-only source endpoints and credentials.
type EISZHSSettings struct {
	APIURL       string
	OAuthURL     string
	ClientID     string
	ClientSecret string
}

// BootstrapSettings contains the optional one-time first administrator credentials.
type BootstrapSettings struct {
	Login       string
	Password    string
	DisplayName string
}

// S3Settings contains provider-neutral S3-compatible client settings.
type S3Settings struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool
}

// Load validates environment values without including secrets in errors.
//
//nolint:funlen // Runtime composition keeps every environment-backed setting visible together.
func Load(lookup LookupEnv) (Settings, error) {
	databaseURL, err := required(lookup, "DATABASE_URL")
	if err != nil {
		return Settings{}, err
	}
	s3, err := loadS3(lookup)
	if err != nil {
		return Settings{}, err
	}
	currentCollection, err := loadCurrentCollection(lookup)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		Port:        valueOr(lookup, "APP_PORT", "8080"),
		Environment: valueOr(lookup, "APP_ENV", "development"),
		StaticDir:   valueOr(lookup, "STATIC_DIR", "../frontend/dist"),
		DatabaseURL: databaseURL,
		Bootstrap: BootstrapSettings{
			Login:       valueOr(lookup, "BOOTSTRAP_ADMIN_LOGIN", ""),
			Password:    valueOr(lookup, "BOOTSTRAP_ADMIN_PASSWORD", ""),
			DisplayName: valueOr(lookup, "BOOTSTRAP_ADMIN_DISPLAY_NAME", ""),
		},
		S3: s3,
		EISZHS: EISZHSSettings{
			APIURL:       valueOr(lookup, "EISZHS_API_URL", "https://xn--80az8a.xn--d1aqf.xn--p1ai/gw/sgs/api/v2/"),
			OAuthURL:     valueOr(lookup, "EISZHS_OAUTH_URL", "https://xn--80az8a.xn--d1aqf.xn--p1ai/idm-services/auth/realms/external-eisgs-idm/protocol/openid-connect/token"),
			ClientID:     valueOr(lookup, "CLIENT_ID", ""),
			ClientSecret: valueOr(lookup, "CLIENT_SECRET", ""),
		},
		CurrentCollection: currentCollection,
	}, nil
}

func loadCurrentCollection(lookup LookupEnv) (CurrentCollectionSettings, error) {
	lease, err := durationValue(lookup, "CURRENT_COLLECTION_LEASE", 2*time.Minute)
	if err != nil {
		return CurrentCollectionSettings{}, err
	}
	renewEvery, err := durationValue(lookup, "CURRENT_COLLECTION_RENEW_EVERY", 30*time.Second)
	if err != nil {
		return CurrentCollectionSettings{}, err
	}
	if renewEvery >= lease {
		return CurrentCollectionSettings{}, fmt.Errorf(
			"environment variable CURRENT_COLLECTION_RENEW_EVERY must be shorter than CURRENT_COLLECTION_LEASE",
		)
	}
	return CurrentCollectionSettings{Lease: lease, RenewEvery: renewEvery}, nil
}

func loadS3(lookup LookupEnv) (S3Settings, error) {
	values := make(map[string]string, 5)
	for _, key := range []string{"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
		value, err := required(lookup, key)
		if err != nil {
			return S3Settings{}, err
		}
		values[key] = value
	}
	pathStyle, err := boolValue(lookup, "S3_PATH_STYLE", false)
	if err != nil {
		return S3Settings{}, err
	}
	return S3Settings{
		Endpoint: values["S3_ENDPOINT"], Region: values["S3_REGION"],
		Bucket: values["S3_BUCKET"], AccessKeyID: values["S3_ACCESS_KEY_ID"],
		SecretAccessKey: values["S3_SECRET_ACCESS_KEY"], PathStyle: pathStyle,
	}, nil
}

func required(lookup LookupEnv, key string) (string, error) {
	value, found := lookup(key)
	if !found || value == "" {
		return "", fmt.Errorf("missing required environment variable %s", key)
	}
	return value, nil
}

func valueOr(lookup LookupEnv, key string, fallback string) string {
	if value, found := lookup(key); found && value != "" {
		return value
	}
	return fallback
}

func boolValue(lookup LookupEnv, key string, fallback bool) (bool, error) {
	value, found := lookup(key)
	if !found || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("environment variable %s must be boolean", key)
	}
	return parsed, nil
}

func durationValue(lookup LookupEnv, key string, fallback time.Duration) (time.Duration, error) {
	value, found := lookup(key)
	if !found || value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("environment variable %s must be a positive duration", key)
	}
	return parsed, nil
}
