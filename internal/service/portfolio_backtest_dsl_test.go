package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	_ "github.com/Cyvadra/toktik/pkg/dsl/catalog"
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
	t.Cleanup(func() { _ = svc.Close() })
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

func TestValidateStrategyBacktestMapsMissingIndicatorSeriesToValidationError(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}

	_, err := svc.ValidateStrategyBacktest(context.Background(), dto.StrategyBacktestRunRequest{
		Market:   "us",
		Asset:    "AAPL",
		Interval: "1d",
		From:     "2026-06-01",
		To:       "2026-06-22",
		Capital:  100000,
		Strategy: "delta-filter",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr *dto.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected dto.ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "delta_ok") || !strings.Contains(err.Error(), "delta") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartStrategyBacktestSkipsSubmissionPreflightDataLoad(t *testing.T) {
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	t.Cleanup(func() { _ = svc.Close() })
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

func TestRunBacktestWritesReportsUnderConfiguredRoot(t *testing.T) {
	reportsRoot := t.TempDir()
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil).WithReportsRoot(reportsRoot)
	t.Cleanup(func() { _ = svc.Close() })
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(cryptoUnderlyingFeed, feed)
		return engine
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	run := &portfolioBacktestRun{id: "run-custom-root", startedAt: &now}

	result, err := svc.runBacktest(context.Background(), run, dto.StrategyBacktestRunRequest{
		Asset:   "BTC",
		From:    "2026-01-01",
		To:      "2026-01-02",
		Capital: 5,
		DSL: `strategy("Report Root DSL")
plot(close, title="Close")`,
	})
	if err != nil {
		t.Fatalf("runBacktest returned error: %v", err)
	}
	if len(result.Summaries) != 1 {
		t.Fatalf("len(result.Summaries) = %d, want 1", len(result.Summaries))
	}
	reportPath := result.Summaries[0].HTMLPath
	if !strings.HasPrefix(reportPath, reportsRoot+string(os.PathSeparator)) {
		t.Fatalf("HTMLPath = %q, want under %q", reportPath, reportsRoot)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file at %q: %v", reportPath, err)
	}
}

func TestExampleWheelPortfolioRunPayloadIsValid(t *testing.T) {
	dslPath := filepath.Join("..", "..", "pkg", "dsl", "scripts", "strategies", "wheel-portfolio-us-sell-put.toktik")
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

func TestCancelStrategyBacktestBeforeRunStarts(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	canceled := false
	run := &portfolioBacktestRun{
		id:          "run-cancel-queued",
		cancel:      func() { canceled = true },
		request:     dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-01-02", Capital: 5},
		status:      backtestStatusQueued,
		createdAt:   now,
		updatedAt:   now,
		subscribers: make(map[chan dto.StrategyBacktestSSEvent]struct{}),
		dirty:       true,
	}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.now = func() time.Time { return now }
	svc.runs[run.id] = run

	status, err := svc.CancelStrategyBacktest(context.Background(), run.id)
	if err != nil {
		t.Fatalf("CancelStrategyBacktest returned error: %v", err)
	}
	if !canceled {
		t.Fatalf("expected run cancel func to be called")
	}
	if status.Status != backtestStatusCanceled {
		t.Fatalf("status = %q, want %q", status.Status, backtestStatusCanceled)
	}
	if !run.finished {
		t.Fatalf("expected run to be terminal after cancellation")
	}
	if run.markRunning(now.Add(time.Second)) {
		t.Fatalf("canceled run should not transition to running")
	}
}

func TestCancelStrategyBacktestPropagatesToExecutionContext(t *testing.T) {
	feed := &cancelAwareFeed{started: make(chan struct{}), release: make(chan struct{})}
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
		DSL: `strategy("cancelable")
plot(close, title="close")`,
	})
	if err != nil {
		t.Fatalf("StartStrategyBacktest returned error: %v", err)
	}

	<-feed.started
	status, err := svc.CancelStrategyBacktest(context.Background(), accepted.RunID)
	if err != nil {
		t.Fatalf("CancelStrategyBacktest returned error: %v", err)
	}
	close(feed.release)
	if status.Status != backtestStatusCanceled {
		t.Fatalf("status = %q, want %q", status.Status, backtestStatusCanceled)
	}
	select {
	case <-feed.canceled:
	case <-time.After(time.Second):
		t.Fatalf("feed did not observe context cancellation")
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
symbol = close > open ? "MSFT" : "AAPL"
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

func TestResolveBacktestPlanRejectsDynamicDSLDataRequests(t *testing.T) {
	svc := NewPortfolioBacktestService(nil, nil)
	_, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:   "us-stocks",
		Asset:    "SPY",
		From:     "2026-01-01",
		To:       "2026-01-02",
		Capital:  100000,
		Interval: "1d",
		DSL: `strategy("Dynamic Data Request")
symbol = close > open ? "AAPL" : "MSFT"
external_close = request.security("us-stocks", symbol, "1d", "close")
plot(external_close, title="External Close")`,
	}, false)
	if err == nil || !strings.Contains(err.Error(), "request.security") {
		t.Fatalf("expected runtime-dynamic request.security validation error, got %v", err)
	}
}

