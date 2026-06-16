package backtest

import (
	"context"
	"math"
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

type parameterizedIndicatorStrategy struct{}

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

func (s *parameterizedIndicatorStrategy) Name() string { return "parameterized-indicator" }

func (s *parameterizedIndicatorStrategy) Init(ctx *SetupContext) error {
	ctx.SetParam("marker", 1)
	marker, _ := ctx.params["marker"].(int)
	if marker <= 0 {
		marker = 1
	}
	ctx.Register("marker", Custom([]string{"close"}, func(inputs map[string][]float64) []float64 {
		closeSeries := inputs["close"]
		out := make([]float64, len(closeSeries))
		for i := range out {
			out[i] = float64(marker)
		}
		return out
	}))
	return nil

}

func (s *parameterizedIndicatorStrategy) OnBar(ctx *BarContext) {
	primary := ctx.PrimaryRef()
	switch ctx.BarIndex() {
	case 0:
		if ctx.Ind("marker") < 5 {
			ctx.Buy(primary, 1)
		}
	case 1:
		if ctx.Position(primary) > 0 {
			ctx.Sell(primary, 1)
		}
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

func TestRunBatchRespectsParameterSpecificIndicators(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	factory := func() Strategy { return &parameterizedIndicatorStrategy{} }
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	paramsA := map[string]interface{}{"marker": 3}
	paramsB := map[string]interface{}{"marker": 7}

	directA, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, factory(), paramsA)
	if err != nil {
		t.Fatalf("direct run A failed: %v", err)
	}
	directB, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, factory(), paramsB)
	if err != nil {
		t.Fatalf("direct run B failed: %v", err)
	}
	if directA.TotalTrades != 2 {
		t.Fatalf("direct run A trades=%d, want 2", directA.TotalTrades)
	}
	if directB.TotalTrades != 0 {
		t.Fatalf("direct run B trades=%d, want 0", directB.TotalTrades)
	}

	batch, err := engine.RunBatch(context.Background(), "test", "TEST", "1h", from, to, factory, []map[string]interface{}{paramsA, paramsB}, 2)
	if err != nil {
		t.Fatalf("RunBatch failed: %v", err)
	}
	if batch[0].Err != nil {
		t.Fatalf("batch result A error: %v", batch[0].Err)
	}
	if batch[1].Err != nil {
		t.Fatalf("batch result B error: %v", batch[1].Err)
	}
	if batch[0].Result.TotalTrades != directA.TotalTrades {
		t.Fatalf("batch A trades=%d, want %d", batch[0].Result.TotalTrades, directA.TotalTrades)
	}
	if batch[1].Result.TotalTrades != directB.TotalTrades {
		t.Fatalf("batch B trades=%d, want %d", batch[1].Result.TotalTrades, directB.TotalTrades)
	}
}

type stubFactorFeed struct{}

func (f *stubFactorFeed) Fields() []string { return []string{"activity"} }

func (f *stubFactorFeed) Load(_ context.Context, req FactorRequest) (*DataSet, error) {
	nBars := 10
	ds := NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	activity := make([]float64, nBars)

	base := req.From
	for i := 0; i < nBars; i++ {
		ts[i] = base.Add(time.Duration(i) * 24 * time.Hour)
		if i%2 == 0 {
			activity[i] = 100
		} else {
			activity[i] = 200
		}
	}

	ds.SetTimestamps(ts)
	ds.AddColumn("activity", activity)
	return ds, nil
}

type factorDrivenStrategy struct {
	factorRef FactorRef
	seen23    float64
	seen24    float64
}

type immediateAndDeferredStrategy struct{}

type scheduledNotionalStrategy struct{}

type primaryContextFactorFeed struct {
	lastReq FactorRequest
}

func (f *primaryContextFactorFeed) Fields() []string { return []string{"value"} }

func (f *primaryContextFactorFeed) Load(_ context.Context, req FactorRequest) (*DataSet, error) {
	f.lastReq = req
	ds := NewDataSet(1)
	ds.SetTimestamps([]time.Time{req.From})
	ds.AddColumn("value", []float64{1})
	return ds, nil
}

type primaryContextFactorStrategy struct{}

func (s *primaryContextFactorStrategy) Name() string { return "primary-context-factor" }

func (s *primaryContextFactorStrategy) Init(ctx *SetupContext) error {
	ctx.AddFactor("primary_context", "1h")
	return nil
}

func (s *primaryContextFactorStrategy) OnBar(_ *BarContext) {}

func (s *immediateAndDeferredStrategy) Name() string { return "immediate-and-deferred" }

func (s *immediateAndDeferredStrategy) Init(_ *SetupContext) error { return nil }

func (s *immediateAndDeferredStrategy) OnBar(ctx *BarContext) {
	if ctx.BarIndex() != 0 {
		return
	}
	ctx.BuyNowWithNote(ctx.PrimaryRef(), 1, "now")
	ctx.Buy(ctx.PrimaryRef(), 1)
}

func (s *scheduledNotionalStrategy) Name() string { return "scheduled-notional" }

func (s *scheduledNotionalStrategy) Init(_ *SetupContext) error { return nil }

func (s *scheduledNotionalStrategy) OnBar(ctx *BarContext) {
	if ctx.BarIndex() != 0 {
		return
	}
	ctx.ScheduleBuyNotionalWithNote(ctx.Time().Add(time.Hour), ctx.PrimaryRef(), 202, "scheduled-notional")
}

func (s *factorDrivenStrategy) Name() string { return "factor-driven" }

func (s *factorDrivenStrategy) Init(ctx *SetupContext) error {
	s.factorRef = ctx.AddFactor("market_activity", "1d")
	ctx.RegisterFactor(s.factorRef, "activity_sma2", SMA("activity", 2))
	return nil
}

func (s *factorDrivenStrategy) OnBar(ctx *BarContext) {
	activity := ctx.Factor(s.factorRef).Field("activity")
	if ctx.BarIndex() == 23 {
		s.seen23 = activity
	}
	if ctx.BarIndex() == 24 {
		s.seen24 = activity
	}

	primary := ctx.PrimaryRef()
	if activity > 150 && ctx.Position(primary) == 0 {
		ctx.Buy(primary, 1)
	}
	if activity <= 150 && ctx.Position(primary) > 0 {
		ctx.Sell(primary, 1)
	}
}

func TestRunWithExternalFactorFeed(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})
	engine.RegisterFactorFeed("market_activity", &stubFactorFeed{})

	strategy := &factorDrivenStrategy{seen23: math.NaN(), seen24: math.NaN()}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, strategy, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.TotalTrades == 0 {
		t.Fatalf("expected trades driven by external factor, got 0")
	}

	if strategy.seen23 != 100 {
		t.Fatalf("unexpected aligned factor value at bar 23: got %v want 100", strategy.seen23)
	}
	if strategy.seen24 != 100 {
		t.Fatalf("unexpected aligned factor value at bar 24: got %v want 100", strategy.seen24)
	}
}

