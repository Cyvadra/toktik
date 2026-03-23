package backtest

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Config controls engine behavior.
type Config struct {
	InitialCapital  float64
	AccountUnit     string
	CommissionModel CommissionModel
	CommissionValue float64
	SlippagePct     float64
	ExecutionMode   ExecutionPriceModel
	ValuationMode   ValuationPriceModel
	TriggerMode     TriggerPriceMode
}

// Engine orchestrates backtests: loads data, computes indicators, and replays bars.
type Engine struct {
	config        Config
	feeds         map[string]DataFeed // market → DataFeed
	factorFeeds   map[string]FactorFeed
	chainProvider OptionsChainProvider
}

// NewEngine creates a backtest engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	return &Engine{
		config:      cfg,
		feeds:       make(map[string]DataFeed),
		factorFeeds: make(map[string]FactorFeed),
	}
}

// RegisterDataFeed associates a DataFeed with a market name.
func (e *Engine) RegisterDataFeed(market string, feed DataFeed) {
	e.feeds[market] = feed
}

// RegisterFactorFeed associates an external factor feed with a factor name.
func (e *Engine) RegisterFactorFeed(name string, feed FactorFeed) {
	e.factorFeeds[name] = feed
}

// SetOptionsChainProvider sets the provider that supplies option chain data
// during bar replay. This enables strategies to dynamically query available
// options at each bar.
func (e *Engine) SetOptionsChainProvider(p OptionsChainProvider) {
	e.chainProvider = p
}

// PreparedData holds pre-loaded and aligned data that can be reused across
// multiple strategy runs. Created by Engine.Prepare, consumed by Engine.replay
// and Engine.RunBatch.
type PreparedData struct {
	PrimaryDS       *DataSet
	SecDataSets     []*DataSet
	AlignMaps       [][]int
	Securities      []securityRegistration
	FactorDataSets  []*DataSet
	FactorAlignMaps [][]int
	Factors         []factorRegistration
	PrimaryRef      SecurityRef
}

// Prepare loads data and computes base indicators for a strategy, returning a
// PreparedData that can be replayed many times with different parameters.
// The strategy is Init'd once to discover security and indicator registrations.
func (e *Engine) Prepare(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*PreparedData, error) {
	setupCtx := NewSetupContext(market, symbol, interval)
	for k, v := range params {
		setupCtx.params[k] = v
	}
	if err := strategy.Init(setupCtx); err != nil {
		return nil, fmt.Errorf("strategy init: %w", err)
	}

	primaryFeed, ok := e.feeds[market]
	if !ok {
		return nil, fmt.Errorf("no DataFeed registered for market %q", market)
	}

	primaryDS, err := primaryFeed.Load(ctx, DataRequest{
		Market: market, Symbol: symbol, Interval: interval, From: from, To: to,
	})
	if err != nil {
		return nil, fmt.Errorf("load primary data: %w", err)
	}
	if primaryDS.Len == 0 {
		return nil, fmt.Errorf("no data returned for %s/%s/%s", market, symbol, interval)
	}

	// Load secondary datasets in parallel
	type secResult struct {
		index int
		ds    *DataSet
		err   error
	}

	secCount := len(setupCtx.securities) - 1
	secDataSets := make([]*DataSet, len(setupCtx.securities))
	secDataSets[0] = primaryDS

	if secCount > 0 {
		results := make(chan secResult, secCount)
		var wg sync.WaitGroup

		for i := 1; i < len(setupCtx.securities); i++ {
			sec := setupCtx.securities[i]
			feed, ok := e.feeds[sec.ref.Market]
			if !ok {
				return nil, fmt.Errorf("no DataFeed registered for market %q (security %s)", sec.ref.Market, sec.ref.Symbol)
			}

			wg.Add(1)
			go func(idx int, f DataFeed, r SecurityRef) {
				defer wg.Done()
				ds, err := f.Load(ctx, DataRequest{
					Market: r.Market, Symbol: r.Symbol, Interval: r.Interval, From: from, To: to,
				})
				results <- secResult{index: idx, ds: ds, err: err}
			}(i, feed, sec.ref)
		}

		go func() { wg.Wait(); close(results) }()

		for r := range results {
			if r.err != nil {
				sec := setupCtx.securities[r.index]
				return nil, fmt.Errorf("load security %s/%s: %w", sec.ref.Market, sec.ref.Symbol, r.err)
			}
			secDataSets[r.index] = r.ds
		}
	}

	// Align secondary data
	alignMaps := make([][]int, len(setupCtx.securities))
	alignMaps[0] = nil
	for i := 1; i < len(secDataSets); i++ {
		alignMaps[i] = alignSeries(primaryDS, secDataSets[i])
	}

	// Load external factor datasets in parallel
	type factorResult struct {
		index int
		ds    *DataSet
		err   error
	}

	factorDataSets := make([]*DataSet, len(setupCtx.factors))
	factorAlignMaps := make([][]int, len(setupCtx.factors))

	if len(setupCtx.factors) > 0 {
		results := make(chan factorResult, len(setupCtx.factors))
		var wg sync.WaitGroup

		for i := range setupCtx.factors {
			factor := setupCtx.factors[i]
			feed, ok := e.factorFeeds[factor.ref.Name]
			if !ok {
				return nil, fmt.Errorf("no FactorFeed registered for factor %q", factor.ref.Name)
			}

			wg.Add(1)
			go func(idx int, f FactorFeed, r FactorRef) {
				defer wg.Done()
				ds, err := f.Load(ctx, FactorRequest{
					Name: r.Name, Interval: r.Interval, From: from, To: to,
				})
				results <- factorResult{index: idx, ds: ds, err: err}
			}(i, feed, factor.ref)
		}

		go func() { wg.Wait(); close(results) }()

		for r := range results {
			if r.err != nil {
				factor := setupCtx.factors[r.index]
				return nil, fmt.Errorf("load factor %s/%s: %w", factor.ref.Name, factor.ref.Interval, r.err)
			}
			factorDataSets[r.index] = r.ds
		}

		for i := range factorDataSets {
			factorAlignMaps[i] = alignSeries(primaryDS, factorDataSets[i])
		}
	}

	// Compute indicators
	for i, sec := range setupCtx.securities {
		ds := secDataSets[i]
		if len(sec.inds) > 0 {
			if err := resolveIndicators(sec.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for security[%d] %s: %w", i, sec.ref.Symbol, err)
			}
		}
	}

	for i, factor := range setupCtx.factors {
		ds := factorDataSets[i]
		if len(factor.inds) > 0 {
			if err := resolveIndicators(factor.inds, ds.Columns); err != nil {
				return nil, fmt.Errorf("indicators for factor[%d] %s: %w", i, factor.ref.Name, err)
			}
		}
	}

	if preloader, ok := strategy.(StrategyPreloader); ok {
		preloadCtx := newPreloadContext(
			setupCtx.primaryRef,
			setupCtx.securities,
			secDataSets,
			alignMaps,
			setupCtx.factors,
			factorDataSets,
			factorAlignMaps,
			setupCtx.params,
		)
		if err := preloader.Preload(preloadCtx); err != nil {
			return nil, fmt.Errorf("strategy preload: %w", err)
		}
	}

	return &PreparedData{
		PrimaryDS:       primaryDS,
		SecDataSets:     secDataSets,
		AlignMaps:       alignMaps,
		Securities:      setupCtx.securities,
		FactorDataSets:  factorDataSets,
		FactorAlignMaps: factorAlignMaps,
		Factors:         setupCtx.factors,
		PrimaryRef:      setupCtx.primaryRef,
	}, nil
}

