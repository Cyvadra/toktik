package bridge

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

type testDataFeed struct {
	fields []string
}

func (f *testDataFeed) Fields() []string { return f.fields }

func (f *testDataFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	nBars := 12
	ds := backtest.NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	open := make([]float64, nBars)
	high := make([]float64, nBars)
	low := make([]float64, nBars)
	closeCol := make([]float64, nBars)
	volume := make([]float64, nBars)
	baseOffset := 0.0
	if req.Symbol == "ALT" {
		baseOffset = 100
	}
	for i := 0; i < nBars; i++ {
		price := 100.0 + baseOffset + float64(i)
		ts[i] = req.From.Add(time.Duration(i) * time.Hour)
		open[i] = price
		high[i] = price + 1
		low[i] = price - 1
		closeCol[i] = price + 0.5
		volume[i] = 1000
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", closeCol)
	ds.AddColumn("volume", volume)
	return ds, nil
}

type testFactorFeed struct{}

func (f *testFactorFeed) Fields() []string { return []string{"dvol"} }

func (f *testFactorFeed) Load(_ context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	nBars := 12
	ds := backtest.NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	dvol := make([]float64, nBars)
	for i := 0; i < nBars; i++ {
		ts[i] = req.From.Add(time.Duration(i) * time.Hour)
		dvol[i] = 50 + float64(i)
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("dvol", dvol)
	return ds, nil
}

func TestDslStrategyParsesAndNames(t *testing.T) {
	src := `strategy("My Test Strategy")
var count = 0
count := count + 1
x = 2 + 3
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "My Test Strategy" {
		t.Errorf("expected name 'My Test Strategy', got %q", ds.Name())
	}
}

func TestDslStrategyDefaultName(t *testing.T) {
	src := `x = 1 + 2`
	ds := New(src)
	if ds.Name() != "dsl_strategy" {
		t.Errorf("expected default name 'dsl_strategy', got %q", ds.Name())
	}
}

func TestDslStrategyParseError(t *testing.T) {
	src := `strategy(`
	ds := New(src)
	if len(ds.ParseErrors()) == 0 {
		t.Error("expected parse errors for incomplete input")
	}
}

func TestDslStrategyFullScript(t *testing.T) {
	src := `strategy("EMA Cross")

// Parameters
fast_len = 10
slow_len = 20

// Variables
var position = 0

// Logic
sma_val = ta.sma(close, fast_len)
if sma_val > 0 {
  position := 1
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "EMA Cross" {
		t.Errorf("expected name 'EMA Cross', got %q", ds.Name())
	}
}

func TestDslStrategyOptionsScript(t *testing.T) {
	src := `strategy("Iron Condor Seller")

// Get options chain
chain = options.chain()
if chain != na {
  // Filter puts and calls
  puts = options.puts(chain)
  calls = options.calls(chain)

  // Find near-expiry contracts
  near_puts = options.expiry_nearest(puts, 30)
  near_calls = options.expiry_nearest(calls, 30)

  // Get best spread contract
  sell_put = options.best_spread(near_puts)
  sell_call = options.best_spread(near_calls)

  if sell_put != na {
    // Build legs
    put_leg = leg.sell(sell_put, 1)
    call_leg = leg.sell(sell_call, 1)
    legs = [put_leg, call_leg]

    // Open the spread
    spread_id = spread.open(legs, "iron_condor")
  }
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "Iron Condor Seller" {
		t.Errorf("expected name 'Iron Condor Seller', got %q", ds.Name())
	}
}

func TestDslStrategyAlphaScript(t *testing.T) {
	src := `strategy("Alpha Momentum")

// WorldQuant-style alpha factors
mom = alpha.ts_delta(close, 20)
vol = alpha.ts_std(close, 20)
zscore = alpha.zscore(close, 50)
rank = alpha.ts_rank(close, 100)
decay = alpha.decay_linear(close, 10)

if zscore > 2 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
if zscore < -2 {
  strategy.entry(id="short", direction=strategy.short, qty=1)
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "Alpha Momentum" {
		t.Errorf("expected name 'Alpha Momentum', got %q", ds.Name())
	}
}

func TestDslStrategyPlotExportsReportSeries(t *testing.T) {
	src := `strategy("Plot Export")
plot(close, title="Close", overlay=true, precision=2)
plot(ta.sma(close, 3), title="SMA 3", precision=3)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(12 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(result.ReportColumns) != 2 {
		t.Fatalf("len(result.ReportColumns) = %d, want 2", len(result.ReportColumns))
	}
	if result.ReportColumns[0].Label != "Close" || !result.ReportColumns[0].Overlay || result.ReportColumns[0].Decimals != 2 {
		t.Fatalf("unexpected first report column: %#v", result.ReportColumns[0])
	}
	if result.ReportColumns[1].Label != "SMA 3" || result.ReportColumns[1].Overlay || result.ReportColumns[1].Decimals != 3 {
		t.Fatalf("unexpected second report column: %#v", result.ReportColumns[1])
	}

	closeSeries := result.Series[result.ReportColumns[0].Source]
	if len(closeSeries) != len(result.Timestamps) {
		t.Fatalf("len(close plot series) = %d, want %d", len(closeSeries), len(result.Timestamps))
	}
	if closeSeries[0] != 100.5 || closeSeries[len(closeSeries)-1] != 111.5 {
		t.Fatalf("unexpected close plot values: first=%v last=%v", closeSeries[0], closeSeries[len(closeSeries)-1])
	}

	smaSeries := result.Series[result.ReportColumns[1].Source]
	if len(smaSeries) != len(result.Timestamps) {
		t.Fatalf("len(sma plot series) = %d, want %d", len(smaSeries), len(result.Timestamps))
	}
	if !math.IsNaN(smaSeries[0]) {
		t.Fatalf("unexpected first sma plot value: got %v want NaN", smaSeries[0])
	}
	if smaSeries[2] != 101.5 {
		t.Fatalf("unexpected third sma plot value: got %v want %v", smaSeries[2], 101.5)
	}
}

func TestDslStrategyNamedStrategyEntryExecutes(t *testing.T) {
	src := `strategy("Named Args Entry")
if bar_index == 0 {
  strategy.entry(id="long", direction=strategy.long, qty=2)
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Trades) == 0 {
		t.Fatal("expected strategy.entry with named args to place at least one trade")
	}
	if result.Trades[0].Qty != 2 {
		t.Fatalf("unexpected first trade qty: got %v want 2", result.Trades[0].Qty)
	}
}

func TestDslStrategySignalPreloadBuildsEntrySignal(t *testing.T) {
	dir := t.TempDir()
	signalPath := filepath.Join(dir, "entry-signals.txt")
	content := "Jan 1, 2024, 00:00\nJan 1, 2024, 03:00\n"
	if err := os.WriteFile(signalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := `strategy("Signal Preload", signal_source="` + signalPath + `", signal_name="entry_signal", signal_time_layout="Jan 2, 2006, 15:04", signal_timezone="UTC", signal_optional_index=true)
plot(entry_signal, title="Entry Signal", precision=0)
if entry_signal == 1 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("len(result.Trades) = %d, want 2", len(result.Trades))
	}
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 1 || series[1] != 0 || series[3] != 1 {
		t.Fatalf("unexpected entry_signal series: %#v", series[:4])
	}
}

func TestDslStrategyRequestSecurityPlotsExternalSeries(t *testing.T) {
	src := `strategy("Security Request")
alt_close = request.security("test", "ALT", "1h", "close")
plot(alt_close, title="ALT Close", precision=1)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 200.5 || series[1] != 201.5 {
		t.Fatalf("unexpected request.security plot series: first=%v second=%v", series[0], series[1])
	}
}

func TestDslStrategyRequestFactorPlotsFactorSeries(t *testing.T) {
	src := `strategy("Factor Request")
dvol = request.factor("dvol", "1h", "dvol")
plot(dvol, title="DVOL", precision=1)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.RegisterFactorFeed("dvol", &testFactorFeed{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 50 || series[1] != 51 {
		t.Fatalf("unexpected request.factor plot series: first=%v second=%v", series[0], series[1])
	}
}

func TestDslStrategyInputDefaultValue(t *testing.T) {
	src := `strategy("Input Defaults")
fast = input(5, title="Fast Length")
slow = input(20, title="Slow Length")
sma_fast = ta.sma(close, fast)
sma_slow = ta.sma(close, slow)
plot(sma_fast, title="Fast SMA", precision=2)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(12 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.ReportColumns) == 0 {
		t.Fatal("expected at least one report column")
	}
	// sma(close, 5): at bar index 5+, should be non-NaN
	series := result.Series[result.ReportColumns[0].Source]
	if len(series) < 6 {
		t.Fatalf("series too short: %d", len(series))
	}
	// First 4 bars should be NaN (not enough history for sma(5))
	if !math.IsNaN(series[3]) {
		t.Fatalf("expected NaN at bar 4, got %g", series[3])
	}
	// Bar 5 onward should have valid values
	if math.IsNaN(series[4]) {
		t.Fatalf("expected non-NaN at bar 5, got NaN")
	}
}

func TestDslStrategyIndicatorChainingOnRequestSecurity(t *testing.T) {
	src := `strategy("Indicator Chaining")
alt_close = request.security("test", "ALT", "1h", "close")
alt_sma3 = ta.sma(alt_close, 3)
plot(alt_sma3, title="ALT SMA3", precision=2)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.ReportColumns) == 0 {
		t.Fatal("expected report column for alt_sma3")
	}
	series := result.Series[result.ReportColumns[0].Source]
	// ALT close at bars 1-3+: 200.5, 201.5, 202.5 → SMA3 at bar 3 = 201.5
	if math.IsNaN(series[2]) {
		t.Fatalf("expected non-NaN at bar 3 of alt_sma3")
	}
	wantSMA3 := (200.5 + 201.5 + 202.5) / 3
	if math.Abs(series[2]-wantSMA3) > 1e-9 {
		t.Fatalf("alt_sma3[2]: expected %g, got %g", wantSMA3, series[2])
	}
}
