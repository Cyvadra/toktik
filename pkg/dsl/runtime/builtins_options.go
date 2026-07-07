package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import "strings"

const (
	OptionStrategyBuyCall           = "BUY_CALL"
	OptionStrategyBuyPut            = "BUY_PUT"
	OptionStrategySellPut           = "SELL_PUT"
	OptionStrategySellCall          = "SELL_CALL"
	OptionStrategyCoveredCall       = "COVERED_CALL"
	OptionStrategyBullCallSpread    = "BULL_CALL_SPREAD"
	OptionStrategyBullPutSpread     = "BULL_PUT_SPREAD"
	OptionStrategyBearCallSpread    = "BEAR_CALL_SPREAD"
	OptionStrategyShortStrangle     = "SHORT_STRANGLE"
	OptionStrategyIronCondor        = "IRON_CONDOR"
	OptionStrategyBuyStraddle       = "BUY_STRADDLE"
	OptionStrategyBuySkewedStraddle = "BUY_SKEWED_STRADDLE"
	OptionStrategyCalendarSpread    = "CALENDAR_SPREAD"
)

// OptionsBridge extends Bridge with options trading capabilities.
// The interpreter checks for this at runtime via type assertion.
type OptionsBridge interface {
	// OptionsChain returns the current bar's options chain as an opaque object.
	OptionsChain() interface{}
	// OptionsChainFor returns an explicit market/symbol option chain.
	OptionsChainFor(market, underlying string) interface{}
	// ChainCalls filters to call options.
	ChainCalls(chain interface{}) interface{}
	// ChainPuts filters to put options.
	ChainPuts(chain interface{}) interface{}
	// ChainExpiryNearest filters to nearest expiry.
	ChainExpiryNearest(chain interface{}, targetDays int) interface{}
	// ChainExpiryRange filters by DTE range.
	ChainExpiryRange(chain interface{}, minDays, maxDays int) interface{}
	// ChainExpiryMin filters to at least minDays DTE.
	ChainExpiryMin(chain interface{}, minDays int) interface{}
	// ChainExpiryMax filters to at most maxDays DTE.
	ChainExpiryMax(chain interface{}, maxDays int) interface{}
	// ChainDeltaRange filters by delta range.
	ChainDeltaRange(chain interface{}, minDelta, maxDelta float64) interface{}
	// ChainMinPremium filters by minimum bid price.
	ChainMinPremium(chain interface{}, minBid float64) interface{}
	// ChainStrikeRange filters by strike range.
	ChainStrikeRange(chain interface{}, min, max float64) interface{}
	// ChainLen returns number of contracts.
	ChainLen(chain interface{}) int
	// ChainBestSpread returns the contract with tightest spread.
	ChainBestSpread(chain interface{}) interface{}
	// ChainSortByDelta returns contracts sorted by delta proximity.
	ChainSortByDelta(chain interface{}, targetDelta float64) []interface{}

	// Contract field accessors.
	ContractSymbol(c interface{}) string
	ContractUnderlying(c interface{}) string
	ContractMarket(c interface{}) string
	ContractType(c interface{}) string // "call" or "put"
	ContractStrike(c interface{}) float64
	ContractExpiry(c interface{}) float64
	ContractDTE(c interface{}) float64
	ContractDelta(c interface{}) float64
	ContractGamma(c interface{}) float64
	ContractVega(c interface{}) float64
	ContractTheta(c interface{}) float64
	ContractIV(c interface{}) float64
	ContractBid(c interface{}) float64
	ContractAsk(c interface{}) float64
	ContractMark(c interface{}) float64
	ContractVolume(c interface{}) float64
	ContractOI(c interface{}) float64

	// Spread management.
	// OpenSpread opens a spread with the given legs. Each leg is [contract, side_string, qty].
	// Returns the spread ID.
	OpenSpread(legs []SpreadLegInput, tag string) int
	// OpenSpreadInGroup opens a spread within a group.
	OpenSpreadInGroup(legs []SpreadLegInput, tag string, groupID int) int
	// CloseSpread closes all legs of a spread.
	CloseSpread(spreadID int)
	// CloseSpreadWithReason closes all legs of a spread with a close note.
	CloseSpreadWithReason(spreadID int, reason string)
	// CloseSpreadLeg closes a single leg.
	CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool
	// SpreadGet returns spread info as [id, tag, barsHeld, realizedPnl, isOpen].
	SpreadGet(spreadID int) SpreadInfo
	// OpenSpreads returns IDs of all open spreads.
	OpenSpreads() []int
	// SpreadPnL returns unrealized PnL of a spread.
	SpreadPnL(spreadID int) float64
	// Spread leg inspection.
	SpreadLegContract(spreadID, legIndex int) interface{}
	SpreadLegEntryPrice(spreadID, legIndex int) float64
	SpreadLegQty(spreadID, legIndex int) float64
	SpreadLegSide(spreadID, legIndex int) string
	SpreadLegIsOpen(spreadID, legIndex int) bool

	// Spread groups.
	GroupOpen(tag string, initAmount, decayFactor float64) int
	GroupClose(groupID int)
	GroupGet(groupID int) GroupInfo
	GroupAddSpread(groupID, spreadID int)
	GroupIncrementRoll(groupID int)
	OpenGroups() []int

	// Scheduling.
	ScheduleCloseSpread(triggerBarOffset int, spreadID int)
	ScheduleCloseSpreadWithReason(triggerBarOffset int, spreadID int, reason string)
	ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int)
	ScheduleCloseGroup(triggerBarOffset int, groupID int)
}

