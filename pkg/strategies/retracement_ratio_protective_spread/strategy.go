package retracementratioprotectivespread

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	strategyName = "retracement-ratio-protective-spread"

	// Phase 1 — Ambush
	ambushBuyAmount  = 5.0  // BTC notional per sell/buy bucket
	ambushTPPartial  = 0.30 // 30% of buy cost → partial take-profit
	ambushTPFull     = 0.50 // 50% of buy cost → full exit → Phase 2
	ambushMinDTE     = 55
	ambushMaxDTE     = 85
	ambushTargetDTE  = 70
	sellDeltaTarget  = 0.50
	buyDeltaTarget   = 0.30
	atrStopMultiple  = 8.0
	atrRollProximity = 2.0
	dteRollMin       = 20.0
	dteRollMax       = 40.0

	// Phase 2 — Trend
	trendInitAmount     = 2.0
	trendMinDTE         = 25
	trendMaxDTE         = 40
	trendTargetDTE      = 35
	rollProfitPct       = 0.50
	rollDeltaIncrease   = 0.20
	decayFactor         = 0.90
	minForceDelta       = 0.05
	maxForceDelta       = 0.70
	longStrikeMultiple  = 1.15
	shortStrikeMultiple = 0.80
	partialRetainRatio  = 2.0 / 3.0

	// Indicator periods
	interval12h        = "12h"
	stdPeriod          = 20
	stdMAPeriod        = 20
	stdRankPeriod      = 100
	dvolRankPeriod     = 200
	dvolQuantilePeriod = 100
	dvolQuantileQ      = 0.95

	// Column names
	colEntrySignal  = "entry_signal"
	colATR14        = "atr14"
	colStdma20PR100 = "stdma20_pr100"
	colDvolPR200    = "dvol_pr200"
	colDvolQ95      = "dvol_q95"
	colDvolValue    = "dvol_value"
)

// ---------------------------------------------------------------------------
// Direction & Phase enums
// ---------------------------------------------------------------------------

type tradeDirection int

const (
	directionLong tradeDirection = iota
	directionShort
)

type phase int

const (
	phaseAmbush phase = iota
	phaseTrend
)

// ---------------------------------------------------------------------------
// Signal types
// ---------------------------------------------------------------------------

type signalType int

const (
	signalNone signalType = iota
	signalInit
)

type signalEvent struct {
	time    time.Time
	sigType signalType
}

// ---------------------------------------------------------------------------
// Group state — tracks one active option position group
// ---------------------------------------------------------------------------

type groupState struct {
	phase                phase
	spreadID             int
	entryUnderlyingPrice float64
	entryATR             float64
	buyQty               float64 // n2 (ambush) / N (trend)
	sellQty              float64 // n1 (ambush only)
	buyCost              float64 // 5 BTC (ambush) / amount (trend)
	entryBuyDelta        float64 // |delta| of buy leg at open (trend)
	trendAmount          float64 // current investment, decays 0.9× each roll
	rebalanceCount       int     // number of 30% partial rebalances done
}

// ---------------------------------------------------------------------------
// Strategy struct
// ---------------------------------------------------------------------------

type strategy struct {
	optutil.PricingMixin

	signals   []signalEvent
	ref12h    backtest.SecurityRef
	dvolRef   backtest.FactorRef
	direction tradeDirection
	groups    []*groupState
}

func (s *strategy) Name() string { return "RetracementRatioProtectiveSpread" }

// ---------------------------------------------------------------------------
// init & catalog registration
// ---------------------------------------------------------------------------

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{"retracement_ratio_protective_spread"},
		Groups:  []string{"options", "spread", "timed"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			dir := directionLong
			directionEnv := os.Getenv("RRPS_DIRECTION")
			if directionEnv == "" {
				directionEnv = os.Getenv("DIRECTION")
			}
			if strings.EqualFold(directionEnv, "short") {
				dir = directionShort
			}
			level := os.Getenv("SIGNAL_LEVEL")
			if level == "" {
				level = "12h"
			}
			dirStr := "long"
			if dir == directionShort {
				dirStr = "short"
			}
			csvPath := fmt.Sprintf("pkg/strategies/retracement_ratio_protective_spread/%s_%s.csv", level, dirStr)
			signals, err := loadSignals(csvPath)
			if err != nil {
				return nil, fmt.Errorf("load signals: %w", err)
			}

			return &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				signals:   signals,
				direction: dir,
			}, nil
		},
	})
}

