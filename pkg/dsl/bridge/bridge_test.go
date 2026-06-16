package bridge

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
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
	step := time.Hour
	if d, err := time.ParseDuration(strings.TrimSpace(req.Interval)); err == nil && d > 0 {
		step = d
	}
	baseOffset := 0.0
	if req.Symbol == "ALT" {
		baseOffset = 100
	}
	for i := 0; i < nBars; i++ {
		price := 100.0 + baseOffset + float64(i)
		ts[i] = req.From.Add(time.Duration(i) * step)
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

type testFundamentalFactorFeed struct {
	lastReq backtest.FactorRequest
}

type testOptionsChainProvider struct{}

func (p *testOptionsChainProvider) AvailableContracts(t time.Time) []backtest.OptionContract {
	expiry := t.Add(30 * 24 * time.Hour)
	return []backtest.OptionContract{
		{Symbol: "C-100", Underlying: "TEST", UnderlyingMarket: "test", Type: backtest.Call, StrikePrice: 100, Expiration: expiry, Delta: 0.50, BidPrice: 5.0, AskPrice: 5.2, MarkPrice: 5.1},
		{Symbol: "C-105", Underlying: "TEST", UnderlyingMarket: "test", Type: backtest.Call, StrikePrice: 105, Expiration: expiry, Delta: 0.38, BidPrice: 2.4, AskPrice: 2.6, MarkPrice: 2.5},
		{Symbol: "C-110", Underlying: "TEST", UnderlyingMarket: "test", Type: backtest.Call, StrikePrice: 110, Expiration: expiry, Delta: 0.29, BidPrice: 2.0, AskPrice: 2.2, MarkPrice: 2.1},
	}
}

func (p *testOptionsChainProvider) AvailableContractsFor(t time.Time, market, underlying string) []backtest.OptionContract {
	expiry := t.Add(30 * 24 * time.Hour)
	if strings.EqualFold(market, "test") && strings.EqualFold(underlying, "ALT") {
		return []backtest.OptionContract{{Symbol: "ALT-C-200", Underlying: "ALT", UnderlyingMarket: "test", Type: backtest.Call, StrikePrice: 200, Expiration: expiry, Delta: 0.41, BidPrice: 6.0, AskPrice: 6.2, MarkPrice: 6.1}}
	}
	return p.AvailableContracts(t)
}

func (f *testFactorFeed) Fields() []string { return []string{"dvol"} }

func (f *testFactorFeed) Load(_ context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	nBars := 12
	ds := backtest.NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	dvol := make([]float64, nBars)
	step := time.Hour
	if d, err := time.ParseDuration(strings.TrimSpace(req.Interval)); err == nil && d > 0 {
		step = d
	}
	for i := 0; i < nBars; i++ {
		ts[i] = req.From.Add(time.Duration(i) * step)
		dvol[i] = 50 + float64(i)
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("dvol", dvol)
	return ds, nil
}

func (f *testFundamentalFactorFeed) Fields() []string { return []string{"value"} }

func (f *testFundamentalFactorFeed) Load(_ context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	f.lastReq = req
	nBars := 12
	ds := backtest.NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	values := make([]float64, nBars)
	step := time.Hour
	if d, err := time.ParseDuration(strings.TrimSpace(req.Interval)); err == nil && d > 0 {
		step = d
	}
	for i := 0; i < nBars; i++ {
		ts[i] = req.From.Add(time.Duration(i) * step)
		values[i] = 24.5 + float64(i)
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("value", values)
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

func TestDslStrategyCanOpenGroupedSpread(t *testing.T) {
	src := `strategy("Grouped Spread")
if bar_index == 0 {
  chain = options.calls(options.chain())
  near = options.expiry_nearest(chain, 30)
  contracts = options.sort_by_delta(near, 0.5)
  if len(contracts) >= 3 {
    gid = group.open("test-group", 5, 1)
    legs = [leg.sell(contracts[0], 1), leg.buy(contracts[1], 1), leg.buy(contracts[2], 1)]
    sid = spread.open_in_group(legs, "test-spread", gid)
    plot(gid, title="gid", precision=0)
    plot(sid, title="sid", precision=0)
  }
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.SpreadSummary == nil || result.SpreadSummary.TotalSpreads != 1 {
		t.Fatalf("unexpected spread summary: %#v", result.SpreadSummary)
	}
	if len(result.SpreadGroups) != 1 {
		t.Fatalf("len(result.SpreadGroups) = %d, want 1", len(result.SpreadGroups))
	}
	if len(result.SpreadGroups[0].SpreadIDs) != 1 {
		t.Fatalf("unexpected spread group contents: %#v", result.SpreadGroups[0])
	}
}

func TestDslContractAccessAfterSortByDelta(t *testing.T) {
	src := `strategy("Contract Access")
chain = options.chain()
calls = options.calls(chain)
contracts = options.sort_by_delta(calls, 0.5)
if len(contracts) > 0 {
  c = contracts[0]
  plot(contract.strike(c), title="strike", precision=2)
	plot(contract.expiry(c), title="expiry", precision=0)
  plot(contract.delta(c), title="delta", precision=4)
  plot(contract.mark(c), title="mark", precision=4)
}
	`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	strikeSeries := result.Series[result.ReportColumns[0].Source]
	expirySeries := result.Series[result.ReportColumns[1].Source]
	deltaSeries := result.Series[result.ReportColumns[2].Source]
	markSeries := result.Series[result.ReportColumns[3].Source]
	if strikeSeries[0] != 100 {
		t.Fatalf("unexpected strike series: %#v", strikeSeries[:1])
	}
	if expirySeries[0] <= 0 {
		t.Fatalf("unexpected expiry series: %#v", expirySeries[:1])
	}
	if deltaSeries[0] != 0.5 {
		t.Fatalf("unexpected delta series: %#v", deltaSeries[:1])
	}
	if markSeries[0] != 5.1 {
		t.Fatalf("unexpected mark series: %#v", markSeries[:1])
	}
}

func TestDslStrategyOptionsChainForExplicitSymbol(t *testing.T) {
	src := `strategy("Explicit Chain")
chain = options.chain("test", "ALT")
calls = options.calls(chain)
if options.len(calls) > 0 {
  c = options.best_spread(calls)
  plot(contract.strike(c), title="strike", precision=2)
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	strikeSeries := result.Series[result.ReportColumns[0].Source]
	if strikeSeries[0] != 200 {
		t.Fatalf("unexpected strike series for explicit symbol chain: %#v", strikeSeries[:1])
	}
}

func TestDslStrategyPortfolioBuiltins(t *testing.T) {
	src := `strategy("Portfolio Helpers")
symbols = portfolio.symbols()
weights = portfolio.weights()
items = portfolio.items()

plot(portfolio.len(), title="count", precision=0)
plot(portfolio.weight("MSFT"), title="msft_weight", precision=2)
plot(len(str.split(config.string("portfolio_symbols", ""), ",")), title="split_count", precision=0)
plot(items[1][1], title="item_weight", precision=2)
plot(str.length(str.join(symbols, "|")), title="joined_len", precision=0)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, NewWithOptions(src, Options{Config: map[string]interface{}{
		"portfolio_symbols": "QQQ,MSFT,AAPL",
		"portfolio_weights": "0.2,0.15,0.1",
	}}), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	countSeries := result.Series[result.ReportColumns[0].Source]
	msftSeries := result.Series[result.ReportColumns[1].Source]
	splitSeries := result.Series[result.ReportColumns[2].Source]
	itemSeries := result.Series[result.ReportColumns[3].Source]
	joinSeries := result.Series[result.ReportColumns[4].Source]
	if countSeries[0] != 3 {
		t.Fatalf("unexpected portfolio count: %#v", countSeries[:1])
	}
	if msftSeries[0] != 0.15 {
		t.Fatalf("unexpected MSFT weight: %#v", msftSeries[:1])
	}
	if splitSeries[0] != 3 {
		t.Fatalf("unexpected split count: %#v", splitSeries[:1])
	}
	if itemSeries[0] != 0.15 {
		t.Fatalf("unexpected second item weight: %#v", itemSeries[:1])
	}
	if joinSeries[0] != 13 {
		t.Fatalf("unexpected joined symbol length: %#v", joinSeries[:1])
	}
}

func TestDslContractAccessInsideWhileLoop(t *testing.T) {
	src := `strategy("Contract Access Loop")
chain = options.chain()
calls = options.calls(chain)
contracts = options.sort_by_delta(calls, 0.5)
idx = 0
while idx < len(contracts) {
  c = contracts[idx]
  plot(contract.strike(c), title="strike", precision=2)
  plot(contract.delta(c), title="delta", precision=4)
  idx = idx + 1
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	strikeSeries := result.Series[result.ReportColumns[0].Source]
	deltaSeries := result.Series[result.ReportColumns[1].Source]
	if strikeSeries[0] != 110 {
		t.Fatalf("unexpected loop strike series: %#v", strikeSeries[:1])
	}
	if deltaSeries[0] != 0.29 {
		t.Fatalf("unexpected loop delta series: %#v", deltaSeries[:1])
	}
}

func TestDslSpreadLegAndGroupRollAccess(t *testing.T) {
	src := `strategy("Spread Leg Access")
if bar_index == 0 {
  chain = options.calls(options.chain())
  near = options.expiry_nearest(chain, 30)
  contracts = options.sort_by_delta(near, 0.5)
  if len(contracts) >= 3 {
    gid = group.open("test-group", 5, 0.9)
    legs = [leg.sell(contracts[0], 1), leg.buy(contracts[1], 2), leg.buy(contracts[2], 3)]
    sid = spread.open_in_group(legs, "test-spread", gid)
    group.increment_roll(gid)
    plot(spread.leg_entry_price(sid, 0), title="entry_price", precision=4)
    plot(spread.leg_qty(sid, 1), title="qty", precision=0)
    plot(contract.strike(spread.leg_contract(sid, 2)), title="strike", precision=0)
    plot(group.get(gid)[2], title="amount", precision=4)
  }
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	entrySeries := result.Series[result.ReportColumns[0].Source]
	qtySeries := result.Series[result.ReportColumns[1].Source]
	strikeSeries := result.Series[result.ReportColumns[2].Source]
	amountSeries := result.Series[result.ReportColumns[3].Source]
	if entrySeries[0] <= 0 {
		t.Fatalf("unexpected entry price series: %#v", entrySeries[:1])
	}
	if qtySeries[0] != 2 {
		t.Fatalf("unexpected qty series: %#v", qtySeries[:1])
	}
	if strikeSeries[0] != 110 {
		t.Fatalf("unexpected strike series: %#v", strikeSeries[:1])
	}
	if amountSeries[0] != 4.5 {
		t.Fatalf("unexpected decayed amount series: %#v", amountSeries[:1])
	}
}

func TestDslSpreadOpenOnValidatesScope(t *testing.T) {
	src := `strategy("Spread Scope")
if bar_index == 0 {
  chain = options.calls(options.chain("test", "ALT"))
  contracts = options.sort_by_delta(chain, 0.4)
  if len(contracts) > 0 {
    legs = [leg.sell(contracts[0], 1)]
    good = spread.open_on("test", "ALT", legs, "alt-spread")
    bad = spread.open_on("test", "TEST", legs, "wrong-spread")
    plot(good, title="good_id", precision=0)
    plot(bad, title="bad_id", precision=0)
  }
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	goodSeries := result.Series[result.ReportColumns[0].Source]
	badSeries := result.Series[result.ReportColumns[1].Source]
	if goodSeries[0] != 1 {
		t.Fatalf("unexpected scoped spread id: %#v", goodSeries[:1])
	}
	if !math.IsNaN(badSeries[0]) {
		t.Fatalf("expected mismatched scoped spread to return na, got %#v", badSeries[:1])
	}
	if len(result.SpreadPositions) != 1 {
		t.Fatalf("unexpected spread positions: %#v", result.SpreadPositions)
	}
	if result.SpreadPositions[0].Tag != "alt-spread" {
		t.Fatalf("unexpected spread tag: got %q want %q", result.SpreadPositions[0].Tag, "alt-spread")
	}
}

func TestDslSpreadCloseReasonIsRecorded(t *testing.T) {
	src := `strategy("Spread Close Reason")
varip tracked_spread = 0

if bar_index == 0 {
  chain = options.calls(options.chain())
  contracts = options.sort_by_delta(chain, 0.5)
  if len(contracts) >= 3 {
    legs = [leg.sell(contracts[0], 1), leg.buy(contracts[1], 1), leg.buy(contracts[2], 1)]
    tracked_spread = spread.open(legs, "open-tag")
  }
}

if bar_index == 1 and tracked_spread > 0 {
  spread.close(tracked_spread, "close-reason")
}
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.SpreadPositions) != 1 {
		t.Fatalf("unexpected spread positions: %#v", result.SpreadPositions)
	}
	if result.SpreadPositions[0].CloseNote != "close-reason" {
		t.Fatalf("unexpected close note: got %q want %q", result.SpreadPositions[0].CloseNote, "close-reason")
	}
}

func TestDslScheduleCloseGroupTargetsGroupSpreads(t *testing.T) {
	src := `strategy("Schedule Group Close")
varip tracked_group1 = 0
varip tracked_group2 = 0
varip tracked_spread1b = 0
varip tracked_spread2 = 0

if bar_index == 0 {
  chain = options.calls(options.chain())
  contracts = options.sort_by_delta(chain, 0.5)
  if len(contracts) >= 3 {
    legs = [leg.sell(contracts[0], 1), leg.buy(contracts[1], 1), leg.buy(contracts[2], 1)]
    tracked_group1 = group.open("g1", 5, 1)
    spread.open_in_group(legs, "g1-a", tracked_group1)
    tracked_spread1b = spread.open_in_group(legs, "g1-b", tracked_group1)
    tracked_group2 = group.open("g2", 5, 1)
    tracked_spread2 = spread.open_in_group(legs, "g2-a", tracked_group2)
    schedule.close_group(1, tracked_group2)
  }
}

group1b_open = 0
group2_open = 0
if tracked_spread1b > 0 {
  info1 = spread.get(tracked_spread1b)
  if info1[4] {
    group1b_open = 1
  }
}
if tracked_spread2 > 0 {
  info2 = spread.get(tracked_spread2)
  if info2[4] {
    group2_open = 1
  }
}

plot(group1b_open, title="group1b_open", precision=0)
plot(group2_open, title="group2_open", precision=0)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	engine.SetOptionsChainProvider(&testOptionsChainProvider{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "15m", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	plotSource := func(label string) string {
		for _, col := range result.ReportColumns {
			if col.Label == label {
				return col.Source
			}
		}
		return ""
	}
	group1Source := plotSource("group1b_open")
	group2Source := plotSource("group2_open")
	if group1Source == "" || group2Source == "" {
		t.Fatalf("missing expected plot columns: %#v", result.ReportColumns)
	}
	group1Series := result.Series[group1Source]
	group2Series := result.Series[group2Source]
	if group1Series[len(group1Series)-1] != 1 {
		t.Fatalf("expected group1 spread to remain open, got series tail %v", group1Series[len(group1Series)-3:])
	}
	if group2Series[len(group2Series)-1] != 0 {
		t.Fatalf("expected scheduled group2 spread close, got series tail %v", group2Series[len(group2Series)-3:])
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

func TestDslStrategyPositionAvgPriceUsesEntryPrice(t *testing.T) {
	src := `strategy("Avg Price")
if bar_index == 0 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
plot(strategy.position_avg_price, title="Avg", precision=1)
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
	if len(series) < 3 {
		t.Fatalf("series too short: %v", series)
	}
	if series[1] != 101 || series[2] != 101 {
		t.Fatalf("expected avg entry price to stay at fill price 101, got %v", series[:3])
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

func TestDslStrategyRequestFundamentalPlotsFactorSeries(t *testing.T) {
	src := `strategy("Fundamental Request")
pe = request.fundamental("us-stocks", "AAPL", "pe")
plot(pe, title="PE", precision=2)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	fundamentalFeed := &testFundamentalFactorFeed{}
	engine.RegisterFactorFeed("pe", fundamentalFeed)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 24.5 || series[1] != 25.5 {
		t.Fatalf("unexpected request.fundamental plot series: first=%v second=%v", series[0], series[1])
	}
	if fundamentalFeed.lastReq.Interval != "1h" || fundamentalFeed.lastReq.Mode != "filled" || fundamentalFeed.lastReq.Market != "us-stocks" || fundamentalFeed.lastReq.Symbol != "AAPL" {
		t.Fatalf("unexpected request.fundamental factor request: %+v", fundamentalFeed.lastReq)
	}
}

func TestDslStrategyRequestFundamentalWithNamedArgsPreloadsFactorSeries(t *testing.T) {
	src := `strategy("Named Fundamental Request")
pe = request.fundamental(symbol="AAPL", factor="pe", market="us-stocks")
plot(pe, title="PE", precision=2)
`

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
	fundamentalFeed := &testFundamentalFactorFeed{}
	engine.RegisterFactorFeed("pe", fundamentalFeed)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 24.5 || series[1] != 25.5 {
		t.Fatalf("unexpected request.fundamental plot series: first=%v second=%v", series[0], series[1])
	}
	if fundamentalFeed.lastReq.Interval != "1h" || fundamentalFeed.lastReq.Mode != "filled" || fundamentalFeed.lastReq.Market != "us-stocks" || fundamentalFeed.lastReq.Symbol != "AAPL" {
		t.Fatalf("unexpected request.fundamental factor request: %+v", fundamentalFeed.lastReq)
	}
}

func TestDslStrategyRequestFundamentalSurvivesRepeatedInit(t *testing.T) {
	src := `strategy("Fundamental Request")
pe = request.fundamental("us-stocks", "AAPL", "pe")
plot(pe, title="PE", precision=2)
`

	strategy := New(src)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)

	run := func() *backtest.Result {
		engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
		engine.RegisterDataFeed("test", &testDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})
		engine.RegisterFactorFeed("pe", &testFundamentalFactorFeed{})
		result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, strategy, nil)
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		return result
	}

	_ = run()
	result := run()
	series := result.Series[result.ReportColumns[0].Source]
	if series[0] != 24.5 || series[1] != 25.5 {
		t.Fatalf("unexpected repeated request.fundamental plot series: first=%v second=%v", series[0], series[1])
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

// TestStrategyPositionSizeProperty verifies strategy.position_size and strategy.cash/equity
// are readable as properties and trigger trades correctly.
func TestStrategyPositionSizeProperty(t *testing.T) {
	// Simple: buy on first bar using strategy.position_size as a property
	src := `strategy("PositionPropTest")
if strategy.position_size == 0 {
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
	if len(result.Trades) == 0 {
		t.Fatal("expected at least one trade from strategy.position_size property check")
	}
}

// TestCrossoverStrategy verifies ta.crossover detects crossovers and generates trades.
func TestCrossoverStrategy(t *testing.T) {
	src := `strategy("CrossoverTest")
fast = ta.sma(close, 3)
slow = ta.sma(close, 5)
buy_sig = ta.crossover(fast, slow)
if buy_sig {
  strategy.entry(id="long", direction=strategy.long, qty=1)
  strategy.close(id="long")
}
`
	// Use a feed with prices that force multiple crossovers
	engine := backtest.NewEngine(backtest.Config{InitialCapital: 10000})
	// Use the zigzag feed override to get crossovers
	engine.RegisterDataFeed("test", &crossoverTestFeed{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(100 * time.Hour)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, New(src), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Trades) == 0 {
		t.Fatal("expected at least one trade from crossover detection")
	}
	t.Logf("crossover trades: %d", len(result.Trades))
}

type crossoverTestFeed struct{}

func (f *crossoverTestFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *crossoverTestFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	// Create a price series with clear crossovers:
	// 50 declining bars then 50 rising bars
	nBars := 100
	ds := backtest.NewDataSet(nBars)
	ts := make([]time.Time, nBars)
	open := make([]float64, nBars)
	high := make([]float64, nBars)
	low := make([]float64, nBars)
	closeCol := make([]float64, nBars)
	volume := make([]float64, nBars)
	for i := 0; i < nBars; i++ {
		var price float64
		if i < 50 {
			price = 100.0 - float64(i)*0.5 // declining: 100 → 75
		} else {
			price = 75.0 + float64(i-50)*1.0 // rising: 75 → 125
		}
		ts[i] = req.From.Add(time.Duration(i) * time.Hour)
		open[i], high[i], low[i], closeCol[i] = price, price+0.5, price-0.5, price
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
