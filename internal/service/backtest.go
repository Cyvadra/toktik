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

	commissionModel, err := parseEnum(req.CommissionModel, commissionModelMap, backtest.CommissionPercent, "commission_model")
	if err != nil {
		return nil, err
	}
	executionMode, err := parseEnum(req.FillMode, executionModeMap, backtest.ExecutionPriceBidAsk, "fill_mode")
	if err != nil {
		return nil, err
	}
	valuationMode, err := parseEnum(req.ValuationMode, valuationModeMap, backtest.ValuationPriceExit, "valuation_mode")
	if err != nil {
		return nil, err
	}
	triggerMode, err := parseEnum(req.TriggerMode, triggerModeMap, backtest.TriggerPriceCanonical, "trigger_mode")
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

// parseEnum normalises value and looks it up in mappings. An empty input
// returns defaultVal. Unknown values produce a ValidationError that names
// the field for the caller.
func parseEnum[T any](value string, mappings map[string]T, defaultVal T, fieldName string) (T, error) {
	key := strings.ToLower(strings.TrimSpace(value))
	if key == "" {
		return defaultVal, nil
	}
	if v, ok := mappings[key]; ok {
		return v, nil
	}
	return defaultVal, dto.NewValidationError("unsupported %s %q", fieldName, value)
}

var commissionModelMap = map[string]backtest.CommissionModel{
	"percent":  backtest.CommissionPercent,
	"none":     backtest.CommissionNone,
	"flat":     backtest.CommissionFlat,
	"per-unit": backtest.CommissionPerUnit,
}

var executionModeMap = map[string]backtest.ExecutionPriceModel{
	"bidask":    backtest.ExecutionPriceBidAsk,
	"canonical": backtest.ExecutionPriceCanonical,
}

var valuationModeMap = map[string]backtest.ValuationPriceModel{
	"exit":  backtest.ValuationPriceExit,
	"close": backtest.ValuationPriceClose,
	"mid":   backtest.ValuationPriceMid,
}

var triggerModeMap = map[string]backtest.TriggerPriceMode{
	"canonical":       backtest.TriggerPriceCanonical,
	"bidask-envelope": backtest.TriggerPriceBidAskEnvelope,
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
	if req.SlippagePct != nil && *req.SlippagePct > 1 {
		return dto.NewValidationError("slippage_pct must be <= 1")
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