func TestResolveBacktestPlanUsesUniverseSymbolsForDynamicOptionChains(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	feed := &validationTestFeed{}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.universes = &stubPortfolioUniverseResolver{members: map[string][]dto.UniverseMember{
		"strong_momentum": {
			{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: from, ValidTo: to},
			{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "NVDA", ValidFrom: from, ValidTo: to},
		},
	}}
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		engine := backtest.NewEngine(cfg)
		engine.RegisterDataFeed(usUnderlyingFeed, feed)
		return engine
	}
	loaded := make([]string, 0, 3)
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loaded = append(loaded, marketName+":"+asset)
		return &stubOptionsChainProvider{}, nil
	}

	plan, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:   "us-stocks",
		From:     "2026-01-01",
		To:       "2026-01-02",
		Capital:  100000,
		Interval: "1d",
		DSL: `strategy("Universe Chain")
symbols = universe.symbols("strong_momentum")
for symbol in symbols {
  chain = options.chain("us", symbol)
}
plot(len(symbols), title="Universe Size")`,
		DSLProfile: &dto.StrategyBacktestDSLProfile{UsesOptions: ptrBool(true), RegularTrade: "none"},
	}, true)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected universe chain loads, got %v", loaded)
	}
	want := map[string]bool{"us:AAPL": true, "us:NVDA": true}
	for _, got := range loaded {
		if !want[got] {
			t.Fatalf("unexpected loaded target %q from %v", got, loaded)
		}
		delete(want, got)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected targets: %+v", want)
	}
	resourcePlan := buildStrategyBacktestResourcePlan(plan)
	if resourcePlan.UniverseSize != 2 || resourcePlan.OptionChainUnderlyings != 2 {
		t.Fatalf("resource plan universe/underlyings = %d/%d, want 2/2", resourcePlan.UniverseSize, resourcePlan.OptionChainUnderlyings)
	}
}

func TestLoadOptionChainUniverseFailsForExplicitTargetWithoutUnderlyingData(t *testing.T) {
	cause := fmt.Errorf("load option precompute timestamps: %w", errOptionPrecomputeNoData)
	_, err := optionPrecomputeNoDataPolicy(optionChainTarget{market: marketUS, asset: "SNDK", required: true}, "1d", time.Time{}, time.Time{}, cause)
	if !errors.Is(err, errOptionPrecomputeNoData) {
		t.Fatalf("expected explicit no-data target to fail, got %v", err)
	}
}

