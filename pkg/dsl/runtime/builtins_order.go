package runtime

import "math"

// OrderBridge extends Bridge with rich order submission capabilities.
// Implementations should be type-asserted from the Bridge at runtime.
type OrderBridge interface {
	// SubmitOrder submits a structured order intent and returns the order ID.
	SubmitOrder(intent OrderIntent) int
}

// RegisterOrderBuiltins registers order.* functions for constructing
// and submitting orders with richer semantics than strategy.entry/close.
//
// Functions:
//
//	order.market(side, qty)                 — submit market order; returns order ID
//	order.market_notional(side, notional)   — submit market order by notional amount
//	order.limit(side, qty, price)           — submit limit order; returns order ID
//	order.stop(side, qty, stop_price)       — submit stop order; returns order ID
//	order.stop_limit(side, qty, stop, limit)— submit stop-limit order; returns order ID
//	order.twap(side, qty, bars)             — submit TWAP market order; returns order ID
//	order.immediate(side, qty)              — submit immediate (fills at current close)
//	order.submit(...)                       — submit via the unified named-parameter intent API
//
// Constants:
//
//	order.buy  = 1.0
//	order.sell = -1.0
func RegisterOrderBuiltins(ip *Interpreter) {
	// Constants.
	ip.Global.Set("order.buy", FloatVal(1))
	ip.Global.Set("order.sell", FloatVal(-1))

	// order.market(side, qty, note="")
	ip.RegisterBuiltinWithParams("order.market", []string{"side", "qty", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side: parseSide(args, 0),
			Qty:  argFloat(args, 1, 1),
			Type: OrderMarket,
			Note: argStr(args, 2, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.market_notional(side, notional, note="")
	ip.RegisterBuiltinWithParams("order.market_notional", []string{"side", "notional", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:     parseSide(args, 0),
			Notional: argFloat(args, 1, 0),
			Type:     OrderMarket,
			Note:     argStr(args, 2, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.limit(side, qty, price, note="")
	ip.RegisterBuiltinWithParams("order.limit", []string{"side", "qty", "price", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:       parseSide(args, 0),
			Qty:        argFloat(args, 1, 1),
			LimitPrice: argFloat(args, 2, 0),
			Type:       OrderLimit,
			Note:       argStr(args, 3, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.stop(side, qty, stop_price, note="")
	ip.RegisterBuiltinWithParams("order.stop", []string{"side", "qty", "stop_price", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:      parseSide(args, 0),
			Qty:       argFloat(args, 1, 1),
			StopPrice: argFloat(args, 2, 0),
			Type:      OrderStop,
			Note:      argStr(args, 3, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.stop_limit(side, qty, stop_price, limit_price, note="")
	ip.RegisterBuiltinWithParams("order.stop_limit", []string{"side", "qty", "stop_price", "limit_price", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:       parseSide(args, 0),
			Qty:        argFloat(args, 1, 1),
			StopPrice:  argFloat(args, 2, 0),
			LimitPrice: argFloat(args, 3, 0),
			Type:       OrderStopLimit,
			Note:       argStr(args, 4, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.twap(side, qty, bars, note="")
	ip.RegisterBuiltinWithParams("order.twap", []string{"side", "qty", "bars", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:     parseSide(args, 0),
			Qty:      argFloat(args, 1, 1),
			TWAPBars: int(argFloat(args, 2, 5)),
			Type:     OrderTWAP,
			Note:     argStr(args, 3, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.immediate(side, qty, note="")
	ip.RegisterBuiltinWithParams("order.immediate", []string{"side", "qty", "note"}, func(args []Value) Value {
		intent := OrderIntent{
			Side:      parseSide(args, 0),
			Qty:       argFloat(args, 1, 1),
			Type:      OrderMarket,
			Immediate: true,
			Note:      argStr(args, 2, ""),
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})

	// order.submit(...) — single extensible entry point for future order semantics.
	ip.RegisterBuiltinWithParams("order.submit", []string{"id", "side", "qty", "notional", "type", "limit", "stop", "twap_bars", "immediate", "note", "ref", "group_ref", "schedule_at"}, func(args []Value) Value {
		intent := OrderIntent{
			ID:         argStr(args, 0, ""),
			Side:       parseSide(args, 1),
			Qty:        argFloat(args, 2, 0),
			Notional:   argFloat(args, 3, 0),
			Type:       parseOrderType(argStr(args, 4, "market")),
			LimitPrice: argFloat(args, 5, 0),
			StopPrice:  argFloat(args, 6, 0),
			TWAPBars:   int(argFloat(args, 7, 0)),
			Immediate:  len(args) >= 9 && args[8].Bool(),
			Note:       argStr(args, 9, ""),
			Ref:        argStr(args, 10, ""),
			GroupRef:   argStr(args, 11, ""),
			ScheduleAt: int(argFloat(args, 12, 0)),
		}
		if intent.ID == "" {
			intent.ID = intent.Note
		}
		if intent.Type == OrderTWAP && intent.TWAPBars <= 0 {
			intent.TWAPBars = 1
		}
		return FloatVal(float64(submitIntent(ip, intent)))
	})
}

func submitIntent(ip *Interpreter, intent OrderIntent) int {
	if ip.Bridge == nil {
		return 0
	}
	ob, ok := ip.Bridge.(OrderBridge)
	if !ok {
		// Fallback: use basic Bridge Buy/Sell for market orders.
		return fallbackOrder(ip, intent)
	}
	return ob.SubmitOrder(intent)
}

func fallbackOrder(ip *Interpreter, intent OrderIntent) int {
	qty := intent.Qty
	if qty == 0 {
		qty = 1
	}
	switch intent.Side {
	case SideBuy:
		ip.Bridge.Buy(qty)
	case SideSell:
		ip.Bridge.Sell(qty)
	}
	return 0
}

func parseSide(args []Value, idx int) OrderSide {
	v := argFloat(args, idx, 1)
	if v < 0 {
		return SideSell
	}
	return SideBuy
}

func parseOrderType(raw string) OrderType {
	switch raw {
	case string(OrderLimit):
		return OrderLimit
	case string(OrderStop):
		return OrderStop
	case string(OrderStopLimit):
		return OrderStopLimit
	case string(OrderTWAP):
		return OrderTWAP
	default:
		return OrderMarket
	}
}

func argFloat(args []Value, idx int, defval float64) float64 {
	if idx >= len(args) {
		return defval
	}
	v := args[idx].Float()
	if math.IsNaN(v) {
		return defval
	}
	return v
}

func argStr(args []Value, idx int, defval string) string {
	if idx >= len(args) {
		return defval
	}
	s := args[idx].Str()
	if s == "" {
		return defval
	}
	return s
}
