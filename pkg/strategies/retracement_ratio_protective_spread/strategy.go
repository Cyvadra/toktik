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

const (
	strategyName  = "retracement-ratio-protective-spread"
	strategyAlias = "retracement_ratio_protective_spread"

	signalSourceEnv = "RETRACEMENT_RATIO_PROTECTIVE_SPREAD_SIGNAL_SOURCE"
	signalPath12h   = "pkg/strategies/retracement_ratio_protective_spread/12h.csv"
	signalPath1d    = "pkg/strategies/retracement_ratio_protective_spread/1d.csv"

	atrPeriod            = 20
	ivLookbackBars       = 200
	dvolInterval         = "12h"
	defaultVolPercentile = 60.0
	// ambushIVPercentileMax   = 55.0
	ambushIVPercentileMax   = 95.0
	ambushTargetDTE         = 70
	ambushMinDTE            = 50
	ambushMaxDTE            = 70
	trendTargetDTE          = 70
	trendMinDTE             = 50
	trendMaxDTE             = 70
	ambushPremiumBaseBTC    = 5.0
	ambushTakeProfit1BTC    = 1.65
	ambushTakeProfit2BTC    = 3.0
	ambushStopATRMultiplier = 8.0
	ambushRollMinDTE        = 20.0
	ambushRollMaxDTE        = 40.0
	ambushRollATRDistance   = 2.0
	ambushTrancheCount      = 3
	trendAmountBaseBTC      = 2.0
	trendRollProfitPct      = 0.50
	trendRollDeltaIncrease  = 0.20
	trendDecayFactor        = 0.90
	minLongDelta            = 0.20
	maxLongDelta            = 0.80
	minShortDelta           = 0.10
	maxShortDelta           = 0.80
	shortStrikeMaxMultiple  = 0.80
	signalTimeLayout        = "2006-01-02 15:04"

	hvPercentileColumn = "hv_pr_100"
	ivPercentileColumn = "iv_pr_200"
)

type strategyPhase int

const (
	phaseAmbush strategyPhase = iota + 1
	phaseTrend
)

type activeState struct {
	phase             strategyPhase
	groupID           int
	spreadIDs         []int
	partialTaken      bool
	entryUnderlying   float64
	entryATR          float64
	entryLongAbsDelta float64
}

type strategy struct {
	optutil.PricingMixin
	optutil.GroupMixin

	signalSource string
	entryTimes   map[int64]struct{}
	dvolRef      backtest.FactorRef
	activeStates []activeState
}

type ambushSelection struct {
	short          backtest.OptionContract
	longLowerHalf  backtest.OptionContract
	longUpperHalf  backtest.OptionContract
	shortPrice     float64
	longLowerPrice float64
	longUpperPrice float64
}

type trendSelection struct {
	long       backtest.OptionContract
	short      backtest.OptionContract
	longPrice  float64
	shortPrice float64
	spreadCost float64
	qty        float64
}

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{strategyAlias},
		Groups:  []string{"options", "spread", "timed"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			source := mustGetSignalSource()
			entryTimes, err := loadSignalTimesForSource(source)
			if err != nil {
				return nil, fmt.Errorf("load signal times for %s=%q: %w", signalSourceEnv, source, err)
			}
			return &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				signalSource: source,
				entryTimes:   entryTimes,
			}, nil
		},
	})
}