// SpreadLegInput describes a leg to open.
type SpreadLegInput struct {
	Contract interface{} // opaque OptionContract
	Side     string      // "buy" or "sell"
	Qty      float64
}

// SpreadInfo summarizes an open/closed spread.
type SpreadInfo struct {
	ID          int
	Tag         string
	BarsHeld    int
	RealizedPnL float64
	IsOpen      bool
	LegCount    int
}

// GroupInfo summarizes a spread group.
type GroupInfo struct {
	ID          int
	Tag         string
	Amount      float64
	RollCount   int
	IsClosed    bool
	SpreadIDs   []int
	SpreadCount int
}

// RegisterOptionsBuiltins adds options.* and spread.* DSL functions.
func RegisterOptionsBuiltins(ip *Interpreter) {
	ob := func() OptionsBridge {
		if ip.Bridge == nil {
			return nil
		}
		if ob, ok := ip.Bridge.(OptionsBridge); ok {
			return ob
		}
		return nil
	}

	// ------- options.chain() -------
	ip.RegisterBuiltin("options.chain", func(args []Value) Value {
		b := ob()
		if b == nil {
			return NaVal()
		}
		ch := b.OptionsChain()
		if len(args) >= 2 {
			ch = b.OptionsChainFor(args[0].Str(), args[1].Str())
		}
		if ch == nil {
			return NaVal()
		}
		return ObjVal(ch)
	})

	// ------- options.calls(chain) -------
	ip.RegisterBuiltin("options.calls", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return ObjVal(b.ChainCalls(args[0].Obj()))
	})

	// ------- options.puts(chain) -------
	ip.RegisterBuiltin("options.puts", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return ObjVal(b.ChainPuts(args[0].Obj()))
	})

	// ------- options.expiry_nearest(chain, target_days) -------
	ip.RegisterBuiltin("options.expiry_nearest", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return ObjVal(b.ChainExpiryNearest(args[0].Obj(), int(args[1].Float())))
	})

	// ------- options.expiry_range(chain, min_days, max_days) -------
	ip.RegisterBuiltin("options.expiry_range", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		return ObjVal(b.ChainExpiryRange(args[0].Obj(), int(args[1].Float()), int(args[2].Float())))
	})

	// ------- options.expiry_min(chain, min_days) -------
	ip.RegisterBuiltin("options.expiry_min", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return ObjVal(b.ChainExpiryMin(args[0].Obj(), int(args[1].Float())))
	})

	// ------- options.expiry_max(chain, max_days) -------
	ip.RegisterBuiltin("options.expiry_max", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return ObjVal(b.ChainExpiryMax(args[0].Obj(), int(args[1].Float())))
	})

	// ------- options.delta_range(chain, min_delta, max_delta) -------
	ip.RegisterBuiltin("options.delta_range", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		return ObjVal(b.ChainDeltaRange(args[0].Obj(), args[1].Float(), args[2].Float()))
	})

	// ------- options.min_premium(chain, min_bid) -------
	ip.RegisterBuiltin("options.min_premium", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return ObjVal(b.ChainMinPremium(args[0].Obj(), args[1].Float()))
	})

	// ------- options.strike_range(chain, min, max) -------
	ip.RegisterBuiltin("options.strike_range", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		return ObjVal(b.ChainStrikeRange(args[0].Obj(), args[1].Float(), args[2].Float()))
	})

	// ------- options.len(chain) -------
	ip.RegisterBuiltin("options.len", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(float64(b.ChainLen(args[0].Obj())))
	})

	// ------- options.best_spread(chain) → contract -------
	ip.RegisterBuiltin("options.best_spread", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		c := b.ChainBestSpread(args[0].Obj())
		if c == nil {
			return NaVal()
		}
		return ObjVal(c)
	})

	// ------- options.sort_by_delta(chain, target) → array of contracts -------
	ip.RegisterBuiltin("options.sort_by_delta", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		contracts := b.ChainSortByDelta(args[0].Obj(), args[1].Float())
		vals := make([]Value, len(contracts))
		for i, c := range contracts {
			vals[i] = ObjVal(c)
		}
		return ArrayVal(vals)
	})

	// ------- options.strategies(context, family?) → array of strategy names -------
	ip.RegisterBuiltinWithParams("options.strategies", []string{"context", "family"}, func(args []Value) Value {
		ctx := marketContextArg(args)
		family := strings.ToLower(strings.TrimSpace(argStr(args, 1, "")))
		strategies := selectOptionStrategies(ctx, family)
		vals := make([]Value, len(strategies))
		for i, strategy := range strategies {
			vals[i] = StringVal(strategy)
		}
		return ArrayVal(vals)
	})

	// ------- options.build_strategy(chain, name, qty?, target_delta?) → legs -------
	ip.RegisterBuiltinWithParams("options.build_strategy", []string{"chain", "name", "qty", "target_delta"}, func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return ArrayVal(nil)
		}
		qty := argFloat(args, 2, 1)
		if qty <= 0 {
			qty = 1
		}
		targetDelta := argFloat(args, 3, 0.35)
		legs := buildOptionStrategyLegs(b, args[0].Obj(), strings.ToUpper(strings.TrimSpace(args[1].Str())), qty, targetDelta)
		return ArrayVal(legs)
	})

	// ------- options.open_strategy(chain, name, qty?, target_delta?, tag?) → spread_id -------
	ip.RegisterBuiltinWithParams("options.open_strategy", []string{"chain", "name", "qty", "target_delta", "tag"}, func(args []Value) Value {
		if !ip.AllowSideEffect("options.open_strategy") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		qty := argFloat(args, 2, 1)
		if qty <= 0 {
			qty = 1
		}
		targetDelta := argFloat(args, 3, 0.35)
		name := strings.ToUpper(strings.TrimSpace(args[1].Str()))
		legs := buildOptionStrategyLegs(b, args[0].Obj(), name, qty, targetDelta)
		inputs := parseLegInputs(legs)
		if len(inputs) == 0 {
			return NaVal()
		}
		tag := argStr(args, 4, name)
		return FloatVal(float64(b.OpenSpread(inputs, tag)))
	})

	// ------- contract field accessors -------
	// contract.symbol, contract.type, contract.strike, contract.dte,
	// contract.expiry,
	// contract.delta, contract.gamma, contract.vega, contract.theta,
	// contract.iv, contract.bid, contract.ask, contract.mark,
	// contract.volume, contract.oi

	ip.RegisterBuiltin("contract.symbol", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return StringVal(b.ContractSymbol(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.underlying", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return StringVal(b.ContractUnderlying(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.market", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return StringVal(b.ContractMarket(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.type", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return StringVal(b.ContractType(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.strike", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractStrike(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.expiry", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractExpiry(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.dte", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractDTE(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.delta", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractDelta(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.gamma", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractGamma(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.vega", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractVega(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.theta", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractTheta(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.iv", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractIV(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.bid", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractBid(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.ask", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractAsk(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.mark", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractMark(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.volume", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractVolume(args[0].Obj()))
	})
	ip.RegisterBuiltin("contract.oi", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.ContractOI(args[0].Obj()))
	})

	// ------- spread management -------

	// spread.open(legs_array, tag) → spread_id
	// Each leg is [contract, "buy"/"sell", qty]
	ip.RegisterBuiltin("spread.open", func(args []Value) Value {
		if !ip.AllowSideEffect("spread.open") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		legsArr := args[0].Array()
		tag := args[1].Str()
		legs := parseLegInputs(legsArr)
		if len(legs) == 0 {
			return NaVal()
		}
		id := b.OpenSpread(legs, tag)
		return FloatVal(float64(id))
	})

	// spread.open_on(market, underlying, legs_array, tag) → spread_id
	ip.RegisterBuiltinWithParams("spread.open_on", []string{"market", "underlying", "legs", "tag"}, func(args []Value) Value {
		if !ip.AllowSideEffect("spread.open_on") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 4 {
			return NaVal()
		}
		legs := parseLegInputs(args[2].Array())
		if len(legs) == 0 || !spreadLegsMatchScope(b, legs, args[0].Str(), args[1].Str()) {
			return NaVal()
		}
		id := b.OpenSpread(legs, args[3].Str())
		return FloatVal(float64(id))
	})

	// spread.open_in_group(legs_array, tag, group_id) → spread_id
	ip.RegisterBuiltin("spread.open_in_group", func(args []Value) Value {
		if !ip.AllowSideEffect("spread.open_in_group") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		legsArr := args[0].Array()
		tag := args[1].Str()
		groupID := int(args[2].Float())
		legs := parseLegInputs(legsArr)
		if len(legs) == 0 {
			return NaVal()
		}
		id := b.OpenSpreadInGroup(legs, tag, groupID)
		return FloatVal(float64(id))
	})

	// spread.open_in_group_on(market, underlying, legs_array, tag, group_id) → spread_id
	ip.RegisterBuiltinWithParams("spread.open_in_group_on", []string{"market", "underlying", "legs", "tag", "group_id"}, func(args []Value) Value {
		if !ip.AllowSideEffect("spread.open_in_group_on") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 5 {
			return NaVal()
		}
		legs := parseLegInputs(args[2].Array())
		if len(legs) == 0 || !spreadLegsMatchScope(b, legs, args[0].Str(), args[1].Str()) {
			return NaVal()
		}
		id := b.OpenSpreadInGroup(legs, args[3].Str(), int(args[4].Float()))
		return FloatVal(float64(id))
	})

	// spread.close(spread_id, reason?)
	ip.RegisterBuiltin("spread.close", func(args []Value) Value {
		if !ip.AllowSideEffect("spread.close") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		spreadID := int(args[0].Float())
		reason := ""
		if len(args) >= 2 {
			reason = args[1].Str()
		}
		if reason != "" {
			b.CloseSpreadWithReason(spreadID, reason)
		} else {
			b.CloseSpread(spreadID)
		}
		return NaVal()
	})

	// spread.close_leg(spread_id, leg_index, close_price) → bool
	ip.RegisterBuiltin("spread.close_leg", func(args []Value) Value {
		if !ip.AllowSideEffect("spread.close_leg") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		ok := b.CloseSpreadLeg(int(args[0].Float()), int(args[1].Float()), args[2].Float())
		return BoolVal(ok)
	})

	// spread.get(spread_id) → [id, tag, bars_held, realized_pnl, is_open, leg_count]
	ip.RegisterBuiltin("spread.get", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		info := b.SpreadGet(int(args[0].Float()))
		return ArrayVal([]Value{
			FloatVal(float64(info.ID)),
			StringVal(info.Tag),
			FloatVal(float64(info.BarsHeld)),
			FloatVal(info.RealizedPnL),
			BoolVal(info.IsOpen),
			FloatVal(float64(info.LegCount)),
		})
	})

	// spread.pnl(spread_id) → unrealized PnL
	ip.RegisterBuiltin("spread.pnl", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		return FloatVal(b.SpreadPnL(int(args[0].Float())))
	})

	// spread.leg_contract(spread_id, leg_index) → contract object
	ip.RegisterBuiltin("spread.leg_contract", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		contract := b.SpreadLegContract(int(args[0].Float()), int(args[1].Float()))
		if contract == nil {
			return NaVal()
		}
		return ObjVal(contract)
	})

	// spread.leg_entry_price(spread_id, leg_index)
	ip.RegisterBuiltin("spread.leg_entry_price", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return FloatVal(b.SpreadLegEntryPrice(int(args[0].Float()), int(args[1].Float())))
	})

	// spread.leg_qty(spread_id, leg_index)
	ip.RegisterBuiltin("spread.leg_qty", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return FloatVal(b.SpreadLegQty(int(args[0].Float()), int(args[1].Float())))
	})

	// spread.leg_side(spread_id, leg_index)
	ip.RegisterBuiltin("spread.leg_side", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return StringVal(b.SpreadLegSide(int(args[0].Float()), int(args[1].Float())))
	})

	// spread.leg_open(spread_id, leg_index)
	ip.RegisterBuiltin("spread.leg_open", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		return BoolVal(b.SpreadLegIsOpen(int(args[0].Float()), int(args[1].Float())))
	})

	// spread.open_ids() → array of open spread IDs
	ip.RegisterBuiltin("spread.open_ids", func(args []Value) Value {
		b := ob()
		if b == nil {
			return ArrayVal(nil)
		}
		ids := b.OpenSpreads()
		vals := make([]Value, len(ids))
		for i, id := range ids {
			vals[i] = FloatVal(float64(id))
		}
		return ArrayVal(vals)
	})

	// spread.count() → number of open spreads
	ip.RegisterBuiltin("spread.count", func(args []Value) Value {
		b := ob()
		if b == nil {
			return FloatVal(0)
		}
		return FloatVal(float64(len(b.OpenSpreads())))
	})

	// ------- spread groups -------

	// group.open(tag, init_amount, decay_factor) → group_id
	ip.RegisterBuiltin("group.open", func(args []Value) Value {
		if !ip.AllowSideEffect("group.open") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		id := b.GroupOpen(args[0].Str(), args[1].Float(), args[2].Float())
		return FloatVal(float64(id))
	})

	// group.close(group_id)
	ip.RegisterBuiltin("group.close", func(args []Value) Value {
		if !ip.AllowSideEffect("group.close") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		b.GroupClose(int(args[0].Float()))
		return NaVal()
	})

	// group.get(group_id) → [id, tag, amount, roll_count, is_closed]
	ip.RegisterBuiltin("group.get", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		info := b.GroupGet(int(args[0].Float()))
		spreadVals := make([]Value, len(info.SpreadIDs))
		for i, sid := range info.SpreadIDs {
			spreadVals[i] = FloatVal(float64(sid))
		}
		return ArrayVal([]Value{
			FloatVal(float64(info.ID)),
			StringVal(info.Tag),
			FloatVal(info.Amount),
			FloatVal(float64(info.RollCount)),
			BoolVal(info.IsClosed),
			ArrayVal(spreadVals),
		})
	})

	// group.add_spread(group_id, spread_id)
	ip.RegisterBuiltin("group.add_spread", func(args []Value) Value {
		if !ip.AllowSideEffect("group.add_spread") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		b.GroupAddSpread(int(args[0].Float()), int(args[1].Float()))
		return NaVal()
	})

	// group.increment_roll(group_id)
	ip.RegisterBuiltin("group.increment_roll", func(args []Value) Value {
		if !ip.AllowSideEffect("group.increment_roll") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		b.GroupIncrementRoll(int(args[0].Float()))
		return NaVal()
	})

	// group.open_ids() → array of open group IDs
	ip.RegisterBuiltin("group.open_ids", func(args []Value) Value {
		b := ob()
		if b == nil {
			return ArrayVal(nil)
		}
		ids := b.OpenGroups()
		vals := make([]Value, len(ids))
		for i, id := range ids {
			vals[i] = FloatVal(float64(id))
		}
		return ArrayVal(vals)
	})

	// ------- scheduling -------

	// schedule.close_spread(bars_offset, spread_id, reason?)
	ip.RegisterBuiltin("schedule.close_spread", func(args []Value) Value {
		if !ip.AllowSideEffect("schedule.close_spread") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		offset := int(args[0].Float())
		spreadID := int(args[1].Float())
		reason := ""
		if len(args) >= 3 {
			reason = args[2].Str()
		}
		if reason != "" {
			b.ScheduleCloseSpreadWithReason(offset, spreadID, reason)
		} else {
			b.ScheduleCloseSpread(offset, spreadID)
		}
		return NaVal()
	})

	// schedule.close_leg(bars_offset, spread_id, leg_index)
	ip.RegisterBuiltin("schedule.close_leg", func(args []Value) Value {
		if !ip.AllowSideEffect("schedule.close_leg") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		b.ScheduleCloseLeg(int(args[0].Float()), int(args[1].Float()), int(args[2].Float()))
		return NaVal()
	})

	// ------- Extended spread/group/schedule functions -------

	// spread.pnl_all() — sum of PnL across all open spreads
	ip.RegisterBuiltin("spread.pnl_all", func(args []Value) Value {
		b := ob()
		if b == nil {
			return FloatVal(0)
		}
		ids := b.OpenSpreads()
		total := 0.0
		for _, id := range ids {
			total += b.SpreadPnL(id)
		}
		return FloatVal(total)
	})

	// group.spread_count(group_id) — returns count of spreads in a group
	ip.RegisterBuiltin("group.spread_count", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return FloatVal(0)
		}
		info := b.GroupGet(int(args[0].Float()))
		return FloatVal(float64(info.SpreadCount))
	})

	// schedule.close_group(bars_offset, group_id) — schedule group close
	ip.RegisterBuiltinWithParams("schedule.close_group", []string{"bars_offset", "group_id"}, func(args []Value) Value {
		if !ip.AllowSideEffect("schedule.close_group") {
			return NaVal()
		}
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		b.ScheduleCloseGroup(int(args[0].Float()), int(args[1].Float()))
		return NaVal()
	})

	// ------- leg builder helpers -------

	// leg.buy(contract, qty) → [contract, "buy", qty]
	ip.RegisterBuiltin("leg.buy", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		return ArrayVal([]Value{args[0], StringVal("buy"), args[1]})
	})

	// leg.sell(contract, qty) → [contract, "sell", qty]
	ip.RegisterBuiltin("leg.sell", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		return ArrayVal([]Value{args[0], StringVal("sell"), args[1]})
	})
}

