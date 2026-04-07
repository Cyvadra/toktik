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
