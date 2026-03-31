// Package buyflashlow implements the "均线过滤版震荡下沿插针评分系统_V2" strategy.
//
// Entry logic (all conditions must hold):
//   - Current bar touches or dips into the lower boundary zone (lowest low of previous
//     lookback bars ± 0.7 × ATR) — "flash low / pin at support"
//   - Bullish pin bar: close is in the upper half of the bar's range
//   - Amplitude percentile rank > minAmpPr across last 100 bars
//   - Composite score (amplitude + volume) meets the threshold
//     (stricter threshold when MAs are in full bearish alignment)
//
// Exit logic:
//   - Trailing drawdown from highest-since-entry exceeds 2 × ATR
package buyflashlow

import (
	"math"
	"strconv"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultLookback       = 20
	defaultMinAmpPr       = 66.0
	defaultScoreThreshold = 3
	defaultStrictScore    = 5
	defaultDvolMinPr      = 60.0
	dvolDataInterval      = "1d"
	dvolPrWindowShortDays = 90
	dvolPrWindowLongDays  = 360
	dvolColumn            = "dvol"
	dvolPrShortColumn     = "dvol_pr_90"
	dvolPrLongColumn      = "dvol_pr_360"
	defaultSpotNotional   = 1e-6
	defaultTargetDTE      = 15
	defaultMinDTE         = 7
	defaultShortDeltaMin  = 0.15
	defaultShortDeltaMax  = 0.35
	defaultPremiumTarget  = 3.0
	defaultMaxContracts   = 100.0
	defaultTakeProfit1    = 0.70
	defaultTakeProfit2    = 0.88
	positionGroupTag      = "buy-flash-low-short-put"
	positionGroupDecay    = 1.0
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "buy-flash-low",
		Aliases: []string{"buy_flash_low"},
		Groups:  []string{"momentum", "single-leg"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeSignalOnly},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &buyFlashLowStrategy{
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				lookback:           catalog.IntOrDefault(cfg.FastPeriod, defaultLookback),
				minAmpPr:           catalog.FloatOrDefault(cfg.PThreshold, defaultMinAmpPr),
				scoreThreshold:     catalog.IntOrDefault(cfg.SlowPeriod, defaultScoreThreshold),
				strictScore:        defaultStrictScore,
				dvolMinPr:          defaultDvolMinPr,
				targetExpiryDays:   catalog.IntOrDefault(cfg.TargetExpiryDays, defaultTargetDTE),
				minExpiryDays:      catalog.IntOrDefault(cfg.MinExpiryDays, defaultMinDTE),
				shortDeltaMin:      catalog.FloatOrDefault(cfg.ShortDeltaMin, defaultShortDeltaMin),
				shortDeltaMax:      catalog.FloatOrDefault(cfg.ShortDeltaMax, defaultShortDeltaMax),
			}, nil
		},
	})
}

type buyFlashLowStrategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	lookback         int
	minAmpPr         float64
	scoreThreshold   int
	strictScore      int
	dvolMinPr        float64
	targetExpiryDays int
	minExpiryDays    int
	shortDeltaMin    float64
	shortDeltaMax    float64
	spotNotional     float64
	premiumTarget    float64
	maxContracts     float64
	takeProfit1      float64
	takeProfit2      float64

	// runtime state
	highestSinceEntry  float64
	optionSpreadIDs    [2]int
	pendingOptionRefs  [2]string
	pendingOptionTimes [2]time.Time
	dvolFactor         backtest.FactorRef
	positionGroupID    int
	nextPendingRefID   int
}

func (s *buyFlashLowStrategy) Name() string { return "BuyFlashLow" }

func (s *buyFlashLowStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

// ReportColumns implements backtest.ReportColumnProvider so the HTML report's
// data window shows key indicator values when hovering over the candlestick chart.
func (s *buyFlashLowStrategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "atr", Label: "ATR", Decimals: 2},
		{Source: "sma_20", Label: "SMA 20", Decimals: 2},
		{Source: "vol_norm", Label: "Volume", Decimals: 0},
		{Source: "vol_sma100", Label: "Vol SMA 100", Decimals: 0},
		{Source: "amp_pr100", Label: "Amp PR%", Decimals: 1},
		{Source: "amp_score", Label: "Amp Score", Decimals: 0},
		{Source: "vol_score", Label: "Vol Score", Decimals: 0},
		{Source: "l_prev", Label: "Support", Decimals: 2},
		{Source: "factor.dvol." + dvolDataInterval + ".close", Label: "DVOL", Decimals: 2},
		{Source: "factor.dvol." + dvolDataInterval + "." + dvolPrShortColumn, Label: "DVOL PR90%", Decimals: 1},
		{Source: "factor.dvol." + dvolDataInterval + "." + dvolPrLongColumn, Label: "DVOL PR360%", Decimals: 1},
	}
}

