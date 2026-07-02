package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
)

const observedUSStockPoolTopLimit = 60

const defaultUSTurnoverIntersectionCacheRefreshInterval = 22 * time.Hour

const defaultUSTurnoverIntersectionWarmupCooldown = 20 * time.Hour

const usTurnoverIntersectionWarmupStateKey = "service:us_turnover_intersection:warmup_state:non_etf_only"

var observedUSStockPoolLookbackDays = []int{7, 20, 60, 120}

type turnoverIntersectionTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type timeTicker struct {
	*time.Ticker
}

func (t timeTicker) Chan() <-chan time.Time {
	return t.C
}

type USTurnoverIntersectionCacheRefresher struct {
	done chan struct{}
}

type usTurnoverIntersectionWarmupState struct {
	LastSuccessAt time.Time `json:"last_success_at"`
}

func (r *USTurnoverIntersectionCacheRefresher) Wait() {
	if r == nil {
		return
	}
	<-r.done
}

type usTurnoverIntersectionScreener interface {
	ScreenUSTurnoverIntersection(ctx context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error)
}

func ResolveObservedUSStockPool(ctx context.Context, screener usTurnoverIntersectionScreener) ([]string, error) {
	if screener == nil {
		return nil, fmt.Errorf("observed us stock pool screener not configured")
	}
	seen := make(map[string]struct{}, observedUSStockPoolTopLimit*len(observedUSStockPoolLookbackDays))
	pool := make([]string, 0, observedUSStockPoolTopLimit*len(observedUSStockPoolLookbackDays))
	for _, lookbackDays := range observedUSStockPoolLookbackDays {
		resp, err := screener.ScreenUSTurnoverIntersection(ctx, dto.ScreenUSTurnoverIntersectionRequest{
			Limit:        observedUSStockPoolTopLimit,
			LookbackDays: lookbackDays,
			NonETFOnly:   true,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve observed us stock pool for %d-day turnover: %w", lookbackDays, err)
		}
		for _, row := range resp.Data {
			symbol := normalizeSymbol(row.Underlying)
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
	return pool, nil
}

func WarmUSTurnoverIntersectionCache(ctx context.Context, screener usTurnoverIntersectionScreener, nonETFOnly bool) error {
	if screener == nil {
		return fmt.Errorf("us turnover intersection screener not configured")
	}
	for _, lookbackDays := range observedUSStockPoolLookbackDays {
		if _, err := screener.ScreenUSTurnoverIntersection(ctx, dto.ScreenUSTurnoverIntersectionRequest{
			Limit:        observedUSStockPoolTopLimit,
			LookbackDays: lookbackDays,
			NonETFOnly:   nonETFOnly,
		}); err != nil {
			return fmt.Errorf("warm us turnover intersection cache for %d-day lookback: %w", lookbackDays, err)
		}
	}
	return nil
}

func StartUSTurnoverIntersectionCacheRefresher(
	ctx context.Context,
	logger *slog.Logger,
	screener usTurnoverIntersectionScreener,
	store cache.Store,
	nonETFOnly bool,
	interval time.Duration,
	cooldown time.Duration,
	refreshTimeout time.Duration,
) *USTurnoverIntersectionCacheRefresher {
	return startUSTurnoverIntersectionCacheRefresher(
		ctx,
		logger,
		screener,
		store,
		nonETFOnly,
		interval,
		cooldown,
		refreshTimeout,
		WarmUSTurnoverIntersectionCache,
		func(d time.Duration) turnoverIntersectionTicker {
			return timeTicker{Ticker: time.NewTicker(d)}
		},
	)
}

func startUSTurnoverIntersectionCacheRefresher(
	ctx context.Context,
	logger *slog.Logger,
	screener usTurnoverIntersectionScreener,
	store cache.Store,
	nonETFOnly bool,
	interval time.Duration,
	cooldown time.Duration,
	refreshTimeout time.Duration,
	warmFn func(context.Context, usTurnoverIntersectionScreener, bool) error,
	newTicker func(time.Duration) turnoverIntersectionTicker,
) *USTurnoverIntersectionCacheRefresher {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultUSTurnoverIntersectionCacheRefreshInterval
	}
	if cooldown <= 0 {
		cooldown = defaultUSTurnoverIntersectionWarmupCooldown
	}
	refresher := &USTurnoverIntersectionCacheRefresher{done: make(chan struct{})}
	go func() {
		defer close(refresher.done)
		if shouldSkip, until, err := shouldSkipUSTurnoverIntersectionWarmup(ctx, store, nonETFOnly, cooldown); err != nil {
			logger.Warn("load us turnover intersection warmup state failed", "non_etf_only", nonETFOnly, "error", err)
		} else if shouldSkip {
			logger.Info("skipped us turnover intersection cache warmup", "non_etf_only", nonETFOnly, "next_eligible_at", until)
		} else if !runUSTurnoverIntersectionCacheRefresh(ctx, logger, screener, store, nonETFOnly, refreshTimeout, warmFn, "warmed", interval) {
			return
		}
		ticker := newTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				logger.Info("stopped us turnover intersection cache refresher")
				return
			case <-ticker.Chan():
			}
			if !runUSTurnoverIntersectionCacheRefresh(ctx, logger, screener, store, nonETFOnly, refreshTimeout, warmFn, "refreshed", interval) {
				return
			}
		}
	}()
	return refresher
}

