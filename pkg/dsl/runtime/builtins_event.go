package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/signals"
)

// RegisterEventBuiltins registers event.* functions for consume-once
// event processing in DSL scripts.
//
// The event module wraps signal events with queue-like consume-once semantics:
// once an event is consumed, it won't appear again even if the DSL logic
// re-evaluates on the same bar.
//
// Functions:
//
//	event.pending      — number of unconsumed events at current bar
//	event.peek()       — returns direction of next unconsumed event without consuming (0 if none)
//	event.peek_action()— returns action code of next unconsumed event without consuming
//	event.peek_name()  — returns name of next unconsumed event without consuming
//	event.peek_qty()   — returns qty of next unconsumed event without consuming
//	event.next()       — consume next event; returns direction (1=long, -1=short, 0=flat/other)
//	event.next_action()— consume next event; returns action code
//	event.consume_all()— consume all remaining events at current bar; returns count consumed
//	event.is_init()    — 1.0 if next unconsumed event has action "init"
//	event.is_add()     — 1.0 if next unconsumed event has action "add"
//	event.is_close()   — 1.0 if next unconsumed event has action "close"
//	event.is_roll()    — 1.0 if next unconsumed event has action "roll"
func RegisterEventBuiltins(ip *Interpreter) {
	// event.pending
	ip.RegisterBuiltin("event.pending", func(args []Value) Value {
		evts := pendingEvents(ip)
		return FloatVal(float64(len(evts)))
	})

	// event.peek() — direction of next unconsumed event
	ip.RegisterBuiltin("event.peek", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		return FloatVal(directionFloat(evts[0].Direction))
	})

	// event.peek_action()
	ip.RegisterBuiltin("event.peek_action", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		return FloatVal(actionFloat(evts[0].Action))
	})

	// event.peek_name()
	ip.RegisterBuiltin("event.peek_name", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return StringVal("")
		}
		return StringVal(evts[0].Name)
	})

	// event.peek_qty()
	ip.RegisterBuiltin("event.peek_qty", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return NaVal()
		}
		if evts[0].Qty == 0 {
			return NaVal()
		}
		return FloatVal(evts[0].Qty)
	})

	// event.next() — consume next event, return direction
	ip.RegisterBuiltin("event.next", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		markConsumed(ip, evts[0])
		return FloatVal(directionFloat(evts[0].Direction))
	})

	// event.next_action() — consume next event, return action code
	ip.RegisterBuiltin("event.next_action", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) == 0 {
			return FloatVal(0)
		}
		markConsumed(ip, evts[0])
		return FloatVal(actionFloat(evts[0].Action))
	})

	// event.consume_all() — consume all remaining events
	ip.RegisterBuiltin("event.consume_all", func(args []Value) Value {
		evts := pendingEvents(ip)
		for _, e := range evts {
			markConsumed(ip, e)
		}
		return FloatVal(float64(len(evts)))
	})

	// event.is_init()
	ip.RegisterBuiltin("event.is_init", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) > 0 && evts[0].Action == signals.ActionInit {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// event.is_add()
	ip.RegisterBuiltin("event.is_add", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) > 0 && evts[0].Action == signals.ActionAdd {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// event.is_close()
	ip.RegisterBuiltin("event.is_close", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) > 0 && evts[0].Action == signals.ActionClose {
			return FloatVal(1)
		}
		return FloatVal(0)
	})

	// event.is_roll()
	ip.RegisterBuiltin("event.is_roll", func(args []Value) Value {
		evts := pendingEvents(ip)
		if len(evts) > 0 && evts[0].Action == signals.ActionRoll {
			return FloatVal(1)
		}
		return FloatVal(0)
	})
}

// pendingEvents returns events at the current bar that haven't been consumed yet.
func pendingEvents(ip *Interpreter) []signals.SignalEvent {
	all := currentEvents(ip)
	if len(all) == 0 {
		return nil
	}
	var pending []signals.SignalEvent
	for _, e := range all {
		key := eventConsumedKey(e)
		if _, ok := ip.varip[key]; !ok {
			pending = append(pending, e)
		}
	}
	return pending
}

// markConsumed marks a single event as consumed using varip storage.
func markConsumed(ip *Interpreter, e signals.SignalEvent) {
	key := eventConsumedKey(e)
	ip.varip[key] = FloatVal(1)
}

// eventConsumedKey returns a unique varip key for consume-once tracking.
func eventConsumedKey(e signals.SignalEvent) string {
	return fmt.Sprintf("_event_consumed_%s", e.ID)
}