// ---------------------------------------------------------------------------
// Init — register securities, indicators, factors
// ---------------------------------------------------------------------------

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	primary := ctx.PrimaryRef()

	// 12h higher-timeframe security for indicators
	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)

	// ATR(14) on primary for stop-loss / roll proximity
	ctx.Register(colATR14, backtest.ATR(14))

	// Rolling StdDev(close, 20) on 12h
	ctx.RegisterOn(s.ref12h, "std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return optutil.RollingStdDev(inputs["close"], stdPeriod)
		},
	))

	// MA(StdDev, 20) on 12h
	ctx.RegisterOn(s.ref12h, "ma_std20", backtest.SMA("std20", stdMAPeriod))

	// Ratio: std20 / ma_std20 on 12h
	ctx.RegisterOn(s.ref12h, "stdma20", backtest.Custom(
		[]string{"std20", "ma_std20"},
		func(inputs map[string][]float64) []float64 {
			std := inputs["std20"]
			maStd := inputs["ma_std20"]
			out := make([]float64, len(std))
			for i := range out {
				if i >= len(maStd) || math.IsNaN(std[i]) || math.IsNaN(maStd[i]) || maStd[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = std[i] / maStd[i]
			}
			return out
		},
	))

	// Percentile rank of stdma20 over 100 bars on 12h — "A" value
	ctx.RegisterOn(s.ref12h, colStdma20PR100, optutil.PercentileRank("stdma20", stdRankPeriod))

	// DVOL factor
	s.dvolRef = ctx.AddFactor("dvol", interval12h)
	// Percentile rank of DVOL close over 200 bars — "B" value
	ctx.RegisterFactor(s.dvolRef, colDvolPR200, optutil.PercentileRank("close", dvolRankPeriod))

	ctx.SetWarmup(120 * 24 * time.Hour)
	return nil
}

// ---------------------------------------------------------------------------
// Preload — precompute derived columns once
// ---------------------------------------------------------------------------

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}
	htf := ctx.Security(s.ref12h)
	if htf == nil || htf.Len() == 0 {
		return fmt.Errorf("12h security unavailable")
	}
	dvol := ctx.Factor(s.dvolRef)
	if dvol == nil || dvol.Len() == 0 {
		return fmt.Errorf("12h dvol factor unavailable")
	}

	// 1. Build entry signal column on 12h, then align to primary.
	entrySignal12h := buildSignalColumn(htf.Timestamps(), s.signals)
	entrySignal := buildTriggeredAlignedSignalColumn(htf.AlignMap(), entrySignal12h, primary.Len())
	if err := primary.SetColumn(colEntrySignal, entrySignal); err != nil {
		return err
	}

	// 2. DVOL 95th-percentile quantile.
	if err := dvol.Quantile(colDvolQ95, "close", dvolQuantilePeriod, dvolQuantileQ); err != nil {
		return err
	}

	// 3. Align DVOL percentile rank and quantile to 12h timestamps.
	dvolPR12h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(colDvolPR200))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(colDvolPR200, dvolPR12h); err != nil {
		return err
	}
	dvolQ9512h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(colDvolQ95))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(colDvolQ95, dvolQ9512h); err != nil {
		return err
	}

	// 4. Align indicators from 12h to primary.
	for _, name := range []string{colStdma20PR100, colDvolPR200, colDvolQ95} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
	}

	// 5. Align DVOL value (close) to primary.
	dvolValue, err := ctx.ColumnAlignedFactorToPrimary(s.dvolRef, "close")
	if err != nil {
		return err
	}
	if err := primary.SetColumn(colDvolValue, dvolValue); err != nil {
		return err
	}

	return nil
}

