package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
	"github.com/massive-com/client-go/v3/rest"
)

const (
	latestUSMarketCacheVersion        = "latest_us_market:v1"
	latestUSMarketStockWindowDays     = 7
	latestUSMarketFMPStockInterval    = fmp.Interval1Hour
	latestUSMarketTurnoverTopLimit    = 30
	latestUSMarketRefreshLookbackDays = 7
)

var latestUSMarketPoolLookbacks = []int{7, 20, 60, 120}

type LatestUSMarketCacheReader interface {
	MergeStockBars(ctx context.Context, symbol string, from, to time.Time, adjusted bool, rows []dto.USStockBarRow) ([]dto.USStockBarRow, bool, error)
	StockBarsDiagnostic(ctx context.Context, symbol string, from, to time.Time) (LatestUSStockDailyCache, bool, error)
	MergeOptionBars(ctx context.Context, symbol string, from, to time.Time, rows []dto.USOptionBarRow) ([]dto.USOptionBarRow, bool, error)
	MergeOptionChain(ctx context.Context, underlying string, expiration time.Time, from, to time.Time, rows []dto.USOptionChainSnapshot) ([]dto.USOptionChainSnapshot, bool, error)
	LatestOptionChainSnapshot(ctx context.Context, underlying string, expiration time.Time) (dto.USOptionChainSnapshot, bool, error)
}

type LatestUSMarketCache struct {
	store cache.Store
	ttl   time.Duration
}

type LatestUSStockDailyCache struct {
	Symbol      string                  `json:"symbol"`
	Provider    string                  `json:"provider"`
	AsOf        time.Time               `json:"as_of"`
	Provisional bool                    `json:"provisional"`
	Adjusted    *bool                   `json:"adjusted,omitempty"`
	Bars        []LatestUSStockDailyBar `json:"bars"`
}

type LatestUSStockDailyBar struct {
	Timestamp    time.Time `json:"timestamp"`
	Symbol       string    `json:"symbol"`
	Open         float32   `json:"open"`
	High         float32   `json:"high"`
	Low          float32   `json:"low"`
	Close        float32   `json:"close"`
	Volume       float64   `json:"volume"`
	Transactions uint64    `json:"transactions"`
	Provider     string    `json:"provider,omitempty"`
}

type LatestUSOptionDailyCache struct {
	Symbol      string                   `json:"symbol"`
	Provider    string                   `json:"provider"`
	AsOf        time.Time                `json:"as_of"`
	Provisional bool                     `json:"provisional"`
	Bars        []LatestUSOptionDailyBar `json:"bars"`
}

type LatestUSOptionDailyBar struct {
	Timestamp         time.Time `json:"timestamp"`
	Symbol            string    `json:"symbol"`
	Underlying        string    `json:"underlying"`
	OptionType        string    `json:"option_type"`
	Expiration        time.Time `json:"expiration"`
	Strike            float64   `json:"strike"`
	Open              float32   `json:"open"`
	High              float32   `json:"high"`
	Low               float32   `json:"low"`
	Close             float32   `json:"close"`
	UnderlyingClose   float32   `json:"underlying_close"`
	ImpliedVolatility float32   `json:"implied_volatility"`
	Delta             float32   `json:"delta"`
	Gamma             float32   `json:"gamma"`
	Vega              float32   `json:"vega"`
	Theta             float32   `json:"theta"`
	Rho               float32   `json:"rho"`
	Volume            float64   `json:"volume"`
	Transactions      uint64    `json:"transactions"`
	Provider          string    `json:"provider,omitempty"`
}

type LatestUSOptionChainCache struct {
	Underlying  string                    `json:"underlying"`
	Provider    string                    `json:"provider"`
	AsOf        time.Time                 `json:"as_of"`
	Provisional bool                      `json:"provisional"`
	Snapshot    dto.USOptionChainSnapshot `json:"snapshot"`
}

type LatestUSMarketRefreshState struct {
	LastStartedAt       time.Time                                   `json:"last_started_at"`
	LastSuccessAt       time.Time                                   `json:"last_success_at"`
	LastFailureAt       time.Time                                   `json:"last_failure_at,omitempty"`
	ConsecutiveFailures int                                         `json:"consecutive_failures,omitempty"`
	LastProbe           string                                      `json:"last_probe,omitempty"`
	LastAlertAt         time.Time                                   `json:"last_alert_at,omitempty"`
	StageResults        map[string]LatestUSMarketRefreshStageResult `json:"stage_results,omitempty"`
}

type LatestUSMarketRefreshStageResult struct {
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}

type LatestUSMarketRefreshResult struct {
	PoolSize         int
	StockBars        int
	StockSymbols     int
	OptionChains     int
	OptionContracts  int
	OptionBars       int
	OptionBarSymbols int
	Partial          bool     `json:"partial,omitempty"`
	Errors           []string `json:"errors,omitempty"`
}

const (
	latestUSMarketStageResolvePool  = "resolve_pool"
	latestUSMarketStageStockBars    = "stock_bars"
	latestUSMarketStageOptionChains = "option_chains"
)

type LatestUSMarketRefresher struct {
	done chan struct{}
}

type latestUSMarketStatusProvider interface {
	MarketStatusNow(ctx context.Context) (*polygonpkg.MarketStatus, error)
}

type latestUSMarketPolygonProvider interface {
	latestUSMarketStatusProvider
	StockAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error)
	OptionChain(ctx context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error)
	OptionAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error)
}

type latestUSMarketScreener interface {
	ScreenUSTurnoverIntersection(ctx context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error)
}

type latestUSMarketNotifier interface {
	Alert(ctx context.Context, message string, attrs ...any)
}

