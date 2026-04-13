package retracementratioprotectivespreadshort

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

const (
	strategyName       = "retracement-ratio-protective-spread-short"
	positionGroupTag   = strategyName
	positionGroupDecay = 1.0

	ambushSellAmount       = 11.0
	ambushBuyAmount        = 10.0
	ambushProfitBaseAmount = 10.0
	ambushMinDTE           = 55
	ambushMaxDTE           = 85
	ambushTargetDTE        = 70
	dteReductionTrigger    = 29.0
	forceCloseDTE          = 20.0
	sellDeltaTarget        = 0.50
	buyDeltaTarget         = 0.30
	atrStopMultiple        = 5
	reboundCloseMultiple   = 3
	trancheCount           = 5
	dteReduceTrancheCount  = 3
	reboundCloseTranches   = 4

	interval12h = "12h"
	interval15m = "15m"

	signalResetIgnoreWindow = 15 * 24 * time.Hour

	stdPeriod          = 20
	stdMAPeriod        = 20
	dvolQuantilePeriod = 100
	dvolQuantileQ      = 0.95
	dvolReducePeriod   = 200
	dvolReduceQ        = 0.60
	dvolReduceScale    = 0.60

	featHeightQuantilePeriod = 100
	featHeightQuantileQ      = 0.90
	reboundFloor             = 0.01
	lowestClosePeriod        = 8
	highestClose3DPeriod     = 3 * 24 * 4
	reboundDropThreshold     = 0.10

	colEntrySignal    = "entry_signal"
	colATR14          = "atr14"
	colDvolQ95        = "dvol_q95"
	colDvolQ60        = "dvol_q60"
	colDvolValue      = "dvol_value"
	colFeatHeight     = "feat_height"
	colFeatHeightQ90  = "feat_height_q90"
	colATR15m         = "atr15m"
	colLowestClose8   = "lowest_close_8"
	colHighestClose3D = "highest_close_3d"
)

var takeProfitThresholds = []float64{0.30, 0.50, 0.80, 1.20}

// ---------------------------------------------------------------------------
// Direction & Signal types
// ---------------------------------------------------------------------------

type tradeDirection int

const (
	directionLong tradeDirection = iota
	directionShort
)

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
// Group state
// ---------------------------------------------------------------------------

type groupState struct {
	groupID                 int
	spreadIDs               []int
	entryTime               time.Time
	entryUnderlyingPrice    float64
	lowestUnderlyingPrice   float64
	entryATR12h             float64
	profitBaseAmount        float64
	tpTriggered             []bool
	extraReductionTriggered bool
	reboundCloseTriggered   bool
	dteReductionTriggered   bool
	reductionCount          int
}

// ---------------------------------------------------------------------------
// Strategy struct
// ---------------------------------------------------------------------------

type strategy struct {
	optutil.PricingMixin

	signals   []signalEvent
	ref12h    backtest.SecurityRef
	ref15m    backtest.SecurityRef
	dvolRef   backtest.FactorRef
	direction tradeDirection
	groups    []*groupState
}

func (s *strategy) Name() string { return "RetracementRatioProtectiveSpreadShort" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: colATR14, Label: "ATR 12H 14", Decimals: 4},
		{Source: colDvolQ95, Label: "DVOL 12H P95", Decimals: 4},
		{Source: colDvolQ60, Label: "DVOL 12H P60", Decimals: 4},
		{Source: colFeatHeight, Label: "Feat Height 15M", Decimals: 4},
		{Source: colFeatHeightQ90, Label: "Feat Height P90 15M", Decimals: 4},
		{Source: colATR15m, Label: "ATR 15M 14", Decimals: 4},
		{Source: colLowestClose8, Label: "Lowest Close 8", Decimals: 4},
		{Source: colHighestClose3D, Label: "Highest Close 3D", Decimals: 4},
	}
}

// ---------------------------------------------------------------------------
// init & catalog registration
// ---------------------------------------------------------------------------

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{"retracement_ratio_protective_spread_short"},
		Groups:  []string{"options", "spread", "timed"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			signals, err := loadSignals("pkg/strategies/retracement_ratio_protective_spread_short/signals_short.csv")
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
				direction: directionShort,
			}, nil
		},
	})
}

