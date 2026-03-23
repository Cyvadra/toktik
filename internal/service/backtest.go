package service

import (
	"context"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

const (
	defaultBacktestCapital         = 1.0
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

	strategy, err := strategies.Build(req.Strategy, req.Params)
	if err != nil {
		return nil, err
	}

	engine := backtest.NewEngine(backtest.Config{
		InitialCapital:  floatDefault(req.Capital, defaultBacktestCapital),
		AccountUnit:     resolveBacktestAccountUnit(req.Symbol),
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
		return backtest.CommissionPercent, dto.NewValidationError("unsupported commission_model %q", value)
	}
}

func parseExecutionMode(value string) (backtest.ExecutionPriceModel, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "bidask":
		return backtest.ExecutionPriceBidAsk, nil
	case "canonical":
		return backtest.ExecutionPriceCanonical, nil
	default:
		return backtest.ExecutionPriceBidAsk, dto.NewValidationError("unsupported fill_mode %q", value)
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
		return backtest.ValuationPriceExit, dto.NewValidationError("unsupported valuation_mode %q", value)
	}
}

func parseTriggerMode(value string) (backtest.TriggerPriceMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "canonical":
		return backtest.TriggerPriceCanonical, nil
	case "bidask-envelope":
		return backtest.TriggerPriceBidAskEnvelope, nil
	default:
		return backtest.TriggerPriceCanonical, dto.NewValidationError("unsupported trigger_mode %q", value)
	}
}

func validateBacktestRequest(req dto.BacktestRequest) error {
	if req.Capital != nil && *req.Capital <= 0 {
		return dto.NewValidationError("capital must be > 0")
	}
	if req.CommissionValue != nil && *req.CommissionValue < 0 {
		return dto.NewValidationError("commission_value must be >= 0")
	}
	if req.SlippagePct != nil && *req.SlippagePct < 0 {
		return dto.NewValidationError("slippage_pct must be >= 0")
	}
	return nil
}

func floatDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func resolveBacktestAccountUnit(symbol string) string {
	base := strings.TrimSpace(cryptooptions.ExtractBaseAsset(symbol))
	if base == "" {
		return ""
	}
	return strings.ToUpper(base)
}