func (s *buyFlashLowStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()
	s.highestSinceEntry = math.NaN()

	lkb := s.lookback
	ctx.SetParam("lookback", lkb)
	ctx.SetParam("min_amp_pr", s.minAmpPr)
	ctx.SetParam("score_threshold", s.scoreThreshold)
	ctx.SetParam("dvol_min_pr", s.dvolMinPr)
	ctx.SetParam("dvol_interval", dvolDataInterval)

	// DVOL factor is used only by the options leg as an additional volatility filter.
	s.dvolFactor = ctx.AddFactor("dvol", dvolDataInterval)
	ctx.RegisterFactor(s.dvolFactor, dvolColumn, backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return inputs["close"]
		},
	))
	ctx.RegisterFactor(s.dvolFactor, dvolPrShortColumn, percentileRank("close", dvolPrWindowShortDays))
	ctx.RegisterFactor(s.dvolFactor, dvolPrLongColumn, percentileRank("close", dvolPrWindowLongDays))

	ctx.Register("atr", backtest.ATR(lkb))
	ctx.Register("sma_2", backtest.SMA("close", 2))
	ctx.Register("sma_6", backtest.SMA("close", 6))
	ctx.Register("sma_10", backtest.SMA("close", 10))
	ctx.Register("sma_15", backtest.SMA("close", 15))
	ctx.Register("sma_20", backtest.SMA("close", 20))
	// vol_norm: unified volume series — prefers "volume", falls back to
	// "tick_count", and is all-NaN when neither column is present.
	ctx.Register("vol_norm", backtest.CustomOptional(
		[]string{},
		[]string{"volume", "tick_count"},
		func(inputs map[string][]float64) []float64 {
			if col, ok := inputs["volume"]; ok && !allNaN(col) {
				return col
			}
			if col, ok := inputs["tick_count"]; ok && !allNaN(col) {
				return col
			}
			// Neither available — return whatever we have (all-NaN) so
			// downstream indicators degrade gracefully.
			if col, ok := inputs["volume"]; ok {
				return col
			}
			if col, ok := inputs["tick_count"]; ok {
				return col
			}
			return make([]float64, 0)
		},
	))
	ctx.Register("vol_sma100", backtest.SMA("vol_norm", 100))

	// Lowest low of the previous lkb bars — ta.lowest(low, lkb)[1] in Pine Script.
	// At bar i this equals min(low[i-lkb], ..., low[i-1]).
	ctx.Register("l_prev", backtest.Custom(
		[]string{"low"},
		func(inputs map[string][]float64) []float64 {
			low := inputs["low"]
			n := len(low)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < lkb {
					out[i] = math.NaN()
					continue
				}
				minVal := math.Inf(1)
				for j := i - lkb; j < i; j++ {
					if !math.IsNaN(low[j]) && low[j] < minVal {
						minVal = low[j]
					}
				}
				if math.IsInf(minVal, 1) {
					out[i] = math.NaN()
				} else {
					out[i] = minVal
				}
			}
			return out
		},
	))

	// Amplitude = (high − low) / close
	ctx.Register("amp", backtest.Custom(
		[]string{"high", "low", "close"},
		func(inputs map[string][]float64) []float64 {
			high := inputs["high"]
			low := inputs["low"]
			cls := inputs["close"]
			n := len(high)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(cls[i]) || cls[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (high[i] - low[i]) / cls[i]
			}
			return out
		},
	))

	// Percentile rank of amplitude over the last 100 bars (ta.percentrank in Pine Script).
	// Returns the percentage of the 100 most-recent historical bars where amp < current amp.
	ctx.Register("amp_pr100", backtest.Custom(
		[]string{"amp"},
		func(inputs map[string][]float64) []float64 {
			amp := inputs["amp"]
			n := len(amp)
			out := make([]float64, n)
			const prPeriod = 100
			for i := 0; i < n; i++ {
				if i < prPeriod || math.IsNaN(amp[i]) {
					out[i] = math.NaN()
					continue
				}
				count := 0
				for j := i - prPeriod; j < i; j++ {
					if !math.IsNaN(amp[j]) && amp[j] < amp[i] {
						count++
					}
				}
				out[i] = float64(count) / float64(prPeriod) * 100
			}
			return out
		},
	))
	ctx.Register("amp_score", backtest.Custom(
		[]string{"amp_pr100"},
		func(inputs map[string][]float64) []float64 {
			ampPr100 := inputs["amp_pr100"]
			n := len(ampPr100)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				out[i] = float64(computeAmpScore(ampPr100[i]))
			}
			return out
		},
	))

	// Volume rank: 1 = highest volume in the window.
	// Mirrors Pine Script's get_vol_rank(len): rank = 1 + #{past bars where vol > current}.
	ctx.Register("vol_rank_10", backtest.Custom([]string{"vol_norm"}, makeVolRank(20)))
	ctx.Register("vol_rank_20", backtest.Custom([]string{"vol_norm"}, makeVolRank(60)))
	ctx.Register("vol_rank_100", backtest.Custom([]string{"vol_norm"}, makeVolRank(180)))
	ctx.Register("vol_score", backtest.Custom(
		[]string{"vol_rank_10", "vol_rank_20", "vol_rank_100", "vol_sma100", "vol_norm"},
		func(inputs map[string][]float64) []float64 {
			volRank10 := inputs["vol_rank_10"]
			volRank20 := inputs["vol_rank_20"]
			volRank100 := inputs["vol_rank_100"]
			volSMA100 := inputs["vol_sma100"]
			volNorm := inputs["vol_norm"]
			n := len(volNorm)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				out[i] = float64(computeVolScore(volRank10[i], volRank20[i], volRank100[i], volSMA100[i], volNorm[i]))
			}
			return out
		},
	))

	return nil
}