func TestLoadOptionChainUniverseWarnsForUniverseTargetWithoutUnderlyingData(t *testing.T) {
	warning, err := optionPrecomputeNoDataPolicy(optionChainTarget{market: marketUS, asset: "SNDK"}, "1d", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), errOptionPrecomputeNoData)
	if err != nil {
		t.Fatalf("expected optional no-data target to continue, got %v", err)
	}
	if warning.Code != "options.underlying_data_omitted" || warning.Symbol != "SNDK" || warning.Details["market"] != marketUS {
		t.Fatalf("warning = %+v", warning)
	}
}

func TestCollectOptionChainTargetsPromotesExplicitUniverseDuplicate(t *testing.T) {
	req := dto.StrategyBacktestRunRequest{Symbols: []string{"AAPL"}}
	targets, _, err := collectOptionChainTargets(req, marketUS, "AAPL", []string{"AAPL", "NVDA"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || !targets[0].required || targets[1].required {
		t.Fatalf("targets = %+v, want required AAPL and optional NVDA", targets)
	}
}

func TestResolveBacktestPlanUsesUniverseSymbolsForCatalogDSL(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	svc := NewPortfolioBacktestService(nil, nil)
	svc.universes = &stubPortfolioUniverseResolver{members: map[string][]dto.UniverseMember{
		"strong_momentum": {
			{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: from, ValidTo: to},
			{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "NVDA", ValidFrom: from, ValidTo: to},
		},
	}}
	loaded := make([]string, 0, 3)
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loaded = append(loaded, marketName+":"+asset)
		return &stubOptionsChainProvider{}, nil
	}

	plan, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:   "us-stocks",
		Asset:    "SPY",
		From:     "2026-01-01",
		To:       "2026-01-02",
		Capital:  100000,
		Interval: "1d",
		Strategy: "strong-momentum-dsl",
	}, true)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected primary and universe chain loads, got %v", loaded)
	}
	want := map[string]bool{"us:SPY": true, "us:AAPL": true, "us:NVDA": true}
	for _, got := range loaded {
		if !want[got] {
			t.Fatalf("unexpected loaded target %q from %v", got, loaded)
		}
		delete(want, got)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected targets: %+v", want)
	}
	resourcePlan := buildStrategyBacktestResourcePlan(plan)
	if resourcePlan.UniverseSize != 2 || resourcePlan.OptionChainUnderlyings != 3 {
		t.Fatalf("resource plan universe/underlyings = %d/%d, want 2/3", resourcePlan.UniverseSize, resourcePlan.OptionChainUnderlyings)
	}
}

func TestResolveBacktestPlanAllowsUniverseOnlyDSL(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	svc := NewPortfolioBacktestService(nil, nil)
	svc.universes = &stubPortfolioUniverseResolver{members: map[string][]dto.UniverseMember{
		"strong_momentum": {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "AAPL", ValidFrom: from, ValidTo: to}},
	}}

	plan, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:           "us-stocks",
		From:             "2026-01-01",
		To:               "2026-01-02",
		Capital:          100000,
		Interval:         "1d",
		MinExpiryDays:    14,
		TargetExpiryDays: 45,
		DSL: `strategy("Universe Only")
symbols = universe.symbols("strong_momentum")
plot(len(symbols), title="Universe Size")`,
	}, false)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if plan.asset != "AAPL" {
		t.Fatalf("asset = %q, want AAPL", plan.asset)
	}
	if len(plan.universeSymbols) != 1 || plan.universeSymbols[0] != "AAPL" {
		t.Fatalf("universe symbols = %v, want AAPL", plan.universeSymbols)
	}
	if len(plan.portfolioSymbols) != 1 || plan.portfolioSymbols[0] != "AAPL" {
		t.Fatalf("portfolio symbols = %v, want primary universe asset only", plan.portfolioSymbols)
	}
	resourcePlan := buildStrategyBacktestResourcePlan(plan)
	if resourcePlan.MinDTE != 14 || resourcePlan.TargetDTE != 45 {
		t.Fatalf("resource DTE = %d/%d, want 14/45", resourcePlan.MinDTE, resourcePlan.TargetDTE)
	}
}

