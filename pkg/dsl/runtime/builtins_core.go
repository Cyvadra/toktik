package runtime

import (
	"math"
	"sort"
)

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// RegisterCoreBuiltins adds language-level builtins that should always exist.
func RegisterCoreBuiltins(ip *Interpreter) {
	RegisterCandidateBuiltins(ip)
	ip.RegisterBuiltin("snapshot", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		value := args[0]
		switch value.Tag() {
		case TagFloat, TagBool, TagSeries:
			current := value.Float()
			if math.IsNaN(current) {
				return NaVal()
			}
			return FloatVal(current)
		default:
			return value
		}
	})
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
	ip.RegisterBuiltinWithParams("array.percentile", []string{"items", "percentile"}, func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		percentile := args[1].Float()
		if math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile < 0 || percentile > 100 {
			return NaVal()
		}
		values := make([]float64, 0, len(args[0].Array()))
		for _, item := range args[0].Array() {
			value := item.Float()
			if !math.IsNaN(value) && !math.IsInf(value, 0) {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			return NaVal()
		}
		sort.Float64s(values)
		position := float64(len(values)-1) * percentile / 100
		lower := int(math.Floor(position))
		upper := int(math.Ceil(position))
		if lower == upper {
			return FloatVal(values[lower])
		}
		weight := position - float64(lower)
		return FloatVal(values[lower] + (values[upper]-values[lower])*weight)
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
