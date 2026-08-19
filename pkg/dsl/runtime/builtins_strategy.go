package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// RegisterStrategyBuiltins adds strategy.* functions that call through the Bridge.
func RegisterStrategyBuiltins(ip *Interpreter) {
	closeEntry := func(name string, args []Value) Value {
		if !ip.AllowSideEffect(name) {
			return NaVal()
		}
		if ip.Bridge == nil || len(args) < 1 {
			return NaVal()
		}
		id := args[0].Str()
		if !ip.Bridge.CloseEntry(id) {
			ip.ReportBuiltinFailure(name, "entry not found or close already pending: "+id)
		}
		return NaVal()
	}

	// strategy.entry(id, direction, qty, limit=na, stop=na, twap_bars=0, immediate=false, note="", notional=0)
	// direction: 1 = long, -1 = short
	// When limit/stop/twap_bars are specified and OrderBridge is available,
	// uses the richer OrderIntent path. Otherwise falls back to basic Buy/Sell.
	ip.RegisterBuiltinWithParams("strategy.entry", []string{"id", "direction", "qty", "limit", "stop", "twap_bars", "immediate", "note", "notional"}, func(args []Value) Value {
		if !ip.AllowSideEffect("strategy.entry") {
			return NaVal()
		}
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
		notional := argFloat(args, 8, 0)

		intent := OrderIntent{ID: id, EntryID: id, Note: note, Qty: qty, Notional: notional, Immediate: immediate}
		if notional > 0 {
			intent.Qty = 0
		}
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
		submitIntent(ip, intent)
		return NaVal()
	})

	// strategy.close(id)
	ip.RegisterBuiltinWithParams("strategy.close", []string{"id"}, func(args []Value) Value {
		return closeEntry("strategy.close", args)
	})

	// strategy.exit(id)
	ip.RegisterBuiltinWithParams("strategy.exit", []string{"id"}, func(args []Value) Value {
		return closeEntry("strategy.exit", args)
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
		if !ip.AllowSideEffect("buy") {
			return NaVal()
		}
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
		if !ip.AllowSideEffect("sell") {
			return NaVal()
		}
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