func TestResolveBacktestPlanUsesParameterizedUniverseSnapshotOnce(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	resolver := &stubPortfolioUniverseResolver{members: map[string][]dto.UniverseMember{
		"strong_momentum": {{UniverseCode: "strong_momentum", Market: "us-stocks", Symbol: "NVDA", ValidFrom: from, ValidTo: to}},
		"value_allocation": {
			{UniverseCode: "value_allocation", Market: "us-stocks", Symbol: "AAPL", ValidFrom: from, ValidTo: to},
			{UniverseCode: "value_allocation", Market: "us-stocks", Symbol: "MSFT", ValidFrom: from, ValidTo: to},
		},
	}}
	svc := NewPortfolioBacktestService(nil, nil)
	svc.universes = resolver

	plan, err := svc.resolveBacktestPlan(context.Background(), nil, dto.StrategyBacktestRunRequest{
		Market:    "us-stocks",
		Asset:     "SPY",
		From:      "2026-01-01",
		To:        "2026-01-02",
		Capital:   100000,
		Interval:  "1d",
		DSLParams: map[string]interface{}{"Universe": "value_allocation"},
		DSL: `strategy("Parameterized Universe")
code = input.string("strong_momentum", title="Universe")
symbols = universe.symbols(code)
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
}
plot(close, title="Close")`,
	}, false)
	if err != nil {
		t.Fatalf("resolveBacktestPlan returned error: %v", err)
	}
	if strings.Join(plan.universeCodes, ",") != "value_allocation" {
		t.Fatalf("universe codes = %v, want value_allocation", plan.universeCodes)
	}
	if resolver.calls["value_allocation"] != 1 || resolver.calls["strong_momentum"] != 0 {
		t.Fatalf("universe query calls = %v, want only value_allocation once", resolver.calls)
	}
	resourcePlan := buildStrategyBacktestResourcePlan(plan)
	if resourcePlan.StaticDataRequests != 2 || resourcePlan.RuntimeDynamicRequests != 0 {
		t.Fatalf("data requests = static:%d dynamic:%d, want static:2 dynamic:0", resourcePlan.StaticDataRequests, resourcePlan.RuntimeDynamicRequests)
	}
}

func TestUniverseIntervalProviderSymbolsAtUsesValidInterval(t *testing.T) {
	provider := &UniverseIntervalProvider{members: map[string][]dto.UniverseMember{
		"strong_momentum": {
			{Symbol: "AAPL", ValidFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
			{Symbol: "NVDA", ValidFrom: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		},
	}}

	jan := provider.SymbolsAt("strong_momentum", time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))
	if strings.Join(jan, ",") != "AAPL" {
		t.Fatalf("January symbols = %v, want AAPL", jan)
	}
	feb := provider.SymbolsAt("strong_momentum", time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC))
	if strings.Join(feb, ",") != "NVDA" {
		t.Fatalf("February symbols = %v, want NVDA", feb)
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

type stubPortfolioUniverseResolver struct {
	members map[string][]dto.UniverseMember
	calls   map[string]int
}

func (s *stubPortfolioUniverseResolver) MemberIntervals(_ context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error) {
	if s.calls == nil {
		s.calls = make(map[string]int)
	}
	s.calls[req.Code]++
	return &dto.UniverseMembersResponse{Market: req.Market, Code: req.Code, From: req.From, To: req.To, Data: append([]dto.UniverseMember(nil), s.members[req.Code]...)}, nil
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

type cancelAwareFeed struct {
	started  chan struct{}
	release  chan struct{}
	canceled chan struct{}
}

func (f *cancelAwareFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *cancelAwareFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	if f.canceled == nil {
		f.canceled = make(chan struct{})
	}
	close(f.started)
	select {
	case <-ctx.Done():
		close(f.canceled)
		return nil, ctx.Err()
	case <-f.release:
		return nil, errors.New("released before cancellation")
	}
}
