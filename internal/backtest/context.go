package backtest

import (
	"math"
	"time"
)

// SecurityRef is an opaque handle to a data security used in a backtest.
// The primary security has Index 0.
type SecurityRef struct {
	Market   string
	Symbol   string
	Interval string
	Index    int // 0 = primary, 1+ = additional securities
}

// securityRegistration captures a security request from Init.
type securityRegistration struct {
	ref  SecurityRef
	inds map[string]Indicator // indicators registered on this security
}

// SetupContext is passed to Strategy.Init for declaring dependencies.
type SetupContext struct {
	primaryRef SecurityRef
	securities []securityRegistration
	factors    []factorRegistration
	params     map[string]interface{}
	nextSecIdx int
	nextFacIdx int
}

// NewSetupContext creates a setup context with the given primary security.
func NewSetupContext(market, symbol, interval string) *SetupContext {
	primary := SecurityRef{Market: market, Symbol: symbol, Interval: interval, Index: 0}
	return &SetupContext{
		primaryRef: primary,
		securities: []securityRegistration{
			{ref: primary, inds: make(map[string]Indicator)},
		},
		factors:    make([]factorRegistration, 0),
		params:     make(map[string]interface{}),
		nextSecIdx: 1,
		nextFacIdx: 0,
	}
}

// AddSecurity requests additional market data for cross-symbol / cross-asset access.
// Comparable to Pine Script's request.security().
func (sc *SetupContext) AddSecurity(market, symbol, interval string) SecurityRef {
	ref := SecurityRef{Market: market, Symbol: symbol, Interval: interval, Index: sc.nextSecIdx}
	sc.nextSecIdx++
	sc.securities = append(sc.securities, securityRegistration{
		ref:  ref,
		inds: make(map[string]Indicator),
	})
	return ref
}

// Register adds an indicator on the primary security.
func (sc *SetupContext) Register(name string, ind Indicator) {
	sc.securities[0].inds[name] = ind
}

// RegisterOn adds an indicator on a specific security.
func (sc *SetupContext) RegisterOn(ref SecurityRef, name string, ind Indicator) {
	for i := range sc.securities {
		if sc.securities[i].ref == ref {
			sc.securities[i].inds[name] = ind
			return
		}
	}
}

// AddFactor requests an external factor series, independent of market symbol/asset.
func (sc *SetupContext) AddFactor(name, interval string) FactorRef {
	ref := FactorRef{Name: name, Interval: interval, Index: sc.nextFacIdx}
	sc.nextFacIdx++
	sc.factors = append(sc.factors, factorRegistration{
		ref:  ref,
		inds: make(map[string]Indicator),
	})
	return ref
}

// RegisterFactor adds an indicator on a specific external factor series.
func (sc *SetupContext) RegisterFactor(ref FactorRef, name string, ind Indicator) {
	for i := range sc.factors {
		if sc.factors[i].ref == ref {
			sc.factors[i].inds[name] = ind
			return
		}
	}
}

// PrimaryRef returns the primary security reference.
func (sc *SetupContext) PrimaryRef() SecurityRef { return sc.primaryRef }

// SetParam sets a named parameter with a default value.
func (sc *SetupContext) SetParam(name string, defaultValue interface{}) {
	if _, exists := sc.params[name]; !exists {
		sc.params[name] = defaultValue
	}
}

// --- SecurityAccessor for cross-symbol access ---

// SecurityAccessor provides read access to a specific security's data during bar replay.
type SecurityAccessor struct {
	data     map[string][]float64 // all columns + indicators for this security
	alignMap []int                // maps primary bar index → this security's bar index
	barIndex int                  // current primary bar index
}

// Field returns the current value of a named field.
func (sa *SecurityAccessor) Field(name string) float64 {
	idx := sa.resolveIndex(0)
	if idx < 0 {
		return math.NaN()
	}
	if col, ok := sa.data[name]; ok && idx < len(col) {
		return col[idx]
	}
	return math.NaN()
}

