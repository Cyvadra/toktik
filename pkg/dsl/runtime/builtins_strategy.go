package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// RegisterStrategyBuiltins adds strategy.* functions that call through the Bridge.
func RegisterStrategyBuiltins(ip *Interpreter) {
	// strategy.entry(id, direction, qty, limit=na, stop=na, twap_bars=0, immediate=false, note="")
	// direction: 1 = long, -1 = short
	// When limit/stop/twap_bars are specified and OrderBridge is available,
	// uses the richer OrderIntent path. Otherwise falls back to basic Buy/Sell.
	ip.RegisterBuiltinWithParams("strategy.entry", []string{"id", "direction", "qty", "limit", "stop", "twap_bars", "immediate", "note"}, func(args []Value) Value {
		if ip.Bridge == nil || len(args) < 2 {
			return NaVal()
		}
		id := args[0].Str()
		dir := args[1].Float()
		qty := argFloat(args, 2, 1)
		limitPrice := argFloat(args, 3, 0)
		stopPrice := argFloat(args, 4, 0)
		twapBars := int(argFloat(args, 5, 0))
		immediate := len(args) >= 7 && args[6].Bool()
		note := argStr(args, 7, id)

		hasAdvanced := limitPrice > 0 || stopPrice > 0 || twapBars > 0 || immediate

		if hasAdvanced {
			if ob, ok := ip.Bridge.(OrderBridge); ok {
				intent := OrderIntent{ID: id, Note: note, Qty: qty}
				if dir >= 0 {
					intent.Side = SideBuy
				} else {
					intent.Side = SideSell
				}
				switch {
				case stopPrice > 0 && limitPrice > 0:
					intent.Type = OrderStopLimit
					intent.StopPrice = stopPrice
					intent.LimitPrice = limitPrice
				case limitPrice > 0:
					intent.Type = OrderLimit
					intent.LimitPrice = limitPrice
				case stopPrice > 0:
					intent.Type = OrderStop
					intent.StopPrice = stopPrice
				case twapBars > 0:
					intent.Type = OrderTWAP
					intent.TWAPBars = twapBars
				default:
					intent.Type = OrderMarket
				}
				intent.Immediate = immediate
				ob.SubmitOrder(intent)
				return NaVal()
			}
		}

		// Fallback: basic entry through Bridge.
		if dir >= 0 {
			ip.Bridge.EntryLong(id, qty)
		} else {
			ip.Bridge.EntryShort(id, qty)
		}
		return NaVal()
	})

	// strategy.close(id)
	ip.RegisterBuiltinWithParams("strategy.close", []string{"id"}, func(args []Value) Value {
		if ip.Bridge == nil || len(args) < 1 {
			return NaVal()
		}
		id := args[0].Str()
		// Close by exiting both sides.
		ip.Bridge.ExitLong(id)
		ip.Bridge.ExitShort(id)
		return NaVal()
	})

	// strategy.exit(id)
	ip.RegisterBuiltinWithParams("strategy.exit", []string{"id"}, func(args []Value) Value {
		if ip.Bridge == nil || len(args) < 1 {
			return NaVal()
		}
		id := args[0].Str()
		ip.Bridge.ExitLong(id)
		ip.Bridge.ExitShort(id)
		return NaVal()
	})

	ip.RegisterBuiltinWithParams("plot", []string{"series", "title", "overlay", "precision"}, func(args []Value) Value {
		value := NaVal()
		if len(args) >= 1 {
			value = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		overlay := false
		if len(args) >= 3 {
			overlay = args[2].Bool()
		}
		precision := 0
		if len(args) >= 4 {
			precision = int(args[3].Float())
			if precision < 0 {
				precision = 0
			}
		}
		return ip.setPlotValue(title, value, precision, overlay)
	})

	// buy(qty)
	ip.RegisterBuiltin("buy", func(args []Value) Value {
		if ip.Bridge == nil {
			return NaVal()
		}
		qty := 1.0
		if len(args) >= 1 {
			qty = args[0].Float()
		}
		ip.Bridge.Buy(qty)
		return NaVal()
	})

	// sell(qty)
	ip.RegisterBuiltin("sell", func(args []Value) Value {
		if ip.Bridge == nil {
			return NaVal()
		}
		qty := 1.0
		if len(args) >= 1 {
			qty = args[0].Float()
		}
		ip.Bridge.Sell(qty)
		return NaVal()
	})

	// strategy.position_size / strategy.position_avg_price / strategy.equity / strategy.cash
	// These are accessed as bare properties (no call parens) in DSL scripts, so they
	// must be registered as auto-invoked properties via RegisterProperty.
	ip.RegisterProperty("strategy.position_size", func() Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.PositionSize())
	})
	ip.RegisterProperty("strategy.position_avg_price", func() Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.PositionAvgPrice())
	})
	ip.RegisterProperty("strategy.equity", func() Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.Equity())
	})
	ip.RegisterProperty("strategy.cash", func() Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.Cash())
	})

	// Constants.
	ip.Global.Set("strategy.long", FloatVal(1))
	ip.Global.Set("strategy.short", FloatVal(-1))
}