func selectOptionStrategies(ctx MarketContext, family string) []string {
	strategies := make([]string, 0, 3)
	add := func(name string) {
		for _, existing := range strategies {
			if existing == name {
				return
			}
		}
		strategies = append(strategies, name)
	}
	if family != "" && family != "trend" && family != "momentum" && family != "index" && family != "value" {
		return strategies
	}
	if family == "value" {
		selectValueOptionStrategies(ctx, add)
		return strategies
	}
	switch ctx.TrendState {
	case "up":
		if ctx.ValuationState == "overvalued" {
			if ctx.IVState == "low" {
				add(OptionStrategyCalendarSpread)
			} else {
				add(OptionStrategyBullCallSpread)
			}
			add(OptionStrategyBullPutSpread)
			return strategies
		}
		if ctx.HVState == "high" || ctx.IVState == "high" {
			add(OptionStrategySellPut)
			add(OptionStrategyBullPutSpread)
		} else {
			add(OptionStrategyBuyCall)
			add(OptionStrategyCalendarSpread)
		}
	case "range":
		if family == "index" && ctx.HVState == "high" {
			add(OptionStrategyIronCondor)
			add(OptionStrategyShortStrangle)
		} else if family == "index" && ctx.HVState == "low" {
			add(OptionStrategyBuyStraddle)
			add(OptionStrategyCalendarSpread)
		} else {
			add(OptionStrategyCalendarSpread)
		}
	case "down":
		if family == "index" || family == "trend" {
			add(OptionStrategyBuyPut)
			add(OptionStrategyBearCallSpread)
		}
	default:
		if ctx.IVState == "low" {
			add(OptionStrategyBuyCall)
		}
	}
	return strategies
}