// makeVolRank returns a compute function that ranks the current bar's volume (1 = highest)
// against the previous `window` bars.
func makeVolRank(window int) func(inputs map[string][]float64) []float64 {
	return func(inputs map[string][]float64) []float64 {
		vol := inputs["vol_norm"]
		n := len(vol)
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			if math.IsNaN(vol[i]) {
				out[i] = math.NaN()
				continue
			}
			rank := 1
			start := i - window
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				if !math.IsNaN(vol[j]) && vol[j] > vol[i] {
					rank++
				}
			}
			out[i] = float64(rank)
		}
		return out
	}
}

func (s *buyFlashLowStrategy) OnBar(ctx *backtest.BarContext) {
	// Skip the warmup period (mirrors Pine Script's bar_index >= 100 guard).
	if ctx.BarIndex() < 100 {
		return
	}
	s.resolvePendingShortPutOpens(ctx)

	primary := ctx.PrimaryRef()
	high := ctx.High()
	low := ctx.Low()
	cls := ctx.Close()
	vol := ctx.Ind("vol_norm")
	now := ctx.Time()
	chain := ctx.OptionsChain()
	contractMap := s.buildContractMap(chain)
	hasOpenOptionPosition := s.manageOpenShortPuts(ctx, now, contractMap)

	atr := ctx.Ind("atr")
	if math.IsNaN(atr) || atr <= 0 {
		return
	}

	// ── MA bearish-alignment check ─────────────────────────────────────────
	// is_bearish = ma2 < ma6+buf && ma6 < ma10+buf && ma10 < ma15+buf && ma15 < ma20+buf
	ma2 := ctx.Ind("sma_2")
	ma6 := ctx.Ind("sma_6")
	ma10 := ctx.Ind("sma_10")
	ma15 := ctx.Ind("sma_15")
	ma20 := ctx.Ind("sma_20")

	isBearish := false
	if !math.IsNaN(ma2) && !math.IsNaN(ma6) && !math.IsNaN(ma10) &&
		!math.IsNaN(ma15) && !math.IsNaN(ma20) {
		buf := 0.05 * atr
		isBearish = ma2 < ma6+buf && ma6 < ma10+buf && ma10 < ma15+buf && ma15 < ma20+buf
	}

	currentThreshold := s.scoreThreshold
	if isBearish {
		currentThreshold = s.strictScore
	}

	hasPendingOrder := func(side backtest.Side) bool {
		for _, order := range ctx.PendingOrders() {
			if order.Security == primary && order.Side == side {
				return true
			}
		}
		return false
	}

	positionQty := ctx.Position(primary)
	hasLongPosition := positionQty > 0
	hasPendingBuy := hasPendingOrder(backtest.Buy)
	hasPendingSell := hasPendingOrder(backtest.Sell)

	// ── Exit management ────────────────────────────────────────────────────
	// Mirror the Pine strategy's state machine: seed the trailing anchor from
	// the signal bar close, then trail against the highest value seen since entry.
	if hasLongPosition {
		if math.IsNaN(s.highestSinceEntry) {
			s.highestSinceEntry = cls
		} else {
			s.highestSinceEntry = math.Max(s.highestSinceEntry, high)
		}
		if !hasPendingSell && s.highestSinceEntry-cls > 2*atr {
			stopPrice := s.highestSinceEntry - 2*atr
			ctx.ClosePositionStopNowWithNote(primary, stopPrice, 0.005, "buy flash low intrabar stop")
			hasPendingSell = true
		}
	} else if !hasPendingBuy {
		s.highestSinceEntry = math.NaN()
	}

	// ── Entry signal evaluation ────────────────────────────────────────────
	if hasLongPosition || hasPendingBuy || hasPendingSell {
		return
	}

	lPrev := ctx.Ind("l_prev")
	if math.IsNaN(lPrev) {
		return
	}

	// in_bot: current bar touches or dips into the support zone
	inBot := low <= (lPrev+0.7*atr) && high >= lPrev

	// Shape: close in upper half of bar (bullish pin) with large relative range
	isPinShape := cls > 0.5*(high+low)
	ampPr100 := ctx.Ind("amp_pr100")
	shapeEntry := isPinShape && !math.IsNaN(ampPr100) && ampPr100 > s.minAmpPr

	if !inBot || !shapeEntry {
		return
	}

	// ── Score computation ──────────────────────────────────────────────────
	ampScore := computeAmpScore(ampPr100)

	volRank10 := ctx.Ind("vol_rank_10")
	volRank20 := ctx.Ind("vol_rank_20")
	volRank100 := ctx.Ind("vol_rank_100")
	volSMA100 := ctx.Ind("vol_sma100")

	volScore := computeVolScore(volRank10, volRank20, volRank100, volSMA100, vol)

	if ampScore+volScore < currentThreshold {
		return
	}

	// ── Entry ──────────────────────────────────────────────────────────────
	if cls > 0 {
		s.highestSinceEntry = cls
		qty := s.spotNotional / cls
		ctx.Buy(primary, qty)
		if !hasOpenOptionPosition {
			if s.shouldOpenShortPut(ctx) {
				s.openShortPutTranches(ctx, chain)
			}
		}
	}
}

