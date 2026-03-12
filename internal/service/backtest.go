package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	defaultBacktestCapital         = 100000.0
	defaultBacktestStrategy        = "golden-cross"
	defaultBacktestCommissionValue = 0.001
	defaultBacktestSlippagePct     = 0.0005
	defaultBacktestFillMode        = "bidask"
	defaultBacktestValuationMode   = "exit"
	defaultBacktestTriggerMode     = "canonical"
	defaultEntryTWAPBars           = 1
	defaultFastPeriod              = 10
	defaultSlowPeriod              = 50
)

// RunBacktest executes a configured backtest and returns the engine result.
func (s *CryptoOptionsService) RunBacktest(ctx context.Context, req dto.BacktestRequest) (*backtest.Result, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	if err := validateBacktestRequest(req); err != nil {
		return nil, err
	}

	commissionModel, err := parseCommissionModel(req.CommissionModel)
	if err != nil {
		return nil, err
	}
	executionMode, err := parseExecutionMode(req.FillMode)
	if err != nil {
		return nil, err
	}
	valuationMode, err := parseValuationMode(req.ValuationMode)
	if err != nil {
		return nil, err
	}
	triggerMode, err := parseTriggerMode(req.TriggerMode)
	if err != nil {
		return nil, err
	}

	strategy, err := buildStrategy(req)
	if err != nil {
		return nil, err
	}

	engine := backtest.NewEngine(backtest.Config{
		InitialCapital:  floatDefault(req.Capital, defaultBacktestCapital),
		CommissionModel: commissionModel,
		CommissionValue: floatDefault(req.CommissionValue, defaultBacktestCommissionValue),
		SlippagePct:     floatDefault(req.SlippagePct, defaultBacktestSlippagePct),
		ExecutionMode:   executionMode,
		ValuationMode:   valuationMode,
		TriggerMode:     triggerMode,
	})

	engine.RegisterDataFeed("crypto-options", datafeed.NewCryptoOptionsDataFeed(s.conn))

	return engine.Run(ctx, "crypto-options", req.Symbol, req.Interval, fromT, toT, strategy, nil)
}

type goldenCrossStrategy struct {
	fastPeriod int
	slowPeriod int
	entryTWAP  int
}

func (s *goldenCrossStrategy) Name() string { return "GoldenCross" }

func (s *goldenCrossStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("fast_period", s.fastPeriod)
	ctx.SetParam("slow_period", s.slowPeriod)

	ctx.Register("sma_fast", backtest.SMA("close", s.fastPeriod))
	ctx.Register("sma_slow", backtest.SMA("close", s.slowPeriod))
	ctx.Register("buy_signal", backtest.Crossover("sma_fast", "sma_slow"))
	ctx.Register("sell_signal", backtest.Crossunder("sma_fast", "sma_slow"))
	return nil
}

func (s *goldenCrossStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	if ctx.Ind("buy_signal") == 1 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		if price > 0 {
			qty := (ctx.Equity() * 0.95) / price
			if s.entryTWAP > 1 {
				ctx.BuyTWAP(primary, qty, s.entryTWAP)
			} else {
				ctx.Buy(primary, qty)
			}
		}
	}

	if ctx.Ind("sell_signal") == 1 && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}

type deltaFilterStrategy struct {
	entryTWAP int
}

func (s *deltaFilterStrategy) Name() string { return "DeltaFilter" }

func (s *deltaFilterStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.Register("ema20", backtest.EMA("close", 20))
	ctx.Register("rsi14", backtest.RSI("close", 14))
	ctx.Register("delta_ok", backtest.Custom(
		[]string{"delta"},
		func(inputs map[string][]float64) []float64 {
			deltaSeries := inputs["delta"]
			out := make([]float64, len(deltaSeries))
			for i, value := range deltaSeries {
				if value > 0.3 && value < 0.7 {
					out[i] = 1
				}
			}
			return out
		},
	))
	return nil
}

