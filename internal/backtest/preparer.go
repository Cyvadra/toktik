package backtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PreparedData holds pre-loaded and aligned data that can be reused across
// multiple strategy runs. Created by DataPreparer.Prepare, consumed by
// Replayer.Replay and Engine.RunBatch.
type PreparedData struct {
	PrimaryDS       *DataSet
	SecDataSets     []*DataSet
	AlignMaps       [][]int
	Securities      []securityRegistration
	FactorDataSets  []*DataSet
	FactorAlignMaps [][]int
	Factors         []factorRegistration
	PrimaryRef      SecurityRef
}

// DataPreparer loads market data, aligns time series, and computes indicators.
type DataPreparer struct {
	feeds       map[string]DataFeed
	factorFeeds map[string]FactorFeed
	progress    ProgressFunc

	mu      sync.Mutex
	dsCache map[DataRequest]*DataSet
}

// Prepare loads data and computes base indicators for a strategy, returning a
// PreparedData that can be replayed many times with different parameters.
// The strategy is Init'd once to discover security and indicator registrations.
func (dp *DataPreparer) Prepare(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*PreparedData, error) {
	startedAt := time.Now()
	setupCtx := NewSetupContext(market, symbol, interval)
	for k, v := range params {
		setupCtx.params[k] = v
	}
	if err := strategy.Init(setupCtx); err != nil {
		return nil, fmt.Errorf("strategy init: %w", err)
	}

	totalSteps := 1 + (len(setupCtx.securities) - 1) + len(setupCtx.factors)
	for _, sec := range setupCtx.securities {
		if len(sec.inds) > 0 {
			totalSteps++
		}
	}
	for _, factor := range setupCtx.factors {
		if len(factor.inds) > 0 {
			totalSteps++
		}
	}
	if _, ok := strategy.(StrategyPreloader); ok {
		totalSteps++
	}
	if setupCtx.warmup > 0 {
		totalSteps++
	}
	completedSteps := 0
	reportStep := func(message string, completed bool) {
		emitProgress(dp.progress, ProgressUpdate{
			Phase:     ProgressPhasePrepare,
			Current:   completedSteps,
			Total:     totalSteps,
			Message:   message,
			StartedAt: startedAt,
			Completed: completed,
		})
	}
	advanceStep := func(message string) {
		completedSteps++
		reportStep(message, false)
	}
	reportStep("initializing strategy", false)

	loadFrom := from
	if setupCtx.warmup > 0 {
		loadFrom = from.Add(-setupCtx.warmup)
	}

	primaryFeed, ok := dp.feeds[market]
	if !ok {
		return nil, fmt.Errorf("no DataFeed registered for market %q", market)
	}

	primaryReq := DataRequest{Market: market, Symbol: symbol, Interval: interval, From: loadFrom, To: to}
	primaryDS, err := dp.loadCached(ctx, primaryFeed, primaryReq)
	if err != nil {
		return nil, fmt.Errorf("load primary data: %w", err)
	}
	advanceStep("loaded primary data")
	if primaryDS.Len == 0 {
		return nil, fmt.Errorf("no data returned for %s/%s/%s", market, symbol, interval)
	}
	// Warn if data doesn't cover the requested warmup period
	if setupCtx.warmup > 0 && primaryDS.Len > 0 && primaryDS.Timestamps[0].After(loadFrom) {
		// Data starts after the requested warmup start; indicators may not seed properly
		// This is acceptable but strategies should be aware indicators may have NaN values
	}

	// Load secondary datasets in parallel
	type secResult struct {
		index int
		ds    *DataSet
		err   error
	}

	secCount := len(setupCtx.securities) - 1
	secDataSets := make([]*DataSet, len(setupCtx.securities))
	secDataSets[0] = primaryDS

	if secCount > 0 {
		results := make(chan secResult, secCount)
		var wg sync.WaitGroup

		for i := 1; i < len(setupCtx.securities); i++ {
			sec := setupCtx.securities[i]
			feed, ok := dp.feeds[sec.ref.Market]
			if !ok {
				return nil, fmt.Errorf("no DataFeed registered for market %q (security %s)", sec.ref.Market, sec.ref.Symbol)
			}

			wg.Add(1)
			go func(idx int, f DataFeed, r SecurityRef) {
				defer wg.Done()
				req := DataRequest{
					Market: r.Market, Symbol: r.Symbol, Interval: r.Interval, From: loadFrom, To: to,
				}
				ds, err := dp.loadCached(ctx, f, req)
				results <- secResult{index: idx, ds: ds, err: err}
			}(i, feed, sec.ref)
		}

		go func() { wg.Wait(); close(results) }()

		for r := range results {
			if r.err != nil {
				sec := setupCtx.securities[r.index]
				return nil, fmt.Errorf("load security %s/%s: %w", sec.ref.Market, sec.ref.Symbol, r.err)
			}
			secDataSets[r.index] = r.ds
			advanceStep(fmt.Sprintf("loaded security %s/%s", setupCtx.securities[r.index].ref.Market, setupCtx.securities[r.index].ref.Symbol))
		}
	}

	// Align secondary data
	alignMaps := make([][]int, len(setupCtx.securities))
	alignMaps[0] = nil
	for i := 1; i < len(secDataSets); i++ {
		alignMaps[i] = alignSeries(primaryDS, secDataSets[i], setupCtx.primaryRef.Interval, setupCtx.securities[i].ref.Interval)
	}

	// Load external factor datasets in parallel
	type factorResult struct {
		index int
		ds    *DataSet
		err   error
	}

	factorDataSets := make([]*DataSet, len(setupCtx.factors))
	factorAlignMaps := make([][]int, len(setupCtx.factors))

	if len(setupCtx.factors) > 0 {
		results := make(chan factorResult, len(setupCtx.factors))
		var wg sync.WaitGroup

		for i := range setupCtx.factors {
			factor := setupCtx.factors[i]
			feed, ok := dp.factorFeeds[factor.ref.Name]
			if !ok {
				return nil, fmt.Errorf("no FactorFeed registered for factor %q", factor.ref.Name)
			}

			wg.Add(1)
			go func(idx int, f FactorFeed, r FactorRef) {
				defer wg.Done()
				ds, err := f.Load(ctx, FactorRequest{
					Name:          r.Name,
					Interval:      r.Interval,
					Mode:          r.Mode,
					Market:        r.Market,
					Symbol:        r.Symbol,
					PrimaryMarket: setupCtx.primaryRef.Market,
					PrimarySymbol: setupCtx.primaryRef.Symbol,
					From:          loadFrom,
					To:            to,
				})
				results <- factorResult{index: idx, ds: ds, err: err}
			}(i, feed, factor.ref)
		}

		go func() { wg.Wait(); close(results) }()

		for r := range results {
			if r.err != nil {
				factor := setupCtx.factors[r.index]
				return nil, fmt.Errorf("load factor %s/%s: %w", factor.ref.Name, factor.ref.Interval, r.err)
			}
			factorDataSets[r.index] = r.ds
			advanceStep(fmt.Sprintf("loaded factor %s/%s", setupCtx.factors[r.index].ref.Name, setupCtx.factors[r.index].ref.Interval))
		}

		for i := range factorDataSets {
			factorAlignMaps[i] = alignSeries(primaryDS, factorDataSets[i], setupCtx.primaryRef.Interval, setupCtx.factors[i].ref.Interval)
		}
	}

	// Compute indicators
	for i, sec := range setupCtx.securities {
		ds := secDataSets[i]
		if len(sec.inds) > 0 {
			if err := resolveIndicators(sec.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for security[%d] %s: %w", i, sec.ref.Symbol, err)
			}
			advanceStep(fmt.Sprintf("computed indicators for %s", sec.ref.Symbol))
		}
	}

	for i, factor := range setupCtx.factors {
		ds := factorDataSets[i]
		if len(factor.inds) > 0 {
			if err := resolveIndicators(factor.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for factor[%d] %s: %w", i, factor.ref.Name, err)
			}
			advanceStep(fmt.Sprintf("computed indicators for factor %s", factor.ref.Name))
		}
	}

	if preloader, ok := strategy.(StrategyPreloader); ok {
		preloadCtx := newPreloadContext(
			setupCtx.primaryRef,
			setupCtx.securities,
			secDataSets,
			alignMaps,
			setupCtx.factors,
			factorDataSets,
			factorAlignMaps,
			setupCtx.params,
		)
		if err := preloader.Preload(preloadCtx); err != nil {
			return nil, fmt.Errorf("strategy preload: %w", err)
		}
		advanceStep("completed strategy preload")
	}

	if setupCtx.warmup > 0 {
		trimmedPrimary, trimmedAlignMaps, trimmedFactorAlignMaps, err := trimPreparedWindow(primaryDS, secDataSets, factorDataSets, setupCtx, from)
		if err != nil {
			return nil, err
		}
		primaryDS = trimmedPrimary
		secDataSets[0] = primaryDS
		alignMaps = trimmedAlignMaps
		factorAlignMaps = trimmedFactorAlignMaps
		advanceStep("trimmed warmup window")
	}
	reportStep("prepared data", true)

	return &PreparedData{
		PrimaryDS:       primaryDS,
		SecDataSets:     secDataSets,
		AlignMaps:       alignMaps,
		Securities:      setupCtx.securities,
		FactorDataSets:  factorDataSets,
		FactorAlignMaps: factorAlignMaps,
		Factors:         setupCtx.factors,
		PrimaryRef:      setupCtx.primaryRef,
	}, nil
}

