// @title           Toktik Options Platform API
// @version         1.0
// @description     Backend data services for multi-market analytics, including market data retrieval, feature/factor/fundamental queries, screening, strategy catalog, and backtesting.
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
	"errors"
	"flag"
	"fmt"
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
	"github.com/gin-gonic/gin"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run is the real entrypoint. Returning errors instead of calling
// os.Exit allows deferred Close() calls to run cleanly on every path.
func run() error {
	appCli.SetupLogger(true, slog.LevelInfo)
	gin.SetMode(gin.ReleaseMode)

	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("load runtime config: %w", err)
	}

	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	addr := flag.String("addr", runtimeCfg.APIServer.ListenAddr, "Listen address")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:    ddlFile,
		Kline:      true,
		SpotKline:  true,
		ChainCache: true,
	})
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}

	factorStore, err := feeds.NewStore(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect factor store: %w", err)
	}
	defer func() {
		if closeErr := factorStore.Close(); closeErr != nil {
			slog.Error("close factor store", "error", closeErr)
		}
	}()

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

	repo := chrepo.NewRepo(conn)
	fundamentalsSvc := service.NewFundamentalsService(repo)
	macroSvc := service.NewMacroService(repo)

	deps := api.Deps{
		Config:            runtimeCfg,
		CryptoOptions:     service.NewCryptoOptionsService(repo),
		USStocks:          service.NewUSStocksService(repo, fundamentalsSvc),
		USOptions:         service.NewUSOptionsService(repo),
		Infra:             service.NewInfraService(repo),
		DataBrowser:       service.NewDataBrowserService(repo),
		Features:          service.NewFeatureService(repo),
		Indicators:        service.NewIndicatorService(repo),
		StrategyBacktests: service.NewPortfolioBacktestService(repo, factorStore),
		CryptoSpot:        service.NewCryptoSpotService(repo),
		Forex:             service.NewForexService(repo),
		Screener:          service.NewScreenerService(repo, cacheStore),
		StrategyCatalog:   service.NewStrategyCatalogService(),
		Factors:           service.NewFactorService(factorStore),
		Fundamentals:      fundamentalsSvc,
		Macro:             macroSvc,
	}

	polygonSvc, err := service.NewPolygonServiceFromConfig(runtimeCfg, cacheStore)
	if err != nil {
		return fmt.Errorf("init polygon service: %w", err)
	}
	deps.Polygon = polygonSvc

	stop := make(chan struct{})
	defer close(stop)
	deps.Stop = stop

	router := api.NewRouterFromDeps(deps)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadHeaderTimeout: runtimeCfg.APIServerReadHeaderTimeout(),
		ReadTimeout:       runtimeCfg.APIServerReadTimeout(),
		WriteTimeout:      runtimeCfg.APIServerWriteTimeout(),
		IdleTimeout:       runtimeCfg.APIServerIdleTimeout(),
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("starting API server", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutting down server", "signal", sig.String())
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}
	slog.Info("server exited")
	return nil
}
