package backtest

import (
	"context"
	"sync"
	"time"
)

// Config controls engine behavior.
type Config struct {
	InitialCapital  float64
	AccountUnit     string
	CommissionModel CommissionModel
	CommissionValue float64
	SlippagePct     float64
	ExecutionMode   ExecutionPriceModel
	ValuationMode   ValuationPriceModel
	TriggerMode     TriggerPriceMode
}

// Engine orchestrates backtests by delegating data loading to a DataPreparer
// and bar-by-bar execution to a Replayer.
type Engine struct {
	preparer DataPreparer
	replayer Replayer
}

// NewEngine creates a backtest engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	return &Engine{
		preparer: DataPreparer{
			feeds:       make(map[string]DataFeed),
			factorFeeds: make(map[string]FactorFeed),
		},
		replayer: Replayer{
			config: cfg,
		},
	}
}

// RegisterDataFeed associates a DataFeed with a market name.
func (e *Engine) RegisterDataFeed(market string, feed DataFeed) {
	e.preparer.feeds[market] = feed
}

// RegisterFactorFeed associates an external factor feed with a factor name.
func (e *Engine) RegisterFactorFeed(name string, feed FactorFeed) {
	e.preparer.factorFeeds[name] = feed
}

// SetOptionsChainProvider sets the provider that supplies option chain data
// during bar replay. This enables strategies to dynamically query available
// options at each bar.
func (e *Engine) SetOptionsChainProvider(p OptionsChainProvider) {
	e.replayer.chainProvider = p
}

// Prepare loads data and computes base indicators for a strategy, returning a
// PreparedData that can be replayed many times with different parameters.
func (e *Engine) Prepare(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*PreparedData, error) {
	return e.preparer.Prepare(ctx, market, symbol, interval, from, to, strategy, params)
}

// Run executes a full backtest: load data, compute indicators, replay bars,
// and return metrics.
func (e *Engine) Run(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*Result, error) {
	prepared, err := e.preparer.Prepare(ctx, market, symbol, interval, from, to, strategy, params)
	if err != nil {
		return nil, err
	}
	return e.replayer.Replay(prepared, strategy, params)
}

// StrategyFactory creates a fresh Strategy instance for each parameter set.
// This is necessary because strategies are stateful (they accumulate positions,
// spread state, etc.) and cannot be safely reused across runs.
type StrategyFactory func() Strategy

// BatchResult pairs a parameter set with its backtest result.
type BatchResult struct {
	Params map[string]interface{}
	Result *Result
	Err    error
}

// RunBatch replays a strategy with multiple parameter sets in parallel.
// The factory function must return a fresh Strategy for each run.
// nWorkers controls parallelism; if <= 0 it defaults to 1.
//
// Each parameter set gets its own Prepare+Replay cycle because different
// params may register different indicators during Init. Raw market data is
// cached by the underlying DataFeed implementations, so the main cost of
// repeated Prepare is indicator recomputation — not data reloading.
func (e *Engine) RunBatch(ctx context.Context, market, symbol, interval string, from, to time.Time, factory StrategyFactory, paramSets []map[string]interface{}, nWorkers int) ([]BatchResult, error) {
	if nWorkers <= 0 {
		nWorkers = 1
	}

	results := make([]BatchResult, len(paramSets))
	sem := make(chan struct{}, nWorkers)
	var wg sync.WaitGroup

	for i, ps := range paramSets {
		wg.Add(1)
		go func(idx int, params map[string]interface{}) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			s := factory()
			prepared, prepErr := e.preparer.Prepare(ctx, market, symbol, interval, from, to, s, params)
			if prepErr != nil {
				results[idx] = BatchResult{Params: params, Err: prepErr}
				return
			}
			res, replayErr := e.replayer.Replay(prepared, s, params)
			results[idx] = BatchResult{Params: params, Result: res, Err: replayErr}
		}(i, ps)
	}

	wg.Wait()
	return results, nil
}