func (s *strategy) Name() string { return "RetracementRatioProtectiveSpread" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "entry_signal", Label: "Entry Signal", Decimals: 0},
		{Source: "atr20", Label: "ATR 20", Decimals: 2},
		{Source: hvPercentileColumn, Label: "HV PR100", Decimals: 1},
		{Source: ivPercentileColumn, Label: "IV PR200 12H", Decimals: 1},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	ctx.Register("atr20", backtest.ATR(atrPeriod))
	ctx.Register("ret", optutil.PercentChange("close"))
	ctx.Register("hv_100", optutil.RollingStdDevIndicator("ret", 10))
	ctx.Register(hvPercentileColumn, optutil.PercentileRank("hv_100", 100))

	s.dvolRef = ctx.AddFactor("dvol", dvolInterval)
	ctx.RegisterFactor(s.dvolRef, ivPercentileColumn, optutil.PercentileRank("close", ivLookbackBars))

	ctx.SetWarmup(160 * 24 * time.Hour)
	ctx.SetParam("signal_source", s.signalSource)
	ctx.SetParam("signal_count", float64(len(s.entryTimes)))
	ctx.SetParam("ambush_credit_base_btc", ambushPremiumBaseBTC)
	ctx.SetParam("trend_amount_base_btc", trendAmountBaseBTC)
	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}

	entrySignal := make([]float64, primary.Len())
	for i, ts := range primary.Timestamps() {
		if _, ok := s.entryTimes[ts.UTC().Unix()]; ok {
			entrySignal[i] = 1
		}
	}
	if err := primary.SetColumn("entry_signal", entrySignal); err != nil {
		return err
	}

	ivAligned, err := ctx.ColumnAlignedFactorToPrimary(s.dvolRef, ivPercentileColumn)
	if err != nil {
		return err
	}
	return primary.SetColumn(ivPercentileColumn, ivAligned)
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)

	s.manageActiveStates(ctx, chain, contractMap)

	if s.isEntrySignal(ctx) && s.currentIVPercentile(ctx) <= ambushIVPercentileMax {
		s.openAmbush(ctx, chain, "外部信号开仓")
	}
}

func (s *strategy) manageActiveStates(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract) {
	if !s.hasActiveStates() {
		return
	}

	nextStates := make([]activeState, 0, len(s.activeStates))
	for _, state := range s.activeStates {
		state.spreadIDs = s.openSpreadIDs(ctx, state.spreadIDs)
		if len(state.spreadIDs) == 0 {
			s.closeGroupIfNeeded(ctx, state.groupID)
			continue
		}

		switch state.phase {
		case phaseAmbush:
			nextStates = append(nextStates, s.manageAmbush(ctx, chain, contractMap, state)...)
		case phaseTrend:
			nextStates = append(nextStates, s.manageTrend(ctx, chain, contractMap, state)...)
		}
	}
	s.activeStates = nextStates
}

func (s *strategy) manageAmbush(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract, state activeState) []activeState {
	totalPnL := s.totalUnrealizedPnL(ctx, state.spreadIDs, contractMap)
	if !math.IsNaN(totalPnL) && totalPnL >= ambushTakeProfit2BTC {
		if s.closeSpreadSet(ctx, state.spreadIDs, contractMap, "一期止盈60%转二期") {
			s.closeGroupIfNeeded(ctx, state.groupID)
			if nextState, ok := s.openTrendState(ctx, chain, trendAmountBaseBTC, "一期止盈转换"); ok {
				return []activeState{nextState}
			}
			return nil
		}
		return []activeState{state}
	}

	stopLevel := state.entryUnderlying + ambushStopATRMultiplier*state.entryATR
	if !math.IsNaN(stopLevel) && ctx.Close() >= stopLevel {
		if s.closeSpreadSet(ctx, state.spreadIDs, contractMap, "一期反向8ATR退出") {
			s.closeGroupIfNeeded(ctx, state.groupID)
			return nil
		}
		return []activeState{state}
	}

	if s.shouldRollAmbush(ctx, contractMap, state) {
		if s.closeSpreadSet(ctx, state.spreadIDs, contractMap, "一期近月滚动") {
			s.closeGroupIfNeeded(ctx, state.groupID)
			if nextState, ok := s.openAmbushState(ctx, chain, "一期滚动重建"); ok {
				return []activeState{nextState}
			}
			return nil
		}
		return []activeState{state}
	}

	if !state.partialTaken && !math.IsNaN(totalPnL) && totalPnL >= ambushTakeProfit1BTC {
		spreadID := s.firstOpenSpreadID(ctx, state.spreadIDs)
		if spreadID > 0 && s.closeSpreadSet(ctx, []int{spreadID}, contractMap, "一期止盈33%减仓") {
			state.partialTaken = true
			state.spreadIDs = s.openSpreadIDs(ctx, state.spreadIDs)
			if len(state.spreadIDs) == 0 {
				s.closeGroupIfNeeded(ctx, state.groupID)
				return nil
			}
		}
	}

	return []activeState{state}
}

