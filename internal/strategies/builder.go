package strategies

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

const (
	defaultStrategyName  = "golden-cross"
	defaultEntryTWAPBars = 1
	defaultFastPeriod    = 10
	defaultSlowPeriod    = 50
)

// GoldenCrossParams holds tuning knobs for the golden-cross strategy.
type GoldenCrossParams struct {
	FastPeriod    *int `json:"fast_period,omitempty"`
	SlowPeriod    *int `json:"slow_period,omitempty"`
	EntryTWAPBars *int `json:"entry_twap_bars,omitempty"`
}

// DeltaFilterParams holds tuning knobs for the delta-filter strategy.
type DeltaFilterParams struct {
	EntryTWAPBars *int `json:"entry_twap_bars,omitempty"`
}

// MADeviationParams holds tuning knobs for MA-deviation spread strategies.
type MADeviationParams struct {
	PositionSize     *float64 `json:"position_size,omitempty"`
	MaxHoldHours     *float64 `json:"max_hold_hours,omitempty"`
	TargetExpiryDays *int     `json:"target_expiry_days,omitempty"`
	MinExpiryDays    *int     `json:"min_expiry_days,omitempty"`
	MinPremium       *float64 `json:"min_premium,omitempty"`
	ShortDeltaMin    *float64 `json:"short_delta_min,omitempty"`
	ShortDeltaMax    *float64 `json:"short_delta_max,omitempty"`
	LongDeltaMin     *float64 `json:"long_delta_min,omitempty"`
	LongDeltaMax     *float64 `json:"long_delta_max,omitempty"`
	EntryTWAPBars    *int     `json:"entry_twap_bars,omitempty"`
}

// Build returns a configured backtest strategy from the strategy name and
// optional JSON parameters blob.
func Build(strategyName string, params json.RawMessage) (backtest.Strategy, error) {
	name := strings.ToLower(strings.TrimSpace(strategyName))
	if name == "" {
		name = defaultStrategyName
	}

	switch name {
	case "golden-cross":
		var p GoldenCrossParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid golden-cross params: %w", err)
			}
		}
		return &goldenCrossStrategy{
			fastPeriod: intDefault(p.FastPeriod, defaultFastPeriod),
			slowPeriod: intDefault(p.SlowPeriod, defaultSlowPeriod),
			entryTWAP:  intDefault(p.EntryTWAPBars, defaultEntryTWAPBars),
		}, nil

	case "delta-filter":
		var p DeltaFilterParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid delta-filter params: %w", err)
			}
		}
		return &deltaFilterStrategy{entryTWAP: intDefault(p.EntryTWAPBars, defaultEntryTWAPBars)}, nil

	case "ma-deviation-bull", "bull-put-spread":
		return buildMADeviationFromParams(BullSpread, params)

	case "ma-deviation-bear", "bear-call-spread":
		return buildMADeviationFromParams(BearSpread, params)

	default:
		return nil, fmt.Errorf("unsupported strategy %q", strategyName)
	}
}

func buildMADeviationFromParams(direction SpreadDirection, raw json.RawMessage) (*MADeviationSpreadStrategy, error) {
	var p MADeviationParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("invalid ma-deviation params: %w", err)
		}
	}
	strategy := &MADeviationSpreadStrategy{
		Direction:        direction,
		PositionSize:     floatDefault(p.PositionSize, 1),
		TargetExpiryDays: intDefault(p.TargetExpiryDays, 15),
		MinExpiryDays:    intDefault(p.MinExpiryDays, 7),
		MinPremium:       floatDefault(p.MinPremium, 0.025),
		ShortDeltaMin:    floatDefault(p.ShortDeltaMin, 0.4),
		ShortDeltaMax:    floatDefault(p.ShortDeltaMax, 0.5),
		LongDeltaMin:     floatDefault(p.LongDeltaMin, 0.1),
		LongDeltaMax:     floatDefault(p.LongDeltaMax, 0.15),
	}
	if p.MaxHoldHours != nil {
		strategy.MaxHoldTime = time.Duration(*p.MaxHoldHours * float64(time.Hour))
	}
	return strategy, nil
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
