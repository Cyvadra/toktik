package dsl

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// QtyFromPctEquity computes a buy quantity from a fraction of total account
// equity divided by the current close price.
//
//	qty := dsl.QtyFromPctEquity(ctx, 0.95)
//
// Returns 0 if the price or equity is non-positive.
func QtyFromPctEquity(ctx *backtest.BarContext, pct float64) float64 {
	price := ctx.Close()
	equity := ctx.Equity()
	if price <= 0 || equity <= 0 || pct <= 0 {
		return 0
	}
	return (equity * pct) / price
}

// QtyFromPctCash computes a buy quantity from a fraction of available cash
// divided by the current close price.
//
// Returns 0 if the price or cash is non-positive.
func QtyFromPctCash(ctx *backtest.BarContext, pct float64) float64 {
	price := ctx.Close()
	cash := ctx.Cash()
	if price <= 0 || cash <= 0 || pct <= 0 {
		return 0
	}
	return (cash * pct) / price
}

// QtyFromNotional computes a buy quantity from a fixed notional USD amount
// divided by the current close price.
//
// Returns 0 if the price is non-positive.
func QtyFromNotional(ctx *backtest.BarContext, notional float64) float64 {
	price := ctx.Close()
	if price <= 0 || notional <= 0 {
		return 0
	}
	return notional / price
}

// QtyFromPctEquityCapped is like QtyFromPctEquity but also caps the budget by
// the current available cash.  Useful when position sizing should not exceed
// remaining cash even if equity is higher.
func QtyFromPctEquityCapped(ctx *backtest.BarContext, pct float64) float64 {
	price := ctx.Close()
	if price <= 0 || pct <= 0 {
		return 0
	}
	budget := math.Min(ctx.Cash(), ctx.Equity()*pct)
	if budget <= 0 {
		return 0
	}
	return budget / price
}