func (s *strategy) manageTrend(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract, state activeState) []activeState {
	spreadID := s.firstOpenSpreadID(ctx, state.spreadIDs)
	if spreadID <= 0 {
		s.closeGroupIfNeeded(ctx, state.groupID)
		return nil
	}

	sp := ctx.Spreads().Get(spreadID)
	if sp == nil || len(sp.Legs) < 2 {
		return []activeState{state}
	}

	needExpiryClose := false
	for _, leg := range sp.Legs {
		if leg.Closed {
			continue
		}
		contract := optutil.ResolveContract(leg.Contract, contractMap)
		if contract.DaysToExpiry(ctx.Time()) <= 1 {
			needExpiryClose = true
			break
		}
	}
	if needExpiryClose {
		if s.closeSpreadSet(ctx, []int{spreadID}, contractMap, "二期到期前平仓") {
			s.closeGroupIfNeeded(ctx, state.groupID)
			return nil
		}
		return []activeState{state}
	}

	shouldRoll, reason := s.shouldRollTrend(sp, contractMap, state)
	if !shouldRoll {
		return []activeState{state}
	}

	if !s.closeSpreadSet(ctx, []int{spreadID}, contractMap, reason) {
		return []activeState{state}
	}

	group := ctx.SpreadGroups().Get(state.groupID)
	if group == nil {
		return nil
	}
	ctx.SpreadGroups().IncrementRoll(state.groupID)
	amount := group.CurrentAmount()
	if amount <= 0 {
		s.closeGroupIfNeeded(ctx, state.groupID)
		return nil
	}

	nextState, ok := s.openTrendInGroupState(ctx, chain, amount, state.groupID, reason)
	if !ok {
		s.closeGroupIfNeeded(ctx, state.groupID)
		return nil
	}
	return []activeState{nextState}
}

func (s *strategy) openAmbush(ctx *backtest.BarContext, chain *backtest.OptionsChain, reason string) bool {
	state, ok := s.openAmbushState(ctx, chain, reason)
	if !ok {
		return false
	}
	s.activeStates = append(s.activeStates, state)
	return true
}

func (s *strategy) openAmbushState(ctx *backtest.BarContext, chain *backtest.OptionsChain, reason string) (activeState, bool) {
	selection, ok := s.selectAmbush(chain, ctx.Time())
	if !ok {
		return activeState{}, false
	}

	shortQtyTotal := ambushPremiumBaseBTC / selection.shortPrice
	if shortQtyTotal <= 0 {
		return activeState{}, false
	}

	groupID := s.OpenGroup(ctx, "retracement-ratio-protective-spread|ambush", ambushPremiumBaseBTC, 1.0)
	if groupID <= 0 {
		return activeState{}, false
	}

	perTrancheQty := shortQtyTotal / float64(ambushTrancheCount)
	spreadIDs := make([]int, 0, ambushTrancheCount)
	for tranche := 0; tranche < ambushTrancheCount; tranche++ {
		spreadID := s.OpenSpreadInGroup(ctx, []backtest.SpreadLeg{
			{Contract: selection.short, Side: backtest.Sell, Qty: perTrancheQty, EntryPrice: selection.shortPrice},
			{Contract: selection.longLowerHalf, Side: backtest.Buy, Qty: perTrancheQty, EntryPrice: selection.longLowerPrice},
			{Contract: selection.longUpperHalf, Side: backtest.Buy, Qty: perTrancheQty, EntryPrice: selection.longUpperPrice},
		}, fmt.Sprintf("一期比例价差|%s|tranche=%d", reason, tranche+1), groupID)
		if spreadID <= 0 {
			s.closeSpreadSet(ctx, spreadIDs, optutil.BuildContractMap(chain), "一期开仓回滚")
			s.closeGroupIfNeeded(ctx, groupID)
			return activeState{}, false
		}
		spreadIDs = append(spreadIDs, spreadID)
	}

	return activeState{
		phase:           phaseAmbush,
		groupID:         groupID,
		spreadIDs:       spreadIDs,
		entryUnderlying: ctx.Close(),
		entryATR:        ctx.Ind("atr20"),
	}, true
}

