// Package dslcatalog registers DSL-based strategies with the strategy catalog.
// Import this package (with _) to enable DSL strategy loading.
package dslcatalog

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// RegisterDSL registers a DSL script as a named strategy in the catalog.
func RegisterDSL(name, source string) error {
	return RegisterDSLWithMetadata(catalog.Registration{
		Name:   name,
		Groups: []string{"dsl"},
	}, source)
}

// RegisterDSLWithMetadata registers a DSL script with explicit catalog metadata.
func RegisterDSLWithMetadata(reg catalog.Registration, source string) error {
	return catalog.TryRegister(catalog.Registration{
		Name:    reg.Name,
		Aliases: append([]string(nil), reg.Aliases...),
		Groups:  append([]string(nil), reg.Groups...),
		Profile: reg.Profile,
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			opts := bridge.Options{
				SignalSource: cfg.SignalSource,
			}
			// Bridge catalog config fields to DSL config.* module.
			opts.Config = catalogToConfigMap(cfg)
			ds := bridge.NewWithOptions(source, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", reg.Name, errs)
			}
			return ds, nil
		},
	})
}

// catalogToConfigMap converts a catalog.Config into a flat map for config.get().
func catalogToConfigMap(cfg catalog.Config) map[string]interface{} {
	m := make(map[string]interface{})
	if cfg.FastPeriod != 0 {
		m["fast_period"] = cfg.FastPeriod
	}
	if cfg.SlowPeriod != 0 {
		m["slow_period"] = cfg.SlowPeriod
	}
	if cfg.MAPeriod != 0 {
		m["ma_period"] = cfg.MAPeriod
	}
	if cfg.PThreshold != 0 {
		m["p_threshold"] = cfg.PThreshold
	}
	if cfg.PositionSize != 0 {
		m["position_size"] = cfg.PositionSize
	}
	if cfg.EntryTWAPBars != 0 {
		m["entry_twap_bars"] = cfg.EntryTWAPBars
	}
	if cfg.TargetExpiryDays != 0 {
		m["target_expiry_days"] = cfg.TargetExpiryDays
	}
	if cfg.MinExpiryDays != 0 {
		m["min_expiry_days"] = cfg.MinExpiryDays
	}
	if cfg.MinPremium != 0 {
		m["min_premium"] = cfg.MinPremium
	}
	if cfg.ShortDeltaMin != 0 {
		m["short_delta_min"] = cfg.ShortDeltaMin
	}
	if cfg.ShortDeltaMax != 0 {
		m["short_delta_max"] = cfg.ShortDeltaMax
	}
	if cfg.LongDeltaMin != 0 {
		m["long_delta_min"] = cfg.LongDeltaMin
	}
	if cfg.LongDeltaMax != 0 {
		m["long_delta_max"] = cfg.LongDeltaMax
	}
	if cfg.MaxHoldTime != 0 {
		m["max_hold_hours"] = cfg.MaxHoldTime.Hours()
	}
	if cfg.Direction != "" {
		m["direction"] = string(cfg.Direction)
	}
	m["entry_price_mode"] = int(cfg.EntryPriceMode)
	m["exit_price_mode"] = int(cfg.ExitPriceMode)
	m["valuation_price_mode"] = int(cfg.ValuationPriceMode)
	return m
}