// ---------------------------------------------------------------------------
// Init — register securities, indicators, factors
// ---------------------------------------------------------------------------

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	primary := ctx.PrimaryRef()

	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)
	s.ref15m = ctx.AddSecurity(primary.Market, primary.Symbol, interval15m)

	ctx.RegisterOn(s.ref12h, colATR14, backtest.ATR(14))

	ctx.RegisterOn(s.ref12h, "std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return optutil.RollingStdDev(inputs["close"], stdPeriod)
		},
	))
	ctx.RegisterOn(s.ref12h, "ma_std20", backtest.SMA("std20", stdMAPeriod))

	s.dvolRef = ctx.AddFactor("dvol", interval12h)
	ctx.RegisterOn(s.ref15m, colATR15m, backtest.ATR(14))
	ctx.RegisterOn(s.ref15m, colLowestClose8, backtest.Lowest("close", lowestClosePeriod))
	ctx.RegisterOn(s.ref15m, colHighestClose3D, backtest.Highest("close", highestClose3DPeriod))
	ctx.RegisterOn(s.ref15m, colFeatHeight, backtest.Custom(
		[]string{"high", "low", "close"},
		func(inputs map[string][]float64) []float64 {
			highs := inputs["high"]
			lows := inputs["low"]
			closes := inputs["close"]
			out := make([]float64, len(closes))
			for i := range out {
				if i >= len(highs) || i >= len(lows) || math.IsNaN(highs[i]) || math.IsNaN(lows[i]) || math.IsNaN(closes[i]) || closes[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (highs[i] - lows[i]) / closes[i]
			}
			return out
		},
	))
	ctx.RegisterOn(s.ref15m, colFeatHeightQ90, backtest.Quantile(colFeatHeight, featHeightQuantilePeriod, featHeightQuantileQ))

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

	entrySignal12h := buildSignalColumn(htf.Timestamps(), s.signals)
	entrySignal := buildTriggeredAlignedSignalColumn(htf.AlignMap(), entrySignal12h, primary.Len())
	if err := primary.SetColumn(colEntrySignal, entrySignal); err != nil {
		return err
	}

	if err := dvol.Quantile(colDvolQ95, "close", dvolQuantilePeriod, dvolQuantileQ); err != nil {
		return err
	}
	if err := dvol.Quantile(colDvolQ60, "close", dvolReducePeriod, dvolReduceQ); err != nil {
		return err
	}

	dvolQ9512h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(colDvolQ95))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(colDvolQ95, dvolQ9512h); err != nil {
		return err
	}

	dvolQ6012h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(colDvolQ60))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(colDvolQ60, dvolQ6012h); err != nil {
		return err
	}

	for _, name := range []string{colATR14, colDvolQ95, colDvolQ60} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
	}

	dvolValue, err := ctx.ColumnAlignedFactorToPrimary(s.dvolRef, "close")
	if err != nil {
		return err
	}
	if err := primary.SetColumn(colDvolValue, dvolValue); err != nil {
		return err
	}

	for _, name := range []string{colATR15m, colFeatHeight, colFeatHeightQ90, colLowestClose8, colHighestClose3D} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref15m, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
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

	s.manageGroups(ctx, contractMap)

	sigVal := ctx.Ind(colEntrySignal)
	if !math.IsNaN(sigVal) && sigVal == 1 {
		if active := s.latestActiveGroup(ctx); active != nil {
			age := ctx.Time().Sub(active.entryTime)
			if age <= signalResetIgnoreWindow {
				fmt.Printf("[%s] ambush signal ignored: age_days=%.2f entry=%s reason=new_signal_ignored_within_15d\n",
					ctx.Time().Format(time.RFC3339), age.Hours()/24, active.entryTime.Format(time.RFC3339))
				return
			}
		}
		s.closeAllGroups(ctx, contractMap, "new_signal_reset")
		s.openAmbushPhase(ctx, chain)
	}
}

// ---------------------------------------------------------------------------
// Single-phase ambush spread
// ---------------------------------------------------------------------------