type slogLatestUSMarketNotifier struct {
	logger *slog.Logger
}

func (n slogLatestUSMarketNotifier) Alert(_ context.Context, message string, attrs ...any) {
	logger := n.logger
	if logger == nil {
		logger = slog.Default()
	}
	base := []any{"alert_channel", "wecom", "alert_type", "latest_market_data_stale"}
	logger.Error(message, append(base, attrs...)...)
}

type LatestUSMarketRefresherConfig struct {
	Runtime   config.Runtime
	Logger    *slog.Logger
	Store     cache.Store
	Screener  latestUSMarketScreener
	FMPClient *fmp.Client
	Polygon   latestUSMarketPolygonProvider
	Notifier  latestUSMarketNotifier
	Now       func() time.Time
}

func NewLatestUSMarketCache(store cache.Store, ttl time.Duration) *LatestUSMarketCache {
	return &LatestUSMarketCache{store: store, ttl: ttl}
}

func StartLatestUSMarketCacheRefresher(ctx context.Context, cfg LatestUSMarketRefresherConfig) *LatestUSMarketRefresher {
	refresher := &LatestUSMarketRefresher{done: make(chan struct{})}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Runtime.LatestMarketData.Enabled {
		logger.Info("latest us market cache refresher disabled", "enabled", false)
		close(refresher.done)
		return refresher
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Notifier == nil {
		cfg.Notifier = slogLatestUSMarketNotifier{logger: logger}
	}
	cacheReader := NewLatestUSMarketCache(cfg.Store, cfg.Runtime.LatestMarketDataRedisTTL())
	logger.Info("starting latest us market cache refresher",
		"enabled", true,
		"redis_ttl", cfg.Runtime.LatestMarketDataRedisTTL().String(),
		"open_interval", cfg.Runtime.LatestMarketDataOpenRefreshInterval().String(),
		"closed_interval", cfg.Runtime.LatestMarketDataClosedRefreshInterval().String(),
		"stock_provider", cfg.Runtime.LatestMarketData.StockProvider,
		"option_provider", cfg.Runtime.LatestMarketData.OptionProvider,
		"option_chain_limit", cfg.Runtime.LatestMarketData.OptionChainLimit,
		"option_aggregate_limit", cfg.Runtime.LatestMarketData.OptionAggregateLimit,
	)
	go func() {
		defer close(refresher.done)
		nextDelay := time.Duration(0)
		for {
			if nextDelay > 0 {
				timer := time.NewTimer(nextDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					logger.Info("stopped latest us market cache refresher")
					return
				case <-timer.C:
				}
			} else if ctx.Err() != nil {
				return
			}
			marketOpen, err := latestUSMarketIsOpen(ctx, cfg.Polygon)
			if err != nil {
				logger.Warn("latest us market status check failed", "error", err)
			}
			logger.Info("checked latest us market status", "market_open", marketOpen)
			if !marketOpen {
				changed, err := latestUSMarketSmokeChanged(ctx, cacheReader, cfg)
				if err != nil {
					logger.Warn("latest us market smoke check failed", "error", err)
				} else if !changed {
					logger.Info("skipped latest us market refresh: smoke unchanged", "next_refresh_in", cfg.Runtime.LatestMarketDataClosedRefreshInterval().String())
					nextDelay = cfg.Runtime.LatestMarketDataClosedRefreshInterval()
					continue
				} else {
					logger.Info("latest us market smoke changed; running refresh")
				}
			}
			result, err := runLatestUSMarketRefresh(ctx, cacheReader, cfg)
			if err != nil {
				logger.Warn("refresh latest us market cache failed", "error", err)
				_ = cacheReader.recordRunResult(ctx, cfg.Now().UTC(), result, err)
				latestUSMarketMaybeAlert(ctx, cacheReader, cfg, marketOpen, err)
			} else {
				logger.Info("refreshed latest us market cache",
					"pool_size", result.PoolSize,
					"stock_symbols", result.StockSymbols,
					"stock_bars", result.StockBars,
					"option_chains", result.OptionChains,
					"option_contracts", result.OptionContracts,
					"option_bar_symbols", result.OptionBarSymbols,
					"option_bars", result.OptionBars,
					"partial", result.Partial,
				)
				_ = cacheReader.recordRunResult(ctx, cfg.Now().UTC(), result, nil)
			}
			latestUSMarketMaybeAlert(ctx, cacheReader, cfg, marketOpen, nil)
			if marketOpen {
				nextDelay = cfg.Runtime.LatestMarketDataOpenRefreshInterval()
			} else {
				nextDelay = cfg.Runtime.LatestMarketDataClosedRefreshInterval()
			}
			logger.Info("scheduled next latest us market refresh", "next_refresh_in", nextDelay.String(), "market_open", marketOpen)
		}
	}()
	return refresher
}

func (r *LatestUSMarketRefresher) Wait() {
	if r == nil || r.done == nil {
		return
	}
	<-r.done
}

func RefreshLatestUSMarketCacheOnce(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig) (LatestUSMarketRefreshResult, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return runLatestUSMarketRefresh(ctx, cacheReader, cfg)
}

func RefreshLatestUSMarketCacheSymbols(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig, symbols []string) (LatestUSMarketRefreshResult, error) {
	var result LatestUSMarketRefreshResult
	if cacheReader == nil || cacheReader.store == nil {
		return result, fmt.Errorf("latest market cache store is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	symbols = normalizeLatestUSMarketSymbols(symbols)
	if len(symbols) == 0 {
		return result, fmt.Errorf("latest market symbols are required")
	}
	refreshCtx := ctx
	cancel := func() {}
	if timeout := cfg.Runtime.LatestMarketDataRefreshTimeout(); timeout > 0 {
		refreshCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	startedAt := cfg.Now().UTC()
	_ = cacheReader.updateState(refreshCtx, func(state *LatestUSMarketRefreshState) {
		state.LastStartedAt = startedAt
	})
	result.PoolSize = len(symbols)
	stockStarted := cfg.Now().UTC()
	stockSymbols, stockBars, err := refreshLatestUSStockBars(refreshCtx, cacheReader, cfg, symbols)
	result.StockSymbols = stockSymbols
	result.StockBars = stockBars
	_ = cacheReader.recordStageResult(context.Background(), latestUSMarketStageStockBars, stockStarted, cfg.Now().UTC(), stockSymbols, len(symbols)-stockSymbols, err)
	if err != nil {
		result.addStageError(err)
		if stockSymbols == 0 {
			_ = cacheReader.recordRunResult(context.Background(), cfg.Now().UTC(), result, err)
			return result, err
		}
	}
	optionStarted := cfg.Now().UTC()
	optionChains, optionContracts, optionBarSymbols, optionBars, err := refreshLatestUSOptionSnapshots(refreshCtx, cacheReader, cfg, symbols)
	result.OptionChains = optionChains
	result.OptionContracts = optionContracts
	result.OptionBarSymbols = optionBarSymbols
	result.OptionBars = optionBars
	_ = cacheReader.recordStageResult(context.Background(), latestUSMarketStageOptionChains, optionStarted, cfg.Now().UTC(), optionChains, len(symbols)-optionChains, err)
	if err != nil {
		result.addStageError(err)
		if optionChains == 0 && !result.hasUsefulWrites() {
			_ = cacheReader.recordRunResult(context.Background(), cfg.Now().UTC(), result, err)
			return result, err
		}
	}
	_ = cacheReader.recordRunResult(context.Background(), cfg.Now().UTC(), result, nil)
	return result, nil
}

func (r *LatestUSMarketRefreshResult) addStageError(err error) {
	if r == nil || err == nil {
		return
	}
	r.Partial = true
	r.Errors = append(r.Errors, err.Error())
}

func (r LatestUSMarketRefreshResult) hasUsefulWrites() bool {
	return r.StockSymbols > 0 || r.OptionChains > 0 || r.OptionBarSymbols > 0
}

func normalizeLatestUSMarketSymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func runLatestUSMarketRefresh(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig) (LatestUSMarketRefreshResult, error) {
	var result LatestUSMarketRefreshResult
	if cacheReader == nil || cacheReader.store == nil {
		return result, fmt.Errorf("latest market cache store is required")
	}
	if cfg.Screener == nil {
		return result, fmt.Errorf("latest market screener is required")
	}
	refreshCtx := ctx
	cancel := func() {}
	if timeout := cfg.Runtime.LatestMarketDataRefreshTimeout(); timeout > 0 {
		refreshCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	startedAt := cfg.Now().UTC()
	_ = cacheReader.updateState(refreshCtx, func(state *LatestUSMarketRefreshState) {
		state.LastStartedAt = startedAt
	})
	poolStarted := cfg.Now().UTC()
	pool, err := ResolveLatestUSMarketPrewarmPool(refreshCtx, cfg.Screener)
	if err != nil {
		_ = cacheReader.recordStageResult(refreshCtx, latestUSMarketStageResolvePool, poolStarted, cfg.Now().UTC(), 0, 1, err)
		return result, err
	}
	pool = prioritizeLatestUSMarketPool(pool, cfg.Runtime.LatestMarketData.AlwaysRefreshSymbols)
	result.PoolSize = len(pool)
	if len(pool) == 0 {
		err := fmt.Errorf("latest market prewarm pool is empty")
		_ = cacheReader.recordStageResult(refreshCtx, latestUSMarketStageResolvePool, poolStarted, cfg.Now().UTC(), 0, 1, err)
		return result, err
	}
	_ = cacheReader.recordStageResult(refreshCtx, latestUSMarketStageResolvePool, poolStarted, cfg.Now().UTC(), len(pool), 0, nil)
	if cfg.Logger != nil {
		cfg.Logger.Info("resolved latest us market prewarm pool", "pool_size", len(pool), "lookbacks", latestUSMarketPoolLookbacks, "top_limit", latestUSMarketTurnoverTopLimit)
	}
	stockStarted := cfg.Now().UTC()
	stockSymbols, stockBars, err := refreshLatestUSStockBars(refreshCtx, cacheReader, cfg, pool)
	result.StockSymbols = stockSymbols
	result.StockBars = stockBars
	_ = cacheReader.recordStageResult(refreshCtx, latestUSMarketStageStockBars, stockStarted, cfg.Now().UTC(), stockSymbols, len(pool)-stockSymbols, err)
	if err != nil {
		result.addStageError(err)
		if stockSymbols == 0 {
			return result, err
		}
	}
	if cfg.Logger != nil {
		cfg.Logger.Info("refreshed latest us stock cache stage", "stock_symbols", stockSymbols, "stock_bars", stockBars)
	}
	optionStarted := cfg.Now().UTC()
	optionChains, optionContracts, optionBarSymbols, optionBars, err := refreshLatestUSOptionSnapshots(refreshCtx, cacheReader, cfg, pool)
	result.OptionChains = optionChains
	result.OptionContracts = optionContracts
	result.OptionBarSymbols = optionBarSymbols
	result.OptionBars = optionBars
	_ = cacheReader.recordStageResult(refreshCtx, latestUSMarketStageOptionChains, optionStarted, cfg.Now().UTC(), optionChains, len(pool)-optionChains, err)
	if err != nil {
		result.addStageError(err)
		if optionChains == 0 && !result.hasUsefulWrites() {
			return result, err
		}
	}
	return result, nil
}

func ResolveLatestUSMarketPrewarmPool(ctx context.Context, screener latestUSMarketScreener) ([]string, error) {
	if screener == nil {
		return nil, fmt.Errorf("latest market screener is required")
	}
	seen := make(map[string]struct{})
	pool := make([]string, 0, latestUSMarketTurnoverTopLimit*len(latestUSMarketPoolLookbacks)*2)
	for _, lookbackDays := range latestUSMarketPoolLookbacks {
		for _, nonETFOnly := range []bool{true, false} {
			resp, err := screener.ScreenUSTurnoverIntersection(ctx, dto.ScreenUSTurnoverIntersectionRequest{
				Limit:        latestUSMarketTurnoverTopLimit,
				LookbackDays: lookbackDays,
				NonETFOnly:   nonETFOnly,
			})
			if err != nil {
				return nil, fmt.Errorf("resolve latest market prewarm pool for %d-day turnover non_etf_only=%t: %w", lookbackDays, nonETFOnly, err)
			}
			for _, row := range resp.Data {
				symbol := strings.ToUpper(strings.TrimSpace(row.Underlying))
				if symbol == "" {
					continue
				}
				if _, ok := seen[symbol]; ok {
					continue
				}
				seen[symbol] = struct{}{}
				pool = append(pool, symbol)
			}
		}
	}
	return pool, nil
}

func prioritizeLatestUSMarketPool(pool []string, priority []string) []string {
	out := make([]string, 0, len(pool)+len(priority))
	added := make(map[string]struct{}, len(pool)+len(priority))
	for _, symbol := range priority {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if _, ok := added[symbol]; ok {
			continue
		}
		added[symbol] = struct{}{}
		out = append(out, symbol)
	}
	for _, symbol := range pool {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if _, ok := added[symbol]; ok {
			continue
		}
		added[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func refreshLatestUSStockBars(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig, symbols []string) (int, int, error) {
	provider := cfg.Runtime.LatestMarketData.StockProvider
	to := cfg.Now().UTC()
	from := to.AddDate(0, 0, -latestUSMarketStockWindowDays)
	workers := clampLatestUSMarketWorkers(cfg.Runtime.LatestMarketData.Workers, len(symbols))
	var mu sync.Mutex
	symbolCount := 0
	barCount := 0
	errCh := make(chan error, 1)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				var bars []LatestUSStockDailyBar
				var err error
				adjusted := true
				if provider == "polygon" {
					bars, err = fetchLatestUSStockBarsFromPolygon(ctx, cfg.Polygon, symbol, from, to)
				} else {
					bars, err = fetchLatestUSStockBarsFromFMP(ctx, cfg.FMPClient, symbol, from, to)
				}
				if err != nil {
					select {
					case errCh <- fmt.Errorf("refresh latest stock bars %s: %w", symbol, err):
					default:
					}
					continue
				}
				if len(bars) == 0 {
					continue
				}
				if err := cacheReader.StoreStockBars(ctx, symbol, provider, adjusted, bars); err != nil {
					select {
					case errCh <- err:
					default:
					}
					continue
				}
				mu.Lock()
				symbolCount++
				barCount += len(bars)
				mu.Unlock()
			}
		}()
	}
	for _, symbol := range symbols {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return symbolCount, barCount, ctx.Err()
		case jobs <- symbol:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return symbolCount, barCount, err
	default:
	}
	return symbolCount, barCount, nil
}

func refreshLatestUSOptionSnapshots(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig, underlyings []string) (int, int, int, int, error) {
	if cfg.Polygon == nil {
		return 0, 0, 0, 0, fmt.Errorf("polygon provider is required for latest options")
	}
	limit := cfg.Runtime.LatestMarketData.OptionChainLimit
	aggregateLimit := cfg.Runtime.LatestMarketData.OptionAggregateLimit
	to := cfg.Now().UTC()
	from := to.AddDate(0, 0, -latestUSMarketRefreshLookbackDays)
	workers := clampLatestUSMarketWorkers(cfg.Runtime.LatestMarketData.Workers, len(underlyings))
	var mu sync.Mutex
	chainCount := 0
	contractCount := 0
	barSymbolCount := 0
	barCount := 0
	errCh := make(chan error, 1)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for underlying := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				contracts, err := cfg.Polygon.OptionChain(ctx, polygonpkg.OptionChainRequest{Underlying: underlying, Limit: limit})
				if err != nil {
					select {
					case errCh <- fmt.Errorf("refresh latest option chain %s: %w", underlying, err):
					default:
					}
					continue
				}
				snapshot := mapLatestUSOptionChainSnapshot(underlying, contracts, to)
				if len(snapshot.Contracts) > 0 {
					if err := cacheReader.StoreOptionChain(ctx, underlying, "polygon", snapshot); err != nil {
						select {
						case errCh <- err:
						default:
						}
						continue
					}
					mu.Lock()
					chainCount++
					contractCount += len(snapshot.Contracts)
					currentChainCount := chainCount
					currentBarSymbolCount := barSymbolCount
					currentBarCount := barCount
					mu.Unlock()
					if cfg.Logger != nil && (currentChainCount == 1 || currentChainCount%10 == 0) {
						cfg.Logger.Info("refreshed latest us option chain progress", "chains", currentChainCount, "underlying", underlying, "contracts", len(snapshot.Contracts), "option_bar_symbols", currentBarSymbolCount, "option_bars", currentBarCount)
					}
				}
				contractSymbols := optionAggregateCandidates(snapshot.Contracts, aggregateLimit)
				for _, symbol := range contractSymbols {
					bars, err := fetchLatestUSOptionBarsFromPolygon(ctx, cfg.Polygon, snapshot, symbol, from, to)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("refresh latest option bars %s: %w", symbol, err):
						default:
						}
						continue
					}
					if len(bars) == 0 {
						continue
					}
					if err := cacheReader.StoreOptionBars(ctx, symbol, "polygon", bars); err != nil {
						select {
						case errCh <- err:
						default:
						}
						continue
					}
					mu.Lock()
					barSymbolCount++
					barCount += len(bars)
					mu.Unlock()
				}
			}
		}()
	}
	for _, underlying := range underlyings {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return chainCount, contractCount, barSymbolCount, barCount, ctx.Err()
		case jobs <- underlying:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return chainCount, contractCount, barSymbolCount, barCount, err
	default:
	}
	return chainCount, contractCount, barSymbolCount, barCount, nil
}