func (s *strategy) openTrend(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64, reason string) bool {
	state, ok := s.openTrendState(ctx, chain, amount, reason)
	if !ok {
		return false
	}
	s.activeStates = append(s.activeStates, state)
	return true
}

func (s *strategy) openTrendState(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64, reason string) (activeState, bool) {
	groupID := s.OpenGroup(ctx, "retracement-ratio-protective-spread|trend", amount, trendDecayFactor)
	if groupID <= 0 {
		return activeState{}, false
	}
	state, ok := s.openTrendInGroupState(ctx, chain, amount, groupID, reason)
	if !ok {
		s.closeGroupIfNeeded(ctx, groupID)
		return activeState{}, false
	}
	return state, true
}

func (s *strategy) openTrendInGroupState(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64, groupID int, reason string) (activeState, bool) {
	selection, ok := s.selectTrend(chain, ctx.Time(), s.currentHVPercentile(ctx), s.currentIVPercentile(ctx), amount)
	if !ok {
		return activeState{}, false
	}

	spreadID := s.OpenSpreadInGroup(ctx, []backtest.SpreadLeg{
		{Contract: selection.long, Side: backtest.Buy, Qty: selection.qty, EntryPrice: selection.longPrice},
		{Contract: selection.short, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.shortPrice},
	}, fmt.Sprintf("二期借记价差|%s|amt=%.2f", reason, amount), groupID)
	if spreadID <= 0 {
		return activeState{}, false
	}

	return activeState{
		phase:             phaseTrend,
		groupID:           groupID,
		spreadIDs:         []int{spreadID},
		entryUnderlying:   ctx.Close(),
		entryATR:          ctx.Ind("atr20"),
		entryLongAbsDelta: math.Abs(selection.long.Delta),
	}, true
}

func (s *strategy) selectAmbush(chain *backtest.OptionsChain, now time.Time) (*ambushSelection, bool) {
	if chain == nil || chain.Len() == 0 {
		return nil, false
	}

	puts := chain.Puts().ExpiryRange(ambushMinDTE, ambushMaxDTE)
	if puts.Len() == 0 {
		return nil, false
	}

	expiries := uniqueExpiriesNearest(puts.Contracts(), now, ambushTargetDTE)
	bestScore := math.Inf(1)
	var best *ambushSelection

	for _, expiry := range expiries {
		contracts := contractsForExpiry(puts.Contracts(), expiry)
		sort.Slice(contracts, func(i, j int) bool {
			return contracts[i].StrikePrice > contracts[j].StrikePrice
		})

		orderedShorts := append([]backtest.OptionContract(nil), contracts...)
		sort.Slice(orderedShorts, func(i, j int) bool {
			di := math.Abs(math.Abs(orderedShorts[i].Delta) - 0.5)
			dj := math.Abs(math.Abs(orderedShorts[j].Delta) - 0.5)
			if di != dj {
				return di < dj
			}
			return orderedShorts[i].StrikePrice > orderedShorts[j].StrikePrice
		})

		for _, short := range orderedShorts {
			shortPrice, ok := s.ValidEntryPrice(backtest.Sell, short)
			if !ok {
				continue
			}

			longLowerHalf, longLowerPrice, longUpperHalf, longUpperPrice, pairScore, ok := s.selectAmbushLongPair(contracts, short, shortPrice)
			if !ok {
				continue
			}

			score := pairScore + math.Abs(math.Abs(short.Delta)-0.5)
			if score >= bestScore {
				continue
			}

			best = &ambushSelection{
				short:          short,
				longLowerHalf:  longLowerHalf,
				longUpperHalf:  longUpperHalf,
				shortPrice:     shortPrice,
				longLowerPrice: longLowerPrice,
				longUpperPrice: longUpperPrice,
			}
			bestScore = score
		}
	}

	return best, best != nil
}