func selectValueOptionStrategies(ctx MarketContext, add func(string)) {
	switch ctx.ValuationState {
	case "undervalued":
		if ctx.IVState == "high" {
			add(OptionStrategySellPut)
			add(OptionStrategyCoveredCall)
		} else if ctx.IVState == "low" {
			if ctx.HVState == "high" {
				add(OptionStrategyBuySkewedStraddle)
			} else {
				add(OptionStrategyBuyCall)
			}
		}
	case "overvalued":
		if ctx.IVState == "low" {
			add(OptionStrategyBuyPut)
		} else if ctx.IVState == "high" {
			add(OptionStrategySellCall)
			add(OptionStrategyBearCallSpread)
		}
	case "fair":
		if ctx.IVState == "high" {
			add(OptionStrategyShortStrangle)
			add(OptionStrategyIronCondor)
		} else if ctx.IVState == "low" {
			add(OptionStrategyCalendarSpread)
		}
	}
}

func buildOptionStrategyLegs(b OptionsBridge, chain interface{}, name string, qty, targetDelta float64) []Value {
	switch name {
	case OptionStrategyBuyCall:
		contract := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		return singleLeg(contract, "buy", qty)
	case OptionStrategyBuyPut:
		contract := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		return singleLeg(contract, "buy", qty)
	case OptionStrategySellPut:
		contract := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		return singleLeg(contract, "sell", qty)
	case OptionStrategySellCall, OptionStrategyCoveredCall:
		contract := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		return singleLeg(contract, "sell", qty)
	case OptionStrategyBullCallSpread:
		short := bestContractByDelta(b, b.ChainCalls(chain), targetDelta/2)
		long := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		return verticalLegs(b, long, short, "call", qty)
	case OptionStrategyBullPutSpread:
		short := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		long := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta/2)
		return verticalLegs(b, long, short, "put", qty)
	case OptionStrategyBearCallSpread:
		short := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		long := fartherOTMContract(b, b.ChainCalls(chain), short)
		return verticalLegs(b, long, short, "call", qty)
	case OptionStrategyShortStrangle:
		call := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		put := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		return twoLegs(call, "sell", put, "sell", qty)
	case OptionStrategyIronCondor:
		shortCall := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		longCall := fartherOTMContract(b, b.ChainCalls(chain), shortCall)
		shortPut := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		longPut := fartherOTMContract(b, b.ChainPuts(chain), shortPut)
		return fourLegs(longPut, "buy", shortPut, "sell", shortCall, "sell", longCall, "buy", qty)
	case OptionStrategyBuyStraddle:
		call := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		put := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		return twoLegs(call, "buy", put, "buy", qty)
	case OptionStrategyBuySkewedStraddle:
		call := bestContractByDelta(b, b.ChainCalls(chain), targetDelta/2)
		put := bestContractByDelta(b, b.ChainPuts(chain), -targetDelta)
		return twoLegs(call, "buy", put, "buy", qty)
	case OptionStrategyCalendarSpread:
		front := bestContractByDelta(b, b.ChainCalls(chain), targetDelta)
		back := fartherSameStrikeContract(b, b.ChainCalls(chain), front)
		if front == nil || back == nil {
			return nil
		}
		return []Value{ArrayVal([]Value{ObjVal(front), StringVal("sell"), FloatVal(qty)}), ArrayVal([]Value{ObjVal(back), StringVal("buy"), FloatVal(qty)})}
	default:
		return nil
	}
}