// ---------------------------------------------------------------------------
// OnBar — main per-bar logic
// ---------------------------------------------------------------------------

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	close := ctx.Close()
	if math.IsNaN(close) {
		return
	}

	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)

	// Manage existing groups first.
	s.manageGroups(ctx, chain, contractMap)

	// Check for entry signal.
	sigVal := ctx.Ind(colEntrySignal)
	if !math.IsNaN(sigVal) && sigVal == 1 {
		// Signal reset: close all existing positions and re-enter Phase 1.
		s.closeAllGroups(ctx, contractMap, "信号重置")
		s.openAmbushPhase(ctx, chain)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Ambush — open ratio spread
// ---------------------------------------------------------------------------

func (s *strategy) openAmbushPhase(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	// DVOL filter: DVOL < 95th percentile of recent 100 bars.
	dvolVal := ctx.Ind(colDvolValue)
	dvolQ95 := ctx.Ind(colDvolQ95)
	if math.IsNaN(dvolVal) || math.IsNaN(dvolQ95) || dvolVal >= dvolQ95 {
		return
	}

	now := ctx.Time()
	optType := s.optionType()

	// Filter chain by type and DTE range.
	var filtered *backtest.OptionsChain
	if optType == backtest.Call {
		filtered = chain.Calls().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	} else {
		filtered = chain.Puts().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	}
	if filtered.Len() == 0 {
		return
	}

	contracts := filtered.Contracts()
	expiries := candidateExpiries(contracts, now, ambushTargetDTE)
	if len(expiries) == 0 {
		return
	}

	sellTarget := s.signedDelta(sellDeltaTarget)
	buyTarget := s.signedDelta(buyDeltaTarget)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		// Sell leg: abs(delta) closest to 0.5.
		sellCandidates := expiryChain.SortByDelta(sellTarget)
		sellContract, sellPrice := s.pickValidContract(backtest.Sell, sellCandidates)
		if sellContract == nil {
			continue
		}

		// Buy legs: abs(delta) closest to 0.3, pick top 2.
		buyCandidates := expiryChain.SortByDelta(buyTarget)
		buy1, buy1Price, buy2, buy2Price := s.pickTwoBuyContracts(buyCandidates, sellContract.Symbol)
		if buy1 == nil || buy2 == nil {
			continue
		}

		// Compute quantities.
		n1 := ambushBuyAmount / sellPrice
		combinedBuyPrice := buy1Price + buy2Price
		if combinedBuyPrice <= 0 {
			continue
		}
		n2 := ambushBuyAmount / combinedBuyPrice
		if n1 <= 0 || n2 <= 0 {
			continue
		}

		// Open 3-leg spread: [sell, buy1, buy2].
		legs := []backtest.SpreadLeg{
			{Contract: *sellContract, Side: backtest.Sell, Qty: n1, EntryPrice: sellPrice},
			{Contract: *buy1, Side: backtest.Buy, Qty: n2, EntryPrice: buy1Price},
			{Contract: *buy2, Side: backtest.Buy, Qty: n2, EntryPrice: buy2Price},
		}

		tag := fmt.Sprintf("伏击|d%.2f/d%.2f,d%.2f|n1=%.2f,n2=%.2f",
			sellContract.Delta, buy1.Delta, buy2.Delta, n1, n2)
		spreadID := ctx.OpenSpread(legs, tag)
		if spreadID <= 0 {
			continue
		}

		gs := &groupState{
			phase:                phaseAmbush,
			spreadID:             spreadID,
			entryUnderlyingPrice: ctx.Close(),
			entryATR:             ctx.Ind(colATR14),
			buyQty:               n2,
			sellQty:              n1,
			buyCost:              ambushBuyAmount,
			rebalanceCount:       0,
		}
		s.groups = append(s.groups, gs)
		return // opened successfully
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Ambush — manage existing spread
// ---------------------------------------------------------------------------

func (s *strategy) manageAmbushPhase(ctx *backtest.BarContext, gs *groupState, chain *backtest.OptionsChain, contractMap optutil.ContractMap) bool {
	sp := ctx.Spreads().Get(gs.spreadID)
	if sp == nil || sp.IsFullyClosed() {
		return false // group is dead
	}

	now := ctx.Time()
	close := ctx.Close()

	// Compute combined unrealized PnL across all open legs.
	pnl := s.spreadUnrealizedPnL(sp, contractMap)

	// Priority 1: 50% full exit → Phase 2.
	if pnl >= ambushTPFull*gs.buyCost {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "伏击50%止盈→趋势")
		s.openTrendPhase(ctx, gs, chain, trendInitAmount)
		return true
	}

	// Priority 2: 30% partial take-profit (only once).
	if pnl >= ambushTPPartial*gs.buyCost && gs.rebalanceCount == 0 {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "伏击30%部分止盈")
		s.rebalanceAmbush(ctx, gs, chain)
		return true
	}

	// Priority 3: 8 ATR stop-loss.
	atr := ctx.Ind(colATR14)
	if s.priceBreachedStop(close, gs.entryUnderlyingPrice, atr) {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "伏击8ATR止损")
		return false // remove group
	}

	// Priority 4: DTE roll — any open leg with 20 < DTE < 40 and price within 2 ATR of entry.
	if s.shouldDTERoll(sp, contractMap, now, close, gs.entryUnderlyingPrice, atr) {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "伏击DTE滚动")
		// Re-enter Phase 1 with a new spread for this group.
		s.reopenAmbushForGroup(ctx, gs, chain)
		return true
	}

	return true // group still alive
}