func clampLatestUSMarketWorkers(workers, jobs int) int {
	if jobs <= 0 {
		return 1
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > jobs {
		return jobs
	}
	return workers
}

func latestUSMarketIsOpen(ctx context.Context, provider latestUSMarketStatusProvider) (bool, error) {
	if provider == nil {
		return false, fmt.Errorf("market status provider is required")
	}
	status, err := provider.MarketStatusNow(ctx)
	if err != nil {
		return false, err
	}
	if status == nil {
		return false, nil
	}
	return status.IsUSStocksOpen(), nil
}

func latestUSMarketSmokeChanged(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig) (bool, error) {
	symbols := cfg.Runtime.LatestMarketData.SmokeSymbols
	if len(symbols) == 0 {
		symbols = []string{"SPY", "AAPL"}
	}
	to := cfg.Now().UTC()
	from := to.AddDate(0, 0, -2)
	parts := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		bars, err := fetchLatestUSStockBarsFromFMP(ctx, cfg.FMPClient, symbol, from, to)
		if err != nil {
			return false, err
		}
		if len(bars) == 0 {
			continue
		}
		bar := bars[len(bars)-1]
		parts = append(parts, fmt.Sprintf("%s:%s:%.4f:%.0f", symbol, bar.Timestamp.Format("2006-01-02"), bar.Close, bar.Volume))
	}
	probe := strings.Join(parts, "|")
	state, _ := cacheReader.loadState(ctx)
	if probe == "" {
		return true, nil
	}
	if state.LastProbe == probe {
		return false, nil
	}
	_ = cacheReader.updateState(ctx, func(state *LatestUSMarketRefreshState) {
		state.LastProbe = probe
	})
	return true, nil
}

