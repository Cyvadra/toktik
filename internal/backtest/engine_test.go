package backtest

import (
	"context"
	"testing"
	"time"
)

type stubDataFeed struct {
	fields []string
}

func (f *stubDataFeed) Fields() []string { return f.fields }

func (f *stubDataFeed) Load(_ context.Context, req DataRequest) (*DataSet, error) {
	nBars := 100
	ds := NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	open := make([]float64, nBars)
	high := make([]float64, nBars)
	low := make([]float64, nBars)
	cl := make([]float64, nBars)
	volume := make([]float64, nBars)

	base := req.From
	for i := 0; i < nBars; i++ {
		ts[i] = base.Add(time.Duration(i) * time.Hour)
		price := 100.0 + float64(i%20)
		open[i] = price
		high[i] = price + 1
		low[i] = price - 1
		cl[i] = price + 0.5
		volume[i] = 1000
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", cl)
	ds.AddColumn("volume", volume)
	return ds, nil
}

type trendStrategy struct{}

func (s *trendStrategy) Name() string { return "trend" }

func (s *trendStrategy) Init(ctx *SetupContext) error {
	ctx.SetParam("fast_period", 5)
	ctx.SetParam("slow_period", 20)
	fast, _ := ctx.params["fast_period"].(int)
	slow, _ := ctx.params["slow_period"].(int)
	if fast <= 0 {
		fast = 5
	}
	if slow <= 0 {
		slow = 20
	}
	ctx.Register("sma_fast", SMA("close", fast))
	ctx.Register("sma_slow", SMA("close", slow))
	return nil
}

func (s *trendStrategy) OnBar(ctx *BarContext) {
	smaFast := ctx.Ind("sma_fast")
	smaSlow := ctx.Ind("sma_slow")
	price := ctx.Close()

	if price > smaFast && price > smaSlow && ctx.Position(ctx.primaryRef) == 0 {
		ctx.Buy(ctx.primaryRef, 1)
	} else if price < smaSlow && ctx.Position(ctx.primaryRef) > 0 {
		ctx.Sell(ctx.primaryRef, 1)
	}
}

func TestRunBatch(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	factory := func() Strategy { return &trendStrategy{} }

	paramSets := []map[string]interface{}{
		{"fast_period": 3, "slow_period": 10},
		{"fast_period": 5, "slow_period": 20},
		{"fast_period": 7, "slow_period": 30},
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	results, err := engine.RunBatch(context.Background(), "test", "TEST", "1h", from, to, factory, paramSets, 2)
	if err != nil {
		t.Fatalf("RunBatch failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", i, r.Err)
			continue
		}
		if r.Result == nil {
			t.Errorf("result[%d] has nil Result", i)
			continue
		}
		if r.Result.BarsCount != 100 {
			t.Errorf("result[%d] BarsCount=%d, want 100", i, r.Result.BarsCount)
		}
	}

	t.Logf("Result 0: trades=%d sharpe=%.4f", results[0].Result.TotalTrades, results[0].Result.SharpeRatio)
	t.Logf("Result 1: trades=%d sharpe=%.4f", results[1].Result.TotalTrades, results[1].Result.SharpeRatio)
	t.Logf("Result 2: trades=%d sharpe=%.4f", results[2].Result.TotalTrades, results[2].Result.SharpeRatio)
}

func TestRunBatchConsistency(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	factory := func() Strategy { return &trendStrategy{} }
	params := map[string]interface{}{"fast_period": 5, "slow_period": 20}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	// Single run
	directResult, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, factory(), params)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Batch with same params
	batchResults, err := engine.RunBatch(context.Background(), "test", "TEST", "1h", from, to, factory, []map[string]interface{}{params}, 1)
	if err != nil {
		t.Fatalf("RunBatch failed: %v", err)
	}

	if batchResults[0].Err != nil {
		t.Fatalf("batch error: %v", batchResults[0].Err)
	}

	if batchResults[0].Result.TotalTrades != directResult.TotalTrades {
		t.Errorf("TotalTrades mismatch: batch=%d direct=%d",
			batchResults[0].Result.TotalTrades, directResult.TotalTrades)
	}
}