func TestImmediateExecutionUsesCurrentBarClose(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, &immediateAndDeferredStrategy{}, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(result.Trades))
	}
	if got := result.Trades[0].Note; got != "now" {
		t.Fatalf("unexpected first trade note: got %q", got)
	}
	if got := result.Trades[0].FillPrice; got != 100.5 {
		t.Fatalf("immediate trade fill price = %v, want current close 100.5", got)
	}
	if !result.Trades[0].Timestamp.Equal(from) {
		t.Fatalf("immediate trade timestamp = %v, want %v", result.Trades[0].Timestamp, from)
	}
	if want := from.Add(time.Hour); !result.Trades[1].Timestamp.Equal(want) {
		t.Fatalf("deferred trade timestamp = %v, want %v", result.Trades[1].Timestamp, want)
	}
}

func TestScheduledNotionalOrderUsesTriggerBarOpen(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, &scheduledNotionalStrategy{}, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}
	trade := result.Trades[0]
	if trade.BarIndex != 1 {
		t.Fatalf("trade.BarIndex = %d, want 1", trade.BarIndex)
	}
	if trade.FillPrice != 101 {
		t.Fatalf("trade.FillPrice = %v, want 101", trade.FillPrice)
	}
	wantQty := 202.0 / 101.0
	if math.Abs(trade.Qty-wantQty) > 1e-9 {
		t.Fatalf("trade.Qty = %.12f, want %.12f", trade.Qty, wantQty)
	}
}

func TestRunReportsProgress(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	var updates []ProgressUpdate
	engine.SetProgressFunc(func(update ProgressUpdate) {
		updates = append(updates, update)
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	if _, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, &trendStrategy{}, nil); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(updates) == 0 {
		t.Fatalf("expected progress updates")
	}

	var sawPrepare bool
	var sawReplayStart bool
	var sawReplayDone bool
	for _, update := range updates {
		switch update.Phase {
		case ProgressPhasePrepare:
			sawPrepare = true
		case ProgressPhaseReplay:
			if update.Current == 0 && update.Total == 100 {
				sawReplayStart = true
			}
			if update.Completed && update.Current == 100 && update.Total == 100 {
				sawReplayDone = true
			}
		}
	}

	if !sawPrepare {
		t.Fatalf("expected prepare progress update")
	}
	if !sawReplayStart {
		t.Fatalf("expected replay start progress update")
	}
	if !sawReplayDone {
		t.Fatalf("expected replay completion progress update")
	}
}

func TestFactorRequestCarriesPrimaryContext(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("us-stocks", &stubDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	feed := &primaryContextFactorFeed{}
	engine.RegisterFactorFeed("primary_context", feed)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	if _, err := engine.Run(context.Background(), "us-stocks", "NVDA", "1h", from, to, &primaryContextFactorStrategy{}, nil); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if feed.lastReq.PrimaryMarket != "us-stocks" || feed.lastReq.PrimarySymbol != "NVDA" {
		t.Fatalf("primary context = %s/%s, want us-stocks/NVDA", feed.lastReq.PrimaryMarket, feed.lastReq.PrimarySymbol)
	}
}