func latestUSMarketMaybeAlert(ctx context.Context, cacheReader *LatestUSMarketCache, cfg LatestUSMarketRefresherConfig, marketOpen bool, cause error) {
	if !marketOpen || cfg.Notifier == nil || cacheReader == nil {
		return
	}
	state, ok := cacheReader.loadState(ctx)
	if !ok {
		return
	}
	now := cfg.Now().UTC()
	if !state.LastAlertAt.IsZero() && now.Sub(state.LastAlertAt) < time.Hour {
		return
	}
	staleAfter := cfg.Runtime.LatestMarketDataStaleAlertAfter()
	stale := state.LastSuccessAt.IsZero() || now.Sub(state.LastSuccessAt) > staleAfter
	failed := state.ConsecutiveFailures > 0 && cause != nil
	if !stale && !failed {
		return
	}
	message := "latest us market data cache is stale"
	attrs := []any{"last_success_at", state.LastSuccessAt, "consecutive_failures", state.ConsecutiveFailures}
	if cause != nil {
		attrs = append(attrs, "error", cause.Error())
	}
	cfg.Notifier.Alert(ctx, message, attrs...)
	_ = cacheReader.updateState(ctx, func(state *LatestUSMarketRefreshState) {
		state.LastAlertAt = now
	})
}

