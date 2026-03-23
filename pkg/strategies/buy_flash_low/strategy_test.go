package buyflashlow

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

type stagedFlashLowFeed struct {
	firstRunBars  int
	secondRunBars int
	firstRunFrom  time.Time
	secondRunFrom time.Time
}

type risingDvolFactorFeed struct{}

func (f *risingDvolFactorFeed) Fields() []string {
	return []string{"open", "high", "low", "close"}
}

func (f *risingDvolFactorFeed) Load(_ context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	if !req.To.After(req.From) {
		ds := backtest.NewDataSet(0)
		ds.SetTimestamps(nil)
		ds.AddColumn("open", nil)
		ds.AddColumn("high", nil)
		ds.AddColumn("low", nil)
		ds.AddColumn("close", nil)
		return ds, nil
	}

	bars := int(req.To.Sub(req.From) / time.Hour)
	if bars <= 0 {
		bars = 1
	}

	ds := backtest.NewDataSet(bars)
	ts := make([]time.Time, bars)
	open := make([]float64, bars)
	high := make([]float64, bars)
	low := make([]float64, bars)
	closeSeries := make([]float64, bars)

	for i := 0; i < bars; i++ {
		ts[i] = req.From.Add(time.Duration(i) * time.Hour)
		base := 50.0 + float64(i)*0.1
		open[i] = base
		high[i] = base + 0.2
		low[i] = base - 0.2
		closeSeries[i] = base + 0.1
	}

	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", closeSeries)
	return ds, nil
}

func (f *stagedFlashLowFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *stagedFlashLowFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	bars := f.secondRunBars
	if req.From.Equal(f.firstRunFrom) {
		bars = f.firstRunBars
	}
	return flashLowDataSet(req.From, bars), nil
}

func flashLowDataSet(from time.Time, bars int) *backtest.DataSet {
	ds := backtest.NewDataSet(bars)
	ts := make([]time.Time, bars)
	open := make([]float64, bars)
	high := make([]float64, bars)
	low := make([]float64, bars)
	closeSeries := make([]float64, bars)
	volume := make([]float64, bars)

	for i := 0; i < bars; i++ {
		ts[i] = from.Add(time.Duration(i) * time.Hour)
		open[i] = 100
		high[i] = 101
		low[i] = 99
		closeSeries[i] = 100.5
		volume[i] = 100
	}

	if bars > 100 {
		open[100] = 100
		high[100] = 110
		low[100] = 98.5
		closeSeries[100] = 109.5
		volume[100] = 1000
	}

	if bars > 101 {
		open[101] = 109
		high[101] = 110
		low[101] = 108
		closeSeries[101] = 109
	}

	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", closeSeries)
	ds.AddColumn("volume", volume)
	return ds
}

func TestStrategyInstanceCanBeReusedAfterUnfilledFinalBarSignal(t *testing.T) {
	firstFrom := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	secondFrom := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 1000})
	engine.RegisterDataFeed("test", &stagedFlashLowFeed{
		firstRunBars:  101,
		secondRunBars: 102,
		firstRunFrom:  firstFrom,
		secondRunFrom: secondFrom,
	})
	engine.RegisterFactorFeed("dvol", &risingDvolFactorFeed{})

	strategy := &buyFlashLowStrategy{
		lookback:       defaultLookback,
		minAmpPr:       defaultMinAmpPr,
		scoreThreshold: defaultScoreThreshold,
		strictScore:    defaultStrictScore,
	}

	firstResult, err := engine.Run(context.Background(), "test", "TEST", "1h", firstFrom, firstFrom.Add(101*time.Hour), strategy, nil)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if firstResult.TotalTrades != 0 {
		t.Fatalf("first run trades=%d, want 0", firstResult.TotalTrades)
	}

	secondResult, err := engine.Run(context.Background(), "test", "TEST", "1h", secondFrom, secondFrom.Add(102*time.Hour), strategy, nil)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if secondResult.TotalTrades != 1 {
		t.Fatalf("second run trades=%d, want 1", secondResult.TotalTrades)
	}
}

type trailingAnchorFeed struct{}

func (f *trailingAnchorFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *trailingAnchorFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	const bars = 103
	ds := backtest.NewDataSet(bars)
	ts := make([]time.Time, bars)
	open := make([]float64, bars)
	high := make([]float64, bars)
	low := make([]float64, bars)
	closeSeries := make([]float64, bars)
	volume := make([]float64, bars)

	for i := 0; i < bars; i++ {
		ts[i] = req.From.Add(time.Duration(i) * time.Hour)
		open[i] = 100
		high[i] = 101
		low[i] = 99
		closeSeries[i] = 100.5
		volume[i] = 100
	}

	open[100] = 100
	high[100] = 120
	low[100] = 98.5
	closeSeries[100] = 110.5
	volume[100] = 1000

	open[101] = 109
	high[101] = 111
	low[101] = 105.5
	closeSeries[101] = 106

	open[102] = 106
	high[102] = 107
	low[102] = 105
	closeSeries[102] = 106.5

	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", closeSeries)
	ds.AddColumn("volume", volume)
	return ds, nil
}

func TestEntrySeedsTrailingAnchorFromSignalClose(t *testing.T) {
	from := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 1000})
	engine.RegisterDataFeed("test", &trailingAnchorFeed{})
	engine.RegisterFactorFeed("dvol", &risingDvolFactorFeed{})

	strategy := &buyFlashLowStrategy{
		lookback:       defaultLookback,
		minAmpPr:       defaultMinAmpPr,
		scoreThreshold: defaultScoreThreshold,
		strictScore:    defaultStrictScore,
	}

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, from.Add(103*time.Hour), strategy, nil)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.TotalTrades != 1 {
		t.Fatalf("expected only the entry trade without a premature trailing exit, got %d trades", result.TotalTrades)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 fill, got %d", len(result.Trades))
	}
	if got := result.Trades[0].Side; got != backtest.Buy {
		t.Fatalf("expected first trade to be a buy, got %v", got)
	}
}
