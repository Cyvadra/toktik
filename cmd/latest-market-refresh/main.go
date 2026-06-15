package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	appCli.SetupLogger(true, slog.LevelInfo)
	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("load runtime config: %w", err)
	}
	clickHouseDSN := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	jsonOut := flag.Bool("json", false, "Print refresh result as JSON")
	symbolsCSV := flag.String("symbols", "", "Comma-separated underlyings to refresh instead of the turnover-derived pool")
	flag.Parse()

	ctx := context.Background()
	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}
	conn, err := appCli.ConnectClickHouse(ctx, *clickHouseDSN, &appCli.SchemaInit{DDLFile: ddlFile, Kline: true, SpotKline: true, ChainCache: true, OptionWall: true})
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}

	store, err := cache.NewStore(ctx, runtimeCfg)
	if err != nil {
		return fmt.Errorf("init cache store: %w", err)
	}
	defer store.Close()

	repo := chrepo.NewRepo(conn)
	fmpAPIKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return fmt.Errorf("read FMP api key: %w", err)
	}
	fmpClient := fmp.New(fmpAPIKey, fmp.WithCacheDir(runtimeCfg.FMP.CacheDir))
	companyProfileProvider := service.NewClickHouseUSStockCompanyProfileProvider(repo)
	latestMarket := service.NewLatestUSMarketCache(store, runtimeCfg.LatestMarketDataRedisTTL())
	screener := service.NewScreenerService(repo, store).WithCompanyProfileProvider(companyProfileProvider).WithLatestMarketCache(latestMarket)
	polygonSvc, err := service.NewPolygonServiceFromConfig(runtimeCfg, store)
	if err != nil {
		return fmt.Errorf("init polygon service: %w", err)
	}

	refreshCfg := service.LatestUSMarketRefresherConfig{
		Runtime:   runtimeCfg,
		Logger:    slog.Default(),
		Store:     store,
		Screener:  screener,
		FMPClient: fmpClient,
		Polygon:   polygonSvc,
		Now:       time.Now,
	}
	var result service.LatestUSMarketRefreshResult
	if strings.TrimSpace(*symbolsCSV) != "" {
		result, err = service.RefreshLatestUSMarketCacheSymbols(ctx, latestMarket, refreshCfg, strings.Split(*symbolsCSV, ","))
	} else {
		result, err = service.RefreshLatestUSMarketCacheOnce(ctx, latestMarket, refreshCfg)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		payload, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(payload))
		return nil
	}
	slog.Info("latest market refresh completed", "pool_size", result.PoolSize, "stock_symbols", result.StockSymbols, "stock_bars", result.StockBars, "option_chains", result.OptionChains, "option_contracts", result.OptionContracts, "option_bar_symbols", result.OptionBarSymbols, "option_bars", result.OptionBars, "partial", result.Partial, "errors", result.Errors)
	return nil
}