func runUSTurnoverIntersectionCacheRefresh(
	ctx context.Context,
	logger *slog.Logger,
	screener usTurnoverIntersectionScreener,
	store cache.Store,
	nonETFOnly bool,
	refreshTimeout time.Duration,
	warmFn func(context.Context, usTurnoverIntersectionScreener, bool) error,
	action string,
	stateTTL time.Duration,
) bool {
	refreshCtx := ctx
	cancel := func() {}
	if refreshTimeout > 0 {
		refreshCtx, cancel = context.WithTimeout(ctx, refreshTimeout)
	}
	defer cancel()
	if err := warmFn(refreshCtx, screener, nonETFOnly); err != nil {
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return false
		}
		logger.Warn("refresh us turnover intersection cache failed", "non_etf_only", nonETFOnly, "error", err)
		return ctx.Err() == nil
	}
	if err := markUSTurnoverIntersectionWarmupSuccess(ctx, store, nonETFOnly, stateTTL); err != nil {
		logger.Warn("store us turnover intersection warmup state failed", "non_etf_only", nonETFOnly, "error", err)
	}
	logger.Info(action+" us turnover intersection cache", "non_etf_only", nonETFOnly, "lookback_days", observedUSStockPoolLookbackDays, "limit", observedUSStockPoolTopLimit)
	return true
}

func shouldSkipUSTurnoverIntersectionWarmup(ctx context.Context, store cache.Store, nonETFOnly bool, cooldown time.Duration) (bool, time.Time, error) {
	if store == nil || cooldown <= 0 {
		return false, time.Time{}, nil
	}
	state, ok, err := loadUSTurnoverIntersectionWarmupState(ctx, store, nonETFOnly)
	if err != nil || !ok || state.LastSuccessAt.IsZero() {
		return false, time.Time{}, err
	}
	nextEligibleAt := state.LastSuccessAt.Add(cooldown)
	if time.Now().Before(nextEligibleAt) {
		return true, nextEligibleAt, nil
	}
	return false, nextEligibleAt, nil
}

func loadUSTurnoverIntersectionWarmupState(ctx context.Context, store cache.Store, nonETFOnly bool) (usTurnoverIntersectionWarmupState, bool, error) {
	var state usTurnoverIntersectionWarmupState
	if store == nil {
		return state, false, nil
	}
	raw, ok, err := store.Get(ctx, usTurnoverIntersectionWarmupStateCacheKey(nonETFOnly))
	if err != nil || !ok {
		return state, ok, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return usTurnoverIntersectionWarmupState{}, false, fmt.Errorf("decode us turnover intersection warmup state: %w", err)
	}
	return state, true, nil
}

func markUSTurnoverIntersectionWarmupSuccess(ctx context.Context, store cache.Store, nonETFOnly bool, ttl time.Duration) error {
	if store == nil {
		return nil
	}
	payload, err := json.Marshal(usTurnoverIntersectionWarmupState{LastSuccessAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("encode us turnover intersection warmup state: %w", err)
	}
	if err := store.Set(ctx, usTurnoverIntersectionWarmupStateCacheKey(nonETFOnly), payload, ttl); err != nil {
		return fmt.Errorf("store us turnover intersection warmup state: %w", err)
	}
	return nil
}

func usTurnoverIntersectionWarmupStateCacheKey(nonETFOnly bool) string {
	if nonETFOnly {
		return usTurnoverIntersectionWarmupStateKey
	}
	return usTurnoverIntersectionWarmupStateKey + ":all"
}

func intervalForWarmupStateTTL(refreshInterval time.Duration) time.Duration {
	if refreshInterval <= 0 {
		refreshInterval = defaultUSTurnoverIntersectionCacheRefreshInterval
	}
	return refreshInterval + time.Hour
}
