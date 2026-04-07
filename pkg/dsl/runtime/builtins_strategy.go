package runtime

// RegisterStrategyBuiltins adds strategy.* functions that call through the Bridge.
func RegisterStrategyBuiltins(ip *Interpreter) {
	// strategy.entry(id, direction, qty)
	// direction: 1 = long, -1 = short
	ip.RegisterBuiltin("strategy.entry", func(args []Value) Value {
		if ip.Bridge == nil || len(args) < 2 {
			return NaVal()
		}
		id := args[0].Str()
		dir := args[1].Float()
		qty := 1.0
		if len(args) >= 3 {
			qty = args[2].Float()
		}
		if dir >= 0 {
			ip.Bridge.EntryLong(id, qty)
		} else {
			ip.Bridge.EntryShort(id, qty)
		}
		return NaVal()
	})

	// strategy.close(id)
	ip.RegisterBuiltin("strategy.close", func(args []Value) Value {
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
	ip.RegisterBuiltin("strategy.exit", func(args []Value) Value {
		if ip.Bridge == nil || len(args) < 1 {
			return NaVal()
		}
		id := args[0].Str()
		ip.Bridge.ExitLong(id)
		ip.Bridge.ExitShort(id)
		return NaVal()
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

	// strategy.position_size
	ip.RegisterBuiltin("strategy.position_size", func(args []Value) Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.PositionSize())
	})

	// strategy.position_avg_price
	ip.RegisterBuiltin("strategy.position_avg_price", func(args []Value) Value {
		if ip.Bridge == nil {
			return FloatVal(0)
		}
		return FloatVal(ip.Bridge.PositionAvgPrice())
	})

	// Constants.
	ip.Global.Set("strategy.long", FloatVal(1))
	ip.Global.Set("strategy.short", FloatVal(-1))
}
