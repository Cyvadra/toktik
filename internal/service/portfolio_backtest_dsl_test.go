package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

func TestResolveRequestedStrategiesBuildsDynamicDSLStrategy(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("Runtime DSL")
length = input.int(5, title="Length", minval=1, maxval=10)
if bar_index == 0 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}`,
		DSLParams: map[string]interface{}{"Length": 7.0},
	}

	resolved, label, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	if label != "runtime-dsl" {
		t.Fatalf("label = %q, want runtime-dsl", label)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	strategy, err := resolved[0].NewStrategy()
	if err != nil {
		t.Fatalf("NewStrategy returned error: %v", err)
	}
	if strategy.Name() != "Runtime DSL" {
		t.Fatalf("strategy name = %q, want Runtime DSL", strategy.Name())
	}
	if resolved[0].Profile.UsesOptions {
		t.Fatalf("expected spot profile, got %+v", resolved[0].Profile)
	}
	if resolved[0].Profile.RegularTrade != strategies.RegularTradeMaterial {
		t.Fatalf("unexpected regular trade mode: %+v", resolved[0].Profile)
	}
	if resolved[0].Runtime.ProfileLabel == "" {
		t.Fatal("expected runtime profile label to be populated")
	}

	dslStrategy, ok := strategy.(*bridge.DslStrategy)
	if !ok {
		t.Fatalf("strategy type = %T, want *bridge.DslStrategy", strategy)
	}
	if len(dslStrategy.ParamSchema()) != 1 {
		t.Fatalf("len(param schema) = %d, want 1", len(dslStrategy.ParamSchema()))
	}
}

func TestResolveRequestedStrategiesDynamicDSLFactoryReturnsFreshInstances(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("Fresh Runtime DSL")
var count = 0
count := count + 1
plot(count, title="Count")`,
	}

	resolved, _, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	first, err := resolved[0].NewStrategy()
	if err != nil {
		t.Fatalf("NewStrategy first returned error: %v", err)
	}
	second, err := resolved[0].NewStrategy()
	if err != nil {
		t.Fatalf("NewStrategy second returned error: %v", err)
	}
	if first == second {
		t.Fatal("NewStrategy returned the same DSL strategy instance twice")
	}
}

