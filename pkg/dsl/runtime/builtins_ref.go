package runtime

import "fmt"

// RegisterRefBuiltins registers ref.* functions for named reference tracking
// across bars. This allows DSL scripts to link orders, positions, and signals
// by storing and retrieving named values that persist across bars.
//
// Functions:
//
//	ref.set(name, value)  — store a value under a name (persists across bars via varip)
//	ref.get(name)         — retrieve a stored value (NaN if not set)
//	ref.has(name)         — 1.0 if the name has a stored value, 0.0 otherwise
//	ref.clear(name)       — remove a stored value; returns 1.0 if existed, 0.0 otherwise
//	ref.inc(name)         — increment stored value by 1 (initializes to 1 if not set)
//	ref.dec(name)         — decrement stored value by 1 (initializes to -1 if not set)
func RegisterRefBuiltins(ip *Interpreter) {
	// ref.set(name, value)
	ip.RegisterBuiltinWithParams("ref.set", []string{"name", "value"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return NaVal()
		}
		val := NaVal()
		if len(args) >= 2 {
			val = args[1]
		}
		ip.varip[refKey(name)] = val
		return val
	})

	// ref.get(name)
	ip.RegisterBuiltinWithParams("ref.get", []string{"name"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return NaVal()
		}
		v, ok := ip.varip[refKey(name)]
		if !ok {
			return NaVal()
		}
		return v
	})

	// ref.has(name)
	ip.RegisterBuiltinWithParams("ref.has", []string{"name"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return FloatVal(0)
		}
		if _, ok := ip.varip[refKey(name)]; ok {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// ref.clear(name)
	ip.RegisterBuiltinWithParams("ref.clear", []string{"name"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return FloatVal(0)
		}
		key := refKey(name)
		if _, ok := ip.varip[key]; ok {
			delete(ip.varip, key)
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// ref.inc(name)
	ip.RegisterBuiltinWithParams("ref.inc", []string{"name"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return NaVal()
		}
		key := refKey(name)
		cur := 0.0
		if v, ok := ip.varip[key]; ok {
			cur = v.Float()
		}
		cur++
		ip.varip[key] = FloatVal(cur)
		return FloatVal(cur)
	})

	// ref.dec(name)
	ip.RegisterBuiltinWithParams("ref.dec", []string{"name"}, func(args []Value) Value {
		name := argStr(args, 0, "")
		if name == "" {
			return NaVal()
		}
		key := refKey(name)
		cur := 0.0
		if v, ok := ip.varip[key]; ok {
			cur = v.Float()
		}
		cur--
		ip.varip[key] = FloatVal(cur)
		return FloatVal(cur)
	})
}

func refKey(name string) string {
	return fmt.Sprintf("_ref_%s", name)
}