func (s *strategy) selectAmbushLongPair(contracts []backtest.OptionContract, short backtest.OptionContract, shortPrice float64) (backtest.OptionContract, float64, backtest.OptionContract, float64, float64, bool) {
	targetHalf := shortPrice / 2
	type pricedContract struct {
		contract backtest.OptionContract
		price    float64
		index    int
	}

	candidates := make([]pricedContract, 0, len(contracts))
	for index, contract := range contracts {
		if contract.Symbol == short.Symbol || contract.StrikePrice >= short.StrikePrice {
			continue
		}
		price, ok := s.ValidEntryPrice(backtest.Buy, contract)
		if !ok {
			continue
		}
		candidates = append(candidates, pricedContract{contract: contract, price: price, index: index})
	}

	bestScore := math.Inf(1)
	var bestLower pricedContract
	var bestUpper pricedContract
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			lower := candidates[i]
			upper := candidates[j]
			if lower.price > upper.price {
				lower, upper = upper, lower
			}
			if lower.price > targetHalf || upper.price < targetHalf {
				continue
			}

			totalGap := math.Abs((lower.price + upper.price) - shortPrice)
			halfGap := math.Abs(targetHalf-lower.price) + math.Abs(upper.price-targetHalf)
			adjacencyPenalty := math.Abs(float64(lower.index-upper.index)-1) * 0.01
			score := totalGap + 0.5*halfGap + adjacencyPenalty
			if score >= bestScore {
				continue
			}

			bestLower = lower
			bestUpper = upper
			bestScore = score
		}
	}

	if math.IsInf(bestScore, 1) {
		return backtest.OptionContract{}, 0, backtest.OptionContract{}, 0, 0, false
	}
	return bestLower.contract, bestLower.price, bestUpper.contract, bestUpper.price, bestScore, true
}

func (s *strategy) selectTrend(chain *backtest.OptionsChain, now time.Time, hvPercentile, ivPercentile, amount float64) (*trendSelection, bool) {
	if chain == nil || chain.Len() == 0 || amount <= 0 {
		return nil, false
	}

	puts := chain.Puts().ExpiryRange(trendMinDTE, trendMaxDTE)
	if puts.Len() == 0 {
		return nil, false
	}

	targetLongAbsDelta := dynamicLongDelta(hvPercentile, ivPercentile)
	expiries := uniqueExpiriesNearest(puts.Contracts(), now, trendTargetDTE)

	for _, expiry := range expiries {
		contracts := contractsForExpiry(puts.Contracts(), expiry)
		orderedLongs := make([]backtest.OptionContract, 0, len(contracts))
		for _, contract := range contracts {
			orderedLongs = append(orderedLongs, contract)
		}
		sort.Slice(orderedLongs, func(i, j int) bool {
			di := math.Abs(math.Abs(orderedLongs[i].Delta) - targetLongAbsDelta)
			dj := math.Abs(math.Abs(orderedLongs[j].Delta) - targetLongAbsDelta)
			if di != dj {
				return di < dj
			}
			return orderedLongs[i].StrikePrice > orderedLongs[j].StrikePrice
		})

		for _, long := range orderedLongs {
			longPrice, ok := s.ValidEntryPrice(backtest.Buy, long)
			if !ok {
				continue
			}

			maxShortStrike := long.StrikePrice * shortStrikeMaxMultiple
			var shortCandidates []backtest.OptionContract
			for _, candidate := range contracts {
				if candidate.Symbol == long.Symbol {
					continue
				}
				absDelta := math.Abs(candidate.Delta)
				if candidate.StrikePrice > maxShortStrike || absDelta < minShortDelta || absDelta > maxShortDelta {
					continue
				}
				shortCandidates = append(shortCandidates, candidate)
			}
			if len(shortCandidates) == 0 {
				continue
			}

			sort.Slice(shortCandidates, func(i, j int) bool {
				di := math.Abs(shortCandidates[i].StrikePrice - maxShortStrike)
				dj := math.Abs(shortCandidates[j].StrikePrice - maxShortStrike)
				if di != dj {
					return di < dj
				}
				return shortCandidates[i].StrikePrice > shortCandidates[j].StrikePrice
			})

			for _, short := range shortCandidates {
				shortPrice, ok := s.ValidEntryPrice(backtest.Sell, short)
				if !ok {
					continue
				}
				spreadCost := longPrice - shortPrice
				if spreadCost <= 0 {
					continue
				}
				qty := amount / spreadCost
				if qty <= 0 {
					continue
				}
				return &trendSelection{
					long:       long,
					short:      short,
					longPrice:  longPrice,
					shortPrice: shortPrice,
					spreadCost: spreadCost,
					qty:        qty,
				}, true
			}
		}
	}

	return nil, false
}

