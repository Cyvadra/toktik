// Package goldencrossdsl demonstrates the DSL by reimplementing the classic
// Golden Cross strategy (SMA fast/slow crossover) without any boilerplate.
//
// The strategy goes long when the fast SMA crosses above the slow SMA and
// exits when the fast SMA crosses back below.
package goldencrossdsl

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/dsl"
)

const (
	defaultFastPeriod = 10
	defaultSlowPeriod = 50
)

func init() {
	(&dsl.Spec{
		Name:    "golden-cross-dsl",
		Aliases: []string{"golden_cross_dsl"},
		Groups:  []string{"trend"},
		Profile: catalog.StrategyProfile{RegularTrade: catalog.RegularTradeMaterial},
	}).RegisterWithConfig(func(cfg catalog.Config) (*dsl.Spec, error) {
		fastPeriod := catalog.IntOrDefault(cfg.FastPeriod, defaultFastPeriod)
		slowPeriod := catalog.IntOrDefault(cfg.SlowPeriod, defaultSlowPeriod)
		entryTWAP := catalog.IntOrDefault(cfg.EntryTWAPBars, 1)

		return &dsl.Spec{
			Params: map[string]interface{}{
				"fast_period": fastPeriod,
				"slow_period": slowPeriod,
			},
			Indicators: []dsl.IndicatorSpec{
				{Name: "sma_fast", Ind: backtest.SMA("close", fastPeriod)},
				{Name: "sma_slow", Ind: backtest.SMA("close", slowPeriod)},
				{Name: "buy_signal", Ind: backtest.Crossover("sma_fast", "sma_slow")},
				{Name: "sell_signal", Ind: backtest.Crossunder("sma_fast", "sma_slow")},
			},
			ReportColumns: []backtest.ReportColumn{
				{Source: "sma_fast", Label: "SMA Fast", Decimals: 2, Overlay: true},
				{Source: "sma_slow", Label: "SMA Slow", Decimals: 2, Overlay: true},
			},
			OnBar: func(ctx *backtest.BarContext) {
				primary := ctx.PrimaryRef()

				if ctx.Ind("buy_signal") == 1 && ctx.Position(primary) == 0 {
					price := ctx.Close()
					if price > 0 {
						qty := dsl.QtyFromPctEquity(ctx, 0.95)
						if qty > 0 {
							if entryTWAP > 1 {
								ctx.BuyTWAP(primary, qty, entryTWAP)
							} else {
								ctx.Buy(primary, qty)
							}
						}
					}
				}

				if ctx.Ind("sell_signal") == 1 && ctx.Position(primary) > 0 {
					ctx.ClosePosition(primary)
				}
			},
		}, nil
	})
}
