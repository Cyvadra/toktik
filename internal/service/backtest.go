package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/strategies"
)

const (
	defaultBacktestCapital         = 100000.0
	defaultBacktestCommissionValue = 0.001
	defaultBacktestSlippagePct     = 0.0005
	DefaultBacktestFillMode        = "bidask"
	DefaultBacktestValuationMode   = "exit"
	DefaultBacktestTriggerMode     = "canonical"
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

	strategy, err := strategies.Build(req)
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
	if req.PositionSize != nil && *req.PositionSize <= 0 {
		return fmt.Errorf("position_size must be > 0")
	}
	if req.MaxHoldHours != nil && *req.MaxHoldHours <= 0 {
		return fmt.Errorf("max_hold_hours must be > 0")
	}
	if req.TargetExpiryDays != nil && *req.TargetExpiryDays <= 0 {
		return fmt.Errorf("target_expiry_days must be >= 1")
	}
	if req.MinExpiryDays != nil && *req.MinExpiryDays <= 0 {
		return fmt.Errorf("min_expiry_days must be >= 1")
	}
	if req.TargetExpiryDays != nil && req.MinExpiryDays != nil && *req.TargetExpiryDays < *req.MinExpiryDays {
		return fmt.Errorf("target_expiry_days must be >= min_expiry_days")
	}
	if req.MinPremium != nil && *req.MinPremium < 0 {
		return fmt.Errorf("min_premium must be >= 0")
	}
	if req.ShortDeltaMin != nil && *req.ShortDeltaMin < 0 {
		return fmt.Errorf("short_delta_min must be >= 0")
	}
	if req.ShortDeltaMax != nil && *req.ShortDeltaMax < 0 {
		return fmt.Errorf("short_delta_max must be >= 0")
	}
	if req.LongDeltaMin != nil && *req.LongDeltaMin < 0 {
		return fmt.Errorf("long_delta_min must be >= 0")
	}
	if req.LongDeltaMax != nil && *req.LongDeltaMax < 0 {
		return fmt.Errorf("long_delta_max must be >= 0")
	}
	if req.ShortDeltaMin != nil && req.ShortDeltaMax != nil && *req.ShortDeltaMin > *req.ShortDeltaMax {
		return fmt.Errorf("short_delta_min must be <= short_delta_max")
	}
	if req.LongDeltaMin != nil && req.LongDeltaMax != nil && *req.LongDeltaMin > *req.LongDeltaMax {
		return fmt.Errorf("long_delta_min must be <= long_delta_max")
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
