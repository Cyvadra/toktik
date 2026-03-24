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
}

// Prepare loads data and computes base indicators for a strategy, returning a
// PreparedData that can be replayed many times with different parameters.
// The strategy is Init'd once to discover security and indicator registrations.
func (dp *DataPreparer) Prepare(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*PreparedData, error) {
	setupCtx := NewSetupContext(market, symbol, interval)
	for k, v := range params {
		setupCtx.params[k] = v
	}
	if err := strategy.Init(setupCtx); err != nil {
		return nil, fmt.Errorf("strategy init: %w", err)
	}

	primaryFeed, ok := dp.feeds[market]
	if !ok {
		return nil, fmt.Errorf("no DataFeed registered for market %q", market)
	}

	primaryDS, err := primaryFeed.Load(ctx, DataRequest{
		Market: market, Symbol: symbol, Interval: interval, From: from, To: to,
	})
	if err != nil {
		return nil, fmt.Errorf("load primary data: %w", err)
	}
	if primaryDS.Len == 0 {
		return nil, fmt.Errorf("no data returned for %s/%s/%s", market, symbol, interval)
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
				ds, err := f.Load(ctx, DataRequest{
					Market: r.Market, Symbol: r.Symbol, Interval: r.Interval, From: from, To: to,
				})
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
		}
	}

	// Align secondary data
	alignMaps := make([][]int, len(setupCtx.securities))
	alignMaps[0] = nil
	for i := 1; i < len(secDataSets); i++ {
		alignMaps[i] = alignSeries(primaryDS, secDataSets[i])
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
					Name: r.Name, Interval: r.Interval, From: from, To: to,
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
		}

		for i := range factorDataSets {
			factorAlignMaps[i] = alignSeries(primaryDS, factorDataSets[i])
		}
	}

	// Compute indicators
	for i, sec := range setupCtx.securities {
		ds := secDataSets[i]
		if len(sec.inds) > 0 {
			if err := resolveIndicators(sec.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for security[%d] %s: %w", i, sec.ref.Symbol, err)
			}
		}
	}

	for i, factor := range setupCtx.factors {
		ds := factorDataSets[i]
		if len(factor.inds) > 0 {
			if err := resolveIndicators(factor.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for factor[%d] %s: %w", i, factor.ref.Name, err)
			}
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
	}

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