func (s *strategy) shouldRollAmbush(ctx *backtest.BarContext, contractMap map[string]backtest.OptionContract, state activeState) bool {
	if len(state.spreadIDs) == 0 {
		return false
	}

	minDTE := math.Inf(1)
	for _, spreadID := range state.spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil {
			continue
		}
		for _, leg := range sp.Legs {
			if leg.Closed {
				continue
			}
			contract := optutil.ResolveContract(leg.Contract, contractMap)
			dte := contract.DaysToExpiry(ctx.Time())
			if dte < minDTE {
				minDTE = dte
			}
		}
	}

	if math.IsInf(minDTE, 1) || minDTE <= ambushRollMinDTE || minDTE >= ambushRollMaxDTE {
		return false
	}
	atr := ctx.Ind("atr20")
	if math.IsNaN(atr) {
		return false
	}
	return math.Abs(ctx.Close()-state.entryUnderlying) <= ambushRollATRDistance*atr
}

func (s *strategy) shouldRollTrend(sp *backtest.SpreadPosition, contractMap map[string]backtest.OptionContract, state activeState) (bool, string) {
	if sp == nil || len(sp.Legs) < 2 {
		return false, ""
	}

	longLeg := sp.Legs[0]
	shortLeg := sp.Legs[1]
	if longLeg.Closed || shortLeg.Closed {
		return false, ""
	}

	longContract := optutil.ResolveContract(longLeg.Contract, contractMap)
	shortContract := optutil.ResolveContract(shortLeg.Contract, contractMap)
	longMark := s.ValuationPriceMode.ExitPrice(longLeg.Side, longContract)
	shortMark := s.ValuationPriceMode.ExitPrice(shortLeg.Side, shortContract)
	if math.IsNaN(longMark) || math.IsNaN(shortMark) {
		return false, ""
	}

	entryCost := longLeg.EntryPrice - shortLeg.EntryPrice
	currentValue := longMark - shortMark
	if entryCost > 0 {
		pnlPct := (currentValue - entryCost) / entryCost
		if pnlPct >= trendRollProfitPct {
			return true, fmt.Sprintf("二期换仓|价值上涨%.0f%%", pnlPct*100)
		}
	}

	currentLongAbsDelta := math.Abs(longContract.Delta)
	if currentLongAbsDelta-state.entryLongAbsDelta >= trendRollDeltaIncrease {
		return true, fmt.Sprintf("二期换仓|Delta增加%.2f", currentLongAbsDelta-state.entryLongAbsDelta)
	}

	return false, ""
}

func (s *strategy) currentHVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Ind(hvPercentileColumn)
	if math.IsNaN(value) {
		return defaultVolPercentile
	}
	return value
}

func (s *strategy) currentIVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Factor(s.dvolRef).Ind(ivPercentileColumn)
	if math.IsNaN(value) {
		return defaultVolPercentile
	}
	return value
}

func (s *strategy) totalUnrealizedPnL(ctx *backtest.BarContext, spreadIDs []int, contractMap map[string]backtest.OptionContract) float64 {
	total := 0.0
	found := false
	for _, spreadID := range spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() {
			continue
		}
		total += sp.TotalUnrealizedPnL(func(contract backtest.OptionContract) float64 {
			resolved := optutil.ResolveContract(contract, contractMap)
			return s.ValuationPriceMode.ExitPrice(backtest.Buy, resolved)
		})
		found = true
	}
	if !found {
		return math.NaN()
	}
	return total
}

func (s *strategy) closeSpreadSet(ctx *backtest.BarContext, spreadIDs []int, contractMap map[string]backtest.OptionContract, reason string) bool {
	for _, spreadID := range spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil {
			continue
		}
		for legIdx := range sp.Legs {
			leg := sp.Legs[legIdx]
			if leg.Closed {
				continue
			}
			contract := optutil.ResolveContract(leg.Contract, contractMap)
			closePrice := s.ExitPriceMode.ExitPrice(leg.Side, contract)
			if !optutil.CloseLeg(ctx, spreadID, legIdx, closePrice, reason) {
				return false
			}
		}
	}
	return true
}