func (s *strategy) rebalanceAmbush(ctx *backtest.BarContext, gs *groupState, chain *backtest.OptionsChain) {
	gs.rebalanceCount++
	newSellQty := gs.sellQty * partialRetainRatio
	newBuyQty := gs.buyQty * partialRetainRatio

	if chain == nil || chain.Len() == 0 {
		return
	}

	now := ctx.Time()
	optType := s.optionType()

	var filtered *backtest.OptionsChain
	if optType == backtest.Call {
		filtered = chain.Calls().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	} else {
		filtered = chain.Puts().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	}
	if filtered.Len() == 0 {
		return
	}

	contracts := filtered.Contracts()
	expiries := candidateExpiries(contracts, now, ambushTargetDTE)

	sellTarget := s.signedDelta(sellDeltaTarget)
	buyTarget := s.signedDelta(buyDeltaTarget)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		sellCandidates := expiryChain.SortByDelta(sellTarget)
		sellContract, sellPrice := s.pickValidContract(backtest.Sell, sellCandidates)
		if sellContract == nil {
			continue
		}

		buyCandidates := expiryChain.SortByDelta(buyTarget)
		buy1, buy1Price, buy2, buy2Price := s.pickTwoBuyContracts(buyCandidates, sellContract.Symbol)
		if buy1 == nil || buy2 == nil {
			continue
		}

		legs := []backtest.SpreadLeg{
			{Contract: *sellContract, Side: backtest.Sell, Qty: newSellQty, EntryPrice: sellPrice},
			{Contract: *buy1, Side: backtest.Buy, Qty: newBuyQty, EntryPrice: buy1Price},
			{Contract: *buy2, Side: backtest.Buy, Qty: newBuyQty, EntryPrice: buy2Price},
		}

		tag := fmt.Sprintf("伏击再平衡|d%.2f/d%.2f,d%.2f|n1=%.2f,n2=%.2f",
			sellContract.Delta, buy1.Delta, buy2.Delta, newSellQty, newBuyQty)
		spreadID := ctx.OpenSpread(legs, tag)
		if spreadID <= 0 {
			continue
		}

		gs.spreadID = spreadID
		gs.sellQty = newSellQty
		gs.buyQty = newBuyQty
		return
	}
}

