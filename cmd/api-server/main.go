// @title           Toktik Options Platform API
// @version         1.0
// @description     Backend data services for multi-market options analytics, including market data retrieval, feature/factor queries, screening, strategy catalog, and backtesting.
// @termsOfService  https://toktik.dev/terms

// @contact.name   Toktik Dev Team
// @contact.url    https://toktik.dev
// @contact.email  dev@toktik.dev

// @license.name  Proprietary
// @license.url   https://toktik.dev/license

// @host      localhost:9010
// @BasePath  /api/v1

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key

package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/Cyvadra/toktik/docs"
	"github.com/Cyvadra/toktik/internal/api"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/service"
	_ "github.com/Cyvadra/toktik/pkg/dsl/catalog"
	"github.com/Cyvadra/toktik/pkg/feeds"
	_ "github.com/Cyvadra/toktik/pkg/feeds/dvol"
)

func main() {
	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		slog.Error("load runtime config", "error", err)
		os.Exit(1)
	}

	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	addr := flag.String("addr", runtimeCfg.APIServer.ListenAddr, "Listen address")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	appCli.SetupLogger(true, slog.LevelInfo)

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		slog.Error("resolve schema", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:    ddlFile,
		Kline:      true,
		SpotKline:  true,
		ChainCache: true,
	})
	if err != nil {
		slog.Error("connect clickhouse", "error", err)
		os.Exit(1)
	}

	factorStore, err := feeds.NewStore(ctx, *dsn)
	if err != nil {
		slog.Error("connect factor store", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := factorStore.Close(); closeErr != nil {
			slog.Error("close factor store", "error", closeErr)
		}
	}()

	repo := chrepo.NewRepo(conn)

	svc := service.NewCryptoOptionsService(repo)
	usStocksSvc := service.NewUSStocksService(repo)
	usOptionsSvc := service.NewUSOptionsService(repo)
	infraSvc := service.NewInfraService(repo)
	featureSvc := service.NewFeatureService(repo)
	indicatorSvc := service.NewIndicatorService(repo)
	strategyBacktestSvc := service.NewPortfolioBacktestService(repo, factorStore)
	cryptoSpotSvc := service.NewCryptoSpotService(repo)
	screenerSvc := service.NewScreenerService(repo)
	strategyCatalogSvc := service.NewStrategyCatalogService()
	factorSvc := service.NewFactorService(factorStore)
	cacheStore, err := cache.NewStore(ctx, runtimeCfg)
	if err != nil {
		slog.Warn("init cache backend failed, falling back to memory cache", "error", err)
		cacheStore = cache.NewMemoryStore()
	}
	defer func() {
		if closeErr := cacheStore.Close(); closeErr != nil {
			slog.Error("close cache store", "error", closeErr)
		}
	}()

	var polygonSvc api.PolygonProvider
	polygonService, err := service.NewPolygonServiceFromConfig(runtimeCfg, cacheStore)
	if err != nil {
		slog.Warn("polygon service disabled", "error", err)
	} else {
		polygonSvc = polygonService
	}

	router := api.NewRouter(svc, usStocksSvc, usOptionsSvc, infraSvc, featureSvc, indicatorSvc, strategyBacktestSvc, cryptoSpotSvc, screenerSvc, strategyCatalogSvc, factorSvc, polygonSvc)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadHeaderTimeout: runtimeCfg.APIServerReadHeaderTimeout(),
		ReadTimeout:       runtimeCfg.APIServerReadTimeout(),
		WriteTimeout:      runtimeCfg.APIServerWriteTimeout(),
		IdleTimeout:       runtimeCfg.APIServerIdleTimeout(),
	}

	// Start server in background
	go func() {
		slog.Info("starting API server", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server exited")
}
