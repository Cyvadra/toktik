package backtest

import "time"

// Side represents the direction of an order or trade.
type Side int

const (
	Buy  Side = 1
	Sell Side = -1
)

func (s Side) String() string {
	if s == Buy {
		return "buy"
	}
	return "sell"
}

// OrderType determines how an order is filled.
type OrderType int

const (
	MarketOrder     OrderType = iota // fills at next bar's open ± slippage
	TWAPMarketOrder                  // fills across the next N bars using market slices
	LimitOrder                       // fills if price reaches limit
	StopOrder                        // triggers market order at stop price
	StopLimitOrder                   // triggers limit order at stop price
)

func (ot OrderType) String() string {
	switch ot {
	case MarketOrder:
		return "market"
	case TWAPMarketOrder:
		return "twap_market"
	case LimitOrder:
		return "limit"
	case StopOrder:
		return "stop"
	case StopLimitOrder:
		return "stop_limit"
	default:
		return "unknown"
	}
}

// Order represents a pending order submitted by a strategy.
type Order struct {
	ID         int
	EntryID    string
	ReduceOnly bool
	Security   SecurityRef
	Side       Side
	Type       OrderType
	Note       string
	Qty        float64
	Notional   float64 // optional delayed sizing amount; resolved at fill time from the execution price
	Price      float64 // limit price (for Limit and StopLimit)
	StopPrice  float64 // trigger price (for Stop and StopLimit)
	TWAPBars   int     // number of bars across which a TWAP market order is sliced
	SubmitBar  int     // bar index when submitted
	SubmitTime time.Time
}

// Trade represents a filled order.
type Trade struct {
	ID         int
	OrderID    int
	EntryID    string
	ReduceOnly bool
	Security   SecurityRef
	Side       Side
	Note       string
	Qty        float64
	FillPrice  float64
	Commission float64
	Slippage   float64
	BarIndex   int
	Timestamp  time.Time
}

// NetAmount returns the net cash impact of this trade (positive = cash inflow).
func (t *Trade) NetAmount() float64 {
	amount := t.Qty * t.FillPrice
	if t.Side == Buy {
		return -(amount + t.Commission)
	}
	return amount - t.Commission
}
