// Package goldencrossdsl re-implements the classic SMA golden-cross strategy
// using the declarative DSL. It serves as a reference example showing how to
// use dsl.Spec with RegisterWithConfig for a config-parameterised strategy.
package goldencrossdsl

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/dsl"
	"github.com/Cyvadra/toktik/pkg/strategies/helpers"
)

const (
	defaultFastPeriod    = 10
	defaultSlowPeriod    = 50
	defaultEntryTWAPBars = 1
	defaultPositionPct   = 0.95
)

func init() {
	dsl.Spec{
		Name:    "golden-cross-dsl",
		Aliases: []string{"golden_cross_dsl"},
		Groups:  []string{"trend"},
		Profile: catalog.StrategyProfile{RegularTrade: catalog.RegularTradeMaterial},
	}.RegisterWithConfig(func(cfg catalog.Config) dsl.Spec {
		fastPeriod := catalog.IntOrDefault(cfg.FastPeriod, defaultFastPeriod)
		slowPeriod := catalog.IntOrDefault(cfg.SlowPeriod, defaultSlowPeriod)
		entryTWAP := catalog.IntOrDefault(cfg.EntryTWAPBars, defaultEntryTWAPBars)
		posPct := helpers.ClampPositionPct(cfg.PositionSize, defaultPositionPct)

		return dsl.Spec{
			Indicators: []dsl.IndicatorDef{
				{Name: "sma_fast", Indicator: backtest.SMA("close", fastPeriod)},
				{Name: "sma_slow", Indicator: backtest.SMA("close", slowPeriod)},
				{Name: "buy_signal", Indicator: backtest.Crossover("sma_fast", "sma_slow")},
				{Name: "sell_signal", Indicator: backtest.Crossunder("sma_fast", "sma_slow")},
			},
			EntryLong: []dsl.Condition{
				func(ctx *backtest.BarContext) bool { return ctx.Ind("buy_signal") == 1 },
			},
			ExitLong: []dsl.Condition{
				func(ctx *backtest.BarContext) bool { return ctx.Ind("sell_signal") == 1 },
			},
			Sizing:   dsl.QtyFromPctEquity(posPct),
			TWAPBars: entryTWAP,
		}
	})
}
