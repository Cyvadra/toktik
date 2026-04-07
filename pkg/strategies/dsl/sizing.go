package dsl

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/helpers"
)

// QtyFromPctEquity returns a SizingFunc that allocates a percentage of total
// equity to the entry, capped by available cash.
//
// pct should be in the range (0, 1]; values outside this range are passed
// directly to helpers.PositionSizeFromEquity which returns 0 for invalid input.
func QtyFromPctEquity(pct float64) SizingFunc {
	return func(ctx *backtest.BarContext) float64 {
		return helpers.PositionSizeFromEquity(ctx.Cash(), ctx.Equity(), ctx.Close(), pct)
	}
}

// QtyFromPctCash returns a SizingFunc that allocates a percentage of available
// cash to the entry.
func QtyFromPctCash(pct float64) SizingFunc {
	return func(ctx *backtest.BarContext) float64 {
		return helpers.PositionSizeFromCash(ctx.Cash(), ctx.Close(), pct)
	}
}

// QtyFromNotional returns a SizingFunc that sizes the order from a fixed
// notional (USD) amount divided by the current close price.
func QtyFromNotional(notional float64) SizingFunc {
	return func(ctx *backtest.BarContext) float64 {
		return helpers.PositionSizeFromNotional(notional, ctx.Close())
	}
}

// QtyFromPctEquityCapped returns a SizingFunc that allocates pct of equity but
// caps the effective percentage at maxPct. This is useful when you want a
// preferred allocation but a hard upper bound on exposure.
func QtyFromPctEquityCapped(pct, maxPct float64) SizingFunc {
	effective := pct
	if effective > maxPct {
		effective = maxPct
	}
	return QtyFromPctEquity(effective)
}
