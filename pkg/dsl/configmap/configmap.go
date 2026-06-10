package configmap

import (
	"fmt"
	"strings"

	"github.com/Cyvadra/toktik/pkg/strategies"
)

type PortfolioItem struct {
	Market string
	Symbol string
	Weight float64
}

func FromStrategyConfig(cfg strategies.Config, items []PortfolioItem) map[string]interface{} {
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
	applyPortfolio(m, items)
	return m
}

func applyPortfolio(m map[string]interface{}, items []PortfolioItem) {
	if len(items) == 0 {
		return
	}
	symbols := make([]string, 0, len(items))
	weights := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
		weights = append(weights, fmt.Sprintf("%g", item.Weight))
	}
	if len(symbols) > 0 {
		m["portfolio_symbols"] = strings.Join(symbols, ",")
		m["portfolio_weights"] = strings.Join(weights, ",")
	}
}