func (s *strategy) openAmbushPhase(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	dvolVal := ctx.Ind(colDvolValue)
	dvolQ95 := ctx.Ind(colDvolQ95)
	dvolQ60 := ctx.Ind(colDvolQ60)
	if math.IsNaN(dvolVal) || math.IsNaN(dvolQ95) || dvolVal >= dvolQ95 {
		return
	}

	positionScale := 1.0
	if !math.IsNaN(dvolQ60) && dvolVal >= dvolQ60 {
		positionScale = dvolReduceScale
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
	if len(expiries) == 0 {
		return
	}

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

		totalBuyPrice := buy1Price + buy2Price
		if totalBuyPrice <= 0 {
			continue
		}

		profitBaseAmount := ambushProfitBaseAmount * positionScale
		totalSellQty := ambushSellAmount * positionScale / sellPrice
		totalBuyQty := ambushBuyAmount * positionScale / totalBuyPrice
		if totalSellQty <= 0 || totalBuyQty <= 0 {
			continue
		}

		groupID := s.openPositionGroup(ctx, profitBaseAmount)
		if groupID <= 0 {
			return
		}

		spreadIDs := s.openAmbushTranches(ctx, groupID, sellContract, sellPrice, buy1, buy1Price, buy2, buy2Price, totalSellQty, totalBuyQty)
		if len(spreadIDs) != trancheCount {
			s.closePositionGroup(ctx, groupID)
			return
		}

		entryATR12h := ctx.Ind(colATR14)
		entryLow := ctx.Low()
		if math.IsNaN(entryLow) {
			entryLow = ctx.Close()
		}

		fmt.Printf("[%s] ambush open: dvol=%.4f q95=%.4f q60=%.4f scale=%.2f sell=%s@%.6f buy1=%s@%.6f buy2=%s@%.6f n_sell=%.4f n_buy=%.4f buy_base=%.4f tranches=%d entry_low=%.4f atr12h_entry=%.6f\n",
			ctx.Time().Format(time.RFC3339), dvolVal, dvolQ95, dvolQ60, positionScale,
			sellContract.Symbol, sellPrice, buy1.Symbol, buy1Price, buy2.Symbol, buy2Price,
			totalSellQty, totalBuyQty, profitBaseAmount, trancheCount, entryLow, entryATR12h)

		s.groups = append(s.groups, &groupState{
			groupID:               groupID,
			spreadIDs:             spreadIDs,
			entryTime:             ctx.Time(),
			entryUnderlyingPrice:  ctx.Close(),
			lowestUnderlyingPrice: entryLow,
			entryATR12h:           entryATR12h,
			profitBaseAmount:      profitBaseAmount,
			tpTriggered:           make([]bool, len(takeProfitThresholds)),
		})
		return
	}
}

func (s *strategy) openAmbushTranches(
	ctx *backtest.BarContext,
	groupID int,
	sellContract *backtest.OptionContract,
	sellPrice float64,
	buy1 *backtest.OptionContract,
	buy1Price float64,
	buy2 *backtest.OptionContract,
	buy2Price float64,
	totalSellQty float64,
	totalBuyQty float64,
) []int {
	spreadIDs := make([]int, 0, trancheCount)
	trancheSellQty := totalSellQty / float64(trancheCount)
	trancheBuyQty := totalBuyQty / float64(trancheCount)
	if trancheSellQty <= 0 || trancheBuyQty <= 0 {
		return nil
	}

	for i := 0; i < trancheCount; i++ {
		legs := []backtest.SpreadLeg{
			{Contract: *sellContract, Side: backtest.Sell, Qty: trancheSellQty, EntryPrice: sellPrice},
			{Contract: *buy1, Side: backtest.Buy, Qty: trancheBuyQty, EntryPrice: buy1Price},
			{Contract: *buy2, Side: backtest.Buy, Qty: trancheBuyQty, EntryPrice: buy2Price},
		}
		tag := fmt.Sprintf("ambush|tranche=%d/%d|n1=%.4f|n2=%.4f", i+1, trancheCount, trancheSellQty, trancheBuyQty)
		spreadID := ctx.OpenSpreadInGroup(legs, tag, groupID)
		if spreadID <= 0 {
			for _, openedID := range spreadIDs {
				s.closeSpreadLegs(ctx, openedID, nil, "open_rollback")
			}
			return nil
		}
		spreadIDs = append(spreadIDs, spreadID)
	}

	return spreadIDs
}