func twoLegs(first interface{}, firstSide string, second interface{}, secondSide string, qty float64) []Value {
	if first == nil || second == nil {
		return nil
	}
	return []Value{
		ArrayVal([]Value{ObjVal(first), StringVal(firstSide), FloatVal(qty)}),
		ArrayVal([]Value{ObjVal(second), StringVal(secondSide), FloatVal(qty)}),
	}
}

func fourLegs(first interface{}, firstSide string, second interface{}, secondSide string, third interface{}, thirdSide string, fourth interface{}, fourthSide string, qty float64) []Value {
	if first == nil || second == nil || third == nil || fourth == nil {
		return nil
	}
	return []Value{
		ArrayVal([]Value{ObjVal(first), StringVal(firstSide), FloatVal(qty)}),
		ArrayVal([]Value{ObjVal(second), StringVal(secondSide), FloatVal(qty)}),
		ArrayVal([]Value{ObjVal(third), StringVal(thirdSide), FloatVal(qty)}),
		ArrayVal([]Value{ObjVal(fourth), StringVal(fourthSide), FloatVal(qty)}),
	}
}

func bestContractByDelta(b OptionsBridge, chain interface{}, targetDelta float64) interface{} {
	contracts := b.ChainSortByDelta(chain, targetDelta)
	for _, contract := range contracts {
		if contractHasPrice(b, contract) {
			return contract
		}
	}
	return nil
}