// loadCached returns a cloned DataSet, loading from the feed only on first
// access for a given DataRequest. If multiple goroutines miss the initial cache
// check concurrently, each will call feed.Load independently; only the first
// to reacquire the lock stores its result — subsequent goroutines discard their
// load and return the already-cached copy. This avoids stale overwrites while
// keeping the lock-free hot path for cache hits.
func (dp *DataPreparer) loadCached(ctx context.Context, feed DataFeed, req DataRequest) (*DataSet, error) {
	dp.mu.Lock()
	if dp.dsCache == nil {
		dp.dsCache = make(map[DataRequest]*DataSet)
	}
	if cached, ok := dp.dsCache[req]; ok {
		dp.mu.Unlock()
		return cached.Clone(), nil
	}
	dp.mu.Unlock()

	ds, err := feed.Load(ctx, req)
	if err != nil {
		return nil, err
	}

	dp.mu.Lock()
	// Another goroutine may have loaded and cached this request while we were
	// loading. Prefer the already-cached copy to avoid duplicate storage.
	if existing, ok := dp.dsCache[req]; ok {
		dp.mu.Unlock()
		return existing.Clone(), nil
	}
	dp.dsCache[req] = ds
	dp.mu.Unlock()

	return ds.Clone(), nil
}

func trimPreparedWindow(primaryDS *DataSet, secDataSets []*DataSet, factorDataSets []*DataSet, setupCtx *SetupContext, from time.Time) (*DataSet, [][]int, [][]int, error) {
	startBar := 0
	for startBar < primaryDS.Len && primaryDS.Timestamps[startBar].Before(from) {
		startBar++
	}
	if startBar >= primaryDS.Len {
		return nil, nil, nil, fmt.Errorf("no data returned for %s/%s/%s at or after requested start", setupCtx.primaryRef.Market, setupCtx.primaryRef.Symbol, setupCtx.primaryRef.Interval)
	}
	if startBar == 0 {
		return primaryDS, buildSecurityAlignMaps(primaryDS, secDataSets, setupCtx), buildFactorAlignMaps(primaryDS, factorDataSets, setupCtx), nil
	}

	trimmedPrimary := primaryDS.Slice(startBar, primaryDS.Len)
	return trimmedPrimary, buildSecurityAlignMaps(trimmedPrimary, secDataSets, setupCtx), buildFactorAlignMaps(trimmedPrimary, factorDataSets, setupCtx), nil
}

func buildSecurityAlignMaps(primaryDS *DataSet, secDataSets []*DataSet, setupCtx *SetupContext) [][]int {
	alignMaps := make([][]int, len(setupCtx.securities))
	alignMaps[0] = nil
	for i := 1; i < len(secDataSets); i++ {
		alignMaps[i] = alignSeries(primaryDS, secDataSets[i], setupCtx.primaryRef.Interval, setupCtx.securities[i].ref.Interval)
	}
	return alignMaps
}

func buildFactorAlignMaps(primaryDS *DataSet, factorDataSets []*DataSet, setupCtx *SetupContext) [][]int {
	alignMaps := make([][]int, len(setupCtx.factors))
	for i := range factorDataSets {
		alignMaps[i] = alignSeries(primaryDS, factorDataSets[i], setupCtx.primaryRef.Interval, setupCtx.factors[i].ref.Interval)
	}
	return alignMaps
}
