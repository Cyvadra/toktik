package optimization

import (
	"context"
	"fmt"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// WalkForwardConfig controls walk-forward cross-validation.
type WalkForwardConfig struct {
	NFolds     int     // number of sequential folds (default: 5)
	TrainRatio float64 // fraction of each fold used for training (default: 0.7)
}

// FoldResult records train vs. test performance for one fold.
type FoldResult struct {
	Fold        int
	TrainBars   int
	TestBars    int
	BestParams  map[string]interface{}
	TrainMetric float64
	TestMetric  float64
	Degradation float64 // (train - test) / |train|; >0 means worse OOS
}

// WalkForwardResult holds the full walk-forward analysis.
type WalkForwardResult struct {
	Folds          []FoldResult
	AvgTrainMetric float64
	AvgTestMetric  float64
	AvgDegradation float64
	Overfitting    bool   // true if AvgDegradation > threshold
	Message        string // human-readable summary
}

// WalkForwardValidator runs a walk-forward optimization to detect overfitting.
type WalkForwardValidator struct {
	engine   *backtest.Engine
	spec     StrategySpec
	optCfg   OptimizerConfig // config used for in-sample optimization
	wfCfg    WalkForwardConfig
	market   string
	symbol   string
	interval string
	from, to time.Time
	factory  backtest.StrategyFactory
}

// NewWalkForwardValidator creates a walk-forward validator.
func NewWalkForwardValidator(
	engine *backtest.Engine,
	spec StrategySpec,
	optCfg OptimizerConfig,
	wfCfg WalkForwardConfig,
	market, symbol, interval string,
	from, to time.Time,
	factory backtest.StrategyFactory,
) *WalkForwardValidator {
	if wfCfg.NFolds <= 0 {
		wfCfg.NFolds = 5
	}
	if wfCfg.TrainRatio <= 0 || wfCfg.TrainRatio >= 1 {
		wfCfg.TrainRatio = 0.7
	}
	return &WalkForwardValidator{
		engine:   engine,
		spec:     spec,
		optCfg:   optCfg,
		wfCfg:    wfCfg,
		market:   market,
		symbol:   symbol,
		interval: interval,
		from:     from,
		to:       to,
		factory:  factory,
	}
}

// Run executes the walk-forward validation.
func (wf *WalkForwardValidator) Run(ctx context.Context) (*WalkForwardResult, error) {
	// Load data once for the full period
	probe := wf.factory()
	prepared, err := wf.engine.Prepare(ctx, wf.market, wf.symbol, wf.interval, wf.from, wf.to, probe, nil)
	if err != nil {
		return nil, fmt.Errorf("prepare data: %w", err)
	}

	nBars := prepared.PrimaryDS.Len
	foldSize := nBars / wf.wfCfg.NFolds
	if foldSize < 20 {
		return nil, fmt.Errorf("not enough bars (%d) for %d folds", nBars, wf.wfCfg.NFolds)
	}

	trainSize := int(float64(foldSize) * wf.wfCfg.TrainRatio)
	if trainSize < 10 {
		return nil, fmt.Errorf("train size too small (%d bars)", trainSize)
	}
	testSize := foldSize - trainSize
	if testSize < 5 {
		return nil, fmt.Errorf("test size too small (%d bars)", testSize)
	}

	folds := make([]FoldResult, wf.wfCfg.NFolds)

	for fold := 0; fold < wf.wfCfg.NFolds; fold++ {
		foldStart := fold * foldSize
		trainEnd := foldStart + trainSize
		testEnd := foldStart + foldSize
		if testEnd > nBars {
			testEnd = nBars
		}
		if trainEnd >= testEnd {
			continue
		}

		// Slice data for train and test
		trainFrom := prepared.PrimaryDS.Timestamps[foldStart]
		trainTo := prepared.PrimaryDS.Timestamps[trainEnd]
		testFrom := trainTo
		testTo := prepared.PrimaryDS.Timestamps[testEnd-1].Add(time.Second) // inclusive

		// --- In-sample: optimize on train set ---
		trainOpt := NewOptimizer(wf.engine, wf.spec, wf.optCfg, wf.market, wf.symbol, wf.interval, trainFrom, trainTo, wf.factory)
		trainResult, err := trainOpt.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("fold %d train optimization: %w", fold, err)
		}

		bestParams := trainResult.BestTrial.Params
		trainMetric := trainResult.BestTrial.MetricValue

		// --- Out-of-sample: test best params ---
		s := wf.factory()
		testResult, err := wf.engine.Run(ctx, wf.market, wf.symbol, wf.interval, testFrom, testTo, s, bestParams)
		if err != nil {
			return nil, fmt.Errorf("fold %d test run: %w", fold, err)
		}

		testMetric := extractMetric(testResult, wf.optCfg.Metric)
		degradation := 0.0
		if trainMetric != 0 {
			degradation = (trainMetric - testMetric) / abs(trainMetric)
		}

		folds[fold] = FoldResult{
			Fold:        fold,
			TrainBars:   trainSize,
			TestBars:    testEnd - trainEnd,
			BestParams:  bestParams,
			TrainMetric: trainMetric,
			TestMetric:  testMetric,
			Degradation: degradation,
		}
	}

	// Compute averages
	var sumTrain, sumTest, sumDeg float64
	validFolds := 0
	for _, f := range folds {
		if f.TrainBars > 0 {
			sumTrain += f.TrainMetric
			sumTest += f.TestMetric
			sumDeg += f.Degradation
			validFolds++
		}
	}

	result := &WalkForwardResult{Folds: folds}
	if validFolds > 0 {
		result.AvgTrainMetric = sumTrain / float64(validFolds)
		result.AvgTestMetric = sumTest / float64(validFolds)
		result.AvgDegradation = sumDeg / float64(validFolds)
	}

	const overfitThreshold = 0.3
	result.Overfitting = result.AvgDegradation > overfitThreshold
	if result.Overfitting {
		result.Message = fmt.Sprintf("overfitting detected: avg metric degrades %.1f%% out-of-sample (threshold: %.0f%%)",
			result.AvgDegradation*100, overfitThreshold*100)
	} else {
		result.Message = fmt.Sprintf("walk-forward passed: avg degradation %.1f%%", result.AvgDegradation*100)
	}

	return result, nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