func contractHasPrice(b OptionsBridge, contract interface{}) bool {
	if contract == nil {
		return false
	}
	return b.ContractMark(contract) > 0 || b.ContractBid(contract) > 0 || b.ContractAsk(contract) > 0
}

func singleLeg(contract interface{}, side string, qty float64) []Value {
	if contract == nil {
		return nil
	}
	return []Value{ArrayVal([]Value{ObjVal(contract), StringVal(side), FloatVal(qty)})}
}

func verticalLegs(b OptionsBridge, long, short interface{}, right string, qty float64) []Value {
	if long == nil || short == nil || !strings.EqualFold(b.ContractType(long), right) || !strings.EqualFold(b.ContractType(short), right) {
		return nil
	}
	return []Value{ArrayVal([]Value{ObjVal(long), StringVal("buy"), FloatVal(qty)}), ArrayVal([]Value{ObjVal(short), StringVal("sell"), FloatVal(qty)})}
}

func fartherSameStrikeContract(b OptionsBridge, chain interface{}, front interface{}) interface{} {
	if front == nil {
		return nil
	}
	targetStrike := b.ContractStrike(front)
	frontDTE := b.ContractDTE(front)
	contracts := b.ChainSortByDelta(chain, b.ContractDelta(front))
	for _, candidate := range contracts {
		if candidate == nil || !contractHasPrice(b, candidate) {
			continue
		}
		if b.ContractStrike(candidate) == targetStrike && b.ContractDTE(candidate) >= frontDTE+7 {
			return candidate
		}
	}
	return nil
}

