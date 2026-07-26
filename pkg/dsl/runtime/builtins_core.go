package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// RegisterCoreBuiltins adds language-level builtins that should always exist.
func RegisterCoreBuiltins(ip *Interpreter) {
	RegisterCandidateBuiltins(ip)
	ip.RegisterBuiltinWithParams("array.contains", []string{"items", "value"}, func(args []Value) Value {
		if len(args) < 2 {
			return BoolVal(false)
		}
		for _, item := range args[0].Array() {
			if valEqual(item, args[1]) {
				return BoolVal(true)
			}
		}
		return BoolVal(false)
	})
	ip.RegisterBuiltin("len", func(args []Value) Value {
		if len(args) < 1 {
			return FloatVal(0)
		}
		value := args[0]
		switch value.tag {
		case TagArray:
			return FloatVal(float64(len(value.array)))
		case TagObject:
			if sized, ok := value.obj.(interface{ Len() int }); ok {
				return FloatVal(float64(sized.Len()))
			}
			return FloatVal(0)
		case TagString:
			return FloatVal(float64(len(value.sval)))
		}
		return FloatVal(0)
	})
}
