package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func buildReportSeries(primary map[string][]float64, primaryLen int, factorColumns []map[string][]float64, factorAlignMaps [][]int, factors []factorRegistration) map[string][]float64 {
	if len(primary) == 0 && len(factorColumns) == 0 {
		return nil
	}

	merged := make(map[string][]float64, len(primary))
	for name, data := range primary {
		merged[name] = data
	}

	limit := len(factorColumns)
	if len(factors) < limit {
		limit = len(factors)
	}
	for i := 0; i < limit; i++ {
		ref := factors[i].ref
		for name, data := range factorColumns[i] {
			aligned := data
			if i < len(factorAlignMaps) && factorAlignMaps[i] != nil {
				aligned = alignColumn(data, factorAlignMaps[i], primaryLen)
			}
			if _, exists := merged[name]; !exists {
				merged[name] = aligned
			}
			merged[factorSeriesKey(ref, name)] = aligned
		}
	}

	return merged
}

func factorSeriesKey(ref FactorRef, name string) string {
	return "factor." + ref.Name + "." + ref.Interval + "." + name
}

// Replayer runs the bar-by-bar strategy execution loop on prepared data.
type Replayer struct {
	config        Config
	chainProvider OptionsChainProvider
}

// Replay runs a strategy against pre-loaded data with the given parameters.
// This is the core bar-replay loop shared by Engine.Run and Engine.RunBatch.
func (r *Replayer) Replay(prepared *PreparedData, strategy Strategy, params map[string]interface{}) (*Result, error) {
	// Init strategy with params to pick up parameter-specific setup
	setupCtx := NewSetupContext(prepared.PrimaryRef.Market, prepared.PrimaryRef.Symbol, prepared.PrimaryRef.Interval)
	for k, v := range params {
		setupCtx.params[k] = v
	}
	if err := strategy.Init(setupCtx); err != nil {
		return nil, fmt.Errorf("strategy init: %w", err)
	}

	// Build per-security column maps (shallow copy — shares underlying arrays)
	secColumns := make([]map[string][]float64, len(prepared.Securities))
	for i := range prepared.Securities {
		ds := prepared.SecDataSets[i]
		cols := make(map[string][]float64, len(ds.Columns))
		for name, data := range ds.Columns {
			cols[name] = data
		}
		secColumns[i] = cols
	}

	factorColumns := make([]map[string][]float64, len(prepared.Factors))
	for i := range prepared.Factors {
		ds := prepared.FactorDataSets[i]
		cols := make(map[string][]float64, len(ds.Columns))
		for name, data := range ds.Columns {
			cols[name] = data
		}
		factorColumns[i] = cols
	}

	// Bar replay
	broker := NewBroker(r.config)

	accessors := make([]*SecurityAccessor, len(prepared.Securities))
	for i := range prepared.Securities {
		accessors[i] = &SecurityAccessor{
			data:     secColumns[i],
			alignMap: prepared.AlignMaps[i],
		}
	}

	factorAccessors := make([]*SecurityAccessor, len(prepared.Factors))
	for i := range prepared.Factors {
		factorAccessors[i] = &SecurityAccessor{
			data:     factorColumns[i],
			alignMap: prepared.FactorAlignMaps[i],
		}
	}

	broker.SetPriceFunc(func(ref SecurityRef) BarPrices {
		idx := ref.Index
		if idx < 0 || idx >= len(accessors) {
			return BarPrices{}
		}
		acc := accessors[idx]
		barIdx := acc.barIndex
		if acc.alignMap != nil && barIdx >= 0 && barIdx < len(acc.alignMap) {
			barIdx = acc.alignMap[barIdx]
		}
		if barIdx < 0 {
			return BarPrices{}
		}
		cols := secColumns[idx]
		getVal := func(name string) float64 {
			if col, ok := cols[name]; ok && barIdx < len(col) {
				return col[barIdx]
			}
			return math.NaN()
		}
		return BarPrices{
			Open:     getVal("open"),
			High:     getVal("high"),
			Low:      getVal("low"),
			Close:    getVal("close"),
			BidOpen:  getVal("bid_open"),
			BidClose: getVal("bid_close"),
			AskOpen:  getVal("ask_open"),
			AskClose: getVal("ask_close"),
		}
	})

	nBars := prepared.PrimaryDS.Len
	equityCurve := make([]float64, nBars)

	spreadTracker := NewSpreadTracker()
	spreadGroupTracker := NewSpreadGroupTracker()
	var scheduledActions []ScheduledAction
	spreadPricing := DefaultSpreadPricingConfig()
	if provider, ok := strategy.(SpreadPricingProvider); ok {
		spreadPricing = provider.SpreadPricingConfig().WithDefaults()
	}
	var reportColumns []ReportColumn
	if provider, ok := strategy.(ReportColumnProvider); ok {
		reportColumns = provider.ReportColumns()
	}

	barCtx := &BarContext{
		primary:            secColumns[0],
		securities:         accessors,
		factors:            factorAccessors,
		broker:             broker,
		params:             setupCtx.params,
		primaryRef:         prepared.PrimaryRef,
		chainProvider:      r.chainProvider,
		spreadTracker:      spreadTracker,
		spreadGroupTracker: spreadGroupTracker,
		scheduledActions:   &scheduledActions,
	}

	secRefList := make([]SecurityRef, len(prepared.Securities))
	for i, sec := range prepared.Securities {
		secRefList[i] = sec.ref
	}
	barCtx.secRefs = secRefList

	factorRefList := make([]FactorRef, len(prepared.Factors))
	for i, factor := range prepared.Factors {
		factorRefList[i] = factor.ref
	}
	barCtx.factorRefs = factorRefList

	defaultSlipPct := r.config.SlippagePct
	// Pre-allocate a single contractMap to reuse across bars, avoiding a
	// per-bar allocation when an OptionsChainProvider is configured.
	var contractMap map[string]OptionContract
	if r.chainProvider != nil {
		contractMap = make(map[string]OptionContract, 256)
	}

	for i := 0; i < nBars; i++ {
		barCtx.barIndex = i
		barCtx.barTime = prepared.PrimaryDS.Timestamps[i]

		for _, acc := range accessors {
			acc.barIndex = i
		}

		if i > 0 {
			broker.ProcessPending(i, prepared.PrimaryDS.Timestamps[i])
		}

		if r.chainProvider != nil {
			clear(contractMap)
			contracts := r.chainProvider.AvailableContracts(prepared.PrimaryDS.Timestamps[i])
			for _, c := range contracts {
				contractMap[c.Symbol] = c
			}
		}

		if len(scheduledActions) > 0 {
			barOpen := secColumns[0]["open"][i]
			barHigh := secColumns[0]["high"][i]
			barLow := secColumns[0]["low"][i]

			var remaining []ScheduledAction
			for _, sa := range scheduledActions {
				if !prepared.PrimaryDS.Timestamps[i].Before(sa.TriggerTime) {
					if !triggeredByBar(sa, barOpen, barHigh, barLow) {
						remaining = append(remaining, sa)
						continue
					}
					switch sa.ActionType {
					case ScheduleOpenSpread:
						if len(sa.OpenLegs) == 0 {
							continue
						}
						tag := sa.OpenTag
						legs := make([]SpreadLeg, len(sa.OpenLegs))
						for legIndex := range sa.OpenLegs {
							leg := sa.OpenLegs[legIndex]
							contract := resolveSpreadContract(leg.Contract, contractMap)
							entryPrice := spreadPricing.EntryMode.EntryPrice(leg.Side, contract)
							entryPrice = applySlippage(entryPrice, leg.Side, sa.SlippagePct, defaultSlipPct)
							if math.IsNaN(entryPrice) || entryPrice <= 0 {
								legs = nil
								break
							}
							leg.Contract = contract
							leg.EntryPrice = entryPrice
							leg.EntryTime = prepared.PrimaryDS.Timestamps[i]
							legs[legIndex] = leg
							if legIndex == 0 {
								tag = appendDeltaNote(tag, "exec_", contract.Delta)
							}
						}
						if len(legs) > 0 {
							barCtx.OpenSpreadWithRef(legs, tag, sa.OpenRef)
						}
					case ScheduleSecurityOrder:
						if sa.SecurityOrder.Type == MarketOrder && (sa.SecurityOrder.Qty > 0 || sa.SecurityOrder.Notional > 0) {
							broker.ExecuteOrderNow(sa.SecurityOrder, i, prepared.PrimaryDS.Timestamps[i])
						}
					case ScheduleCloseLeg:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && sa.LegIndex >= 0 && sa.LegIndex < len(sp.Legs) && !sp.Legs[sa.LegIndex].Closed {
							contract := resolveSpreadContract(sp.Legs[sa.LegIndex].Contract, contractMap)
							entrySide := sp.Legs[sa.LegIndex].Side
							closePrice := spreadPricing.ExitMode.ExitPrice(entrySide, contract)
							exitSide := Sell
							if entrySide == Sell {
								exitSide = Buy
							}
							closePrice = applySlippage(closePrice, exitSide, sa.SlippagePct, defaultSlipPct)
							reason := appendDeltaNote(sa.CloseReason, "exec_", contract.Delta)
							closeCustomData := upsertTradeCustomData(sa.CloseCustomData, TradeCustomDataKeyCloseTriggerTime, sa.TriggerTime.UTC().Format(time.RFC3339Nano))
							barCtx.CloseSpreadLegWithReasonAndData(sa.SpreadID, sa.LegIndex, closePrice, reason, closeCustomData)
						}
					case ScheduleCloseSpread:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && !sp.IsFullyClosed() {
							for legIndex := range sp.Legs {
								if sp.Legs[legIndex].Closed {
									continue
								}
								contract := resolveSpreadContract(sp.Legs[legIndex].Contract, contractMap)
								entrySide := sp.Legs[legIndex].Side
								closePrice := spreadPricing.ExitMode.ExitPrice(entrySide, contract)
								exitSide := Sell
								if entrySide == Sell {
									exitSide = Buy
								}
								closePrice = applySlippage(closePrice, exitSide, sa.SlippagePct, defaultSlipPct)
								reason := appendDeltaNote(sa.CloseReason, "exec_", contract.Delta)
								closeCustomData := upsertTradeCustomData(sa.CloseCustomData, TradeCustomDataKeyCloseTriggerTime, sa.TriggerTime.UTC().Format(time.RFC3339Nano))
								barCtx.CloseSpreadLegWithReasonAndData(sa.SpreadID, legIndex, closePrice, reason, closeCustomData)
							}
						}
					}
				} else {
					remaining = append(remaining, sa)
				}
			}
			scheduledActions = remaining
		}

		strategy.OnBar(barCtx)

		spreadMarketValue := 0.0
		for _, sp := range spreadTracker.OpenSpreads() {
			for _, leg := range sp.Legs {
				if leg.Closed {
					continue
				}
				contract := resolveSpreadContract(leg.Contract, contractMap)
				markPrice := spreadPricing.ValuationMode.ExitPrice(leg.Side, contract)
				if leg.Side == Buy {
					spreadMarketValue += leg.Qty * markPrice
				} else {
					spreadMarketValue -= leg.Qty * markPrice
				}
			}
		}
		equityCurve[i] = broker.Equity() + spreadMarketValue
	}

	result := computeResult(
		strategy.Name(),
		broker.Trades(),
		equityCurve,
		prepared.PrimaryDS.Timestamps,
		r.config.InitialCapital,
		r.config.AccountUnit,
		buildReportSeries(secColumns[0], prepared.PrimaryDS.Len, factorColumns, prepared.FactorAlignMaps, prepared.Factors),
		reportColumns,
	)

	result.SpreadSummary = computeSpreadSummary(spreadTracker)
	result.SpreadPositions = buildSpreadPositionReports(spreadTracker, result.EndTime)
	result.SpreadGroups = buildSpreadGroupReports(spreadGroupTracker, spreadTracker, result.EndTime)

	return result, nil
}