func (s *strategy) reopenAmbushForGroup(ctx *backtest.BarContext, gs *groupState, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	now := ctx.Time()
	optType := s.optionType()

	var filtered *backtest.OptionsChain
	if optType == backtest.Call {
		filtered = chain.Calls().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	} else {
		filtered = chain.Puts().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	}
	if filtered.Len() == 0 {
		return
	}

	contracts := filtered.Contracts()
	expiries := candidateExpiries(contracts, now, ambushTargetDTE)

	sellTarget := s.signedDelta(sellDeltaTarget)
	buyTarget := s.signedDelta(buyDeltaTarget)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		sellCandidates := expiryChain.SortByDelta(sellTarget)
		sellContract, sellPrice := s.pickValidContract(backtest.Sell, sellCandidates)
		if sellContract == nil {
			continue
		}

		buyCandidates := expiryChain.SortByDelta(buyTarget)
		buy1, buy1Price, buy2, buy2Price := s.pickTwoBuyContracts(buyCandidates, sellContract.Symbol)
		if buy1 == nil || buy2 == nil {
			continue
		}

		n1 := ambushBuyAmount / sellPrice
		combinedBuyPrice := buy1Price + buy2Price
		if combinedBuyPrice <= 0 {
			continue
		}
		n2 := ambushBuyAmount / combinedBuyPrice
		if n1 <= 0 || n2 <= 0 {
			continue
		}

		legs := []backtest.SpreadLeg{
			{Contract: *sellContract, Side: backtest.Sell, Qty: n1, EntryPrice: sellPrice},
			{Contract: *buy1, Side: backtest.Buy, Qty: n2, EntryPrice: buy1Price},
			{Contract: *buy2, Side: backtest.Buy, Qty: n2, EntryPrice: buy2Price},
		}

		tag := fmt.Sprintf("伏击DTE滚动|d%.2f/d%.2f,d%.2f|n1=%.2f,n2=%.2f",
			sellContract.Delta, buy1.Delta, buy2.Delta, n1, n2)
		spreadID := ctx.OpenSpread(legs, tag)
		if spreadID <= 0 {
			continue
		}

		gs.spreadID = spreadID
		gs.entryUnderlyingPrice = ctx.Close()
		gs.entryATR = ctx.Ind(colATR14)
		gs.buyQty = n2
		gs.sellQty = n1
		gs.buyCost = ambushBuyAmount
		gs.rebalanceCount = 0
		return
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Trend Following — open debit spread
// ---------------------------------------------------------------------------

func (s *strategy) openTrendPhase(ctx *backtest.BarContext, gs *groupState, chain *backtest.OptionsChain, amount float64) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	now := ctx.Time()
	optType := s.optionType()

	// Compute dynamic delta: Clamp(0.2, 0.8, (2*A + B) / 300).
	a := ctx.Ind(colStdma20PR100)
	b := ctx.Ind(colDvolPR200)
	if math.IsNaN(a) || math.IsNaN(b) {
		return
	}
	dynDelta := clamp(0.2, 0.8, (2*a+b)/300.0)
	signedDynDelta := s.signedDelta(dynDelta)

	// Filter chain by type and DTE range.
	var filtered *backtest.OptionsChain
	if optType == backtest.Call {
		filtered = chain.Calls().ExpiryRange(trendMinDTE, trendMaxDTE)
	} else {
		filtered = chain.Puts().ExpiryRange(trendMinDTE, trendMaxDTE)
	}
	if filtered.Len() == 0 {
		return
	}

	contracts := filtered.Contracts()
	expiries := candidateExpiries(contracts, now, trendTargetDTE)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		// Buy leg: sort by dynamic delta, pick first valid with |delta| in [0.05, 0.7].
		buyCandidates := expiryChain.SortByDelta(signedDynDelta)
		buyContract, buyPrice := s.pickValidContractWithDeltaFilter(backtest.Buy, buyCandidates)
		if buyContract == nil {
			continue
		}

		// Sell leg: same expiry, strike boundary filter.
		sellContract, sellPrice := s.pickTrendSellLeg(expiryContracts, buyContract)
		if sellContract == nil {
			continue
		}

		spreadCost := buyPrice - sellPrice
		if spreadCost <= 0 {
			continue
		}

		qty := amount / spreadCost
		if qty <= 0 {
			continue
		}

		legs := []backtest.SpreadLeg{
			{Contract: *buyContract, Side: backtest.Buy, Qty: qty, EntryPrice: buyPrice},
			{Contract: *sellContract, Side: backtest.Sell, Qty: qty, EntryPrice: sellPrice},
		}

		tag := fmt.Sprintf("趋势|A=%.1f|B=%.1f|dBuy=%.2f|dSell=%.2f|n=%.2f",
			a, b, buyContract.Delta, sellContract.Delta, qty)
		spreadID := ctx.OpenSpread(legs, tag)
		if spreadID <= 0 {
			continue
		}

		gs.phase = phaseTrend
		gs.spreadID = spreadID
		gs.entryBuyDelta = math.Abs(buyContract.Delta)
		gs.trendAmount = amount
		gs.entryUnderlyingPrice = ctx.Close()
		gs.buyCost = amount
		return
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Trend — manage existing spread
// ---------------------------------------------------------------------------

func (s *strategy) manageTrendPhase(ctx *backtest.BarContext, gs *groupState, chain *backtest.OptionsChain, contractMap optutil.ContractMap) bool {
	sp := ctx.Spreads().Get(gs.spreadID)
	if sp == nil || sp.IsFullyClosed() {
		return false
	}

	now := ctx.Time()

	// Expiry check: any leg DTE <= 1 → close all, remove group.
	for _, leg := range sp.Legs {
		if leg.Closed {
			continue
		}
		contract := optutil.ResolveContract(leg.Contract, contractMap)
		if contract.DaysToExpiry(now) <= 1 {
			s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "趋势到期平仓")
			return false
		}
	}

	// Roll conditions: both Cond1 AND Cond2 must hold.
	cond1 := s.prev12hBarUnfavorable(ctx)

	// Cond2a: spread value increase >= 50%.
	cond2a := false
	if len(sp.Legs) >= 2 {
		buyLeg := sp.Legs[0]
		sellLeg := sp.Legs[1]
		if !buyLeg.Closed && !sellLeg.Closed {
			buyMark := s.LegValuationPrice(buyLeg, contractMap)
			sellMark := s.LegValuationPrice(sellLeg, contractMap)
			if !math.IsNaN(buyMark) && !math.IsNaN(sellMark) {
				entrySpreadCost := buyLeg.EntryPrice - sellLeg.EntryPrice
				currentSpreadValue := buyMark - sellMark
				if entrySpreadCost > 0 {
					pnlPct := (currentSpreadValue - entrySpreadCost) / entrySpreadCost
					if pnlPct >= rollProfitPct {
						cond2a = true
					}
				}
			}
		}
	}

	// Cond2b: buy leg |delta| increased by >= 0.20.
	cond2b := false
	if len(sp.Legs) >= 1 && !sp.Legs[0].Closed {
		currentContract := optutil.ResolveContract(sp.Legs[0].Contract, contractMap)
		currentAbsDelta := math.Abs(currentContract.Delta)
		if currentAbsDelta-gs.entryBuyDelta >= rollDeltaIncrease {
			cond2b = true
		}
	}

	if cond1 && (cond2a || cond2b) {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, "趋势换仓")
		newAmount := gs.trendAmount * decayFactor
		s.openTrendPhase(ctx, gs, chain, newAmount)
		return true
	}

	return true
}

