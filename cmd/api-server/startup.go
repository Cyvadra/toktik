package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Cyvadra/toktik/internal/api"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/logorepo"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/universerepo"
	"github.com/Cyvadra/toktik/pkg/feeds"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	redisStartupRetryCount = 10
	redisStartupRetryDelay = 3 * time.Second
)

type apiCoreServices struct {
	fundamentals    *service.FundamentalsService
	macro           *service.MacroService
	financeCalendar *service.FinanceCalendarService
	usStocks        *service.USStocksService
	screener        *service.ScreenerService
	universes       *service.UniverseService
	backtests       *service.PortfolioBacktestService
	latestMarket    *service.LatestUSMarketCache
	logos           api.LogoProvider
	fmpClient       *fmp.Client
}

type apiRefresherGroup struct {
	turnover     *service.USTurnoverIntersectionCacheRefresher
	latestMarket *service.LatestUSMarketRefresher
}

func (g apiRefresherGroup) Wait() {
	if g.turnover != nil {
		g.turnover.Wait()
	}
	if g.latestMarket != nil {
		g.latestMarket.Wait()
	}
}

func buildAPICoreServices(runtimeCfg config.Runtime, repo *chrepo.Repo, calendarRepo *calendarrepo.Repo, universeRepo *universerepo.Repo, logoRepo *logorepo.Repo, cacheStore cache.Store, factorStore *feeds.Store) (*apiCoreServices, error) {
	fundamentalsSvc := service.NewFundamentalsService(repo)
	macroSvc := service.NewMacroService(repo)
	fmpAPIKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return nil, fmt.Errorf("read FMP api key: %w", err)
	}
	fmpClient := fmp.New(fmpAPIKey, fmp.WithCacheDir(runtimeCfg.FMP.CacheDir))
	financeCalendarSvc := service.NewFinanceCalendarService(calendarRepo, fmpClient, cacheStore)
	companyProfileProvider := service.NewClickHouseUSStockCompanyProfileProvider(repo)
	latestMarket := service.NewLatestUSMarketCache(cacheStore, runtimeCfg.LatestMarketDataRedisTTL())
	screenerSvc := service.NewScreenerService(repo, cacheStore).
		WithCompanyProfileProvider(companyProfileProvider).
		WithLatestMarketCache(latestMarket)
	universeSvc := service.NewUniverseService(repo, universeRepo).
		WithRebuildStart(runtimeCfg.UniverseRebuildStart()).
		WithTurnoverScreener(screenerSvc)
	backtests := service.NewPortfolioBacktestService(repo, factorStore).
		WithReportsRoot(runtimeCfg.Paths.ReportsRoot).
		WithUniverseService(universeSvc)

	return &apiCoreServices{
		fundamentals:    fundamentalsSvc,
		macro:           macroSvc,
		financeCalendar: financeCalendarSvc,
		usStocks: service.NewUSStocksService(repo, fundamentalsSvc).
			WithCompanyProfileProvider(companyProfileProvider).
			WithLatestMarketCache(latestMarket),
		screener:     screenerSvc,
		universes:    universeSvc,
		backtests:    backtests,
		latestMarket: latestMarket,
		logos:        service.NewUSStockLogoService(logoRepo, fmpClient, cacheStore),
		fmpClient:    fmpClient,
	}, nil
}

func buildAPIDeps(runtimeCfg config.Runtime, repo *chrepo.Repo, factorStore *feeds.Store, services *apiCoreServices, polygonSvc *service.PolygonService, cacheStore cache.Store, apiKeyAuth api.APIKeyAuthenticator, stop chan struct{}) api.Deps {
	return api.Deps{
		Config:            runtimeCfg,
		CryptoOptions:     service.NewCryptoOptionsService(repo),
		USStocks:          services.usStocks,
		USOptions:         service.NewUSOptionsService(repo).WithPolygonClient(polygonSvc).WithCache(cacheStore).WithLatestMarketCache(services.latestMarket),
		Infra:             service.NewInfraService(repo),
		DataBrowser:       service.NewDataBrowserService(repo),
		Features:          service.NewFeatureService(repo),
		Indicators:        service.NewIndicatorService(repo),
		StrategyBacktests: services.backtests,
		CryptoSpot:        service.NewCryptoSpotService(repo),
		Forex:             service.NewForexService(repo),
		Screener:          services.screener,
		Universes:         services.universes,
		StrategyCatalog:   service.NewStrategyCatalogService(),
		Factors:           service.NewFactorService(factorStore).WithMacroService(services.macro),
		Fundamentals:      services.fundamentals,
		Macro:             services.macro,
		FinanceCalendar:   services.financeCalendar,
		Logos:             services.logos,
		Polygon:           polygonSvc,
		APIKeys:           apiKeyAuth,
		Stop:              stop,
	}
}

func initAPIStore(ctx context.Context, runtimeCfg config.Runtime) (cache.Store, error) {
	if !runtimeCfg.Redis.Enabled {
		return cache.NewMemoryStore(), nil
	}

	var lastErr error
	for attempt := 1; attempt <= redisStartupRetryCount; attempt++ {
		store, err := cache.NewRedisStore(ctx, runtimeCfg)
		if err == nil {
			if attempt > 1 {
				slog.Info("redis cache connection recovered", "attempt", attempt)
			}
			return store, nil
		}
		lastErr = err
		slog.Warn("redis cache connection failed", "attempt", attempt, "max_attempts", redisStartupRetryCount, "retry_delay", redisStartupRetryDelay.String(), "error", err)
		if attempt == redisStartupRetryCount {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("init redis cache store: %w", ctx.Err())
		case <-time.After(redisStartupRetryDelay):
		}
	}
	return nil, fmt.Errorf("init redis cache store after %d attempts: %w", redisStartupRetryCount, lastErr)
}

func startAPIRefreshers(ctx context.Context, runtimeCfg config.Runtime, services *apiCoreServices, polygonSvc *service.PolygonService, cacheStore cache.Store) apiRefresherGroup {
	return apiRefresherGroup{
		turnover: service.StartUSTurnoverIntersectionCacheRefresher(
			ctx,
			slog.Default(),
			services.screener,
			cacheStore,
			true,
			runtimeCfg.APIServerWarmupRefreshInterval(),
			runtimeCfg.APIServerWarmupCooldown(),
			15*time.Minute,
		),
		latestMarket: service.StartLatestUSMarketCacheRefresher(ctx, service.LatestUSMarketRefresherConfig{
			Runtime:   runtimeCfg,
			Logger:    slog.Default(),
			Store:     cacheStore,
			Screener:  services.screener,
			FMPClient: services.fmpClient,
			Polygon:   polygonSvc,
			Now:       time.Now,
		}),
	}
}