func TestResolveRequestedStrategiesAcceptsVariableNameAliasForDSLParam(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("Alias Param")
fast = input.int(5, title="Fast Length", minval=1, maxval=10)
if bar_index == 0 {
  buy(1)
}`,
		DSLParams: map[string]interface{}{"fast": 6.0},
	}

	resolved, _, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
}

func TestResolveRequestedStrategiesRejectsInvalidDSLParamRange(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("Runtime DSL")
length = input.int(5, title="Length", minval=1, maxval=10)
if bar_index == 0 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}`,
		DSLParams: map[string]interface{}{"Length": 11.0},
	}

	_, _, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err == nil {
		t.Fatal("expected error for out-of-range DSL param")
	}
	if !strings.Contains(err.Error(), "must be <= 10") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRequestedStrategiesAppliesDSLProfileOverride(t *testing.T) {
	usesOptions := true
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("Spread Runtime")
chain = options.chain()
if chain != na {
  plot(close, title="Close")
}`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{
			UsesOptions:  &usesOptions,
			RegularTrade: "none",
		},
	}

	resolved, _, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	if !resolved[0].Profile.UsesOptions || resolved[0].Profile.RegularTrade != strategies.RegularTradeNone {
		t.Fatalf("unexpected profile override result: %+v", resolved[0].Profile)
	}
}

func TestResolveRequestedStrategiesInfersOptionsProfileFromAST(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-02-01",
		Capital: 5,
		DSL: `strategy("AST Options")
contracts = options.chain()
if contracts != na {
  plot(close, title="Close")
}`,
	}

	resolved, _, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), "BTC")
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	if !resolved[0].Profile.UsesOptions {
		t.Fatalf("expected options profile, got %+v", resolved[0].Profile)
	}
	if resolved[0].Profile.RegularTrade != strategies.RegularTradeNone {
		t.Fatalf("regular trade = %q, want none", resolved[0].Profile.RegularTrade)
	}
}

func TestValidateStrategyBacktestRunRequestRejectsMixedStrategyAndDSL(t *testing.T) {
	err := validateStrategyBacktestRunRequest(dto.StrategyBacktestRunRequest{
		Asset:    "BTC",
		From:     "2026-01-01",
		To:       "2026-02-01",
		Capital:  5,
		Strategy: "golden-cross",
		DSL:      `strategy("Runtime DSL")`,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStrategyBacktestRunRequestRejectsDSLFieldsWithoutDSL(t *testing.T) {
	err := validateStrategyBacktestRunRequest(dto.StrategyBacktestRunRequest{
		Asset:     "BTC",
		From:      "2026-01-01",
		To:        "2026-02-01",
		Capital:   5,
		DSLParams: map[string]interface{}{"Length": 7.0},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "dsl_params requires dsl") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDescribeResolvedStrategiesUsesStrategyNameForSingleResult(t *testing.T) {
	items := []strategies.ResolvedStrategy{{
		Strategy: &stubNamedStrategy{name: "Runtime DSL"},
	}}
	if got := describeResolvedStrategies(items, "fallback"); got != "Runtime DSL" {
		t.Fatalf("describeResolvedStrategies = %q, want Runtime DSL", got)
	}
}

func TestValidateStrategyBacktestPerformsPreparePreflight(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(cryptoUnderlyingFeed, feed)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}

	resp, err := svc.ValidateStrategyBacktest(context.Background(), dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 5,
		DSL: `strategy("Runtime DSL")
length = input.int(5, title="Length", minval=1, maxval=10)
plot(ta.sma(close, length), title="SMA")`,
		DSLParams: map[string]interface{}{"Length": 6.0},
	})
	if err != nil {
		t.Fatalf("ValidateStrategyBacktest returned error: %v", err)
	}
	if resp.StrategyCount != 1 || len(resp.Strategies) != 1 {
		t.Fatalf("unexpected validation response: %+v", resp)
	}
	if len(resp.Strategies[0].DSLParams) != 1 || resp.Strategies[0].DSLParams[0].Title != "Length" {
		t.Fatalf("unexpected DSL params: %+v", resp.Strategies[0].DSLParams)
	}
	if resp.Strategies[0].ProfileSource != "inferred" {
		t.Fatalf("profile source = %q, want inferred", resp.Strategies[0].ProfileSource)
	}
	if len(resp.Strategies[0].Warnings) != 1 {
		t.Fatalf("expected one warning, got %+v", resp.Strategies[0].Warnings)
	}
	if resp.Strategies[0].Runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if resp.Strategies[0].Runtime.CapitalUnit != "USD" {
		t.Fatalf("capital unit = %q, want USD", resp.Strategies[0].Runtime.CapitalUnit)
	}
	if resp.Strategies[0].Runtime.OptionsChainRequired {
		t.Fatalf("expected spot DSL to skip options chain, got %+v", resp.Strategies[0].Runtime)
	}
	if feed.loads == 0 {
		t.Fatal("expected prepare preflight to load market data")
	}
}

func TestStartStrategyBacktestSkipsSubmissionPreflightDataLoad(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(cryptoUnderlyingFeed, feed)
		return engine
	}

	accepted, err := svc.StartStrategyBacktest(context.Background(), dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 5,
		DSL: `strategy("Runtime DSL")
length = input.int(5, title="Length", minval=1, maxval=10)
plot(ta.sma(close, length), title="SMA")`,
		DSLParams: map[string]interface{}{"Length": 6.0},
	})
	if err != nil {
		t.Fatalf("StartStrategyBacktest returned error: %v", err)
	}
	if accepted.RunID == "" {
		t.Fatal("expected run id")
	}
	if feed.loads != 0 {
		t.Fatalf("StartStrategyBacktest should accept without preflight data loads, got %d", feed.loads)
	}
}

func TestExampleWheelPortfolioRunPayloadIsValid(t *testing.T) {
	dslPath := filepath.Join("..", "..", "docs", "examples", "wheel-portfolio-us-sell-put.dsl")
	payloadPath := filepath.Join("..", "..", "docs", "examples", "wheel-portfolio-us-sell-put.run.json")

	dslSrc, err := os.ReadFile(dslPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", dslPath, err)
	}
	payloadBytes, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", payloadPath, err)
	}

	var req dto.StrategyBacktestRunRequest
	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", payloadPath, err)
	}
	if strings.TrimSpace(req.DSL) != strings.TrimSpace(string(dslSrc)) {
		t.Fatalf("payload DSL does not match example source")
	}
	if len(req.Symbols) != 6 || len(req.Weights) != 6 {
		t.Fatalf("unexpected symbols/weights lengths: %d/%d", len(req.Symbols), len(req.Weights))
	}
	if req.Symbols[0] != "QQQ" || req.Weights[0] != 0.2 {
		t.Fatalf("unexpected first portfolio leg: %q / %v", req.Symbols[0], req.Weights[0])
	}

	resolved, label, err := resolveRequestedStrategies(req, strategies.DefaultConfig(), resolvePrimaryBacktestAsset(req))
	if err != nil {
		t.Fatalf("resolveRequestedStrategies returned error: %v", err)
	}
	if label != "weighted-wheel-put-writer" {
		t.Fatalf("label = %q, want weighted-wheel-put-writer", label)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if !resolved[0].Profile.UsesOptions {
		t.Fatalf("expected options profile, got %+v", resolved[0].Profile)
	}
	if resolved[0].Profile.RegularTrade != strategies.RegularTradeNone {
		t.Fatalf("regular trade = %q, want none", resolved[0].Profile.RegularTrade)
	}
	config := backtestDSLConfigMap(strategies.DefaultConfig(), req)
	if got := config["portfolio_symbols"]; got != "QQQ,GLD,MSFT,AAPL,TSLA,TQQQ" {
		t.Fatalf("portfolio_symbols = %v, want QQQ,GLD,MSFT,AAPL,TSLA,TQQQ", got)
	}
	if got := config["portfolio_weights"]; got != "0.2,0.1,0.15,0.1,0.3,0.15" {
		t.Fatalf("portfolio_weights = %v, want 0.2,0.1,0.15,0.1,0.3,0.15", got)
	}
}

func TestValidateStrategyBacktestMarksExplicitDSLProfile(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(cryptoUnderlyingFeed, feed)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}
	svc.chainLoader = func(context.Context, string, string, string, time.Time, time.Time) (backtest.OptionsChainProvider, error) {
		return &stubOptionsChainProvider{}, nil
	}
	usesOptions := true

	resp, err := svc.ValidateStrategyBacktest(context.Background(), dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 5,
		DSL: `strategy("Runtime DSL")
plot(close, title="Close")`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{
			UsesOptions:  &usesOptions,
			RegularTrade: "none",
		},
	})
	if err != nil {
		t.Fatalf("ValidateStrategyBacktest returned error: %v", err)
	}
	if resp.Strategies[0].ProfileSource != "explicit" {
		t.Fatalf("profile source = %q, want explicit", resp.Strategies[0].ProfileSource)
	}
	if len(resp.Strategies[0].Warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", resp.Strategies[0].Warnings)
	}
	if resp.Strategies[0].Runtime == nil || !resp.Strategies[0].Runtime.OptionsChainRequired {
		t.Fatalf("expected runtime to require options chain, got %+v", resp.Strategies[0].Runtime)
	}
	if resp.Strategies[0].Runtime.CapitalUnit != "BTC" {
		t.Fatalf("capital unit = %q, want BTC", resp.Strategies[0].Runtime.CapitalUnit)
	}
}

func TestResolveBacktestPlanLoadsMultipleOptionChainTargets(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}
	loaded := make([]string, 0, 4)
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loaded = append(loaded, marketName+":"+asset)
		return &stubOptionsChainProvider{}, nil
	}

	req := dto.StrategyBacktestRunRequest{
		Market:  "us",
		Asset:   "QQQ",
		Symbols: []string{"MSFT", "AAPL"},
		Weights: []float64{0.2, 0.1},
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 100000,
		DSL: `strategy("Multi Chain")
qqq = options.chain("us", "QQQ")
msft = options.chain("us", "MSFT")
plot(close, title="Close")`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{UsesOptions: ptrBool(true), RegularTrade: "none"},
	}

	_, err := svc.resolveBacktestPlan(context.Background(), nil, req, false)
	if err != nil {
		t.Fatalf("validation resolveBacktestPlan returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("validation plan should not load option chains, got %v", loaded)
	}

	_, err = svc.resolveBacktestPlan(context.Background(), nil, req, true)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected three option chain loads, got %v", loaded)
	}
	want := map[string]bool{"us:QQQ": true, "us:MSFT": true, "us:AAPL": true}
	for _, got := range loaded {
		if !want[got] {
			t.Fatalf("unexpected loaded target %q from %v", got, loaded)
		}
		delete(want, got)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected targets: %+v", want)
	}
}

func TestResolveBacktestPlanRejectsDynamicOptionChainWithoutScope(t *testing.T) {
	svc := NewPortfolioBacktestService(nil, nil)
	_, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:  "us",
		Asset:   "QQQ",
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 100000,
		DSL: `strategy("Dynamic Chain")
symbol = config.string("target_symbol", "MSFT")
chain = options.chain("us", symbol)
plot(close, title="Close")`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{UsesOptions: ptrBool(true), RegularTrade: "none"},
	}, false)
	if err == nil {
		t.Fatal("expected dynamic option chain without symbols/portfolio to fail validation")
	}
	if !strings.Contains(err.Error(), "dynamic options.chain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveBacktestPlanAllowsDynamicOptionChainWithSymbolsScope(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}
	loaded := make([]string, 0, 2)
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loaded = append(loaded, marketName+":"+asset)
		return &stubOptionsChainProvider{}, nil
	}
	_, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:  "us",
		Asset:   "QQQ",
		Symbols: []string{"MSFT"},
		Weights: []float64{1},
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 100000,
		DSL: `strategy("Dynamic Chain")
for symbol in portfolio.symbols() {
  chain = options.chain("us", symbol)
}
plot(close, title="Close")`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{UsesOptions: ptrBool(true), RegularTrade: "none"},
	}, true)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected primary and scoped symbol chain loads, got %v", loaded)
	}
}

func ptrBool(v bool) *bool { return &v }

type stubNamedStrategy struct{ name string }

func (s *stubNamedStrategy) Name() string { return s.name }

func (s *stubNamedStrategy) Init(*backtest.SetupContext) error { return nil }

func (s *stubNamedStrategy) OnBar(*backtest.BarContext) {}

type validationTestFeed struct {
	loads int
}

func (f *validationTestFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *validationTestFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	f.loads++
	ts := []time.Time{req.From, req.From.Add(time.Hour), req.From.Add(2 * time.Hour), req.From.Add(3 * time.Hour), req.From.Add(4 * time.Hour), req.From.Add(5 * time.Hour)}
	ds := backtest.NewDataSet(len(ts))
	ds.SetTimestamps(ts)
	ds.AddColumn("open", []float64{100, 101, 102, 103, 104, 105})
	ds.AddColumn("high", []float64{101, 102, 103, 104, 105, 106})
	ds.AddColumn("low", []float64{99, 100, 101, 102, 103, 104})
	ds.AddColumn("close", []float64{100.5, 101.5, 102.5, 103.5, 104.5, 105.5})
	ds.AddColumn("volume", []float64{10, 11, 12, 13, 14, 15})
	return ds, nil
}
