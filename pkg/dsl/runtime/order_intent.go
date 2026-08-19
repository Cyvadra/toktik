package runtime

// OrderType describes the execution mechanism for an order intent.
type OrderType string

const (
	OrderMarket    OrderType = "market"
	OrderLimit     OrderType = "limit"
	OrderStop      OrderType = "stop"
	OrderStopLimit OrderType = "stop_limit"
	OrderTWAP      OrderType = "twap"
)

// OrderSide describes the direction of an order.
type OrderSide string

const (
	SideBuy  OrderSide = "buy"
	SideSell OrderSide = "sell"
)

// OrderIntent represents a structured order request from DSL code.
// It decouples the DSL-level order specification from the backtest engine's OrderBuilder,
// allowing richer order semantics (limit, stop, TWAP, scheduled) to flow from DSL to engine.
type OrderIntent struct {
	// ID is a human-readable identifier for this order (e.g., strategy.entry id).
	ID string

	// EntryID identifies the logical strategy entry affected by the order.
	// Generic order.submit calls leave this empty.
	EntryID string

	// Side is the order direction: "buy" or "sell".
	Side OrderSide

	// Qty is the number of units to trade. If zero, Notional is used instead.
	Qty float64

	// Notional is the dollar amount for delayed sizing (mutually exclusive with Qty).
	Notional float64

	// Type is the order execution mechanism.
	Type OrderType

	// LimitPrice is the limit price for limit/stop-limit orders.
	LimitPrice float64

	// StopPrice is the trigger price for stop/stop-limit orders.
	StopPrice float64

	// TWAPBars is the number of bars over which to slice a TWAP order.
	TWAPBars int

	// Note is a free-form annotation attached to the order.
	Note string

	// Ref is a unique reference for linking orders (e.g., to a signal event).
	Ref string

	// Immediate indicates the order should fill at current bar close
	// instead of the next bar's open.
	Immediate bool

	// ScheduleAt specifies bars-in-future offset for scheduled execution.
	// 0 means immediate (default). Positive values delay execution.
	ScheduleAt int

	// GroupRef links this order to a group for coordinated management.
	GroupRef string
}

// IsMarket returns true if this is a plain market order.
func (o OrderIntent) IsMarket() bool {
	return o.Type == "" || o.Type == OrderMarket
}

// HasQty returns true if a quantity is specified.
func (o OrderIntent) HasQty() bool {
	return o.Qty > 0
}

// HasNotional returns true if a notional amount is specified.
func (o OrderIntent) HasNotional() bool {
	return o.Notional > 0
}

// EffectiveQty returns Qty if set, otherwise returns 0 (notional sizing is deferred to engine).
func (o OrderIntent) EffectiveQty() float64 {
	if o.Qty > 0 {
		return o.Qty
	}
	return 0
}
