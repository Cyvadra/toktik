package backtest

import "time"

// OrderBuilder provides a fluent interface for constructing orders.
// It simplifies the numerous Buy*/Sell* method variants by consolidating
// options into a single builder pattern.
//
// Example usage:
//
//	ctx.Order(primary).
//	    Side(Buy).
//	    Qty(100).
//	    Note("trend entry").
//	    Submit()
//
//	ctx.Order(primary).
//	    Sell().
//	    Qty(100).
//	    TWAP(5).
//	    Submit()
type OrderBuilder struct {
	ctx      *BarContext
	security SecurityRef
	side     Side
	qty      float64
	notional float64
	note     string
	// Order type specifics
	orderType OrderType
	price     float64 // limit price
	stopPrice float64 // stop trigger price
	twapBars  int
	// Execution flags
	immediate bool // execute on current bar close
}

// Order creates a new OrderBuilder for the given security.
func (bc *BarContext) Order(ref SecurityRef) *OrderBuilder {
	return &OrderBuilder{
		ctx:       bc,
		security:  ref,
		orderType: MarketOrder,
	}
}

// Buy sets the order side to Buy.
func (ob *OrderBuilder) Buy() *OrderBuilder {
	ob.side = Buy
	return ob
}

// Sell sets the order side to Sell.
func (ob *OrderBuilder) Sell() *OrderBuilder {
	ob.side = Sell
	return ob
}

// Side sets the order side.
func (ob *OrderBuilder) Side(s Side) *OrderBuilder {
	ob.side = s
	return ob
}

// Qty sets the order quantity.
func (ob *OrderBuilder) Qty(qty float64) *OrderBuilder {
	ob.qty = qty
	return ob
}

// Notional sets the order notional value (resolved to quantity at fill time).
func (ob *OrderBuilder) Notional(notional float64) *OrderBuilder {
	ob.notional = notional
	return ob
}

// Note adds a descriptive note to the order.
func (ob *OrderBuilder) Note(note string) *OrderBuilder {
	ob.note = note
	return ob
}

// Limit converts the order to a limit order at the specified price.
func (ob *OrderBuilder) Limit(price float64) *OrderBuilder {
	ob.orderType = LimitOrder
	ob.price = price
	return ob
}

// Stop converts the order to a stop order at the specified trigger price.
func (ob *OrderBuilder) Stop(stopPrice float64) *OrderBuilder {
	ob.orderType = StopOrder
	ob.stopPrice = stopPrice
	return ob
}

// StopLimit converts the order to a stop-limit order.
func (ob *OrderBuilder) StopLimit(stopPrice, limitPrice float64) *OrderBuilder {
	ob.orderType = StopLimitOrder
	ob.stopPrice = stopPrice
	ob.price = limitPrice
	return ob
}

// TWAP converts the order to a TWAP market order spread over N bars.
func (ob *OrderBuilder) TWAP(bars int) *OrderBuilder {
	ob.orderType = TWAPMarketOrder
	ob.twapBars = bars
	return ob
}

// Immediate flags the order for execution on the current bar's close
// instead of the next bar's open.
func (ob *OrderBuilder) Immediate() *OrderBuilder {
	ob.immediate = true
	return ob
}

// Submit queues the order for processing.
// Returns the order ID, or 0 if the order could not be submitted.
func (ob *OrderBuilder) Submit() int {
	order := Order{
		Security:   ob.security,
		Side:       ob.side,
		Type:       ob.orderType,
		Note:       ob.note,
		Qty:        ob.qty,
		Notional:   ob.notional,
		Price:      ob.price,
		StopPrice:  ob.stopPrice,
		TWAPBars:   ob.twapBars,
		SubmitBar:  ob.ctx.barIndex,
		SubmitTime: ob.ctx.barTime,
	}

	if ob.immediate {
		// Execute immediately at current bar close
		_, ok := ob.ctx.broker.ExecuteOrderAtCloseNow(order, ob.ctx.barIndex, ob.ctx.barTime)
		if ok {
			return order.ID
		}
		return 0
	}

	return ob.ctx.broker.SubmitOrder(order)
}