func (s *strategy) manageGroups(ctx *backtest.BarContext, contractMap optutil.ContractMap) {
	var remaining []*groupState
	for _, gs := range s.groups {
		if s.manageGroup(ctx, gs, contractMap) {
			remaining = append(remaining, gs)
		}
	}
	s.groups = remaining
}

func (s *strategy) manageGroup(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap) bool {
	if gs == nil {
		return false
	}
	if len(s.activeSpreadIDs(ctx, gs)) == 0 {
		s.closePositionGroup(ctx, gs.groupID)
		return false
	}
	s.updateLowestUnderlyingPrice(ctx, gs)

	now := ctx.Time()
	if s.shouldForceCloseForDTE(ctx, gs, contractMap, now, forceCloseDTE) {
		s.closeTrackedGroup(ctx, gs, contractMap, "dte_le_20")
		return false
	}

	if s.priceBreachedStop(ctx.Close(), gs.entryUnderlyingPrice, gs.entryATR12h) {
		s.closeTrackedGroup(ctx, gs, contractMap, s.atrStopReason(gs.entryATR12h))
		return false
	}

	if !gs.reboundCloseTriggered && s.shouldTriggerReboundClose(ctx.Close(), gs) {
		activeCount := len(s.activeSpreadIDs(ctx, gs))
		closeCount := reboundCloseTranches
		if activeCount < closeCount {
			closeCount = activeCount
		}
		if closeCount > 0 {
			pnl := s.groupCombinedPnL(ctx, gs, contractMap)
			if !s.closeActiveTranches(ctx, gs, contractMap, closeCount, s.reboundCloseReason(gs), pnl) {
				s.closePositionGroup(ctx, gs.groupID)
				return false
			}
			gs.reboundCloseTriggered = true
			if len(s.activeSpreadIDs(ctx, gs)) == 0 {
				fmt.Printf("[%s] ambush close: reason=position_exhausted active_tranches=0\n", ctx.Time().Format(time.RFC3339))
				s.closePositionGroup(ctx, gs.groupID)
				return false
			}
			return true
		}
	}

	if !gs.dteReductionTriggered && s.shouldForceCloseForDTE(ctx, gs, contractMap, now, dteReductionTrigger) {
		gs.dteReductionTriggered = true
		if !s.closeActiveTranches(ctx, gs, contractMap, dteReduceTrancheCount, "dte_le_29_reduce_60", math.NaN()) {
			s.closePositionGroup(ctx, gs.groupID)
			return false
		}
		if len(s.activeSpreadIDs(ctx, gs)) == 0 {
			fmt.Printf("[%s] ambush close: reason=position_exhausted active_tranches=0\n", ctx.Time().Format(time.RFC3339))
			s.closePositionGroup(ctx, gs.groupID)
			return false
		}
		return true
	}

	pnl := s.groupCombinedPnL(ctx, gs, contractMap)
	reasons := s.pendingReductionReasons(ctx, gs, pnl)
	for _, reason := range reasons {
		if !s.closeOneActiveTranche(ctx, gs, contractMap, reason, pnl) {
			break
		}
	}

	if len(s.activeSpreadIDs(ctx, gs)) == 0 {
		fmt.Printf("[%s] ambush close: reason=position_exhausted active_tranches=0\n", ctx.Time().Format(time.RFC3339))
		s.closePositionGroup(ctx, gs.groupID)
		return false
	}
	return true
}

