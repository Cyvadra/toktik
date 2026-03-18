package optimization

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// SearchMethod controls how the parameter space is explored.
type SearchMethod string

const (
	SearchGrid   SearchMethod = "grid"
	SearchRandom SearchMethod = "random"
)

// Metric names for ranking results.
const (
	MetricSharpe       = "sharpe_ratio"
	MetricCalmar       = "calmar_ratio"
	MetricReturn       = "annualized_return"
	MetricMaxDrawdown  = "max_drawdown"
	MetricWinRate      = "win_rate"
	MetricProfitFactor = "profit_factor"
)

// OptimizerConfig controls a parameter optimization run.
type OptimizerConfig struct {
	Method          SearchMethod
	NTrials         int           // number of random trials (ignored for grid)
	NWorkers        int           // parallel backtest workers
	Metric          string        // which metric to maximize (default: sharpe_ratio)
	Seed            int64         // random seed (for reproducibility)
	Timeout         time.Duration // 0 = no timeout
	MaxCombinations int           // safety cap for grid search (0 = unlimited)
}

// Trial records one parameter set and its backtest result.
type Trial struct {
	Index       int
	Params      map[string]interface{}
	MetricValue float64
	Result      *backtest.Result
	Err         error
}

// OptimizationResult holds the outcome of a parameter search.
type OptimizationResult struct {
	BestTrial Trial
	Trials    []Trial
	Elapsed   time.Duration
}

// Optimizer searches a strategy's parameter space using the existing Engine.
type Optimizer struct {
	engine   *backtest.Engine
	spec     StrategySpec
	config   OptimizerConfig
	market   string
	symbol   string
	interval string
	from, to time.Time
	factory  backtest.StrategyFactory
}

// NewOptimizer creates a parameter optimizer.
func NewOptimizer(
	engine *backtest.Engine,
	spec StrategySpec,
	config OptimizerConfig,
	market, symbol, interval string,
	from, to time.Time,
	factory backtest.StrategyFactory,
) *Optimizer {
	if config.Metric == "" {
		config.Metric = MetricSharpe
	}
	if config.NWorkers <= 0 {
		config.NWorkers = 1
	}
	return &Optimizer{
		engine:   engine,
		spec:     spec,
		config:   config,
		market:   market,
		symbol:   symbol,
		interval: interval,
		from:     from,
		to:       to,
		factory:  factory,
	}
}

// Run executes the parameter search and returns ranked results.
func (o *Optimizer) Run(ctx context.Context) (*OptimizationResult, error) {
	if err := o.spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid strategy spec: %w", err)
	}

	// Apply timeout if configured
	if o.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.config.Timeout)
		defer cancel()
	}

	// Generate parameter sets
	var paramSets []map[string]interface{}
	switch o.config.Method {
	case SearchGrid:
		paramSets = o.spec.GridCombinations(o.config.MaxCombinations)
		if paramSets == nil {
			return nil, fmt.Errorf("grid search exceeds max combinations (%d)", o.config.MaxCombinations)
		}
	case SearchRandom:
		n := o.config.NTrials
		if n <= 0 {
			n = 100
		}
		paramSets = o.spec.RandomCombinations(n, o.config.Seed)
	default:
		return nil, fmt.Errorf("unsupported search method %q", o.config.Method)
	}

	if len(paramSets) == 0 {
		return nil, fmt.Errorf("no parameter combinations generated")
	}

	start := time.Now()

	// Run all parameter sets via Engine.RunBatch
	batchResults, err := o.engine.RunBatch(ctx, o.market, o.symbol, o.interval, o.from, o.to, o.factory, paramSets, o.config.NWorkers)
	if err != nil {
		return nil, fmt.Errorf("batch run: %w", err)
	}

	// Convert to Trials and extract metric
	trials := make([]Trial, len(batchResults))
	for i, br := range batchResults {
		trials[i] = Trial{
			Index:  i,
			Params: br.Params,
			Result: br.Result,
			Err:    br.Err,
		}
		if br.Err == nil && br.Result != nil {
			trials[i].MetricValue = extractMetric(br.Result, o.config.Metric)
		}
	}

	// Sort trials by metric (descending, except max_drawdown which is ascending)
	sort.Slice(trials, func(i, j int) bool {
		// Errors sink to the bottom
		if trials[i].Err != nil {
			return false
		}
		if trials[j].Err != nil {
			return true
		}
		if o.config.Metric == MetricMaxDrawdown {
			return trials[i].MetricValue < trials[j].MetricValue // lower drawdown is better
		}
		return trials[i].MetricValue > trials[j].MetricValue
	})

	best := trials[0]
	return &OptimizationResult{
		BestTrial: best,
		Trials:    trials,
		Elapsed:   time.Since(start),
	}, nil
}

func extractMetric(r *backtest.Result, metric string) float64 {
	switch metric {
	case MetricSharpe:
		return r.SharpeRatio
	case MetricReturn:
		return r.AnnualizedReturn
	case MetricMaxDrawdown:
		return r.MaxDrawdown
	case MetricWinRate:
		return r.WinRate
	case MetricProfitFactor:
		return r.ProfitFactor
	case MetricCalmar:
		if r.MaxDrawdown == 0 {
			return 0
		}
		return r.AnnualizedReturn / r.MaxDrawdown
	default:
		return r.SharpeRatio
	}
}
