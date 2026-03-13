package strategies

import (
	"fmt"
	"strings"

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
		return NewBullPutSpreadStrategy(), nil
	case "ma-deviation-bear", "bear-call-spread":
		return NewBearCallSpreadStrategy(), nil
	default:
		return nil, fmt.Errorf("unsupported strategy %q", req.Strategy)
	}
}

func intDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