func (s *strategy) pendingReductionReasons(ctx *backtest.BarContext, gs *groupState, pnl float64) []string {
	reasons := make([]string, 0, len(takeProfitThresholds)+1)
	for i, threshold := range takeProfitThresholds {
		if i >= len(gs.tpTriggered) {
			break
		}
		if gs.tpTriggered[i] {
			continue
		}
		if pnl >= gs.profitBaseAmount*threshold {
			gs.tpTriggered[i] = true
			reasons = append(reasons, takeProfitReason(threshold))
		}
	}

	extraNow := s.conditionReductionExtra(ctx)
	if extraNow && !gs.extraReductionTriggered {
		gs.extraReductionTriggered = true
		reasons = append(reasons, "conditionReductionExtra")
	}

	return reasons
}

func (s *strategy) conditionReductionExtra(ctx *backtest.BarContext) bool {
	sec15m := ctx.Security(s.ref15m)
	if sec15m == nil {
		return false
	}

	featHeight := sec15m.Ind(colFeatHeight)
	featHeightQ90 := sec15m.Ind(colFeatHeightQ90)
	atr15m := sec15m.Ind(colATR15m)
	lowestClose8 := sec15m.Ind(colLowestClose8)
	highestClose3D := sec15m.Ind(colHighestClose3D)
	low := sec15m.Field("low")
	open := sec15m.Field("open")
	close := sec15m.Field("close")

	if math.IsNaN(featHeight) || math.IsNaN(featHeightQ90) || math.IsNaN(atr15m) || math.IsNaN(lowestClose8) || math.IsNaN(highestClose3D) || math.IsNaN(low) || math.IsNaN(open) || math.IsNaN(close) || close <= 0 {
		return false
	}

	conditionRebound1 := featHeight >= featHeightQ90 && featHeight >= reboundFloor
	conditionRebound2 := low < lowestClose8+atr15m && close > open
	conditionRebound3 := (highestClose3D-close)/close > reboundDropThreshold
	return conditionRebound1 && conditionRebound2 && conditionRebound3
}

func (s *strategy) closeOneActiveTranche(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap, reason string, pnl float64) bool {
	return s.closeActiveTranches(ctx, gs, contractMap, 1, reason, pnl)
}

func (s *strategy) closeActiveTranches(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap, count int, reason string, pnl float64) bool {
	closed := 0
	for _, spreadID := range gs.spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() {
			continue
		}
		s.closeSpreadLegs(ctx, spreadID, contractMap, reason)
		gs.reductionCount++
		closed++
		if closed >= count {
			break
		}
	}
	if closed == 0 {
		return false
	}
	remaining := len(s.activeSpreadIDs(ctx, gs))
	fmt.Printf("[%s] ambush reduce: reason=%s pnl=%.4f closed_tranches=%d reduction=%d remaining_tranches=%d\n",
		ctx.Time().Format(time.RFC3339), reason, pnl, closed, gs.reductionCount, remaining)
	return true
}

func (s *strategy) shouldForceCloseForDTE(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap, now time.Time, threshold float64) bool {
	for _, spreadID := range gs.spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil {
			continue
		}
		for _, leg := range sp.Legs {
			if leg.Closed {
				continue
			}
			contract := optutil.ResolveContract(leg.Contract, contractMap)
			if contract.DaysToExpiry(now) <= threshold {
				return true
			}
		}
	}
	return false
}

func (s *strategy) activeSpreadIDs(ctx *backtest.BarContext, gs *groupState) []int {
	active := make([]int, 0, len(gs.spreadIDs))
	for _, spreadID := range gs.spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp != nil && !sp.IsFullyClosed() {
			active = append(active, spreadID)
		}
	}
	return active
}

func (s *strategy) updateLowestUnderlyingPrice(ctx *backtest.BarContext, gs *groupState) {
	if gs == nil {
		return
	}
	low := ctx.Low()
	if math.IsNaN(low) {
		return
	}
	if math.IsNaN(gs.lowestUnderlyingPrice) || low < gs.lowestUnderlyingPrice {
		gs.lowestUnderlyingPrice = low
	}
}

// ---------------------------------------------------------------------------
// Group close helpers
// ---------------------------------------------------------------------------