func fetchLatestUSStockBarsFromFMP(ctx context.Context, client *fmp.Client, symbol string, from, to time.Time) ([]LatestUSStockDailyBar, error) {
	if client == nil {
		return nil, fmt.Errorf("fmp client is required")
	}
	intraday, err := client.IntradayPrices(ctx, symbol, latestUSMarketFMPStockInterval, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	return aggregateFMPIntradayStockBars(symbol, intraday), nil
}

func aggregateFMPIntradayStockBars(symbol string, intraday []fmp.IntradayBar) []LatestUSStockDailyBar {
	type dailyBar struct {
		bar       LatestUSStockDailyBar
		firstSeen time.Time
		lastSeen  time.Time
	}

	byDay := make(map[time.Time]dailyBar)
	normalizedSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	for _, row := range intraday {
		ts, ok := parseFMPIntradayEastern(row.Date)
		if !ok {
			continue
		}
		day := normalizeCalendarDate(ts)
		current, exists := byDay[day]
		if !exists {
			current = dailyBar{bar: LatestUSStockDailyBar{Timestamp: day, Symbol: normalizedSymbol, Open: float32(row.Open), High: float32(row.High), Low: float32(row.Low), Close: float32(row.Close), Volume: row.Volume, Provider: "fmp"}, firstSeen: ts, lastSeen: ts}
			byDay[day] = current
			continue
		}
		if ts.Before(current.firstSeen) {
			current.firstSeen = ts
			current.bar.Open = float32(row.Open)
		}
		if ts.After(current.lastSeen) {
			current.lastSeen = ts
			current.bar.Close = float32(row.Close)
		}
		if high := float32(row.High); high > current.bar.High {
			current.bar.High = high
		}
		if low := float32(row.Low); current.bar.Low == 0 || low < current.bar.Low {
			current.bar.Low = low
		}
		current.bar.Volume += row.Volume
		byDay[day] = current
	}

	out := make([]LatestUSStockDailyBar, 0, len(byDay))
	for _, current := range byDay {
		out = append(out, current.bar)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out
}

func parseFMPIntradayEastern(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.Time{}, false
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", value, location)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func fetchLatestUSStockBarsFromPolygon(ctx context.Context, provider latestUSMarketPolygonProvider, symbol string, from, to time.Time) ([]LatestUSStockDailyBar, error) {
	if provider == nil {
		return nil, fmt.Errorf("polygon provider is required")
	}
	bars, err := provider.StockAggregates(ctx, polygonpkg.AggregateRequest{Ticker: symbol, Multiplier: 1, Timespan: "day", From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Adjusted: rest.Ptr(true), Sort: "asc", Limit: 50000})
	if err != nil {
		return nil, err
	}
	out := make([]LatestUSStockDailyBar, 0, len(bars))
	for _, bar := range bars {
		out = append(out, LatestUSStockDailyBar{
			Timestamp:    unixMillisDay(bar.Timestamp),
			Symbol:       strings.ToUpper(strings.TrimSpace(symbol)),
			Open:         float32(bar.Open),
			High:         float32(bar.High),
			Low:          float32(bar.Low),
			Close:        float32(bar.Close),
			Volume:       bar.Volume,
			Transactions: uint64(maxIntPtr(bar.TradeCount)),
			Provider:     "polygon",
		})
	}
	return out, nil
}

func fetchLatestUSOptionBarsFromPolygon(ctx context.Context, provider latestUSMarketPolygonProvider, snapshot dto.USOptionChainSnapshot, symbol string, from, to time.Time) ([]LatestUSOptionDailyBar, error) {
	bars, err := provider.OptionAggregates(ctx, polygonpkg.AggregateRequest{Ticker: symbol, Multiplier: 1, Timespan: "day", From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Adjusted: rest.Ptr(true), Sort: "asc", Limit: 50000})
	if err != nil {
		return nil, err
	}
	contract := findChainContract(snapshot.Contracts, symbol)
	out := make([]LatestUSOptionDailyBar, 0, len(bars))
	for _, bar := range bars {
		out = append(out, LatestUSOptionDailyBar{
			Timestamp:         unixMillisDay(bar.Timestamp),
			Symbol:            strings.ToUpper(strings.TrimSpace(symbol)),
			Underlying:        snapshot.Underlying,
			OptionType:        contract.OptionType,
			Expiration:        contract.Expiration,
			Strike:            contract.Strike,
			Open:              float32(bar.Open),
			High:              float32(bar.High),
			Low:               float32(bar.Low),
			Close:             float32(bar.Close),
			UnderlyingClose:   contract.UnderlyingClose,
			ImpliedVolatility: contract.ImpliedVolatility,
			Delta:             contract.Delta,
			Gamma:             contract.Gamma,
			Vega:              contract.Vega,
			Theta:             contract.Theta,
			Rho:               contract.Rho,
			Volume:            bar.Volume,
			Transactions:      uint64(maxIntPtr(bar.TradeCount)),
			Provider:          "polygon",
		})
	}
	return out, nil
}

func mapLatestUSOptionChainSnapshot(underlying string, contracts []polygonpkg.OptionChainContract, timestamp time.Time) dto.USOptionChainSnapshot {
	snapshot := dto.USOptionChainSnapshot{Timestamp: timestamp.UTC(), Underlying: strings.ToUpper(strings.TrimSpace(underlying)), Contracts: make([]dto.USOptionChainContract, 0, len(contracts))}
	for _, contract := range contracts {
		expiration, _ := time.Parse("2006-01-02", contract.Contract.ExpirationDate)
		optionType := strings.ToUpper(strings.TrimSpace(contract.Contract.ContractType))
		switch optionType {
		case "CALL":
			optionType = "C"
		case "PUT":
			optionType = "P"
		}
		row := dto.USOptionChainContract{
			Symbol:          strings.ToUpper(strings.TrimSpace(contract.Contract.Ticker)),
			OptionType:      optionType,
			Expiration:      normalizeCalendarDate(expiration),
			Strike:          sanitizeFloat64(contract.Contract.StrikePrice),
			Close:           sanitizeFloat32(float32(contract.Day.Close)),
			UnderlyingClose: sanitizeFloat32(float32(firstFloat(contract.UnderlyingAsset.Price, contract.UnderlyingAsset.Value))),
			Volume:          sanitizeFloat64(contract.Day.Volume),
		}
		if contract.ImpliedVolatility != nil {
			row.ImpliedVolatility = sanitizeFloat32(float32(*contract.ImpliedVolatility))
		}
		if contract.Greeks != nil {
			row.Delta = sanitizeFloat32(float32(contract.Greeks.Delta))
			row.Gamma = sanitizeFloat32(float32(contract.Greeks.Gamma))
			row.Vega = sanitizeFloat32(float32(contract.Greeks.Vega))
			row.Theta = sanitizeFloat32(float32(contract.Greeks.Theta))
		}
		if row.Symbol != "" {
			snapshot.Contracts = append(snapshot.Contracts, row)
		}
	}
	return snapshot
}

func optionAggregateCandidates(contracts []dto.USOptionChainContract, limit int) []string {
	if limit <= 0 || len(contracts) == 0 {
		return nil
	}
	sorted := append([]dto.USOptionChainContract(nil), contracts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Volume == sorted[j].Volume {
			return sorted[i].Symbol < sorted[j].Symbol
		}
		return sorted[i].Volume > sorted[j].Volume
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	out := make([]string, 0, len(sorted))
	for _, contract := range sorted {
		if contract.Symbol != "" {
			out = append(out, contract.Symbol)
		}
	}
	return out
}

func findChainContract(contracts []dto.USOptionChainContract, symbol string) dto.USOptionChainContract {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	for _, contract := range contracts {
		if strings.ToUpper(strings.TrimSpace(contract.Symbol)) == symbol {
			return contract
		}
	}
	return dto.USOptionChainContract{Symbol: symbol}
}

func (c *LatestUSMarketCache) StoreStockBars(ctx context.Context, symbol, provider string, adjusted bool, bars []LatestUSStockDailyBar) error {
	payload := LatestUSStockDailyCache{Symbol: strings.ToUpper(strings.TrimSpace(symbol)), Provider: provider, AsOf: time.Now().UTC(), Provisional: true, Adjusted: &adjusted, Bars: bars}
	return c.setJSON(ctx, latestUSStockKey(symbol), payload)
}

func (c *LatestUSMarketCache) StoreOptionBars(ctx context.Context, symbol, provider string, bars []LatestUSOptionDailyBar) error {
	payload := LatestUSOptionDailyCache{Symbol: strings.ToUpper(strings.TrimSpace(symbol)), Provider: provider, AsOf: time.Now().UTC(), Provisional: true, Bars: bars}
	return c.setJSON(ctx, latestUSOptionKey(symbol), payload)
}

func (c *LatestUSMarketCache) StoreOptionChain(ctx context.Context, underlying, provider string, snapshot dto.USOptionChainSnapshot) error {
	payload := LatestUSOptionChainCache{Underlying: strings.ToUpper(strings.TrimSpace(underlying)), Provider: provider, AsOf: time.Now().UTC(), Provisional: true, Snapshot: snapshot}
	return c.setJSON(ctx, latestUSOptionChainKey(underlying), payload)
}

func (c *LatestUSMarketCache) MergeStockBars(ctx context.Context, symbol string, from, to time.Time, adjusted bool, rows []dto.USStockBarRow) ([]dto.USStockBarRow, bool, error) {
	var payload LatestUSStockDailyCache
	if ok, err := c.getJSON(ctx, latestUSStockKey(symbol), &payload); err != nil || !ok {
		return rows, false, err
	}
	if payload.Adjusted == nil || *payload.Adjusted != adjusted {
		return rows, false, nil
	}
	latest := make([]dto.USStockBarRow, 0, len(payload.Bars))
	for _, bar := range payload.Bars {
		if !inTimeRange(bar.Timestamp, from, to) {
			continue
		}
		latest = append(latest, dto.USStockBarRow{Timestamp: normalizeCalendarDate(bar.Timestamp), Symbol: bar.Symbol, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume, Transactions: bar.Transactions})
	}
	if len(latest) == 0 {
		return rows, false, nil
	}
	return mergeLatestBarsByDate(rows, latest, func(row dto.USStockBarRow) time.Time { return row.Timestamp }), true, nil
}

func (c *LatestUSMarketCache) StockBarsDiagnostic(ctx context.Context, symbol string, from, to time.Time) (LatestUSStockDailyCache, bool, error) {
	var payload LatestUSStockDailyCache
	if ok, err := c.getJSON(ctx, latestUSStockKey(symbol), &payload); err != nil || !ok {
		return payload, false, err
	}
	filtered := payload.Bars[:0]
	for _, bar := range payload.Bars {
		if inTimeRange(bar.Timestamp, from, to) {
			filtered = append(filtered, bar)
		}
	}
	payload.Bars = filtered
	return payload, true, nil
}

func (c *LatestUSMarketCache) MergeOptionBars(ctx context.Context, symbol string, from, to time.Time, rows []dto.USOptionBarRow) ([]dto.USOptionBarRow, bool, error) {
	var payload LatestUSOptionDailyCache
	if ok, err := c.getJSON(ctx, latestUSOptionKey(symbol), &payload); err != nil || !ok {
		return rows, false, err
	}
	latest := make([]dto.USOptionBarRow, 0, len(payload.Bars))
	for _, bar := range payload.Bars {
		if !inTimeRange(bar.Timestamp, from, to) {
			continue
		}
		latest = append(latest, dto.USOptionBarRow{Timestamp: normalizeCalendarDate(bar.Timestamp), Symbol: bar.Symbol, Underlying: bar.Underlying, OptionType: bar.OptionType, Expiration: bar.Expiration, Strike: bar.Strike, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, UnderlyingClose: bar.UnderlyingClose, ImpliedVolatility: bar.ImpliedVolatility, Delta: bar.Delta, Gamma: bar.Gamma, Vega: bar.Vega, Theta: bar.Theta, Rho: bar.Rho, Volume: bar.Volume, Transactions: bar.Transactions})
	}
	if len(latest) == 0 {
		return rows, false, nil
	}
	return mergeLatestBarsByDate(rows, latest, func(row dto.USOptionBarRow) time.Time { return row.Timestamp }), true, nil
}

func (c *LatestUSMarketCache) MergeOptionChain(ctx context.Context, underlying string, expiration time.Time, from, to time.Time, rows []dto.USOptionChainSnapshot) ([]dto.USOptionChainSnapshot, bool, error) {
	snapshot, ok, err := c.LatestOptionChainSnapshot(ctx, underlying, expiration)
	if err != nil || !ok {
		return rows, false, err
	}
	if !inTimeRange(snapshot.Timestamp, from, to) {
		return rows, false, nil
	}
	merged := mergeLatestOptionChainSnapshots(rows, snapshot)
	return merged, true, nil
}

func mergeLatestOptionChainSnapshots(rows []dto.USOptionChainSnapshot, latest dto.USOptionChainSnapshot) []dto.USOptionChainSnapshot {
	byDay := make(map[int64]dto.USOptionChainSnapshot, len(rows)+1)
	for _, row := range rows {
		byDay[normalizeCalendarDate(row.Timestamp).Unix()] = row
	}
	byDay[normalizeCalendarDate(latest.Timestamp).Unix()] = latest
	out := make([]dto.USOptionChainSnapshot, 0, len(byDay))
	for _, row := range byDay {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out
}

func (c *LatestUSMarketCache) LatestOptionChainSnapshot(ctx context.Context, underlying string, expiration time.Time) (dto.USOptionChainSnapshot, bool, error) {
	var payload LatestUSOptionChainCache
	if ok, err := c.getJSON(ctx, latestUSOptionChainKey(underlying), &payload); err != nil || !ok {
		return dto.USOptionChainSnapshot{}, false, err
	}
	snapshot := payload.Snapshot
	if !expiration.IsZero() {
		contracts := make([]dto.USOptionChainContract, 0, len(snapshot.Contracts))
		for _, contract := range snapshot.Contracts {
			if normalizeCalendarDate(contract.Expiration).Equal(normalizeCalendarDate(expiration)) {
				contracts = append(contracts, contract)
			}
		}
		snapshot.Contracts = contracts
	}
	if len(snapshot.Contracts) == 0 {
		return dto.USOptionChainSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

func mergeLatestBarsByDate[T any](rows, latest []T, timestamp func(T) time.Time) []T {
	merged := make(map[int64]T, len(rows)+len(latest))
	for _, row := range rows {
		merged[normalizeCalendarDate(timestamp(row)).Unix()] = row
	}
	for _, row := range latest {
		merged[normalizeCalendarDate(timestamp(row)).Unix()] = row
	}
	out := make([]T, 0, len(merged))
	for _, row := range merged {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return timestamp(out[i]).Before(timestamp(out[j])) })
	return out
}

func (c *LatestUSMarketCache) setJSON(ctx context.Context, key string, value any) error {
	if c == nil || c.store == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, key, payload, c.ttl)
}

func (c *LatestUSMarketCache) getJSON(ctx context.Context, key string, out any) (bool, error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	payload, ok, err := c.store.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return false, err
	}
	return true, nil
}

func (c *LatestUSMarketCache) recordSuccess(ctx context.Context, at time.Time) error {
	return c.updateState(ctx, func(state *LatestUSMarketRefreshState) {
		state.LastSuccessAt = at
		state.LastFailureAt = time.Time{}
		state.ConsecutiveFailures = 0
	})
}

func (c *LatestUSMarketCache) recordFailure(ctx context.Context, at time.Time) error {
	return c.updateState(ctx, func(state *LatestUSMarketRefreshState) {
		state.LastFailureAt = at
		state.ConsecutiveFailures++
	})
}

func (c *LatestUSMarketCache) recordRunResult(ctx context.Context, at time.Time, result LatestUSMarketRefreshResult, err error) error {
	if err == nil || result.hasUsefulWrites() {
		return c.recordSuccess(ctx, at)
	}
	return c.recordFailure(ctx, at)
}

func (c *LatestUSMarketCache) recordStageResult(ctx context.Context, stage string, startedAt, completedAt time.Time, successCount, failureCount int, err error) error {
	return c.updateState(ctx, func(state *LatestUSMarketRefreshState) {
		if state.StageResults == nil {
			state.StageResults = make(map[string]LatestUSMarketRefreshStageResult)
		}
		entry := LatestUSMarketRefreshStageResult{StartedAt: startedAt, CompletedAt: completedAt, SuccessCount: successCount}
		if failureCount > 0 {
			entry.FailureCount = failureCount
		}
		if err != nil {
			entry.LastError = err.Error()
		}
		state.StageResults[stage] = entry
	})
}

func (c *LatestUSMarketCache) updateState(ctx context.Context, update func(*LatestUSMarketRefreshState)) error {
	state, _ := c.loadState(ctx)
	update(&state)
	return c.setJSON(ctx, latestUSStateKey(), state)
}

func (c *LatestUSMarketCache) loadState(ctx context.Context) (LatestUSMarketRefreshState, bool) {
	var state LatestUSMarketRefreshState
	ok, err := c.getJSON(ctx, latestUSStateKey(), &state)
	if err != nil {
		return state, false
	}
	return state, ok
}

func latestUSStockKey(symbol string) string {
	return latestUSMarketCacheVersion + ":stocks:" + strings.ToUpper(strings.TrimSpace(symbol)) + ":1d"
}

func latestUSOptionKey(symbol string) string {
	return latestUSMarketCacheVersion + ":options:" + strings.ToUpper(strings.TrimSpace(symbol)) + ":1d"
}

func latestUSOptionChainKey(underlying string) string {
	return latestUSMarketCacheVersion + ":option_chain:" + strings.ToUpper(strings.TrimSpace(underlying))
}

func latestUSStateKey() string {
	return latestUSMarketCacheVersion + ":state"
}

func inTimeRange(ts, from, to time.Time) bool {
	ts = ts.UTC()
	if !from.IsZero() && ts.Before(from.UTC()) {
		return false
	}
	if !to.IsZero() && !ts.Before(to.UTC()) {
		return false
	}
	return true
}

func unixMillisDay(ms int64) time.Time {
	return normalizeCalendarDate(time.UnixMilli(ms).UTC())
}

func maxIntPtr(value *int) int {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}

func firstFloat(values ...*float64) float64 {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return 0
}

func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