// ---------------------------------------------------------------------------
// Group management
// ---------------------------------------------------------------------------

func (s *strategy) manageGroups(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap optutil.ContractMap) {
	var remaining []*groupState

	for _, gs := range s.groups {
		var alive bool
		switch gs.phase {
		case phaseAmbush:
			alive = s.manageAmbushPhase(ctx, gs, chain, contractMap)
		case phaseTrend:
			alive = s.manageTrendPhase(ctx, gs, chain, contractMap)
		}
		if alive {
			remaining = append(remaining, gs)
		}
	}

	s.groups = remaining
}

func (s *strategy) closeAllGroups(ctx *backtest.BarContext, contractMap optutil.ContractMap, reason string) {
	for _, gs := range s.groups {
		s.closeSpreadLegs(ctx, gs.spreadID, contractMap, reason)
	}
	s.groups = nil
}

func (s *strategy) closeSpreadLegs(ctx *backtest.BarContext, spreadID int, contractMap optutil.ContractMap, reason string) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		contract := optutil.ResolveContract(sp.Legs[i].Contract, contractMap)
		closePrice := s.ExitPriceMode.ExitPrice(sp.Legs[i].Side, contract)
		if !math.IsNaN(closePrice) && closePrice > 0 {
			ctx.CloseSpreadLegWithReason(spreadID, i, closePrice, reason)
		}
	}
}

// ---------------------------------------------------------------------------
// Direction helpers
// ---------------------------------------------------------------------------

func (s *strategy) optionType() backtest.OptionType {
	if s.direction == directionLong {
		return backtest.Call
	}
	return backtest.Put
}

// signedDelta returns |target| with the correct sign for the current direction.
// Calls have positive delta, puts have negative delta.
func (s *strategy) signedDelta(absDelta float64) float64 {
	if s.direction == directionShort {
		return -absDelta
	}
	return absDelta
}

func (s *strategy) priceBreachedStop(close, entryPrice, atr float64) bool {
	if math.IsNaN(atr) || atr <= 0 {
		return false
	}
	if s.direction == directionLong {
		return close < entryPrice-atrStopMultiple*atr
	}
	return close > entryPrice+atrStopMultiple*atr
}

// prev12hBarUnfavorable checks whether the previous 12h bar has an unfavorable
// candle pattern for the current trend direction.
// Long trend: bearish (close < open) → triggers roll.
// Short trend: bullish (close > open) → triggers roll.
func (s *strategy) prev12hBarUnfavorable(ctx *backtest.BarContext) bool {
	htf := ctx.Security(s.ref12h)
	if htf == nil {
		return false
	}
	prevClose := htf.FieldAt("close", 1)
	prevOpen := htf.FieldAt("open", 1)
	if math.IsNaN(prevClose) || math.IsNaN(prevOpen) {
		return false
	}
	if s.direction == directionLong {
		return prevClose < prevOpen // bearish bar
	}
	return prevClose > prevOpen // bullish bar
}

// ---------------------------------------------------------------------------
// Spread analysis helpers
// ---------------------------------------------------------------------------

func (s *strategy) spreadUnrealizedPnL(sp *backtest.SpreadPosition, contractMap optutil.ContractMap) float64 {
	total := 0.0
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		markPrice := s.LegValuationPrice(sp.Legs[i], contractMap)
		if math.IsNaN(markPrice) {
			continue
		}
		total += sp.Legs[i].UnrealizedPnL(markPrice)
	}
	return total
}