func (s *buyFlashLowStrategy) shouldOpenShortPut(ctx *backtest.BarContext) bool {
	dvol := ctx.Factor(s.dvolFactor)
	pr90 := dvol.Ind("dvol_pr_90")
	pr360 := dvol.Ind("dvol_pr_360")
	return (!math.IsNaN(pr90) && pr90 >= s.dvolMinPr) || (!math.IsNaN(pr360) && pr360 >= s.dvolMinPr)
}

func (s *buyFlashLowStrategy) applyDefaults() {
	pricingDefaults := backtest.DefaultSpreadPricingConfig()
	if s.EntryPriceMode == backtest.OptionPriceModeUnspecified {
		s.EntryPriceMode = pricingDefaults.EntryMode
	}
	if s.ExitPriceMode == backtest.OptionPriceModeUnspecified {
		s.ExitPriceMode = pricingDefaults.ExitMode
	}
	if s.ValuationPriceMode == backtest.OptionPriceModeUnspecified {
		s.ValuationPriceMode = pricingDefaults.ValuationMode
	}
	if s.targetExpiryDays == 0 {
		s.targetExpiryDays = defaultTargetDTE
	}
	if s.minExpiryDays == 0 {
		s.minExpiryDays = defaultMinDTE
	}
	if s.shortDeltaMin == 0 {
		s.shortDeltaMin = defaultShortDeltaMin
	}
	if s.shortDeltaMax == 0 {
		s.shortDeltaMax = defaultShortDeltaMax
	}
	if s.spotNotional <= 0 {
		s.spotNotional = defaultSpotNotional
	}
	if s.premiumTarget <= 0 {
		s.premiumTarget = defaultPremiumTarget
	}
	if s.maxContracts <= 0 {
		s.maxContracts = defaultMaxContracts
	}
	if s.takeProfit1 <= 0 {
		s.takeProfit1 = defaultTakeProfit1
	}
	if s.takeProfit2 <= 0 {
		s.takeProfit2 = defaultTakeProfit2
	}
}

