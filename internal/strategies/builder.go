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
	defaultSMAPeriod     = 24 // default underlying SMA period (e.g. 24 × 1 h bars = 1 day)
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
	case "bull-put-spread":
		return &bullPutSpreadStrategy{
			smaPeriod: intDefault(req.SMAPeriod, defaultSMAPeriod),
			entryTWAP: entryTWAPBars,
		}, nil
	case "bear-call-spread":
		return &bearCallSpreadStrategy{
			smaPeriod: intDefault(req.SMAPeriod, defaultSMAPeriod),
			entryTWAP: entryTWAPBars,
		}, nil
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
