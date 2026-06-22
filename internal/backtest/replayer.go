package backtest

import (
	"fmt"
	"log/slog"
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

func mergeReportSeries(base map[string][]float64, extra map[string][]float64) map[string][]float64 {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		merged := make(map[string][]float64, len(extra))
		for name, data := range extra {
			merged[name] = data
		}
		return merged
	}
	merged := make(map[string][]float64, len(base)+len(extra))
	for name, data := range base {
		merged[name] = data
	}
	for name, data := range extra {
		merged[name] = data
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
	progress      ProgressFunc
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
	replayStartedAt := time.Now()
	emitProgress(r.progress, ProgressUpdate{
		Phase:     ProgressPhaseReplay,
		Current:   0,
		Total:     nBars,
		Message:   "replaying bars",
		StartedAt: replayStartedAt,
		Completed: false,
	})

	spreadTracker := NewSpreadTracker()
	spreadGroupTracker := NewSpreadGroupTracker()
	spreadGroupEquity := newSpreadGroupEquityAccumulator()
	var scheduledActions []ScheduledAction
	spreadPricing := DefaultSpreadPricingConfig()
	if provider, ok := strategy.(SpreadPricingProvider); ok {
		spreadPricing = provider.SpreadPricingConfig().WithDefaults()
	}
	warningSink := &replayWarningSink{}
	valuationState := newSpreadValuationState(r.config.ValuationMissingPolicy, warningSink)

	barCtx := &BarContext{
		barTimes:           prepared.PrimaryDS.Timestamps,
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
	progressStep := nBars / 200
	if progressStep < 1 {
		progressStep = 1
	}
	lastProgressAt := time.Now()

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
				contractMap[ContractLookupKey(c.ChainMarket(), c.ChainUnderlying(), c.Symbol)] = c
			}
			refreshOpenSpreadContracts(spreadTracker, contractMap)
		}

		if len(scheduledActions) > 0 {
			// Safely get primary bar prices for trigger checks
			getBarVal := func(name string) float64 {
				if col, ok := secColumns[0][name]; ok && i < len(col) {
					return col[i]
				}
				return math.NaN()
			}
			barOpen := getBarVal("open")
			barHigh := getBarVal("high")
			barLow := getBarVal("low")

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
								slog.Warn("spread leg execution skipped: invalid entry price",
									"tag", sa.OpenTag,
									"legIndex", legIndex,
									"contract", contract.Symbol,
									"entryPrice", entryPrice,
									"barTime", prepared.PrimaryDS.Timestamps[i],
								)
								warningSink.add(BacktestWarning{
									Severity:  WarningSeverityWarning,
									Code:      "spread.open_invalid_entry_price",
									Message:   "scheduled spread leg open skipped because entry price was invalid",
									BarIndex:  &i,
									Timestamp: &barCtx.barTime,
									LegIndex:  &legIndex,
									Symbol:    contract.Symbol,
								})
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
							if sa.OpenGroupID > 0 {
								barCtx.OpenSpreadInGroupWithRef(legs, tag, sa.OpenRef, sa.OpenGroupID)
							} else {
								barCtx.OpenSpreadWithRef(legs, tag, sa.OpenRef)
							}
						}
					case ScheduleSecurityOrder:
						if sa.SecurityOrder.Type == MarketOrder && (sa.SecurityOrder.Qty > 0 || sa.SecurityOrder.Notional > 0) {
							broker.ExecuteOrderNow(sa.SecurityOrder, i, prepared.PrimaryDS.Timestamps[i])
						}
					case ScheduleCloseLeg:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && sa.LegIndex >= 0 && sa.LegIndex < len(sp.Legs) && !sp.Legs[sa.LegIndex].Closed {
							closePrice, reason, closeCustomData := scheduledCloseLegFill(sa, sp.Legs[sa.LegIndex], contractMap, spreadPricing, defaultSlipPct)
							barCtx.CloseSpreadLegWithReasonAndData(sa.SpreadID, sa.LegIndex, closePrice, reason, closeCustomData)
						}
					case ScheduleCloseSpread:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && !sp.IsFullyClosed() {
							for legIndex := range sp.Legs {
								if sp.Legs[legIndex].Closed {
									continue
								}
								closePrice, reason, closeCustomData := scheduledCloseLegFill(sa, sp.Legs[legIndex], contractMap, spreadPricing, defaultSlipPct)
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
		forceCloseExpiredOptionLegs(barCtx, spreadTracker, contractMap, spreadPricing, valuationState, i, prepared.PrimaryDS.Timestamps[i])

		spreadMarketValue := calculateSpreadMarketValue(spreadTracker, contractMap, spreadPricing, valuationState, i, prepared.PrimaryDS.Timestamps[i])
		equityCurve[i] = broker.Equity() + spreadMarketValue
		spreadGroupEquity.Observe(spreadGroupTracker, spreadTracker, contractMap, spreadPricing, prepared.PrimaryDS.Timestamps[i])

		current := i + 1
		if r.progress != nil && (current == nBars || current%progressStep == 0) {
			now := time.Now()
			if current == nBars || now.Sub(lastProgressAt) >= 150*time.Millisecond {
				emitProgress(r.progress, ProgressUpdate{
					Phase:     ProgressPhaseReplay,
					Current:   current,
					Total:     nBars,
					Message:   prepared.PrimaryDS.Timestamps[i].UTC().Format(time.RFC3339),
					StartedAt: replayStartedAt,
					Completed: current == nBars,
				})
				lastProgressAt = now
			}
		}
	}

	var reportColumns []ReportColumn
	if provider, ok := strategy.(ReportColumnProvider); ok {
		reportColumns = provider.ReportColumns()
	}

	result := computeResult(
		strategy.Name(),
		broker.Trades(),
		equityCurve,
		prepared.PrimaryDS.Timestamps,
		r.config.InitialCapital,
		r.config.AccountUnit,
		mergeReportSeries(
			buildReportSeries(secColumns[0], prepared.PrimaryDS.Len, factorColumns, prepared.FactorAlignMaps, prepared.Factors),
			func() map[string][]float64 {
				if provider, ok := strategy.(ReportSeriesProvider); ok {
					return provider.ReportSeries()
				}
				return nil
			}(),
		),
		reportColumns,
	)

	result.SpreadSummary = computeSpreadSummary(spreadTracker)
	result.SpreadPositions = buildSpreadPositionReports(spreadTracker, result.EndTime)
	result.TotalFees += totalSpreadFees(spreadTracker)
	result.SpreadGroups = buildSpreadGroupReports(spreadGroupTracker, spreadTracker, result.EndTime)
	result.SpreadGroups = applySpreadGroupEquityStats(result.SpreadGroups, spreadGroupEquity.Snapshot())
	result.Warnings = warningSink.all()

	return result, nil
}

func totalSpreadFees(tracker *SpreadTracker) float64 {
	if tracker == nil {
		return 0
	}
	total := 0.0
	for _, spread := range tracker.All() {
		if spread == nil {
			continue
		}
		total += spread.TotalFees()
	}
	return total
}

func scheduledCloseLegFill(sa ScheduledAction, leg SpreadLeg, contractMap map[string]OptionContract, spreadPricing SpreadPricingConfig, defaultSlipPct float64) (float64, string, []TradeCustomData) {
	contract := resolveSpreadContract(leg.Contract, contractMap)
	closePrice := spreadPricing.ExitMode.ExitPrice(leg.Side, contract)
	exitSide := Sell
	if leg.Side == Sell {
		exitSide = Buy
	}
	closePrice = applySlippage(closePrice, exitSide, sa.SlippagePct, defaultSlipPct)
	reason := appendDeltaNote(sa.CloseReason, "exec_", contract.Delta)
	customData := upsertTradeCustomData(sa.CloseCustomData, TradeCustomDataKeyCloseTriggerTime, sa.TriggerTime.UTC().Format(time.RFC3339Nano))
	return closePrice, reason, customData
}

// resolveSpreadContract returns the updated contract from the map if available.
func resolveSpreadContract(contract OptionContract, contractMap map[string]OptionContract) OptionContract {
	for _, key := range ContractLookupKeys(contract) {
		if updated, ok := contractMap[key]; ok {
			return updated
		}
	}
	return contract
}

func refreshOpenSpreadContracts(spreadTracker *SpreadTracker, contractMap map[string]OptionContract) {
	if spreadTracker == nil || len(contractMap) == 0 {
		return
	}
	for _, sp := range spreadTracker.OpenSpreads() {
		for legIndex := range sp.Legs {
			if sp.Legs[legIndex].Closed {
				continue
			}
			for _, key := range ContractLookupKeys(sp.Legs[legIndex].Contract) {
				if updated, ok := contractMap[key]; ok {
					sp.Legs[legIndex].Contract = updated
					break
				}
			}
		}
	}
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