func (s *strategy) closeAllGroups(ctx *backtest.BarContext, contractMap optutil.ContractMap, reason string) {
	for _, gs := range s.groups {
		s.closeTrackedGroup(ctx, gs, contractMap, reason)
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

func (s *strategy) closeTrackedGroup(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap, reason string) {
	if gs == nil {
		return
	}
	pnl := s.groupCombinedPnL(ctx, gs, contractMap)
	fmt.Printf("[%s] ambush close: reason=%s pnl=%.4f active_tranches=%d\n",
		ctx.Time().Format(time.RFC3339), reason, pnl, len(s.activeSpreadIDs(ctx, gs)))
	for _, spreadID := range gs.spreadIDs {
		s.closeSpreadLegs(ctx, spreadID, contractMap, reason)
	}
	s.closePositionGroup(ctx, gs.groupID)
	gs.groupID = 0
	gs.spreadIDs = nil
}

func (s *strategy) groupCombinedPnL(ctx *backtest.BarContext, gs *groupState, contractMap optutil.ContractMap) float64 {
	if gs == nil {
		return 0
	}
	total := 0.0
	for _, spreadID := range gs.spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil {
			continue
		}
		total += sp.TotalRealizedPnL()
		total += s.spreadUnrealizedPnL(sp, contractMap)
	}
	return total
}

func (s *strategy) openPositionGroup(ctx *backtest.BarContext, initAmount float64) int {
	if ctx.SpreadGroups() == nil {
		return 0
	}
	return ctx.SpreadGroups().Open(positionGroupTag, initAmount, positionGroupDecay, ctx.Time())
}

func (s *strategy) closePositionGroup(ctx *backtest.BarContext, groupID int) {
	if groupID <= 0 || ctx.SpreadGroups() == nil {
		return
	}
	ctx.SpreadGroups().Close(groupID)
}

func (s *strategy) latestActiveGroup(ctx *backtest.BarContext) *groupState {
	for i := len(s.groups) - 1; i >= 0; i-- {
		gs := s.groups[i]
		if gs == nil {
			continue
		}
		if len(s.activeSpreadIDs(ctx, gs)) > 0 {
			return gs
		}
	}
	return nil
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

func (s *strategy) signedDelta(absDelta float64) float64 {
	if s.direction == directionShort {
		return -absDelta
	}
	return absDelta
}

func (s *strategy) atrStopReason(atr float64) string {
	if math.IsNaN(atr) || atr <= 0 {
		return "atr_stop_4_5x, atr12h_entry=NaN"
	}
	return fmt.Sprintf("atr_stop_4_5x, atr12h_entry=%.6f", atr)
}

func (s *strategy) reboundCloseReason(gs *groupState) string {
	if gs == nil {
		return "rebound_close_80, lowest_since_entry=NaN, atr12h_entry=NaN"
	}
	lowest := "NaN"
	if !math.IsNaN(gs.lowestUnderlyingPrice) {
		lowest = fmt.Sprintf("%.4f", gs.lowestUnderlyingPrice)
	}
	atr := "NaN"
	if !math.IsNaN(gs.entryATR12h) {
		atr = fmt.Sprintf("%.6f", gs.entryATR12h)
	}
	return fmt.Sprintf("rebound_close_80, lowest_since_entry=%s, atr12h_entry=%s", lowest, atr)
}

func (s *strategy) shouldTriggerReboundClose(close float64, gs *groupState) bool {
	if gs == nil || math.IsNaN(close) || math.IsNaN(gs.lowestUnderlyingPrice) || math.IsNaN(gs.entryATR12h) || gs.entryATR12h <= 0 {
		return false
	}
	return close-gs.lowestUnderlyingPrice > reboundCloseMultiple*gs.entryATR12h
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

func takeProfitReason(threshold float64) string {
	switch threshold {
	case 0.30:
		return "tp_30"
	case 0.50:
		return "tp_50"
	case 0.80:
		return "tp_80"
	case 1.20:
		return "tp_120"
	default:
		return fmt.Sprintf("tp_%.0f", threshold*100)
	}
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
		} else {
			c := candidates[i]
			second = &c
			secondPrice = price
			return first, firstPrice, second, secondPrice
		}
	}
	return first, firstPrice, second, secondPrice
}

// ---------------------------------------------------------------------------
// Signal loading
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
			continue
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
// Signal alignment
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
// Chain helpers
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
