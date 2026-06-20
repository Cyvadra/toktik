package bridge

import "github.com/Cyvadra/toktik/pkg/dsl/runtime"

// SubmitOrder implements runtime.OrderBridge by delegating to backtest.OrderBuilder.
func (b *barContextBridge) SubmitOrder(intent runtime.OrderIntent) int {
	ob := b.ctx.Order(b.primaryRef())

	switch intent.Side {
	case runtime.SideBuy:
		ob.Buy()
	case runtime.SideSell:
		ob.Sell()
	default:
		ob.Buy()
	}

	if intent.Notional > 0 {
		ob.Notional(intent.Notional)
	} else {
		qty := intent.Qty
		if qty == 0 {
			qty = 1
		}
		ob.Qty(qty)
	}

	if intent.Note != "" {
		ob.Note(intent.Note)
	}

	switch intent.Type {
	case runtime.OrderLimit:
		ob.Limit(intent.LimitPrice)
	case runtime.OrderStop:
		ob.Stop(intent.StopPrice)
	case runtime.OrderStopLimit:
		ob.StopLimit(intent.StopPrice, intent.LimitPrice)
	case runtime.OrderTWAP:
		ob.TWAP(intent.TWAPBars)
	}

	if intent.Immediate {
		ob.Immediate()
	}

	return ob.Submit()
}
