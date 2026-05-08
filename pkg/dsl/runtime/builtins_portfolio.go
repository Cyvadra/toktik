package runtime

import (
	"strconv"
	"strings"
)

// RegisterPortfolioBuiltins exposes portfolio-scoped helpers backed by the
// request-level config payload injected by the backtest service.
func RegisterPortfolioBuiltins(ip *Interpreter) {
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
	readWeights := func() []float64 {
		parts := readCSV("portfolio_weights")
		if len(parts) == 0 {
			return nil
		}
		out := make([]float64, 0, len(parts))
		for _, part := range parts {
			value, err := strconv.ParseFloat(part, 64)
			if err != nil {
				continue
			}
			out = append(out, value)
		}
		return out
	}

	ip.RegisterBuiltin("portfolio.symbols", func(args []Value) Value {
		symbols := readCSV("portfolio_symbols")
		vals := make([]Value, len(symbols))
		for i, symbol := range symbols {
			vals[i] = StringVal(symbol)
		}
		return ArrayVal(vals)
	})

	ip.RegisterBuiltin("portfolio.weights", func(args []Value) Value {
		weights := readWeights()
		vals := make([]Value, len(weights))
		for i, weight := range weights {
			vals[i] = FloatVal(weight)
		}
		return ArrayVal(vals)
	})

	ip.RegisterBuiltin("portfolio.len", func(args []Value) Value {
		return FloatVal(float64(len(readCSV("portfolio_symbols"))))
	})

	ip.RegisterBuiltinWithParams("portfolio.symbol", []string{"index", "defval"}, func(args []Value) Value {
		index := int(argFloat(args, 0, -1))
		defval := argStr(args, 1, "")
		symbols := readCSV("portfolio_symbols")
		if index < 0 || index >= len(symbols) {
			return StringVal(defval)
		}
		return StringVal(symbols[index])
	})

	ip.RegisterBuiltinWithParams("portfolio.weight", []string{"symbol", "defval"}, func(args []Value) Value {
		symbol := strings.ToUpper(strings.TrimSpace(argStr(args, 0, "")))
		defval := argFloat(args, 1, 0)
		if symbol == "" {
			return FloatVal(defval)
		}
		symbols := readCSV("portfolio_symbols")
		weights := readWeights()
		for index, candidate := range symbols {
			if !strings.EqualFold(candidate, symbol) {
				continue
			}
			if index < len(weights) {
				return FloatVal(weights[index])
			}
			return FloatVal(defval)
		}
		return FloatVal(defval)
	})

	ip.RegisterBuiltin("portfolio.items", func(args []Value) Value {
		symbols := readCSV("portfolio_symbols")
		weights := readWeights()
		items := make([]Value, 0, len(symbols))
		for index, symbol := range symbols {
			weight := 0.0
			if index < len(weights) {
				weight = weights[index]
			}
			items = append(items, ArrayVal([]Value{StringVal(symbol), FloatVal(weight)}))
		}
		return ArrayVal(items)
	})
}
