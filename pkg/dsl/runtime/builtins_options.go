package runtime

// OptionsBridge extends Bridge with options trading capabilities.
// The interpreter checks for this at runtime via type assertion.
type OptionsBridge interface {
	// OptionsChain returns the current bar's options chain as an opaque object.
	OptionsChain() interface{}
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
	ContractType(c interface{}) string // "call" or "put"
	ContractStrike(c interface{}) float64
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
	// CloseSpreadLeg closes a single leg.
	CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool
	// SpreadGet returns spread info as [id, tag, barsHeld, realizedPnl, isOpen].
	SpreadGet(spreadID int) SpreadInfo
	// OpenSpreads returns IDs of all open spreads.
	OpenSpreads() []int
	// SpreadPnL returns unrealized PnL of a spread.
	SpreadPnL(spreadID int) float64

	// Spread groups.
	GroupOpen(tag string, initAmount, decayFactor float64) int
	GroupClose(groupID int)
	GroupGet(groupID int) GroupInfo
	GroupAddSpread(groupID, spreadID int)
	OpenGroups() []int

	// Scheduling.
	ScheduleCloseSpread(triggerBarOffset int, spreadID int)
	ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int)
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
	ID        int
	Tag       string
	Amount    float64
	RollCount int
	IsClosed  bool
	SpreadIDs []int
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

	// ------- contract field accessors -------
	// contract.symbol, contract.type, contract.strike, contract.dte,
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

	// spread.open_in_group(legs_array, tag, group_id) → spread_id
	ip.RegisterBuiltin("spread.open_in_group", func(args []Value) Value {
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

	// spread.close(spread_id)
	ip.RegisterBuiltin("spread.close", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 1 {
			return NaVal()
		}
		b.CloseSpread(int(args[0].Float()))
		return NaVal()
	})

	// spread.close_leg(spread_id, leg_index, close_price) → bool
	ip.RegisterBuiltin("spread.close_leg", func(args []Value) Value {
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
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		id := b.GroupOpen(args[0].Str(), args[1].Float(), args[2].Float())
		return FloatVal(float64(id))
	})

	// group.close(group_id)
	ip.RegisterBuiltin("group.close", func(args []Value) Value {
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
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		b.GroupAddSpread(int(args[0].Float()), int(args[1].Float()))
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

	// schedule.close_spread(bars_offset, spread_id)
	ip.RegisterBuiltin("schedule.close_spread", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 2 {
			return NaVal()
		}
		b.ScheduleCloseSpread(int(args[0].Float()), int(args[1].Float()))
		return NaVal()
	})

	// schedule.close_leg(bars_offset, spread_id, leg_index)
	ip.RegisterBuiltin("schedule.close_leg", func(args []Value) Value {
		b := ob()
		if b == nil || len(args) < 3 {
			return NaVal()
		}
		b.ScheduleCloseLeg(int(args[0].Float()), int(args[1].Float()), int(args[2].Float()))
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
