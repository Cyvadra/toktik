package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import "math"

// RegisterMathBuiltins adds math.* functions.
func RegisterMathBuiltins(ip *Interpreter) {
	ip.RegisterBuiltin("math.abs", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Abs(args[0].Float()))
	})
	ip.RegisterBuiltin("math.ceil", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Ceil(args[0].Float()))
	})
	ip.RegisterBuiltin("math.floor", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Floor(args[0].Float()))
	})
	ip.RegisterBuiltin("math.round", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Round(args[0].Float()))
	})
	ip.RegisterBuiltin("math.sqrt", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Sqrt(args[0].Float()))
	})
	ip.RegisterBuiltin("math.pow", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		return FloatVal(math.Pow(args[0].Float(), args[1].Float()))
	})
	ip.RegisterBuiltin("math.log", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Log(args[0].Float()))
	})
	ip.RegisterBuiltin("math.log10", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Log10(args[0].Float()))
	})
	ip.RegisterBuiltin("math.exp", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		return FloatVal(math.Exp(args[0].Float()))
	})
	ip.RegisterBuiltin("math.max", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		return FloatVal(math.Max(args[0].Float(), args[1].Float()))
	})
	ip.RegisterBuiltin("math.min", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		return FloatVal(math.Min(args[0].Float(), args[1].Float()))
	})
	ip.RegisterBuiltin("math.sign", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		v := args[0].Float()
		if v > 0 {
			return FloatVal(1)
		}
		if v < 0 {
			return FloatVal(-1)
		}
		return FloatVal(0)
	})
	ip.RegisterBuiltin("math.avg", func(args []Value) Value {
		if len(args) == 0 {
			return NaVal()
		}
		sum := 0.0
		for _, a := range args {
			sum += a.Float()
		}
		return FloatVal(sum / float64(len(args)))
	})

	// nz(x, replacement=0) — replaces na with replacement.
	ip.RegisterBuiltin("nz", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		if args[0].IsNa() || math.IsNaN(args[0].Float()) {
			if len(args) >= 2 {
				return args[1]
			}
			return FloatVal(0)
		}
		return args[0]
	})

	// na(x) — returns true if x is na.
	ip.RegisterBuiltin("na", func(args []Value) Value {
		if len(args) < 1 {
			return BoolVal(true)
		}
		v := args[0]
		if v.IsNa() {
			return BoolVal(true)
		}
		switch v.Tag() {
		case TagFloat, TagSeries:
			return BoolVal(math.IsNaN(v.Float()))
		default:
			return BoolVal(false)
		}
	})
}
