package runtime

// RegisterRequestBuiltins adds request.* functions supplied by the DSL bridge.
func RegisterRequestBuiltins(ip *Interpreter, securityFn func(args []Value) Value, factorFn func(args []Value) Value) {
	if securityFn != nil {
		ip.RegisterBuiltinWithParams("request.security", []string{"market", "symbol", "interval", "field"}, securityFn)
	}
	if factorFn != nil {
		ip.RegisterBuiltinWithParams("request.factor", []string{"name", "interval", "field"}, factorFn)
	}
}
