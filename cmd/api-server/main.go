//	@title			Toktik Options Platform API
//	@version		1.0
//	@description	Backend data services for multi-market analytics, including market data retrieval, feature/factor/fundamental queries, screening, strategy catalog, and backtesting.
//	@termsOfService	https://toktik.dev/terms

//	@contact.name	Toktik Dev Team
//	@contact.url	https://toktik.dev
//	@contact.email	dev@toktik.dev

//	@license.name	Proprietary
//	@license.url	https://toktik.dev/license

//	@host		localhost:9010
//	@BasePath	/api/v1

//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						X-API-Key

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

	"github.com/Cyvadra/toktik/internal/api"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/chrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/service"
	_ "github.com/Cyvadra/toktik/pkg/dsl/catalog"
	"github.com/Cyvadra/toktik/pkg/feeds"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
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

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}

	mysqlDSN, err := runtimeCfg.MySQLDSN()
	if err != nil {
		return fmt.Errorf("build mysql dsn: %w", err)
	}
	gormDB, err := gorm.Open(mysql.Open(mysqlDSN), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("mysql sql db: %w", err)
	}
	defer func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			slog.Error("close mysql", "error", closeErr)
		}
	}()

	calendarRepo := calendarrepo.New(gormDB)
	if err := calendarRepo.AutoMigrate(ctx); err != nil {
		return fmt.Errorf("migrate finance calendar tables: %w", err)
	}

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:    ddlFile,
		Kline:      true,
		SpotKline:  true,
		ChainCache: true,
		OptionWall: true,
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

	cacheStore, err := initAPIStore(ctx, runtimeCfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := cacheStore.Close(); closeErr != nil {
			slog.Error("close cache store", "error", closeErr)
		}
	}()

	repo := chrepo.NewRepo(conn)
	apiServices, err := buildAPICoreServices(runtimeCfg, repo, calendarRepo, cacheStore, factorStore)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := apiServices.backtests.Close(); closeErr != nil {
			slog.Error("close backtest service", "error", closeErr)
		}
	}()

	polygonSvc, err := service.NewPolygonServiceFromConfig(runtimeCfg, cacheStore)
	if err != nil {
		return fmt.Errorf("init polygon service: %w", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	deps := buildAPIDeps(runtimeCfg, repo, factorStore, apiServices, polygonSvc, cacheStore, stop)

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

	refreshers := startAPIRefreshers(ctx, runtimeCfg, apiServices, polygonSvc, cacheStore)

	select {
	case <-signalCtx.Done():
		slog.Info("shutting down server", "reason", "signal")
	case err := <-serverErr:
		cancel()
		refreshers.Wait()
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}

	cancel()
	refreshers.Wait()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}
	slog.Info("server exited")
	return nil
}
