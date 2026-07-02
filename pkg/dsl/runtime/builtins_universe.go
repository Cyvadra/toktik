package runtime

import "strings"

type UniverseBridge interface {
	UniverseSymbols(code string) []string
}

func RegisterUniverseBuiltins(ip *Interpreter) {
	values := func(symbols []string) []Value {
		vals := make([]Value, len(symbols))
		for i, symbol := range symbols {
			vals[i] = StringVal(symbol)
		}
		return vals
	}

	readCSV := func(name string) []string {
		if ip.Bridge == nil {
			return nil
		}
		cb, ok := ip.Bridge.(ConfigBridge)
		if !ok {
			return nil
		}
		raw := strings.TrimSpace(cb.ConfigString(name, ""))
		if raw == "" {
			return nil
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}

	ip.RegisterBuiltinWithParams("universe.symbols", []string{"code"}, func(args []Value) Value {
		code := strings.ToLower(strings.TrimSpace(argStr(args, 0, "")))
		if ip.Bridge != nil {
			if ub, ok := ip.Bridge.(UniverseBridge); ok {
				return ArrayVal(values(ub.UniverseSymbols(code)))
			}
		}
		key := "universe_" + strings.NewReplacer("-", "_", ".", "_", ":", "_", "/", "_").Replace(code) + "_symbols"
		return ArrayVal(values(readCSV(key)))
	})
}