// replay runs a strategy against pre-loaded data with the given parameters.
// This is the core bar-replay loop shared by Run and RunBatch.
func (e *Engine) replay(prepared *PreparedData, strategy Strategy, params map[string]interface{}) (*Result, error) {
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
	broker := NewBroker(e.config)

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
		primary:          secColumns[0],
		securities:       accessors,
		factors:          factorAccessors,
		broker:           broker,
		params:           setupCtx.params,
		primaryRef:       prepared.PrimaryRef,
		chainProvider:    e.chainProvider,
		spreadTracker:    spreadTracker,
		scheduledActions: &scheduledActions,
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

	currentSpreadContract := func(contract OptionContract, contractMap map[string]OptionContract) OptionContract {
		if updated, ok := contractMap[contract.Symbol]; ok {
			return updated
		}
		return contract
	}

	applySpreadSlippage := func(price float64, side Side, actionSlip float64) float64 {
		if math.IsNaN(price) || price <= 0 {
			return price
		}
		slipPct := actionSlip
		if slipPct <= 0 {
			slipPct = e.config.SlippagePct
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

	appendDeltaNote := func(base, label string, delta float64) string {
		if math.IsNaN(delta) {
			return base
		}
		note := fmt.Sprintf("%sDelta=%.4f", label, delta)
		if strings.TrimSpace(base) == "" {
			return note
		}
		return base + " | " + note
	}

	triggeredByBar := func(action ScheduledAction, barOpen, barHigh, barLow float64) bool {
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

	for i := 0; i < nBars; i++ {
		barCtx.barIndex = i
		barCtx.barTime = prepared.PrimaryDS.Timestamps[i]

		for _, acc := range accessors {
			acc.barIndex = i
		}

		if i > 0 {
			broker.ProcessPending(i, prepared.PrimaryDS.Timestamps[i])
		}

		var contractMap map[string]OptionContract
		if e.chainProvider != nil {
			contracts := e.chainProvider.AvailableContracts(prepared.PrimaryDS.Timestamps[i])
			contractMap = make(map[string]OptionContract, len(contracts))
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
							contract := currentSpreadContract(leg.Contract, contractMap)
							entryPrice := spreadPricing.EntryMode.EntryPrice(leg.Side, contract)
							entryPrice = applySpreadSlippage(entryPrice, leg.Side, sa.SlippagePct)
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
						if sa.SecurityOrder.Type == MarketOrder && sa.SecurityOrder.Qty > 0 {
							broker.ExecuteOrderNow(sa.SecurityOrder, i, prepared.PrimaryDS.Timestamps[i])
						}
					case ScheduleCloseLeg:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && sa.LegIndex >= 0 && sa.LegIndex < len(sp.Legs) && !sp.Legs[sa.LegIndex].Closed {
							contract := currentSpreadContract(sp.Legs[sa.LegIndex].Contract, contractMap)
							entrySide := sp.Legs[sa.LegIndex].Side
							closePrice := spreadPricing.ExitMode.ExitPrice(entrySide, contract)
							exitSide := Sell
							if entrySide == Sell {
								exitSide = Buy
							}
							closePrice = applySpreadSlippage(closePrice, exitSide, sa.SlippagePct)
							reason := appendDeltaNote(sa.CloseReason, "exec_", contract.Delta)
							barCtx.CloseSpreadLegWithReasonAndData(sa.SpreadID, sa.LegIndex, closePrice, reason, sa.CloseCustomData)
						}
					case ScheduleCloseSpread:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && !sp.IsFullyClosed() {
							for legIndex := range sp.Legs {
								if sp.Legs[legIndex].Closed {
									continue
								}
								contract := currentSpreadContract(sp.Legs[legIndex].Contract, contractMap)
								entrySide := sp.Legs[legIndex].Side
								closePrice := spreadPricing.ExitMode.ExitPrice(entrySide, contract)
								exitSide := Sell
								if entrySide == Sell {
									exitSide = Buy
								}
								closePrice = applySpreadSlippage(closePrice, exitSide, sa.SlippagePct)
								reason := appendDeltaNote(sa.CloseReason, "exec_", contract.Delta)
								barCtx.CloseSpreadLegWithReasonAndData(sa.SpreadID, legIndex, closePrice, reason, sa.CloseCustomData)
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
				contract := currentSpreadContract(leg.Contract, contractMap)
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
		e.config.InitialCapital,
		e.config.AccountUnit,
		secColumns[0],
		reportColumns,
	)

	result.SpreadSummary = computeSpreadSummary(spreadTracker)
	result.SpreadPositions = buildSpreadPositionReports(spreadTracker, result.EndTime)

	return result, nil
}

// Run executes a full backtest. Steps:
// 1. Strategy.Init — collects indicator registrations and security requests.
// 2. Load primary + secondary DataSets (secondaries in parallel).
// 3. Align secondary timestamps to primary.
// 4. Preflight: resolve indicator DAG, compute all indicators vectorized.
// 5. Bar replay: broker fills → OnBar → mark-to-market.
// 6. Compute metrics and return Result.
func (e *Engine) Run(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*Result, error) {
	prepared, err := e.Prepare(ctx, market, symbol, interval, from, to, strategy, params)
	if err != nil {
		return nil, err
	}
	return e.replay(prepared, strategy, params)
}

// StrategyFactory creates a fresh Strategy instance for each parameter set.
// This is necessary because strategies are stateful (they accumulate positions,
// spread state, etc.) and cannot be safely reused across runs.
type StrategyFactory func() Strategy

// BatchResult pairs a parameter set with its backtest result.
type BatchResult struct {
	Params map[string]interface{}
	Result *Result
	Err    error
}

// RunBatch loads data once and replays a strategy with multiple parameter sets
// in parallel. The factory function must return a fresh Strategy for each run.
//
// Parameter sets may change indicator registrations and dependency graphs during
// Strategy.Init, so each run must prepare its own indicator state. This favors
// correctness over dataset reuse.
//
// nWorkers controls parallelism; if <= 0 it defaults to 1.
func (e *Engine) RunBatch(ctx context.Context, market, symbol, interval string, from, to time.Time, factory StrategyFactory, paramSets []map[string]interface{}, nWorkers int) ([]BatchResult, error) {
	if nWorkers <= 0 {
		nWorkers = 1
	}

	// Run each parameter set, limited by nWorkers
	results := make([]BatchResult, len(paramSets))
	sem := make(chan struct{}, nWorkers)
	var wg sync.WaitGroup

	for i, ps := range paramSets {
		wg.Add(1)
		go func(idx int, params map[string]interface{}) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			s := factory()
			res, err := e.Run(ctx, market, symbol, interval, from, to, s, params)
			results[idx] = BatchResult{Params: params, Result: res, Err: err}
		}(i, ps)
	}

	wg.Wait()
	return results, nil
}