func (s *deltaFilterStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	deltaOK := ctx.Ind("delta_ok")
	rsi := ctx.Ind("rsi14")

	if deltaOK == 1 && rsi < 30 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		if price > 0 {
			qty := (ctx.Equity() * 0.5) / price
			if s.entryTWAP > 1 {
				ctx.BuyTWAP(primary, qty, s.entryTWAP)
			} else {
				ctx.Buy(primary, qty)
			}
		}
	}

	if (deltaOK == 0 || rsi > 70) && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}

func buildStrategy(req dto.BacktestRequest) (backtest.Strategy, error) {
	strategyName := strings.ToLower(strings.TrimSpace(req.Strategy))
	if strategyName == "" {
		strategyName = defaultBacktestStrategy
	}

	entryTWAPBars := intDefault(req.EntryTWAPBars, defaultEntryTWAPBars)

	switch strategyName {
	case "golden-cross":
		return &goldenCrossStrategy{
			fastPeriod: intDefault(req.FastPeriod, defaultFastPeriod),
			slowPeriod: intDefault(req.SlowPeriod, defaultSlowPeriod),
			entryTWAP:  entryTWAPBars,
		}, nil
	case "delta-filter":
		return &deltaFilterStrategy{entryTWAP: entryTWAPBars}, nil
	default:
		return nil, fmt.Errorf("unsupported strategy %q", req.Strategy)
	}
}

func parseCommissionModel(value string) (backtest.CommissionModel, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "percent":
		return backtest.CommissionPercent, nil
	case "none":
		return backtest.CommissionNone, nil
	case "flat":
		return backtest.CommissionFlat, nil
	case "per-unit":
		return backtest.CommissionPerUnit, nil
	default:
		return backtest.CommissionPercent, fmt.Errorf("unsupported commission_model %q", value)
	}
}

func parseExecutionMode(value string) (backtest.ExecutionPriceModel, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "bidask":
		return backtest.ExecutionPriceBidAsk, nil
	case "canonical":
		return backtest.ExecutionPriceCanonical, nil
	default:
		return backtest.ExecutionPriceBidAsk, fmt.Errorf("unsupported fill_mode %q", value)
	}
}

func parseValuationMode(value string) (backtest.ValuationPriceModel, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "exit":
		return backtest.ValuationPriceExit, nil
	case "close":
		return backtest.ValuationPriceClose, nil
	case "mid":
		return backtest.ValuationPriceMid, nil
	default:
		return backtest.ValuationPriceExit, fmt.Errorf("unsupported valuation_mode %q", value)
	}
}

func parseTriggerMode(value string) (backtest.TriggerPriceMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "canonical":
		return backtest.TriggerPriceCanonical, nil
	case "bidask-envelope":
		return backtest.TriggerPriceBidAskEnvelope, nil
	default:
		return backtest.TriggerPriceCanonical, fmt.Errorf("unsupported trigger_mode %q", value)
	}
}

func validateBacktestRequest(req dto.BacktestRequest) error {
	if req.Capital != nil && *req.Capital <= 0 {
		return fmt.Errorf("capital must be > 0")
	}
	if req.CommissionValue != nil && *req.CommissionValue < 0 {
		return fmt.Errorf("commission_value must be >= 0")
	}
	if req.SlippagePct != nil && *req.SlippagePct < 0 {
		return fmt.Errorf("slippage_pct must be >= 0")
	}
	if req.EntryTWAPBars != nil && *req.EntryTWAPBars <= 0 {
		return fmt.Errorf("entry_twap_bars must be >= 1")
	}
	if req.FastPeriod != nil && *req.FastPeriod <= 0 {
		return fmt.Errorf("fast_period must be >= 1")
	}
	if req.SlowPeriod != nil && *req.SlowPeriod <= 0 {
		return fmt.Errorf("slow_period must be >= 1")
	}
	if req.FastPeriod != nil && req.SlowPeriod != nil && *req.FastPeriod >= *req.SlowPeriod {
		return fmt.Errorf("fast_period must be < slow_period")
	}
	return nil
}

func floatDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func intDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
