package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import (
	"github.com/Cyvadra/toktik/internal/signals"
)

// SignalBridge provides access to structured signal events at the current bar.
// Implementations should be type-asserted from the Bridge at runtime.
type SignalBridge interface {
	// SignalEvents returns all signal events at the current bar.
	SignalEvents() []signals.SignalEvent
}

// RegisterSignalBuiltins registers signal.* functions for accessing
// structured signal event data in DSL scripts.
//
// Functions:
//
//	signal.active        — 1.0 if any signal event at current bar, else 0.0
//	signal.count         — number of signal events at current bar
//	signal.direction     — direction of first event: 1=long, -1=short, 0=flat/none
//	signal.action        — action code: 1=init, 2=add, 3=close, 4=roll, 0=other
//	signal.qty           — quantity of first event (NaN if unset)
//	signal.name          — name string of first event ("" if none)
//	signal.source        — source string of first event ("" if none)
//	signal.remarks       — remarks string of first event ("" if none)
//	signal.ref           — ref string of first event ("" if none)
//	signal.group_ref     — group_ref string of first event ("" if none)
//	signal.consumed      — 1.0 if current bar's signals have been consumed
//	signal.consume()     — mark current bar's signals as consumed; returns 1.0
func RegisterSignalBuiltins(ip *Interpreter) {
	// signal.active
	ip.RegisterBuiltin("signal.active", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) > 0 {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// signal.count
	ip.RegisterBuiltin("signal.count", func(args []Value) Value {
		evts := currentEvents(ip)
		return FloatVal(float64(len(evts)))
	})

	// signal.direction
	ip.RegisterBuiltin("signal.direction", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		return FloatVal(directionFloat(evts[0].Direction))
	})

	// signal.action
	ip.RegisterBuiltin("signal.action", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		return FloatVal(actionFloat(evts[0].Action))
	})

	// signal.qty
	ip.RegisterBuiltin("signal.qty", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return NaVal()
		}
		if evts[0].Qty == 0 {
			return NaVal()
		}
		return FloatVal(evts[0].Qty)
	})

	// signal.name
	ip.RegisterBuiltin("signal.name", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].Name)
	})

	// signal.source
	ip.RegisterBuiltin("signal.source", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].Source)
	})

	// signal.remarks
	ip.RegisterBuiltin("signal.remarks", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].Remarks)
	})

	// signal.ref
	ip.RegisterBuiltin("signal.ref", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].Ref)
	})

	// signal.group_ref
	ip.RegisterBuiltin("signal.group_ref", func(args []Value) Value {
		evts := currentEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].GroupRef)
	})

	// signal.consumed — check if current bar's signals were consumed
	ip.RegisterBuiltin("signal.consumed", func(args []Value) Value {
		key := consumedKey(ip)
		if key == "" {
			return FloatVal(0)
		}
		if _, ok := ip.varip[key]; ok {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// signal.consume() — mark current bar's signals as consumed
	ip.RegisterBuiltin("signal.consume", func(args []Value) Value {
		key := consumedKey(ip)
		if key == "" {
			return FloatVal(0)
		}
		ip.varip[key] = FloatVal(1)
		return FloatVal(1)
	})
}

// currentEvents retrieves signal events at the current bar via the SignalBridge.
func currentEvents(ip *Interpreter) []signals.SignalEvent {
	if ip.Bridge == nil {
		return nil
	}
	sb, ok := ip.Bridge.(SignalBridge)
	if !ok {
		return nil
	}
	return sb.SignalEvents()
}

// consumedKey returns a varip storage key for consume-once tracking.
func consumedKey(ip *Interpreter) string {
	evts := currentEvents(ip)
	if len(evts) == 0 {
		return ""
	}
	return "_signal_consumed_" + evts[0].ID
}

func directionFloat(d signals.SignalDirection) float64 {
	switch d {
	case signals.DirectionLong:
		return 1
	case signals.DirectionShort:
		return -1
	case signals.DirectionFlat:
		return 0
	default:
		return 0
	}
}

func actionFloat(a signals.SignalAction) float64 {
	switch a {
	case signals.ActionInit:
		return 1
	case signals.ActionAdd:
		return 2
	case signals.ActionClose:
		return 3
	case signals.ActionRoll:
		return 4
	default:
		return 0
	}
}