// resolveSpreadContract returns the updated contract from the map if available.
func resolveSpreadContract(contract OptionContract, contractMap map[string]OptionContract) OptionContract {
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

// applySlippage adjusts a price for slippage based on trade side.
func applySlippage(price float64, side Side, actionSlip, defaultSlip float64) float64 {
	if math.IsNaN(price) || price <= 0 {
		return price
	}
	slipPct := actionSlip
	if slipPct <= 0 {
		slipPct = defaultSlip
	}
	if slipPct <= 0 {
		return price
	}
	slip := price * slipPct
	if side == Buy {
		return price + slip
	}
	return price - slip
}

// appendDeltaNote appends a delta annotation to a note string.
func appendDeltaNote(base, label string, delta float64) string {
	if math.IsNaN(delta) {
		return base
	}
	note := fmt.Sprintf("%sDelta=%.4f", label, delta)
	if strings.TrimSpace(base) == "" {
		return note
	}
	return base + " | " + note
}

// triggeredByBar checks whether a scheduled action should trigger on the current bar.
func triggeredByBar(action ScheduledAction, barOpen, barHigh, barLow float64) bool {
	if action.OrderType == SpreadOrderMarket {
		return true
	}
	if math.IsNaN(action.TriggerPrice) || action.TriggerPrice <= 0 {
		return false
	}
	low := barLow
	high := barHigh
	if math.IsNaN(low) {
		low = barOpen
	}
	if math.IsNaN(high) {
		high = barOpen
	}
	if math.IsNaN(low) || math.IsNaN(high) {
		return false
	}

	side := action.TriggerSide
	if side != Buy && side != Sell {
		side = Buy
	}

	switch action.OrderType {
	case SpreadOrderStop:
		if side == Buy {
			return high >= action.TriggerPrice
		}
		return low <= action.TriggerPrice
	case SpreadOrderLimit:
		if side == Buy {
			return low <= action.TriggerPrice
		}
		return high >= action.TriggerPrice
	default:
		return false
	}
}