func (s *buyFlashLowStrategy) buildContractMap(chain *backtest.OptionsChain) map[string]backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	contractMap := make(map[string]backtest.OptionContract, chain.Len())
	for _, contract := range chain.Contracts() {
		contractMap[contract.Symbol] = contract
	}
	return contractMap
}

func (s *buyFlashLowStrategy) manageOpenShortPuts(ctx *backtest.BarContext, now time.Time, contractMap map[string]backtest.OptionContract) bool {
	active := false
	for i, spreadID := range s.optionSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.optionSpreadIDs[i] = 0
			continue
		}

		leg := sp.Legs[0]
		contract := s.currentContract(leg.Contract, contractMap)
		markPrice := s.valuationPrice(leg, contractMap)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)

		shouldClose := false
		closeReason := ""
		if contract.DaysToExpiry(now) <= 1 {
			shouldClose = true
			closeReason = "sell put到期前平仓"
		} else if i == 0 && !math.IsNaN(pnlPct) && pnlPct >= s.takeProfit1 {
			shouldClose = true
			closeReason = "sell put止盈70%"
		} else if i == 1 && !math.IsNaN(pnlPct) && pnlPct >= s.takeProfit2 {
			shouldClose = true
			closeReason = "sell put止盈88%"
		}

		if shouldClose {
			closePrice := s.exitPrice(leg, contractMap)
			if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(spreadID, 0, closePrice, closeReason) {
				s.optionSpreadIDs[i] = 0
				continue
			}
		}

		active = true
	}
	for i, ref := range s.pendingOptionRefs {
		if ref != "" {
			active = true
			if !s.pendingOptionTimes[i].IsZero() && ctx.Time().After(s.pendingOptionTimes[i]) {
				s.pendingOptionRefs[i] = ""
				s.pendingOptionTimes[i] = time.Time{}
			}
		}
	}
	if !active {
		s.closeGroup(ctx, s.positionGroupID)
	}
	return active
}

func (s *buyFlashLowStrategy) openShortPutTranches(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	triggerTime := ctx.NextBarTime()
	if triggerTime.IsZero() {
		return
	}
	if chain == nil || chain.Len() == 0 {
		return
	}
	contract := s.selectShortPut(chain)
	if contract == nil {
		return
	}

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, *contract)
	if math.IsNaN(entryPrice) || entryPrice <= 0 {
		return
	}

	totalQty := s.premiumTarget / entryPrice
	if totalQty > s.maxContracts {
		totalQty = s.maxContracts
	}
	if totalQty <= 0 {
		return
	}

	firstQty := totalQty / 2
	secondQty := totalQty - firstQty
	tranches := []struct {
		qty float64
		tag string
	}{
		{qty: firstQty, tag: "buy-flash-low-short-put-tp70"},
		{qty: secondQty, tag: "buy-flash-low-short-put-runner"},
	}

	groupID := s.openGroupID(ctx)
	opened := false

	for i, tranche := range tranches {
		if tranche.qty <= 0 {
			continue
		}
		pendingRef := s.nextPendingOptionRef()
		ctx.ScheduleOpenSpreadInGroupWithRef(triggerTime, []backtest.SpreadLeg{{
			Contract:   *contract,
			Side:       backtest.Sell,
			Qty:        tranche.qty,
			EntryPrice: entryPrice,
		}}, tranche.tag, pendingRef, groupID)
		s.pendingOptionRefs[i] = pendingRef
		s.pendingOptionTimes[i] = triggerTime
		opened = true
	}

	if !opened {
		s.closeGroup(ctx, groupID)
	}
}

func (s *buyFlashLowStrategy) openGroupID(ctx *backtest.BarContext) int {
	if s.positionGroupID > 0 {
		return s.positionGroupID
	}
	if ctx.SpreadGroups() == nil {
		return 0
	}
	s.positionGroupID = ctx.SpreadGroups().Open(positionGroupTag, s.premiumTarget, positionGroupDecay, ctx.Time())
	return s.positionGroupID
}

func (s *buyFlashLowStrategy) closeGroup(ctx *backtest.BarContext, groupID int) {
	if groupID <= 0 {
		return
	}
	if ctx.SpreadGroups() != nil {
		ctx.SpreadGroups().Close(groupID)
	}
	if s.positionGroupID == groupID {
		s.positionGroupID = 0
	}
}

