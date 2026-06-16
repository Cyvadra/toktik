package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// RegisterRequestBuiltins adds request.* functions supplied by the DSL bridge.
func RegisterRequestBuiltins(ip *Interpreter, securityFn func(args []Value) Value, factorFn func(args []Value) Value, fundamentalFn func(args []Value) Value) {
	if securityFn != nil {
		ip.RegisterBuiltinWithParams("request.security", []string{"market", "symbol", "interval", "field"}, securityFn)
	}
	if factorFn != nil {
		ip.RegisterBuiltinWithParams("request.factor", []string{"name", "interval", "field"}, factorFn)
	}
	if fundamentalFn != nil {
		ip.RegisterBuiltinWithParams("request.fundamental", []string{"market", "symbol", "factor", "mode"}, fundamentalFn)
	}
}