// FieldAt returns the value of a named field N bars ago.
func (sa *SecurityAccessor) FieldAt(name string, barsAgo int) float64 {
	idx := sa.resolveIndex(barsAgo)
	if idx < 0 {
		return math.NaN()
	}
	if col, ok := sa.data[name]; ok && idx < len(col) {
		return col[idx]
	}
	return math.NaN()
}

// Ind returns the current value of a named indicator.
func (sa *SecurityAccessor) Ind(name string) float64 {
	return sa.Field(name)
}

// IndAt returns the value of a named indicator N bars ago.
func (sa *SecurityAccessor) IndAt(name string, barsAgo int) float64 {
	return sa.FieldAt(name, barsAgo)
}

func (sa *SecurityAccessor) resolveIndex(barsAgo int) int {
	if sa.alignMap == nil {
		// Same-resolution: direct index
		idx := sa.barIndex - barsAgo
		if idx < 0 {
			return -1
		}
		return idx
	}
	primaryIdx := sa.barIndex - barsAgo
	if primaryIdx < 0 || primaryIdx >= len(sa.alignMap) {
		return -1
	}
	return sa.alignMap[primaryIdx]
}

// --- BarContext for per-bar access ---

// BarContext is passed to Strategy.OnBar for each bar during replay.
type BarContext struct {
	barIndex   int
	barTime    time.Time
	primary    map[string][]float64 // primary columns + indicators
	securities []*SecurityAccessor  // index 0 = primary, 1+ = additional
	factors    []*SecurityAccessor  // external factors, independent of security universe
	broker     *Broker
	params     map[string]interface{}
	primaryRef SecurityRef
	secRefs    []SecurityRef
	factorRefs []FactorRef

	// Options trading extensions
	chainProvider    OptionsChainProvider
	spreadTracker    *SpreadTracker
	scheduledActions *[]ScheduledAction
}

// BarIndex returns the current bar index (0-based).
func (bc *BarContext) BarIndex() int { return bc.barIndex }

// Time returns the current bar's timestamp.
func (bc *BarContext) Time() time.Time { return bc.barTime }

// --- Primary field shortcuts ---

// Open returns the current bar's open price (canonical alias).
func (bc *BarContext) Open() float64 { return bc.fieldAt("open", 0) }

// High returns the current bar's high price.
func (bc *BarContext) High() float64 { return bc.fieldAt("high", 0) }

// Low returns the current bar's low price.
func (bc *BarContext) Low() float64 { return bc.fieldAt("low", 0) }

// Close returns the current bar's close price.
func (bc *BarContext) Close() float64 { return bc.fieldAt("close", 0) }

// Volume returns the current bar's volume (or NaN if unavailable).
func (bc *BarContext) Volume() float64 { return bc.fieldAt("volume", 0) }

// Field returns the current value of any named field on the primary security.
func (bc *BarContext) Field(name string) float64 { return bc.fieldAt(name, 0) }

// FieldAt returns the value of a named field N bars ago on the primary.
func (bc *BarContext) FieldAt(name string, barsAgo int) float64 {
	return bc.fieldAt(name, barsAgo)
}

// Ind returns the current value of a named indicator on the primary.
func (bc *BarContext) Ind(name string) float64 { return bc.fieldAt(name, 0) }

// IndAt returns the value of a named indicator N bars ago on the primary.
func (bc *BarContext) IndAt(name string, barsAgo int) float64 {
	return bc.fieldAt(name, barsAgo)
}

func (bc *BarContext) fieldAt(name string, barsAgo int) float64 {
	idx := bc.barIndex - barsAgo
	if idx < 0 {
		return math.NaN()
	}
	if col, ok := bc.primary[name]; ok && idx < len(col) {
		return col[idx]
	}
	return math.NaN()
}

// --- Cross-symbol access ---

// Security returns an accessor for reading data from another security.
func (bc *BarContext) Security(ref SecurityRef) *SecurityAccessor {
	if ref.Index >= 0 && ref.Index < len(bc.securities) {
		acc := bc.securities[ref.Index]
		acc.barIndex = bc.barIndex
		return acc
	}
	return &SecurityAccessor{data: nil, barIndex: bc.barIndex}
}