func (s *buyFlashLowStrategy) openSpreadInGroup(ctx *backtest.BarContext, legs []backtest.SpreadLeg, tag string, groupID int) int {
	if groupID > 0 && ctx.SpreadGroups() != nil {
		spreadID := ctx.OpenSpreadInGroup(legs, tag, groupID)
		if spreadID > 0 {
			ctx.SpreadGroups().AddSpread(groupID, spreadID)
		}
		return spreadID
	}
	return ctx.OpenSpread(legs, tag)
}

func (s *buyFlashLowStrategy) resolvePendingShortPutOpens(ctx *backtest.BarContext) {
	tracker := ctx.Spreads()
	if tracker == nil {
		return
	}
	for i, ref := range s.pendingOptionRefs {
		if ref == "" {
			continue
		}
		resolved := false
		for _, spread := range tracker.All() {
			if spread != nil && spread.Ref == ref {
				s.optionSpreadIDs[i] = spread.ID
				s.pendingOptionRefs[i] = ""
				s.pendingOptionTimes[i] = time.Time{}
				resolved = true
				break
			}
		}
		if !resolved && !s.pendingOptionTimes[i].IsZero() && ctx.Time().After(s.pendingOptionTimes[i]) {
			s.pendingOptionRefs[i] = ""
			s.pendingOptionTimes[i] = time.Time{}
		}
	}
}

func (s *buyFlashLowStrategy) nextPendingOptionRef() string {
	s.nextPendingRefID++
	return s.Name() + "/short-put/" + strconv.Itoa(s.nextPendingRefID)
}

func (s *buyFlashLowStrategy) selectShortPut(chain *backtest.OptionsChain) *backtest.OptionContract {
	puts := chain.Puts()
	if puts.Len() == 0 {
		return nil
	}

	expiryFiltered := puts.ExpiryRange(s.minExpiryDays, s.targetExpiryDays)
	if expiryFiltered.Len() == 0 {
		expiryFiltered = puts.ExpiryMin(s.minExpiryDays)
	}
	if expiryFiltered.Len() == 0 {
		return nil
	}
	expiryFiltered = expiryFiltered.ExpiryNearest(s.targetExpiryDays)

	targeted := expiryFiltered.DeltaRange(-s.shortDeltaMax, -s.shortDeltaMin)
	if targeted.Len() > 0 {
		if contract := targeted.BestSpread(); contract != nil {
			return contract
		}
	}

	targetDelta := -(s.shortDeltaMin + s.shortDeltaMax) / 2
	sorted := expiryFiltered.SortByDelta(targetDelta)
	for i := range sorted {
		contract := sorted[i]
		entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, contract)
		if !math.IsNaN(entryPrice) && entryPrice > 0 {
			return &contract
		}
	}

	return nil
}

func (s *buyFlashLowStrategy) currentContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if contractMap == nil {
		return contract
	}
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func (s *buyFlashLowStrategy) exitPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ExitPriceMode.ExitPrice(leg.Side, contract)
}

func (s *buyFlashLowStrategy) valuationPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ValuationPriceMode.ExitPrice(leg.Side, contract)
}

func percentileRank(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			series := inputs[source]
			n := len(series)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < period || math.IsNaN(series[i]) {
					out[i] = math.NaN()
					continue
				}
				count := 0
				for j := i - period; j < i; j++ {
					if !math.IsNaN(series[j]) && series[j] < series[i] {
						count++
					}
				}
				out[i] = float64(count) / float64(period) * 100
			}
			return out
		},
	)
}

func computeAmpScore(ampPr100 float64) int {
	score := 0
	if ampPr100 > 77 {
		score++
	}
	if ampPr100 > 90 {
		score++
	}
	return score
}

func computeVolScore(volRank10, volRank20, volRank100, volSMA100, vol float64) int {
	score := 0
	if !math.IsNaN(volRank10) && volRank10 <= 3 {
		score++
	}
	if !math.IsNaN(volRank20) && volRank20 <= 6 {
		score++
	}
	if !math.IsNaN(volRank100) && volRank100 <= 10 &&
		!math.IsNaN(volSMA100) && volSMA100 > 0 && vol > 2*volSMA100 {
		score++
	}
	return score
}

// allNaN reports whether every element in a slice is NaN.
func allNaN(s []float64) bool {
	for _, v := range s {
		if !math.IsNaN(v) {
			return false
		}
	}
	return true
}
