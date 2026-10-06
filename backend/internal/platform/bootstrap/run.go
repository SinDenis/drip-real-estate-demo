// Package bootstrap assembles application services and delivery adapters.
package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/eiszhs"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/objectstorage"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/postgres"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/security"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/config"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/httpserver"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/observability"
)

// Run assembles and serves the application until the context is canceled.
func Run(ctx context.Context, output io.Writer, lookup config.LookupEnv) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	settings, err := config.Load(lookup)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	logger := observability.NewLogger(output, "api", settings.Environment)
	server, closeResources, err := buildServer(ctx, settings, logger)
	if err != nil {
		return err
	}
	defer closeResources()
	logger.Info("API starting", "event", "api.starting", "port", settings.Port)
	return httpserver.Serve(ctx, server)
}

//nolint:funlen // Composition keeps every production dependency visible in one place.
func buildServer(
	ctx context.Context,
	settings config.Settings,
	logger *slog.Logger,
) (*http.Server, func(), error) {
	database, err := postgres.NewStore(ctx, settings.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	access := application.NewAccessService(database, security.NewCredentials())
	if err := access.EnsureBootstrap(ctx, application.BootstrapCredentials{
		Login:       settings.Bootstrap.Login,
		Password:    settings.Bootstrap.Password,
		DisplayName: settings.Bootstrap.DisplayName,
	}); err != nil {
		database.Close()
		return nil, nil, err
	}
	storage, err := objectstorage.New(ctx, settings.S3)
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	source := eiszhs.New(eiszhs.Settings{
		APIURL: settings.EISZHS.APIURL, OAuthURL: settings.EISZHS.OAuthURL,
		ClientID: settings.EISZHS.ClientID, ClientSecret: settings.EISZHS.ClientSecret,
	}, &http.Client{Timeout: 30 * time.Second}, database, database)
	catalog := application.NewCatalogService(database, source, time.Now)
	sessions := application.NewAnalyticalSessionService(access, database, time.Now)
	currentCollection := application.NewCurrentCollectionServiceWithTiming(
		access, database, source, eiszhs.NewNormalizer(), storage, time.Now,
		application.CurrentCollectionTiming{
			Lease: settings.CurrentCollection.Lease, RenewEvery: settings.CurrentCollection.RenewEvery,
		},
	)
	handler := applicationHandler(
		settings, logger, database, storage, access, catalog, sessions, currentCollection,
	)
	server := &http.Server{
		Addr:              ":" + settings.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server, database.Close, nil
}

func applicationHandler(
	settings config.Settings,
	logger *slog.Logger,
	database *postgres.Store,
	storage *objectstorage.Store,
	access *application.AccessService,
	catalog *application.CatalogService,
	sessions *application.AnalyticalSessionService,
	currentCollection *application.CurrentCollectionService,
) http.Handler {
	readiness := application.NewReadinessService(database, storage)
	api := httpapi.NewHandler(httpapi.Dependencies{
		Readiness: readiness, Access: access, Catalog: catalog, Sessions: sessions,
		CurrentCollection: currentCollection,
		SecureCookies:     settings.Environment != "local" && settings.Environment != "development",
		Logger:            logger,
	})
	router := http.NewServeMux()
	router.Handle("/api/", api)
	router.Handle("/health/", api)
	router.Handle("/", spaHandler(settings.StaticDir))
	return observability.RequestLogger(logger, router)
}

func spaHandler(staticDir string) http.Handler {
	files := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cleanPath := path.Clean("/" + request.URL.Path)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		assetPath := filepath.Join(staticDir, filepath.FromSlash(relativePath))
		asset, err := os.Stat(assetPath)
		if relativePath == "" || err != nil || !asset.Mode().IsRegular() {
			cleanPath = "/"
		}
		request.URL.Path = cleanPath
		files.ServeHTTP(writer, request)
	})
}

// Main runs the production process and exits non-zero after a startup failure.
func Main() {
	logger := observability.NewLogger(os.Stderr, "api", "startup")
	if err := Run(context.Background(), os.Stdout, os.LookupEnv); err != nil {
		observability.LogError(logger, "api.stopped", fmt.Errorf("run API: %w", err))
		os.Exit(1)
	}
}