func (s *strategy) shouldDTERoll(sp *backtest.SpreadPosition, contractMap optutil.ContractMap, now time.Time, close, entryPrice, atr float64) bool {
	if math.IsNaN(atr) || atr <= 0 {
		return false
	}
	// Price must be within 2 ATR of entry.
	if math.Abs(close-entryPrice) >= atrRollProximity*atr {
		return false
	}
	// Any open leg with 20 < DTE < 40.
	for _, leg := range sp.Legs {
		if leg.Closed {
			continue
		}
		contract := optutil.ResolveContract(leg.Contract, contractMap)
		dte := contract.DaysToExpiry(now)
		if dte > dteRollMin && dte < dteRollMax {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Contract selection helpers
// ---------------------------------------------------------------------------

func (s *strategy) pickValidContract(side backtest.Side, candidates []backtest.OptionContract) (*backtest.OptionContract, float64) {
	for i := range candidates {
		price := s.EntryPriceMode.EntryPrice(side, candidates[i])
		if !math.IsNaN(price) && price > 0 {
			return &candidates[i], price
		}
	}
	return nil, 0
}

func (s *strategy) pickValidContractWithDeltaFilter(side backtest.Side, candidates []backtest.OptionContract) (*backtest.OptionContract, float64) {
	for i := range candidates {
		absDelta := math.Abs(candidates[i].Delta)
		if absDelta < minForceDelta || absDelta > maxForceDelta {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(side, candidates[i])
		if !math.IsNaN(price) && price > 0 {
			return &candidates[i], price
		}
	}
	return nil, 0
}

func (s *strategy) pickTwoBuyContracts(candidates []backtest.OptionContract, excludeSymbol string) (*backtest.OptionContract, float64, *backtest.OptionContract, float64) {
	var first, second *backtest.OptionContract
	var firstPrice, secondPrice float64

	for i := range candidates {
		if candidates[i].Symbol == excludeSymbol {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(backtest.Buy, candidates[i])
		if math.IsNaN(price) || price <= 0 {
			continue
		}
		if first == nil {
			c := candidates[i]
			first = &c
			firstPrice = price
		} else if second == nil {
			c := candidates[i]
			second = &c
			secondPrice = price
			return first, firstPrice, second, secondPrice
		}
	}
	return first, firstPrice, second, secondPrice
}

// pickTrendSellLeg selects the sell leg for Phase 2 trend spread.
// Long: strike >= K_buy * 1.15, closest to boundary.
// Short: strike <= K_buy * 0.80, closest to boundary.
func (s *strategy) pickTrendSellLeg(expiryContracts []backtest.OptionContract, buyContract *backtest.OptionContract) (*backtest.OptionContract, float64) {
	var boundary float64
	if s.direction == directionLong {
		boundary = buyContract.StrikePrice * longStrikeMultiple
	} else {
		boundary = buyContract.StrikePrice * shortStrikeMultiple
	}

	// Filter by strike boundary and same option type.
	filtered := make([]backtest.OptionContract, 0, len(expiryContracts))
	for _, c := range expiryContracts {
		if c.Symbol == buyContract.Symbol {
			continue
		}
		if c.Type != buyContract.Type {
			continue
		}
		absDelta := math.Abs(c.Delta)
		if absDelta < minForceDelta || absDelta > maxForceDelta {
			continue
		}
		if s.direction == directionLong && c.StrikePrice >= boundary {
			filtered = append(filtered, c)
		} else if s.direction == directionShort && c.StrikePrice <= boundary {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, 0
	}

	// Sort by distance to boundary strike, ascending.
	sort.Slice(filtered, func(i, j int) bool {
		di := math.Abs(filtered[i].StrikePrice - boundary)
		dj := math.Abs(filtered[j].StrikePrice - boundary)
		if di != dj {
			return di < dj
		}
		return filtered[i].SpreadRatio() < filtered[j].SpreadRatio()
	})

	for i := range filtered {
		price := s.EntryPriceMode.EntryPrice(backtest.Sell, filtered[i])
		if !math.IsNaN(price) && price > 0 {
			return &filtered[i], price
		}
	}
	return nil, 0
}

// ---------------------------------------------------------------------------
// Signal loading (adapted from dual_spreads pattern)
// ---------------------------------------------------------------------------

func loadSignals(relPath string) ([]signalEvent, error) {
	path := relPath
	if _, err := os.Stat(path); err != nil {
		wd, _ := os.Getwd()
		path = wd + "/" + relPath
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("signal file not found: %s", relPath)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open signal file %s: %w", path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV %s: %w", path, err)
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	events := make([]signalEvent, 0, len(records))

	for i, record := range records {
		if i == 0 {
			continue // skip header
		}
		if len(record) < 4 {
			continue
		}

		lower := strings.ToLower(strings.TrimSpace(record[3]))
		if !strings.Contains(lower, "init") {
			continue
		}

		dateStr := strings.TrimSpace(record[2])
		var ts time.Time
		layouts := []string{"2006/1/2 15:04", "2006-01-02 15:04", "2006/01/02 15:04"}
		parsed := false
		for _, layout := range layouts {
			if t, err := time.ParseInLocation(layout, dateStr, utc8); err == nil {
				ts = t
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		events = append(events, signalEvent{time: ts.UTC(), sigType: signalInit})
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].time.Before(events[j].time)
	})
	return events, nil
}

// ---------------------------------------------------------------------------
// Signal alignment (adapted from dual_spreads pattern)
// ---------------------------------------------------------------------------

func buildSignalColumn(timestamps []time.Time, events []signalEvent) []float64 {
	values := make([]float64, len(timestamps))
	if len(timestamps) == 0 || len(events) == 0 {
		return values
	}

	assigned := make(map[int]signalEvent)
	for _, event := range events {
		idx := primaryBarIndexForSignal(timestamps, event.time)
		if idx < 0 {
			continue
		}
		existing, ok := assigned[idx]
		if !ok || event.time.Before(existing.time) {
			assigned[idx] = event
		}
	}

	for idx, event := range assigned {
		if event.sigType == signalInit {
			values[idx] = 1
		}
	}
	return values
}

func buildTriggeredAlignedSignalColumn(alignMap []int, values []float64, length int) []float64 {
	out := make([]float64, length)
	prevMapped := -1
	for i := 0; i < length; i++ {
		mapped := -1
		if i < len(alignMap) {
			mapped = alignMap[i]
		}
		if mapped < 0 || mapped >= len(values) {
			prevMapped = mapped
			continue
		}
		if mapped != prevMapped && values[mapped] != 0 {
			out[i] = values[mapped]
		}
		prevMapped = mapped
	}
	return out
}

func primaryBarIndexForSignal(timestamps []time.Time, eventTime time.Time) int {
	if len(timestamps) == 0 || eventTime.IsZero() {
		return -1
	}
	idx := sort.Search(len(timestamps), func(i int) bool {
		return timestamps[i].After(eventTime)
	})
	if idx == 0 {
		if timestamps[0].After(eventTime) {
			return -1
		}
		return 0
	}
	if idx >= len(timestamps) {
		return len(timestamps) - 1
	}
	return idx - 1
}

func alignSeriesValues(targetTimes, sourceTimes []time.Time, sourceValues []float64) ([]float64, error) {
	if len(sourceTimes) != len(sourceValues) {
		return nil, fmt.Errorf("timestamp/value length mismatch: %d vs %d", len(sourceTimes), len(sourceValues))
	}
	out := make([]float64, len(targetTimes))
	for i := range out {
		out[i] = math.NaN()
	}
	if len(targetTimes) == 0 || len(sourceTimes) == 0 {
		return out, nil
	}
	for i, ts := range targetTimes {
		idx := sort.Search(len(sourceTimes), func(j int) bool {
			return sourceTimes[j].After(ts)
		}) - 1
		if idx < 0 || idx >= len(sourceValues) {
			continue
		}
		out[i] = sourceValues[idx]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Chain helpers (adapted from dual_spreads pattern)
// ---------------------------------------------------------------------------

func candidateExpiries(contracts []backtest.OptionContract, now time.Time, targetDTE int) []time.Time {
	seen := make(map[time.Time]struct{}, len(contracts))
	expiries := make([]time.Time, 0, len(contracts))
	for _, contract := range contracts {
		if _, ok := seen[contract.Expiration]; ok {
			continue
		}
		seen[contract.Expiration] = struct{}{}
		expiries = append(expiries, contract.Expiration)
	}

	sort.Slice(expiries, func(i, j int) bool {
		di := math.Abs(expiries[i].Sub(now).Hours()/24 - float64(targetDTE))
		dj := math.Abs(expiries[j].Sub(now).Hours()/24 - float64(targetDTE))
		if di != dj {
			return di < dj
		}
		return expiries[i].Before(expiries[j])
	})

	return expiries
}

func contractsForExpiry(contracts []backtest.OptionContract, expiry time.Time) []backtest.OptionContract {
	filtered := make([]backtest.OptionContract, 0, len(contracts))
	for _, contract := range contracts {
		if contract.Expiration.Equal(expiry) {
			filtered = append(filtered, contract)
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func clamp(lo, hi, value float64) float64 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}
