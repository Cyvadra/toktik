package backtest

import (
	"context"
	"fmt"
	"math"
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
	chainProvider OptionsChainProvider
}

// NewEngine creates a backtest engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	return &Engine{
		config: cfg,
		feeds:  make(map[string]DataFeed),
	}
}

// RegisterDataFeed associates a DataFeed with a market name.
func (e *Engine) RegisterDataFeed(market string, feed DataFeed) {
	e.feeds[market] = feed
}

// SetOptionsChainProvider sets the provider that supplies option chain data
// during bar replay. This enables strategies to dynamically query available
// options at each bar.
func (e *Engine) SetOptionsChainProvider(p OptionsChainProvider) {
	e.chainProvider = p
}

// Run executes a full backtest. Steps:
// 1. Strategy.Init — collects indicator registrations and security requests.
// 2. Load primary + secondary DataSets (secondaries in parallel).
// 3. Align secondary timestamps to primary.
// 4. Preflight: resolve indicator DAG, compute all indicators vectorized.
// 5. Bar replay: broker fills → OnBar → mark-to-market.
// 6. Compute metrics and return Result.
func (e *Engine) Run(ctx context.Context, market, symbol, interval string, from, to time.Time, strategy Strategy, params map[string]interface{}) (*Result, error) {
	// --- Step 1: Init ---
	setupCtx := NewSetupContext(market, symbol, interval)
	if params != nil {
		for k, v := range params {
			setupCtx.params[k] = v
		}
	}
	if err := strategy.Init(setupCtx); err != nil {
		return nil, fmt.Errorf("strategy init: %w", err)
	}

	primaryRef := setupCtx.primaryRef

	// --- Step 2: Load data ---
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

	secCount := len(setupCtx.securities) - 1 // exclude primary at index 0
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

	// --- Step 3: Align secondary data ---
	alignMaps := make([][]int, len(setupCtx.securities))
	alignMaps[0] = nil // primary doesn't need alignment
	for i := 1; i < len(secDataSets); i++ {
		alignMaps[i] = alignSeries(primaryDS, secDataSets[i])
	}

	// --- Step 4: Preflight indicator computation ---
	// Build per-security column maps and compute indicators
	secColumns := make([]map[string][]float64, len(setupCtx.securities))

	for i, sec := range setupCtx.securities {
		ds := secDataSets[i]
		cols := make(map[string][]float64, len(ds.Columns))
		for name, data := range ds.Columns {
			cols[name] = data
		}

		// Resolve indicators for this security
		if len(sec.inds) > 0 {
			if err := resolveIndicators(sec.inds, cols); err != nil {
				return nil, fmt.Errorf("indicators for security[%d] %s: %w", i, sec.ref.Symbol, err)
			}
		}

		secColumns[i] = cols
	}

	// --- Step 5: Bar replay ---
	broker := NewBroker(e.config)

	// Build security accessors (persistent across bars, barIndex is updated each bar)
	accessors := make([]*SecurityAccessor, len(setupCtx.securities))
	for i := range setupCtx.securities {
		accessors[i] = &SecurityAccessor{
			data:     secColumns[i],
			alignMap: alignMaps[i],
		}
	}

	// Price function for broker: resolves OHLC from the correct security's columns
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

	nBars := primaryDS.Len
	equityCurve := make([]float64, nBars)
	allTrades := make([]Trade, 0)

	spreadTracker := NewSpreadTracker()
	var scheduledActions []ScheduledAction
	spreadPricing := DefaultSpreadPricingConfig()
	if provider, ok := strategy.(SpreadPricingProvider); ok {
		spreadPricing = provider.SpreadPricingConfig().WithDefaults()
	}

	barCtx := &BarContext{
		primary:          secColumns[0],
		securities:       accessors,
		broker:           broker,
		params:           setupCtx.params,
		primaryRef:       primaryRef,
		chainProvider:    e.chainProvider,
		spreadTracker:    spreadTracker,
		scheduledActions: &scheduledActions,
	}

	secRefList := make([]SecurityRef, len(setupCtx.securities))
	for i, sec := range setupCtx.securities {
		secRefList[i] = sec.ref
	}
	barCtx.secRefs = secRefList

	currentSpreadContract := func(contract OptionContract, contractMap map[string]OptionContract) OptionContract {
		if updated, ok := contractMap[contract.Symbol]; ok {
			return updated
		}
		return contract
	}

	for i := 0; i < nBars; i++ {
		barCtx.barIndex = i
		barCtx.barTime = primaryDS.Timestamps[i]

		// Update all accessor bar indices
		for _, acc := range accessors {
			acc.barIndex = i
		}

		// Process pending orders from previous bar
		if i > 0 {
			fills := broker.ProcessPending(i, primaryDS.Timestamps[i])
			allTrades = append(allTrades, fills...)
		}

		// Cache options chain for this bar (used by scheduled actions and equity)
		var contractMap map[string]OptionContract
		if e.chainProvider != nil {
			contracts := e.chainProvider.AvailableContracts(primaryDS.Timestamps[i])
			contractMap = make(map[string]OptionContract, len(contracts))
			for _, c := range contracts {
				contractMap[c.Symbol] = c
			}
		}

		// Process scheduled actions (time-based position management)
		if len(scheduledActions) > 0 {
			var remaining []ScheduledAction
			for _, sa := range scheduledActions {
				if !primaryDS.Timestamps[i].Before(sa.TriggerTime) {
					// Action triggered
					switch sa.ActionType {
					case ScheduleCloseLeg:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && sa.LegIndex >= 0 && sa.LegIndex < len(sp.Legs) && !sp.Legs[sa.LegIndex].Closed {
							contract := currentSpreadContract(sp.Legs[sa.LegIndex].Contract, contractMap)
							closePrice := spreadPricing.ExitMode.ExitPrice(sp.Legs[sa.LegIndex].Side, contract)
							barCtx.CloseSpreadLeg(sa.SpreadID, sa.LegIndex, closePrice)
						}
					case ScheduleCloseSpread:
						sp := spreadTracker.Get(sa.SpreadID)
						if sp != nil && !sp.IsFullyClosed() {
							for legIndex := range sp.Legs {
								if sp.Legs[legIndex].Closed {
									continue
								}
								contract := currentSpreadContract(sp.Legs[legIndex].Contract, contractMap)
								closePrice := spreadPricing.ExitMode.ExitPrice(sp.Legs[legIndex].Side, contract)
								barCtx.CloseSpreadLeg(sa.SpreadID, legIndex, closePrice)
							}
						}
					}
				} else {
					remaining = append(remaining, sa)
				}
			}
			scheduledActions = remaining
		}

		// Call strategy
		strategy.OnBar(barCtx)

		// Record equity. Spread entry and exit cashflows already adjust broker cash,
		// so open spread contribution must be current market value, not unrealized PnL.
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

	// --- Step 6: Compute metrics ---
	result := computeResult(
		strategy.Name(),
		allTrades,
		equityCurve,
		primaryDS.Timestamps,
		e.config.InitialCapital,
		e.config.AccountUnit,
		secColumns[0], // include primary indicator series for visualization
	)

	// Attach spread summary to result
	result.SpreadSummary = computeSpreadSummary(spreadTracker)
	result.SpreadPositions = buildSpreadPositionReports(spreadTracker, result.EndTime)

	return result, nil
}
