package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

// ConfigBridge provides access to catalog-level strategy configuration.
// Implementations should be type-asserted from the Bridge at runtime.
type ConfigBridge interface {
	// ConfigFloat returns a float64 config value by name, or defval if not set.
	ConfigFloat(name string, defval float64) float64
	// ConfigString returns a string config value by name, or defval if not set.
	ConfigString(name string, defval string) string
}

// RegisterConfigBuiltins registers config.* functions for accessing
// catalog-level strategy configuration from DSL scripts.
//
// Functions:
//
//	config.get(name, defval)     — get config value as float (named lookup)
//	config.string(name, defval)  — get config value as string
//
// These read from catalog.Config fields set via API or JSON params,
// allowing DSL scripts to consume the same configuration as Go strategies.
func RegisterConfigBuiltins(ip *Interpreter) {
	// config.get(name, defval)
	ip.RegisterBuiltinWithParams("config.get", []string{"name", "defval"}, func(args []Value) Value {
		name := ""
		if len(args) >= 1 {
			name = args[0].Str()
		}
		defval := 0.0
		if len(args) >= 2 {
			defval = args[1].Float()
		}
		if name == "" || ip.Bridge == nil {
			return FloatVal(defval)
		}
		cb, ok := ip.Bridge.(ConfigBridge)
		if !ok {
			return FloatVal(defval)
		}
		return FloatVal(cb.ConfigFloat(name, defval))
	})

	// config.string(name, defval)
	ip.RegisterBuiltinWithParams("config.string", []string{"name", "defval"}, func(args []Value) Value {
		name := ""
		if len(args) >= 1 {
			name = args[0].Str()
		}
		defval := ""
		if len(args) >= 2 {
			defval = args[1].Str()
		}
		if name == "" || ip.Bridge == nil {
			return StringVal(defval)
		}
		cb, ok := ip.Bridge.(ConfigBridge)
		if !ok {
			return StringVal(defval)
		}
		return StringVal(cb.ConfigString(name, defval))
	})
}
