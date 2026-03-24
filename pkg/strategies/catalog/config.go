package catalog

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

// TradeDirection restricts which sides of a strategy are active.
type TradeDirection string

const (
	DirectionBoth      TradeDirection = "both"       // long and short (default)
	DirectionLongOnly  TradeDirection = "long_only"  // long / call entries only
	DirectionShortOnly TradeDirection = "short_only" // short / put entries only
)

// Config is the unified runtime strategy configuration shared across strategies.
// Individual strategy factories read only the fields they need.
type Config struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	FastPeriod    int
	SlowPeriod    int
	EntryTWAPBars int

	MAPeriod   int
	PThreshold float64

	PositionSize float64
	MaxHoldTime  time.Duration

	TargetExpiryDays int
	MinExpiryDays    int
	MinPremium       float64

	ShortDeltaMin float64
	ShortDeltaMax float64
	LongDeltaMin  float64
	LongDeltaMax  float64

	Direction TradeDirection // "both" | "long_only" | "short_only"
}

type jsonConfig struct {
	EntryPriceMode     *string  `json:"entry_price_mode,omitempty"`
	ExitPriceMode      *string  `json:"exit_price_mode,omitempty"`
	ValuationPriceMode *string  `json:"valuation_price_mode,omitempty"`
	FastPeriod         *int     `json:"fast_period,omitempty"`
	SlowPeriod         *int     `json:"slow_period,omitempty"`
	EntryTWAPBars      *int     `json:"entry_twap_bars,omitempty"`
	MAPeriod           *int     `json:"ma_period,omitempty"`
	PThreshold         *float64 `json:"p_threshold,omitempty"`
	PositionSize       *float64 `json:"position_size,omitempty"`
	MaxHoldHours       *float64 `json:"max_hold_hours,omitempty"`
	TargetExpiryDays   *int     `json:"target_expiry_days,omitempty"`
	MinExpiryDays      *int     `json:"min_expiry_days,omitempty"`
	MinPremium         *float64 `json:"min_premium,omitempty"`
	ShortDeltaMin      *float64 `json:"short_delta_min,omitempty"`
	ShortDeltaMax      *float64 `json:"short_delta_max,omitempty"`
	LongDeltaMin       *float64 `json:"long_delta_min,omitempty"`
	LongDeltaMax       *float64 `json:"long_delta_max,omitempty"`
	Direction          *string  `json:"direction,omitempty"`
}

// DefaultConfig returns a baseline config that matches existing behavior.
func DefaultConfig() Config {
	pricingDefaults := backtest.DefaultSpreadPricingConfig()
	return Config{
		EntryPriceMode:     pricingDefaults.EntryMode,
		ExitPriceMode:      pricingDefaults.ExitMode,
		ValuationPriceMode: pricingDefaults.ValuationMode,
		FastPeriod:         defaultFastPeriod,
		SlowPeriod:         defaultSlowPeriod,
		EntryTWAPBars:      defaultEntryTWAPBars,
		Direction:          DirectionBoth,
	}
}

// ConfigFromJSON parses strategy params used by API requests.
func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		return cfg, nil
	}

	var jc jsonConfig
	if err := json.Unmarshal(raw, &jc); err != nil {
		return cfg, fmt.Errorf("invalid strategy params: %w", err)
	}

	if jc.EntryPriceMode != nil {
		mode, err := ParseOptionPriceMode(*jc.EntryPriceMode)
		if err != nil {
			return cfg, fmt.Errorf("entry_price_mode: %w", err)
		}
		cfg.EntryPriceMode = mode
	}
	if jc.ExitPriceMode != nil {
		mode, err := ParseOptionPriceMode(*jc.ExitPriceMode)
		if err != nil {
			return cfg, fmt.Errorf("exit_price_mode: %w", err)
		}
		cfg.ExitPriceMode = mode
	}
	if jc.ValuationPriceMode != nil {
		mode, err := ParseOptionPriceMode(*jc.ValuationPriceMode)
		if err != nil {
			return cfg, fmt.Errorf("valuation_price_mode: %w", err)
		}
		cfg.ValuationPriceMode = mode
	}

	if jc.FastPeriod != nil {
		cfg.FastPeriod = *jc.FastPeriod
	}
	if jc.SlowPeriod != nil {
		cfg.SlowPeriod = *jc.SlowPeriod
	}
	if jc.EntryTWAPBars != nil {
		cfg.EntryTWAPBars = *jc.EntryTWAPBars
	}
	if jc.MAPeriod != nil {
		cfg.MAPeriod = *jc.MAPeriod
	}
	if jc.PThreshold != nil {
		cfg.PThreshold = *jc.PThreshold
	}
	if jc.PositionSize != nil {
		cfg.PositionSize = *jc.PositionSize
	}
	if jc.MaxHoldHours != nil {
		cfg.MaxHoldTime = time.Duration(*jc.MaxHoldHours * float64(time.Hour))
	}
	if jc.TargetExpiryDays != nil {
		cfg.TargetExpiryDays = *jc.TargetExpiryDays
	}
	if jc.MinExpiryDays != nil {
		cfg.MinExpiryDays = *jc.MinExpiryDays
	}
	if jc.MinPremium != nil {
		cfg.MinPremium = *jc.MinPremium
	}
	if jc.ShortDeltaMin != nil {
		cfg.ShortDeltaMin = *jc.ShortDeltaMin
	}
	if jc.ShortDeltaMax != nil {
		cfg.ShortDeltaMax = *jc.ShortDeltaMax
	}
	if jc.LongDeltaMin != nil {
		cfg.LongDeltaMin = *jc.LongDeltaMin
	}
	if jc.LongDeltaMax != nil {
		cfg.LongDeltaMax = *jc.LongDeltaMax
	}
	if jc.Direction != nil {
		d := TradeDirection(strings.ToLower(strings.TrimSpace(*jc.Direction)))
		switch d {
		case DirectionBoth, DirectionLongOnly, DirectionShortOnly:
			cfg.Direction = d
		default:
			return cfg, fmt.Errorf("direction: unknown value %q, want both|long_only|short_only", *jc.Direction)
		}
	}

	return cfg, nil
}

// IntOrDefault returns value if non-zero, otherwise fallback.
// It is a convenience helper used by strategy factories to apply parameter defaults.
func IntOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// FloatOrDefault returns value if non-zero, otherwise fallback.
// It is a convenience helper used by strategy factories to apply parameter defaults.
func FloatOrDefault(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

// ParseOptionPriceMode parses CLI/API values for option pricing modes.
func ParseOptionPriceMode(value string) (backtest.OptionPriceMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mark_close", "mark-close", "mark", "":
		return backtest.OptionPriceMarkClose, nil
	case "bidask", "bid-ask":
		return backtest.OptionPriceBidAsk, nil
	default:
		return backtest.OptionPriceModeUnspecified, fmt.Errorf("unsupported value %q (supported: mark_close, bidask)", value)
	}
}
