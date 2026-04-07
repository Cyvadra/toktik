package runtime

// RegisterInputBuiltins adds input() and input.* parameter functions.
// In backtesting mode, inputs use their default values unless the caller
// has placed an override in ip.Inputs[title].
func RegisterInputBuiltins(ip *Interpreter) {
	// input(defval, title="", minval=na, maxval=na, step=na)
	// Returns the input value: Inputs[title] if provided, else defval.
	ip.RegisterBuiltinWithParams("input", []string{"defval", "title", "minval", "maxval", "step"}, func(args []Value) Value {
		defval := NaVal()
		if len(args) >= 1 {
			defval = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		if title != "" && ip.Inputs != nil {
			if v, ok := ip.Inputs[title]; ok {
				return FloatVal(v)
			}
		}
		return defval
	})

	// input.int(defval, title="", minval=na, maxval=na, step=1)
	ip.RegisterBuiltinWithParams("input.int", []string{"defval", "title", "minval", "maxval", "step"}, func(args []Value) Value {
		defval := NaVal()
		if len(args) >= 1 {
			defval = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		if title != "" && ip.Inputs != nil {
			if v, ok := ip.Inputs[title]; ok {
				return FloatVal(float64(int(v)))
			}
		}
		if defval.tag != TagNa {
			return FloatVal(float64(int(defval.Float())))
		}
		return defval
	})

	// input.float(defval, title="", minval=na, maxval=na, step=na)
	ip.RegisterBuiltinWithParams("input.float", []string{"defval", "title", "minval", "maxval", "step"}, func(args []Value) Value {
		defval := NaVal()
		if len(args) >= 1 {
			defval = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		if title != "" && ip.Inputs != nil {
			if v, ok := ip.Inputs[title]; ok {
				return FloatVal(v)
			}
		}
		return defval
	})

	// input.bool(defval, title="")
	ip.RegisterBuiltinWithParams("input.bool", []string{"defval", "title"}, func(args []Value) Value {
		defval := BoolVal(false)
		if len(args) >= 1 {
			defval = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		if title != "" && ip.Inputs != nil {
			if v, ok := ip.Inputs[title]; ok {
				return BoolVal(v != 0)
			}
		}
		return defval
	})

	// input.string(defval, title="", options=[])
	ip.RegisterBuiltinWithParams("input.string", []string{"defval", "title", "options"}, func(args []Value) Value {
		defval := StringVal("")
		if len(args) >= 1 {
			defval = args[0]
		}
		title := ""
		if len(args) >= 2 {
			title = args[1].Str()
		}
		if title != "" && ip.Inputs != nil {
			if v, ok := ip.Inputs[title]; ok {
				return FloatVal(v)
			}
		}
		return defval
	})
}