func fartherOTMContract(b OptionsBridge, chain interface{}, anchor interface{}) interface{} {
	if anchor == nil {
		return nil
	}
	anchorStrike := b.ContractStrike(anchor)
	anchorType := strings.ToLower(strings.TrimSpace(b.ContractType(anchor)))
	contracts := b.ChainSortByDelta(chain, b.ContractDelta(anchor)/2)
	for _, candidate := range contracts {
		if candidate == nil || !contractHasPrice(b, candidate) || !strings.EqualFold(b.ContractType(candidate), anchorType) {
			continue
		}
		strike := b.ContractStrike(candidate)
		if anchorType == "call" && strike > anchorStrike {
			return candidate
		}
		if anchorType == "put" && strike < anchorStrike {
			return candidate
		}
	}
	return nil
}

// parseLegInputs converts an array of [contract, side, qty] values to SpreadLegInput.
func parseLegInputs(legs []Value) []SpreadLegInput {
	var out []SpreadLegInput
	for _, l := range legs {
		arr := l.Array()
		if len(arr) < 3 {
			continue
		}
		out = append(out, SpreadLegInput{
			Contract: arr[0].Obj(),
			Side:     arr[1].Str(),
			Qty:      arr[2].Float(),
		})
	}
	return out
}

func spreadLegsMatchScope(b OptionsBridge, legs []SpreadLegInput, market, underlying string) bool {
	targetMarket := strings.TrimSpace(market)
	targetUnderlying := strings.TrimSpace(underlying)
	if targetMarket == "" || targetUnderlying == "" {
		return false
	}
	for _, leg := range legs {
		if leg.Contract == nil {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(b.ContractMarket(leg.Contract)), targetMarket) {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(b.ContractUnderlying(leg.Contract)), targetUnderlying) {
			return false
		}
	}
	return true
}