// Factor returns an accessor for reading data from an external factor series.
func (bc *BarContext) Factor(ref FactorRef) *SecurityAccessor {
	if ref.Index >= 0 && ref.Index < len(bc.factors) {
		acc := bc.factors[ref.Index]
		acc.barIndex = bc.barIndex
		return acc
	}
	return &SecurityAccessor{data: nil, barIndex: bc.barIndex}
}

// --- Trading commands ---

// Buy submits a market buy order for the given security.
func (bc *BarContext) Buy(ref SecurityRef, qty float64) {
	bc.BuyWithNote(ref, qty, "")
}

// BuyWithNote submits a market buy order with a short note.
func (bc *BarContext) BuyWithNote(ref SecurityRef, qty float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Buy,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// Sell submits a market sell order for the given security.
func (bc *BarContext) Sell(ref SecurityRef, qty float64) {
	bc.SellWithNote(ref, qty, "")
}

// SellWithNote submits a market sell order with a short note.
func (bc *BarContext) SellWithNote(ref SecurityRef, qty float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Sell,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// BuyNowWithNote executes a market buy on the current bar.
func (bc *BarContext) BuyNowWithNote(ref SecurityRef, qty float64, note string) bool {
	if qty <= 0 {
		return false
	}
	_, ok := bc.broker.ExecuteOrderNow(Order{
		Security:   ref,
		Side:       Buy,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	}, bc.barIndex, bc.barTime)
	return ok
}

// SellNowWithNote executes a market sell on the current bar.
func (bc *BarContext) SellNowWithNote(ref SecurityRef, qty float64, note string) bool {
	if qty <= 0 {
		return false
	}
	_, ok := bc.broker.ExecuteOrderNow(Order{
		Security:   ref,
		Side:       Sell,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	}, bc.barIndex, bc.barTime)
	return ok
}

// ScheduleBuyWithNote executes a market buy on the first primary bar at/after triggerTime.
func (bc *BarContext) ScheduleBuyWithNote(triggerTime time.Time, ref SecurityRef, qty float64, note string) {
	bc.ScheduleSecurityOrder(triggerTime, Order{
		Security:   ref,
		Side:       Buy,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// ScheduleSellWithNote executes a market sell on the first primary bar at/after triggerTime.
func (bc *BarContext) ScheduleSellWithNote(triggerTime time.Time, ref SecurityRef, qty float64, note string) {
	bc.ScheduleSecurityOrder(triggerTime, Order{
		Security:   ref,
		Side:       Sell,
		Type:       MarketOrder,
		Note:       note,
		Qty:        qty,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// BuyTWAP submits a market buy order sliced evenly across the next N bars.
func (bc *BarContext) BuyTWAP(ref SecurityRef, qty float64, bars int) {
	bc.BuyTWAPWithNote(ref, qty, bars, "")
}

// BuyTWAPWithNote submits a market buy order sliced evenly across the next N bars with a short note.
func (bc *BarContext) BuyTWAPWithNote(ref SecurityRef, qty float64, bars int, note string) {
	if bars <= 1 {
		bc.BuyWithNote(ref, qty, note)
		return
	}
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Buy,
		Type:       TWAPMarketOrder,
		Note:       note,
		Qty:        qty,
		TWAPBars:   bars,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// SellTWAP submits a market sell order sliced evenly across the next N bars.
func (bc *BarContext) SellTWAP(ref SecurityRef, qty float64, bars int) {
	bc.SellTWAPWithNote(ref, qty, bars, "")
}

// SellTWAPWithNote submits a market sell order sliced evenly across the next N bars with a short note.
func (bc *BarContext) SellTWAPWithNote(ref SecurityRef, qty float64, bars int, note string) {
	if bars <= 1 {
		bc.SellWithNote(ref, qty, note)
		return
	}
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Sell,
		Type:       TWAPMarketOrder,
		Note:       note,
		Qty:        qty,
		TWAPBars:   bars,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// BuyLimit submits a limit buy order.
func (bc *BarContext) BuyLimit(ref SecurityRef, qty, price float64) {
	bc.BuyLimitWithNote(ref, qty, price, "")
}

// BuyLimitWithNote submits a limit buy order with a short note.
func (bc *BarContext) BuyLimitWithNote(ref SecurityRef, qty, price float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Buy,
		Type:       LimitOrder,
		Note:       note,
		Qty:        qty,
		Price:      price,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// SellLimit submits a limit sell order.
func (bc *BarContext) SellLimit(ref SecurityRef, qty, price float64) {
	bc.SellLimitWithNote(ref, qty, price, "")
}

// SellLimitWithNote submits a limit sell order with a short note.
func (bc *BarContext) SellLimitWithNote(ref SecurityRef, qty, price float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Sell,
		Type:       LimitOrder,
		Note:       note,
		Qty:        qty,
		Price:      price,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// BuyStop submits a stop buy order.
func (bc *BarContext) BuyStop(ref SecurityRef, qty, stopPrice float64) {
	bc.BuyStopWithNote(ref, qty, stopPrice, "")
}

// BuyStopWithNote submits a stop buy order with a short note.
func (bc *BarContext) BuyStopWithNote(ref SecurityRef, qty, stopPrice float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Buy,
		Type:       StopOrder,
		Note:       note,
		Qty:        qty,
		StopPrice:  stopPrice,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// SellStop submits a stop sell order.
func (bc *BarContext) SellStop(ref SecurityRef, qty, stopPrice float64) {
	bc.SellStopWithNote(ref, qty, stopPrice, "")
}

// SellStopWithNote submits a stop sell order with a short note.
func (bc *BarContext) SellStopWithNote(ref SecurityRef, qty, stopPrice float64, note string) {
	bc.broker.SubmitOrder(Order{
		Security:   ref,
		Side:       Sell,
		Type:       StopOrder,
		Note:       note,
		Qty:        qty,
		StopPrice:  stopPrice,
		SubmitBar:  bc.barIndex,
		SubmitTime: bc.barTime,
	})
}

// ClosePosition submits an order to flatten the position for a security.
func (bc *BarContext) ClosePosition(ref SecurityRef) {
	pos := bc.broker.Positions().Get(ref)
	if pos.Qty > 0 {
		bc.Sell(ref, pos.Qty)
	} else if pos.Qty < 0 {
		bc.Buy(ref, -pos.Qty)
	}
}

// ClosePositionTWAP submits a TWAP order to flatten the position over N bars.
func (bc *BarContext) ClosePositionTWAP(ref SecurityRef, bars int) {
	pos := bc.broker.Positions().Get(ref)
	if pos.Qty > 0 {
		bc.SellTWAP(ref, pos.Qty, bars)
	} else if pos.Qty < 0 {
		bc.BuyTWAP(ref, -pos.Qty, bars)
	}
}

// CancelOrders cancels all pending orders for a security.
func (bc *BarContext) CancelOrders(ref SecurityRef) {
	bc.broker.CancelOrders(ref)
}

// --- Account state ---

// Position returns the current quantity for a security (positive = long, negative = short).
func (bc *BarContext) Position(ref SecurityRef) float64 {
	return bc.broker.Positions().Get(ref).Qty
}

// Equity returns the total account equity (cash + positions marked to market).
func (bc *BarContext) Equity() float64 {
	return bc.broker.Equity()
}

// Cash returns the current cash balance.
func (bc *BarContext) Cash() float64 {
	return bc.broker.Cash()
}

// PositionUnrealizedPnL returns current unrealized PnL for a specific security position.
func (bc *BarContext) PositionUnrealizedPnL(ref SecurityRef) float64 {
	return bc.broker.PositionUnrealizedPnL(ref)
}

// TotalPnL returns current strategy PnL relative to initial capital.
// For options spreads, this includes mark-to-market value of open legs.
func (bc *BarContext) TotalPnL() float64 {
	return bc.broker.TotalPnL() + bc.spreadUnrealizedEquity()
}

// Param returns a named strategy parameter.
func (bc *BarContext) Param(name string) interface{} {
	return bc.params[name]
}

// ParamInt returns a named parameter as int, or the fallback if missing/wrong type.
func (bc *BarContext) ParamInt(name string, fallback int) int {
	if v, ok := bc.params[name]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return fallback
}

// ParamFloat returns a named parameter as float64, or the fallback.
func (bc *BarContext) ParamFloat(name string, fallback float64) float64 {
	if v, ok := bc.params[name]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return fallback
}

// PrimaryRef returns the SecurityRef for the primary data series.
func (bc *BarContext) PrimaryRef() SecurityRef {
	return bc.primaryRef
}

// SecurityRefs returns all registered security references (primary first).
func (bc *BarContext) SecurityRefs() []SecurityRef {
	out := make([]SecurityRef, len(bc.secRefs))
	copy(out, bc.secRefs)
	return out
}

// FactorRefs returns all registered external factor references.
func (bc *BarContext) FactorRefs() []FactorRef {
	out := make([]FactorRef, len(bc.factorRefs))
	copy(out, bc.factorRefs)
	return out
}

// PendingOrders returns a snapshot of all unfilled pending orders.
func (bc *BarContext) PendingOrders() []Order {
	pending := bc.broker.pending
	out := make([]Order, len(pending))
	copy(out, pending)
	return out
}

// --- Options chain access ---

// OptionsChain returns the current bar's options chain, filtered and queryable.
// Returns nil if no OptionsChainProvider is configured.
func (bc *BarContext) OptionsChain() *OptionsChain {
	if bc.chainProvider == nil {
		return NewOptionsChain(nil, bc.barTime)
	}
	contracts := bc.chainProvider.AvailableContracts(bc.barTime)
	return NewOptionsChain(contracts, bc.barTime)
}

// --- Spread operations ---

// OpenSpread opens a multi-leg spread position.
// Each leg specifies the contract, side, quantity, and entry price.
// Returns the spread ID for later reference.
func (bc *BarContext) OpenSpread(legs []SpreadLeg, tag string) int {
	return bc.OpenSpreadWithRef(legs, tag, "")
}

// OpenSpreadWithRef opens a multi-leg spread position with an internal execution ref.
func (bc *BarContext) OpenSpreadWithRef(legs []SpreadLeg, tag, ref string) int {
	if bc.spreadTracker == nil {
		return 0
	}
	// Fill entry time on legs
	for i := range legs {
		legs[i].EntryTime = bc.barTime
	}
	// Record cash impact: selling = cash inflow, buying = cash outflow
	for i := range legs {
		amount := legs[i].Qty * legs[i].EntryPrice
		if legs[i].Side == Sell {
			bc.broker.AdjustCash(amount)
		} else {
			bc.broker.AdjustCash(-amount)
		}
	}
	return bc.spreadTracker.OpenWithRef(legs, bc.barTime, bc.barIndex, tag, ref)
}

// CloseSpreadLeg closes a specific leg of a spread at the given price.
func (bc *BarContext) CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool {
	return bc.CloseSpreadLegWithReason(spreadID, legIndex, closePrice, "")
}

// CloseSpreadLegWithReason closes a specific leg of a spread at the given price with a short reason.
func (bc *BarContext) CloseSpreadLegWithReason(spreadID, legIndex int, closePrice float64, closeReason string) bool {
	return bc.CloseSpreadLegWithReasonAndData(spreadID, legIndex, closePrice, closeReason, nil)
}

// CloseSpreadLegWithReasonAndData closes a specific leg of a spread at the given
// price, attaching custom report data to the close event.
func (bc *BarContext) CloseSpreadLegWithReasonAndData(spreadID, legIndex int, closePrice float64, closeReason string, closeCustomData []TradeCustomData) bool {
	if bc.spreadTracker == nil {
		return false
	}
	sp := bc.spreadTracker.Get(spreadID)
	if sp == nil || legIndex < 0 || legIndex >= len(sp.Legs) {
		return false
	}
	leg := &sp.Legs[legIndex]
	if leg.Closed {
		return false
	}
	// Cash impact of closing
	amount := leg.Qty * closePrice
	if leg.Side == Sell {
		// Closing a short: buy to close = cash outflow
		bc.broker.AdjustCash(-amount)
	} else {
		// Closing a long: sell to close = cash inflow
		bc.broker.AdjustCash(amount)
	}
	return bc.spreadTracker.CloseLegWithReasonAndData(spreadID, legIndex, closePrice, bc.barTime, closeReason, closeCustomData)
}

// CloseSpread closes all open legs of a spread using the provided price function.
func (bc *BarContext) CloseSpread(spreadID int, priceFn func(OptionContract) float64) {
	if bc.spreadTracker == nil {
		return
	}
	sp := bc.spreadTracker.Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if !sp.Legs[i].Closed {
			price := priceFn(sp.Legs[i].Contract)
			bc.CloseSpreadLeg(spreadID, i, price)
		}
	}
}

// Spreads returns the spread tracker for querying open/closed spreads.
func (bc *BarContext) Spreads() *SpreadTracker {
	return bc.spreadTracker
}

// --- Scheduled actions ---

// ScheduleCloseLeg schedules automatic closing of a spread leg at a future time.
func (bc *BarContext) ScheduleCloseLeg(triggerTime time.Time, spreadID, legIndex int) {
	bc.ScheduleCloseLegOrder(triggerTime, spreadID, legIndex, SpreadOrderMarket, Sell, math.NaN(), 0, "")
}

// ScheduleCloseLegOrder schedules closing a specific spread leg with trigger semantics.
// triggerSide follows standard order semantics: Buy triggers on upward moves; Sell on downward moves.
func (bc *BarContext) ScheduleCloseLegOrder(triggerTime time.Time, spreadID, legIndex int, orderType SpreadOrderType, triggerSide Side, triggerPrice, slippagePct float64, closeReason string) {
	if bc.scheduledActions == nil {
		return
	}
	*bc.scheduledActions = append(*bc.scheduledActions, ScheduledAction{
		TriggerTime:  triggerTime,
		SpreadID:     spreadID,
		LegIndex:     legIndex,
		ActionType:   ScheduleCloseLeg,
		OrderType:    orderType,
		TriggerSide:  triggerSide,
		TriggerPrice: triggerPrice,
		SlippagePct:  slippagePct,
		CloseReason:  closeReason,
	})
}

// ScheduleCloseSpread schedules automatic closing of all legs at a future time.
func (bc *BarContext) ScheduleCloseSpread(triggerTime time.Time, spreadID int) {
	bc.ScheduleCloseSpreadOrder(triggerTime, spreadID, SpreadOrderMarket, Sell, math.NaN(), 0, "")
}

// ScheduleCloseSpreadOrder schedules closing all spread legs with trigger semantics.
func (bc *BarContext) ScheduleCloseSpreadOrder(triggerTime time.Time, spreadID int, orderType SpreadOrderType, triggerSide Side, triggerPrice, slippagePct float64, closeReason string) {
	if bc.scheduledActions == nil {
		return
	}
	*bc.scheduledActions = append(*bc.scheduledActions, ScheduledAction{
		TriggerTime:  triggerTime,
		SpreadID:     spreadID,
		LegIndex:     -1,
		ActionType:   ScheduleCloseSpread,
		OrderType:    orderType,
		TriggerSide:  triggerSide,
		TriggerPrice: triggerPrice,
		SlippagePct:  slippagePct,
		CloseReason:  closeReason,
	})
}

// ScheduleCloseAfter schedules closing all legs of a spread after a duration from now.
func (bc *BarContext) ScheduleCloseAfter(d time.Duration, spreadID int) {
	bc.ScheduleCloseSpread(bc.barTime.Add(d), spreadID)
}

// ScheduleCloseLegAfter schedules closing a specific leg after a duration from now.
func (bc *BarContext) ScheduleCloseLegAfter(d time.Duration, spreadID, legIndex int) {
	bc.ScheduleCloseLeg(bc.barTime.Add(d), spreadID, legIndex)
}

// ScheduleOpenSpread schedules opening a spread at/after triggerTime as a market-style action.
func (bc *BarContext) ScheduleOpenSpread(triggerTime time.Time, legs []SpreadLeg, tag string) {
	bc.ScheduleOpenSpreadWithRef(triggerTime, legs, tag, "")
}

// ScheduleOpenSpreadWithRef schedules opening a spread at/after triggerTime with an internal execution ref.
func (bc *BarContext) ScheduleOpenSpreadWithRef(triggerTime time.Time, legs []SpreadLeg, tag, ref string) {
	bc.ScheduleOpenSpreadOrderWithRef(triggerTime, legs, tag, ref, SpreadOrderMarket, Buy, math.NaN(), 0)
}

// ScheduleOpenSpreadOrder schedules opening a spread with trigger semantics.
// triggerSide follows standard order semantics: Buy triggers on upward moves; Sell on downward moves.
func (bc *BarContext) ScheduleOpenSpreadOrder(triggerTime time.Time, legs []SpreadLeg, tag string, orderType SpreadOrderType, triggerSide Side, triggerPrice, slippagePct float64) {
	bc.ScheduleOpenSpreadOrderWithRef(triggerTime, legs, tag, "", orderType, triggerSide, triggerPrice, slippagePct)
}

// ScheduleOpenSpreadOrderWithRef schedules opening a spread with trigger semantics and an internal execution ref.
func (bc *BarContext) ScheduleOpenSpreadOrderWithRef(triggerTime time.Time, legs []SpreadLeg, tag, ref string, orderType SpreadOrderType, triggerSide Side, triggerPrice, slippagePct float64) {
	if bc.scheduledActions == nil {
		return
	}
	legsCopy := make([]SpreadLeg, len(legs))
	copy(legsCopy, legs)
	*bc.scheduledActions = append(*bc.scheduledActions, ScheduledAction{
		TriggerTime:  triggerTime,
		ActionType:   ScheduleOpenSpread,
		OrderType:    orderType,
		TriggerSide:  triggerSide,
		TriggerPrice: triggerPrice,
		SlippagePct:  slippagePct,
		OpenLegs:     legsCopy,
		OpenTag:      tag,
		OpenRef:      ref,
	})
}

// ScheduleSecurityOrder executes an order on the first primary bar at/after triggerTime.
// Scheduled security orders are filled on the trigger bar rather than being deferred to the next bar.
func (bc *BarContext) ScheduleSecurityOrder(triggerTime time.Time, order Order) {
	if bc.scheduledActions == nil {
		return
	}
	orderCopy := order
	*bc.scheduledActions = append(*bc.scheduledActions, ScheduledAction{
		TriggerTime:   triggerTime,
		ActionType:    ScheduleSecurityOrder,
		SecurityOrder: orderCopy,
	})
}

func (bc *BarContext) spreadUnrealizedEquity() float64 {
	if bc.spreadTracker == nil {
		return 0
	}

	contractMap := map[string]OptionContract{}
	if bc.chainProvider != nil {
		for _, c := range bc.chainProvider.AvailableContracts(bc.barTime) {
			contractMap[c.Symbol] = c
		}
	}

	spreadMarketValue := 0.0
	for _, sp := range bc.spreadTracker.OpenSpreads() {
		for _, leg := range sp.Legs {
			if leg.Closed {
				continue
			}

			contract := leg.Contract
			if updated, ok := contractMap[contract.Symbol]; ok {
				contract = updated
			}

			markPrice := optionPriceFallback(contract.MarkPrice, optionMidPrice(contract), contract.BidPrice, contract.AskPrice)
			if !optionPriceValid(markPrice) {
				continue
			}

			if leg.Side == Buy {
				spreadMarketValue += leg.Qty * markPrice
			} else {
				spreadMarketValue -= leg.Qty * markPrice
			}
		}
	}

	return spreadMarketValue
}