func (s *strategy) openSpreadIDs(ctx *backtest.BarContext, spreadIDs []int) []int {
	filtered := make([]int, 0, len(spreadIDs))
	for _, spreadID := range spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() {
			continue
		}
		filtered = append(filtered, spreadID)
	}
	return filtered
}

func (s *strategy) firstOpenSpreadID(ctx *backtest.BarContext, spreadIDs []int) int {
	for _, spreadID := range spreadIDs {
		sp := ctx.Spreads().Get(spreadID)
		if sp != nil && !sp.IsFullyClosed() {
			return spreadID
		}
	}
	return 0
}

func (s *strategy) hasActiveStates() bool {
	return len(s.activeStates) > 0
}

func (s *strategy) closeGroupIfNeeded(ctx *backtest.BarContext, groupID int) {
	if groupID > 0 {
		s.CloseGroup(ctx, groupID)
	}
}

func (s *strategy) applyDefaults() {
	s.ApplyPricingDefaults()
	if s.entryTimes == nil {
		s.entryTimes = map[int64]struct{}{}
	}
}

func (s *strategy) isEntrySignal(ctx *backtest.BarContext) bool {
	return ctx.Ind("entry_signal") == 1
}

func dynamicLongDelta(hvPercentile, ivPercentile float64) float64 {
	value := ((2 * hvPercentile) + ivPercentile) / 300.0
	value -= 0.1
	if value < minLongDelta {
		return minLongDelta
	}
	if value > maxLongDelta {
		return maxLongDelta
	}
	return value
}

func uniqueExpiriesNearest(contracts []backtest.OptionContract, now time.Time, targetDTE int) []time.Time {
	seen := make(map[int64]time.Time, len(contracts))
	for _, contract := range contracts {
		key := contract.Expiration.UTC().Unix()
		seen[key] = contract.Expiration
	}
	expiries := make([]time.Time, 0, len(seen))
	for _, expiry := range seen {
		expiries = append(expiries, expiry)
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
	expUTC := expiry.UTC()
	for _, contract := range contracts {
		if contract.Expiration.UTC().Equal(expUTC) {
			filtered = append(filtered, contract)
		}
	}
	return filtered
}

func mustGetSignalSource() string {
	raw := strings.TrimSpace(os.Getenv(signalSourceEnv))
	source, err := parseSignalSource(raw)
	if err != nil {
		fmt.Printf("[%s] invalid or missing %s=%q; expected one of: 12h, 1d\n", strategyName, signalSourceEnv, raw)
		panic(err.Error())
	}
	return source
}

func parseSignalSource(raw string) (string, error) {
	source := strings.ToLower(strings.TrimSpace(raw))
	switch source {
	case "12h", "1d":
		return source, nil
	case "":
		return "", fmt.Errorf("missing required environment variable %s", signalSourceEnv)
	default:
		return "", fmt.Errorf("invalid environment variable %s=%q", signalSourceEnv, raw)
	}
}

func loadSignalTimesForSource(source string) (map[int64]struct{}, error) {
	switch source {
	case "12h":
		return loadSignalTimesFromCSV(signalPath12h)
	case "1d":
		return loadSignalTimesFromCSV(signalPath1d)
	default:
		return nil, fmt.Errorf("unsupported signal source %q", source)
	}
}

func loadSignalTimesFromCSV(relPath string) (map[int64]struct{}, error) {
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

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse csv %s: %w", path, err)
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	times := make(map[int64]struct{}, len(records))
	for i, record := range records {
		if i == 0 || len(record) < 3 {
			continue
		}
		dateStr := strings.TrimSpace(record[2])
		if dateStr == "" {
			continue
		}
		ts, err := time.ParseInLocation(signalTimeLayout, dateStr, utc8)
		if err != nil {
			continue
		}
		times[ts.UTC().Unix()] = struct{}{}
	}
	return times, nil
}
