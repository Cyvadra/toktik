package runtime

import (
	"fmt"
	"strings"
)

// RegisterStrBuiltins adds str.* functions.
func RegisterStrBuiltins(ip *Interpreter) {
	ip.RegisterBuiltin("str.contains", func(args []Value) Value {
		if len(args) < 2 {
			return BoolVal(false)
		}
		return BoolVal(strings.Contains(args[0].Str(), args[1].Str()))
	})
	ip.RegisterBuiltin("str.length", func(args []Value) Value {
		if len(args) < 1 {
			return FloatVal(0)
		}
		return FloatVal(float64(len(args[0].Str())))
	})
	ip.RegisterBuiltin("str.upper", func(args []Value) Value {
		if len(args) < 1 {
			return StringVal("")
		}
		return StringVal(strings.ToUpper(args[0].Str()))
	})
	ip.RegisterBuiltin("str.lower", func(args []Value) Value {
		if len(args) < 1 {
			return StringVal("")
		}
		return StringVal(strings.ToLower(args[0].Str()))
	})

	ip.RegisterBuiltinWithParams("str.split", []string{"s", "sep"}, func(args []Value) Value {
		if len(args) < 1 {
			return ArrayVal(nil)
		}
		s := args[0].Str()
		sep := argStr(args, 1, ",")
		parts := strings.Split(s, sep)
		vals := make([]Value, 0, len(parts))
		for _, part := range parts {
			vals = append(vals, StringVal(part))
		}
		return ArrayVal(vals)
	})

	ip.RegisterBuiltinWithParams("str.join", []string{"parts", "sep"}, func(args []Value) Value {
		if len(args) < 1 || args[0].Tag() != TagArray {
			return StringVal("")
		}
		sep := argStr(args, 1, ",")
		items := args[0].Array()
		parts := make([]string, 0, len(items))
		for _, item := range items {
			if item.Tag() == TagString {
				parts = append(parts, item.Str())
				continue
			}
			parts = append(parts, item.String())
		}
		return StringVal(strings.Join(parts, sep))
	})

	ip.RegisterBuiltin("str.tostring", func(args []Value) Value {
		if len(args) < 1 {
			return StringVal("")
		}
		return StringVal(args[0].String())
	})
	ip.RegisterBuiltin("str.format", func(args []Value) Value {
		if len(args) < 1 {
			return StringVal("")
		}
		fmtStr := args[0].Str()
		ifaces := make([]interface{}, len(args)-1)
		for i := 1; i < len(args); i++ {
			ifaces[i-1] = args[i].String()
		}
		return StringVal(fmt.Sprintf(fmtStr, ifaces...))
	})
}
