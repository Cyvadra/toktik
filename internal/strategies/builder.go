package strategies

import (
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	defaultStrategyName  = "golden-cross"
	defaultEntryTWAPBars = 1
	defaultFastPeriod    = 10
	defaultSlowPeriod    = 50
)

// Build returns a configured backtest strategy from the request payload.
func Build(req dto.BacktestRequest) (backtest.Strategy, error) {
	strategyName := strings.ToLower(strings.TrimSpace(req.Strategy))
	if strategyName == "" {
		strategyName = defaultStrategyName
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
	case "ma-deviation-bull", "bull-put-spread":
		return buildMADeviationSpreadStrategy(BullSpread, req), nil
	case "ma-deviation-bear", "bear-call-spread":
		return buildMADeviationSpreadStrategy(BearSpread, req), nil
	default:
		return nil, fmt.Errorf("unsupported strategy %q", req.Strategy)
	}
}

func buildMADeviationSpreadStrategy(direction SpreadDirection, req dto.BacktestRequest) *MADeviationSpreadStrategy {
	strategy := &MADeviationSpreadStrategy{
		Direction:        direction,
		PositionSize:     floatDefault(req.PositionSize, 1),
		TargetExpiryDays: intDefault(req.TargetExpiryDays, 15),
		MinExpiryDays:    intDefault(req.MinExpiryDays, 7),
		MinPremium:       floatDefault(req.MinPremium, 0.025),
		ShortDeltaMin:    floatDefault(req.ShortDeltaMin, 0.4),
		ShortDeltaMax:    floatDefault(req.ShortDeltaMax, 0.5),
		LongDeltaMin:     floatDefault(req.LongDeltaMin, 0.1),
		LongDeltaMax:     floatDefault(req.LongDeltaMax, 0.15),
	}
	if req.MaxHoldHours != nil {
		strategy.MaxHoldTime = time.Duration(*req.MaxHoldHours * float64(time.Hour))
	}
	return strategy
}

func intDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func floatDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}