// SpreadOrderBuilder provides a fluent interface for scheduling spread actions.
// This consolidates the various ScheduleOpenSpread*/ScheduleCloseSpread* methods.
type SpreadOrderBuilder struct {
	ctx        *BarContext
	action     ScheduledAction
	legs       []SpreadLeg
	triggerSet bool
}

// ScheduleSpread creates a new SpreadOrderBuilder for spread operations.
func (bc *BarContext) ScheduleSpread() *SpreadOrderBuilder {
	return &SpreadOrderBuilder{
		ctx: bc,
		action: ScheduledAction{
			OrderType: SpreadOrderMarket,
		},
	}
}

// At sets the trigger time for the scheduled action.
func (sob *SpreadOrderBuilder) At(t time.Time) *SpreadOrderBuilder {
	sob.action.TriggerTime = t
	return sob
}

// OpenLegs sets the legs to open (for spread entry).
func (sob *SpreadOrderBuilder) OpenLegs(legs []SpreadLeg, tag string) *SpreadOrderBuilder {
	sob.action.ActionType = ScheduleOpenSpread
	sob.action.OpenLegs = legs
	sob.action.OpenTag = tag
	return sob
}

// WithRef sets an internal reference for tracking.
func (sob *SpreadOrderBuilder) WithRef(ref string) *SpreadOrderBuilder {
	sob.action.OpenRef = ref
	return sob
}

// InGroup assigns the spread to a group.
func (sob *SpreadOrderBuilder) InGroup(groupID int) *SpreadOrderBuilder {
	sob.action.OpenGroupID = groupID
	return sob
}

// CloseLeg schedules closing a specific leg.
func (sob *SpreadOrderBuilder) CloseLeg(spreadID, legIndex int) *SpreadOrderBuilder {
	sob.action.ActionType = ScheduleCloseLeg
	sob.action.SpreadID = spreadID
	sob.action.LegIndex = legIndex
	return sob
}

// CloseSpread schedules closing all legs of a spread.
func (sob *SpreadOrderBuilder) CloseSpread(spreadID int) *SpreadOrderBuilder {
	sob.action.ActionType = ScheduleCloseSpread
	sob.action.SpreadID = spreadID
	sob.action.LegIndex = -1
	return sob
}

// Reason adds a close reason.
func (sob *SpreadOrderBuilder) Reason(reason string) *SpreadOrderBuilder {
	sob.action.CloseReason = reason
	return sob
}

// CustomData adds custom metadata to the close event.
func (sob *SpreadOrderBuilder) CustomData(data []TradeCustomData) *SpreadOrderBuilder {
	sob.action.CloseCustomData = data
	return sob
}

// StopTrigger sets the order to trigger on stop price.
func (sob *SpreadOrderBuilder) StopTrigger(side Side, price float64) *SpreadOrderBuilder {
	sob.action.OrderType = SpreadOrderStop
	sob.action.TriggerSide = side
	sob.action.TriggerPrice = price
	sob.triggerSet = true
	return sob
}

// LimitTrigger sets the order to trigger on limit price.
func (sob *SpreadOrderBuilder) LimitTrigger(side Side, price float64) *SpreadOrderBuilder {
	sob.action.OrderType = SpreadOrderLimit
	sob.action.TriggerSide = side
	sob.action.TriggerPrice = price
	sob.triggerSet = true
	return sob
}

// Slippage sets an override slippage percentage.
func (sob *SpreadOrderBuilder) Slippage(pct float64) *SpreadOrderBuilder {
	sob.action.SlippagePct = pct
	return sob
}

// Submit schedules the action for processing.
func (sob *SpreadOrderBuilder) Submit() {
	if sob.ctx.scheduledActions == nil {
		return
	}
	*sob.ctx.scheduledActions = append(*sob.ctx.scheduledActions, sob.action)
}
