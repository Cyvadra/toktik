package strategies

import (
	"math"
	"os"
	"strconv"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func init() {
	Register(Registration{
		Name:    "turtle-trend-simp",
		Aliases: []string{"turtle_trend_simp", "turtle-trend"},
		Groups:  []string{"trend", "single-leg"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return &turtleTrendSimpStrategy{
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				Direction:          cfg.Direction,
			}, nil
		},
	})
}

// turtleTrendSimpStrategy implements the "期权趋势替代" strategy.
//
// Long side: buys Calls when price breaks above Max(DonchianUpper20, BollingerUpper20)
// under low-volatility conditions, using options as a trend replacement for spot.
//
// Short side: buys Puts when price breaks below Min(DonchianLower20, BollingerLower20) - 0.5*ATR
// under the same low-volatility conditions.
type turtleTrendSimpStrategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode
	Direction          TradeDirection // both | long_only | short_only
	Debug              bool
	DebugEvery         int

	// internal state per side
	longSlots    [3]*slotState // max 1 initial + 2 add-ons for longs (calls)
	shortSlots   [2]*slotState // max 1 initial + 1 add-on for shorts (puts)
	longRemoved  []*slotState  // detached long positions no longer block new entries
	shortRemoved []*slotState  // detached short positions no longer block new entries

	lastLongEntryPrice  float64 // price at last long entry/add-on
	lastShortEntryPrice float64 // price at last short entry/add-on
	longAddCount        int     // number of long add-ons executed
	shortAddCount       int     // number of short add-ons executed

	// Spot trading state (signal reference system)
	longSpotOpen  bool    // whether a long spot position is currently open
	shortSpotOpen bool    // whether a short spot position is currently open
	longSpotHigh  float64 // BTC_max: highest price since long spot entry
	shortSpotLow  float64 // BTC_min: lowest price since short spot entry
}

// slotState tracks a single option position slot.
type slotState struct {
	spreadID   int
	entryPrice float64 // underlying price at entry
}

var (
	_ = (*turtleTrendSimpStrategy).shouldDebugBar
	_ = (*turtleTrendSimpStrategy).debugf
	_ = chainLen
)

func (s *turtleTrendSimpStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

func (s *turtleTrendSimpStrategy) Name() string { return "TurtleTrendSimp" }

func (s *turtleTrendSimpStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	// Donchian Channel (20): upper = Highest(high,20), lower = Lowest(low,20)
	ctx.Register("dc20", backtest.Donchian("high", "low", 20))

	// Bollinger Bands (20, 2σ)
	ctx.Register("bb20", backtest.Bollinger("close", 20, 2))

	// ATR(20) for add-on spacing and short entry offset
	ctx.Register("atr20", backtest.ATR(20))

	// Rolling StdDev of close over 20 bars, then MA(StdDev, 20)
	ctx.Register("std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs["close"], 20)
		},
	))
	ctx.Register("ma_std20", backtest.SMA("std20", 20))
	ctx.Register("stdma20", backtest.Custom(
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

	// IV quantile proxy: use average option IV from the chain.
	// We compute this at runtime in OnBar from the options chain,
	// but we still need the rolling quantile infrastructure.
	// We'll compute IV quantile directly in OnBar from chain data.

	return nil
}

func (s *turtleTrendSimpStrategy) rollCloseReason(absDelta, pnlPct float64) string {
	if absDelta > 0.55 && !math.IsNaN(pnlPct) && pnlPct > 0.66 {
		return "换仓：Delta超标且浮盈超过66%"
	}
	if absDelta > 0.55 {
		return "换仓：Delta超标(>|0.55|)"
	}
	if !math.IsNaN(pnlPct) && pnlPct > 0.66 {
		return "换仓：浮盈超过66%"
	}
	return "换仓"
}

func (s *turtleTrendSimpStrategy) OnBar(ctx *backtest.BarContext) {
	barIndex := ctx.BarIndex()
	now := ctx.Time()
	close := ctx.Close()
	if math.IsNaN(close) {
		return
	}

	atr := ctx.Ind("atr20")

	// --- Manage existing positions ---
	chain := ctx.OptionsChain()
	var contractMap map[string]backtest.OptionContract
	if chain != nil {
		contractMap = make(map[string]backtest.OptionContract)
		for _, c := range chain.Contracts() {
			contractMap[c.Symbol] = c
		}
	}

	// Check active long slots.
	for i := range s.longSlots {
		slot := s.longSlots[i]
		if slot == nil {
			continue
		}
		sp := ctx.Spreads().Get(slot.spreadID)
		if sp == nil || sp.IsFullyClosed() {
			s.longSlots[i] = nil
			continue
		}
		leg := &sp.Legs[0]
		if leg.Closed {
			s.longSlots[i] = nil
			continue
		}

		currentContract := s.currentContract(leg.Contract, contractMap)
		markPrice := s.valuationPriceForLeg(*leg, contractMap)

		if s.shouldCloseForExpiry(currentContract, now) {
			ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason("到期前一天平仓", currentContract.Delta))
			s.longSlots[i] = nil
			continue
		}

		// When the main slot suffers an 80% premium drawdown, detach the whole side.
		if i == 0 && s.hitRemovalThreshold(*leg, markPrice) {
			s.detachLongSeries(barIndex)
			break
		}

		// Rolling: |Delta| > 0.55 or unrealized profit > 66%
		absDelta := math.Abs(currentContract.Delta)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
		needsRoll := false
		if absDelta > 0.55 {
			needsRoll = true
		}
		if !math.IsNaN(pnlPct) && pnlPct > 0.66 {
			needsRoll = true
		}

		if needsRoll {
			ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason(s.rollCloseReason(absDelta, pnlPct), currentContract.Delta))
			s.longSlots[i] = s.openCallOption(ctx, chain, close, "active-long", "换仓")
		}
	}

	// Check active short slots.
	for i := range s.shortSlots {
		slot := s.shortSlots[i]
		if slot == nil {
			continue
		}
		sp := ctx.Spreads().Get(slot.spreadID)
		if sp == nil || sp.IsFullyClosed() {
			s.shortSlots[i] = nil
			continue
		}
		leg := &sp.Legs[0]
		if leg.Closed {
			s.shortSlots[i] = nil
			continue
		}

		currentContract := s.currentContract(leg.Contract, contractMap)
		markPrice := s.valuationPriceForLeg(*leg, contractMap)

		if s.shouldCloseForExpiry(currentContract, now) {
			ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason("到期前一天平仓", currentContract.Delta))
			s.shortSlots[i] = nil
			continue
		}

		if i == 0 && s.hitRemovalThreshold(*leg, markPrice) {
			s.detachShortSeries(barIndex)
			break
		}

		// Rolling: |Delta| > 0.55 or profit > 66%
		absDelta := math.Abs(currentContract.Delta)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
		needsRoll := false
		if absDelta > 0.55 {
			needsRoll = true
		}
		if !math.IsNaN(pnlPct) && pnlPct > 0.66 {
			needsRoll = true
		}

		if needsRoll {
			ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason(s.rollCloseReason(absDelta, pnlPct), currentContract.Delta))
			s.shortSlots[i] = s.openPutOption(ctx, chain, close, "active-short", "换仓")
		}
	}

	// Check detached long positions. They keep rolling/profit-taking, but never block new entries.
	if len(s.longRemoved) > 0 {
		updated := make([]*slotState, 0, len(s.longRemoved))
		for _, slot := range s.longRemoved {
			if slot == nil {
				continue
			}
			sp := ctx.Spreads().Get(slot.spreadID)
			if sp == nil || sp.IsFullyClosed() {
				continue
			}
			leg := &sp.Legs[0]
			if leg.Closed {
				continue
			}

			currentContract := s.currentContract(leg.Contract, contractMap)
			markPrice := s.valuationPriceForLeg(*leg, contractMap)

			if s.shouldCloseForExpiry(currentContract, now) {
				ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason("到期前一天平仓", currentContract.Delta))
				continue
			}

			absDelta := math.Abs(currentContract.Delta)
			pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
			needsRoll := false
			if absDelta > 0.55 {
				needsRoll = true
			}
			if !math.IsNaN(pnlPct) && pnlPct > 0.66 {
				needsRoll = true
			}

			if needsRoll {
				ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason(s.rollCloseReason(absDelta, pnlPct), currentContract.Delta))
				if reopened := s.openCallOption(ctx, chain, close, "removed-long", "换仓"); reopened != nil {
					updated = append(updated, reopened)
				}
				continue
			}

			updated = append(updated, slot)
		}
		s.longRemoved = updated
	}

	// Check detached short positions. They keep rolling/profit-taking, but never block new entries.
	if len(s.shortRemoved) > 0 {
		updated := make([]*slotState, 0, len(s.shortRemoved))
		for _, slot := range s.shortRemoved {
			if slot == nil {
				continue
			}
			sp := ctx.Spreads().Get(slot.spreadID)
			if sp == nil || sp.IsFullyClosed() {
				continue
			}
			leg := &sp.Legs[0]
			if leg.Closed {
				continue
			}

			currentContract := s.currentContract(leg.Contract, contractMap)
			markPrice := s.valuationPriceForLeg(*leg, contractMap)

			if s.shouldCloseForExpiry(currentContract, now) {
				ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason("到期前一天平仓", currentContract.Delta))
				continue
			}

			absDelta := math.Abs(currentContract.Delta)
			pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
			needsRoll := false
			if absDelta > 0.55 {
				needsRoll = true
			}
			if !math.IsNaN(pnlPct) && pnlPct > 0.66 {
				needsRoll = true
			}

			if needsRoll {
				ctx.CloseSpreadLegWithReason(sp.ID, 0, s.exitPriceForLeg(*leg, contractMap), s.withDeltaReason(s.rollCloseReason(absDelta, pnlPct), currentContract.Delta))
				if reopened := s.openPutOption(ctx, chain, close, "removed-short", "换仓"); reopened != nil {
					updated = append(updated, reopened)
				}
				continue
			}

			updated = append(updated, slot)
		}
		s.shortRemoved = updated
	}

	// --- Spot trailing-stop management ---
	primary := ctx.PrimaryRef()
	if s.longSpotOpen {
		if close > s.longSpotHigh {
			s.longSpotHigh = close
		}
		if !math.IsNaN(atr) && close < s.longSpotHigh-3*atr {
			if ctx.Position(primary) > 0 {
				ctx.ClosePosition(primary)
			}
			s.longSpotOpen = false
			s.longSpotHigh = 0
		}
	}
	if s.shortSpotOpen {
		if close < s.shortSpotLow {
			s.shortSpotLow = close
		}
		if !math.IsNaN(atr) && close > s.shortSpotLow+3*atr {
			if ctx.Position(primary) < 0 {
				ctx.ClosePosition(primary)
			}
			s.shortSpotOpen = false
			s.shortSpotLow = 0
		}
	}

	// --- Check signal conditions ---
	lowVolOK, _ := s.checkLowVol(ctx)
	if !lowVolOK {
		return
	}

	dcUpper := ctx.IndAt("dc20_upper", 1)
	dcLower := ctx.IndAt("dc20_lower", 1)
	bbUpper := ctx.IndAt("bb20_upper", 1)
	bbLower := ctx.IndAt("bb20_lower", 1)
	atrSignal := ctx.IndAt("atr20", 1)

	if math.IsNaN(dcUpper) || math.IsNaN(bbUpper) || math.IsNaN(dcLower) || math.IsNaN(bbLower) || math.IsNaN(atrSignal) {

		return
	}

	// --- Long entry ---
	// Breakout above prior-bar Max(Donchian upper 20, Bollinger upper 20)
	longBreakout := math.Max(dcUpper, bbUpper)
	shortBreakout := math.Min(dcLower, bbLower) - 0.5*atrSignal

	if close > longBreakout && s.Direction != DirectionShortOnly {
		if !s.longSpotOpen {
			// Spot system: open a $100 reference long position.
			spotQty := 100.0 / close
			ctx.BuyWithNote(primary, spotQty, "海龟简易-Spot-Long：首仓")
			s.longSpotOpen = true
			s.longSpotHigh = close

			// Option system: open primary Call position alongside spot entry.
			if s.countLongSlots() == 0 {
				s.longAddCount = 0
				if slot := s.openCallOption(ctx, chain, close, "active-long-0", "首仓"); slot != nil {
					s.longSlots[0] = slot
					s.lastLongEntryPrice = close
				}
			}
		} else if s.longSlots[0] != nil && s.longAddCount < 2 {
			// Option add-on: price must have risen 0.75 * ATR since last entry
			if close >= s.lastLongEntryPrice+0.75*atr {
				slotIdx := s.nextFreeLongSlot()
				if slotIdx >= 0 {
					if slot := s.openCallOption(ctx, chain, close, "active-long-add", "加仓"); slot != nil {
						s.longSlots[slotIdx] = slot
						s.lastLongEntryPrice = close
						s.longAddCount++
					}
				}
			}
		}
	}

	// --- Short entry ---
	// Breakout below prior-bar Min(Donchian lower 20, Bollinger lower 20) - 0.5*ATR
	if close < shortBreakout && s.Direction != DirectionLongOnly {
		if !s.shortSpotOpen {
			// Spot system: open a $100 reference short position.
			spotQty := 100.0 / close
			ctx.SellWithNote(primary, spotQty, "海龟简易-Spot-Short：首仓")
			s.shortSpotOpen = true
			s.shortSpotLow = close

			// Option system: open primary Put position alongside spot entry.
			if s.countShortSlots() == 0 {
				s.shortAddCount = 0
				if slot := s.openPutOption(ctx, chain, close, "active-short-0", "首仓"); slot != nil {
					s.shortSlots[0] = slot
					s.lastShortEntryPrice = close
				}
			}
		} else if s.shortSlots[0] != nil && s.shortAddCount < 1 {
			// Option add-on: price must have fallen 0.75 * ATR since last entry
			if close <= s.lastShortEntryPrice-0.75*atr {
				slotIdx := s.nextFreeShortSlot()
				if slotIdx >= 0 {
					if slot := s.openPutOption(ctx, chain, close, "active-short-add", "加仓"); slot != nil {
						s.shortSlots[slotIdx] = slot
						s.lastShortEntryPrice = close
						s.shortAddCount++
					}
				}
			}
		}
	}
}

// checkLowVol checks whether at least one of the last 3 bars has
// stdma20 = Std(Close,20) / MA(Std(Close,20),20) below the 35th percentile
// of the past 120 bars.
func (s *turtleTrendSimpStrategy) checkLowVol(ctx *backtest.BarContext) (bool, string) {
	for barsAgo := 0; barsAgo <= 2; barsAgo++ {
		stdMa := ctx.IndAt("stdma20", barsAgo)
		if math.IsNaN(stdMa) {
			continue
		}
		// Compute percentile rank of stdma20 within the last 120 bars of stdma20.
		count := 0
		total := 0
		for k := barsAgo; k < barsAgo+120; k++ {
			v := ctx.IndAt("stdma20", k)
			if math.IsNaN(v) {
				continue
			}
			total++
			if v < stdMa {
				count++
			}
		}
		if total > 0 {
			rank := float64(count) / float64(total)
			if rank < 0.35 {
				return true, "rank below 35th percentile in lookback"
			}
		}
	}
	return false, "no bar in last 3 satisfied low-vol rank < 0.35"
}

// openCallOption opens a single Call option per the execution standard.
// DTE ≈ 35, Delta ≈ 0.33, sized by IV quantile.
func (s *turtleTrendSimpStrategy) openCallOption(ctx *backtest.BarContext, chain *backtest.OptionsChain, underlyingPrice float64, slotRef, reason string) *slotState {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	calls := chain.Calls().ExpiryNearest(35)
	if calls.Len() == 0 {
		return nil
	}

	sorted := calls.SortByDelta(0.33)
	if len(sorted) == 0 {
		return nil
	}
	contract := sorted[0]

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, contract)
	if math.IsNaN(entryPrice) || entryPrice <= 0 {
		return nil
	}

	// Size: x * 1 BTC based on IV quantile
	x := s.ivCoefficient(chain, false)
	qty := x / entryPrice
	if qty <= 0 {
		return nil
	}

	tag := "海龟简易-Long"
	if reason != "" {
		tag += "：" + reason
	}
	tag = s.withDeltaReason(tag, contract.Delta)

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   contract,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, tag)

	if spreadID > 0 {
		return &slotState{
			spreadID:   spreadID,
			entryPrice: underlyingPrice,
		}
	}
	return nil
}

// openPutOption opens a single Put option per the execution standard.
// DTE ≈ 35, Delta ≈ -0.33, sized by IV quantile.
func (s *turtleTrendSimpStrategy) openPutOption(ctx *backtest.BarContext, chain *backtest.OptionsChain, underlyingPrice float64, slotRef, reason string) *slotState {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	puts := chain.Puts().ExpiryNearest(35)
	if puts.Len() == 0 {
		return nil
	}

	sorted := puts.SortByDelta(-0.33)
	if len(sorted) == 0 {
		return nil
	}
	contract := sorted[0]

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, contract)
	if math.IsNaN(entryPrice) || entryPrice <= 0 {
		return nil
	}

	// Size: x * 1 BTC based on IV quantile (short/put variant with IV >= 85% bracket)
	x := s.ivCoefficient(chain, true)
	qty := x / entryPrice
	if qty <= 0 {
		return nil
	}

	tag := "海龟简易-Short"
	if reason != "" {
		tag += "：" + reason
	}
	tag = s.withDeltaReason(tag, contract.Delta)

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   contract,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, tag)

	if spreadID > 0 {
		return &slotState{
			spreadID:   spreadID,
			entryPrice: underlyingPrice,
		}
	}
	return nil
}

// ivCoefficient computes the position sizing coefficient x based on IV percentile.
// It uses the average IV of ATM options from the chain as a proxy for IV index,
// then computes the 120-bar quantile rank.
func (s *turtleTrendSimpStrategy) ivCoefficient(chain *backtest.OptionsChain, isPut bool) float64 {
	// TODO: next version
	_, _ = chain, isPut
	return 1.0
}

func (s *turtleTrendSimpStrategy) currentContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if contractMap == nil {
		return contract
	}
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func (s *turtleTrendSimpStrategy) exitPriceForLeg(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ExitPriceMode.ExitPrice(leg.Side, contract)
}

func (s *turtleTrendSimpStrategy) valuationPriceForLeg(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ValuationPriceMode.ExitPrice(leg.Side, contract)
}

func (s *turtleTrendSimpStrategy) hitRemovalThreshold(leg backtest.SpreadLeg, markPrice float64) bool {
	return !math.IsNaN(markPrice) && leg.EntryPrice > 0 && markPrice <= leg.EntryPrice*0.2
}

func (s *turtleTrendSimpStrategy) shouldCloseForExpiry(contract backtest.OptionContract, now time.Time) bool {
	return contract.DaysToExpiry(now) <= 1
}

func (s *turtleTrendSimpStrategy) detachLongSeries(_ int) {
	moved := 0
	for _, slot := range s.longSlots {
		if slot == nil {
			continue
		}
		s.longRemoved = append(s.longRemoved, slot)
		moved++
	}
	s.longSlots = [3]*slotState{}
	s.longAddCount = 0
	s.lastLongEntryPrice = 0
}

func (s *turtleTrendSimpStrategy) detachShortSeries(_ int) {
	moved := 0
	for _, slot := range s.shortSlots {
		if slot == nil {
			continue
		}
		s.shortRemoved = append(s.shortRemoved, slot)
		moved++
	}
	s.shortSlots = [2]*slotState{}
	s.shortAddCount = 0
	s.lastShortEntryPrice = 0
}

func (s *turtleTrendSimpStrategy) countLongSlots() int {
	n := 0
	for _, slot := range s.longSlots {
		if slot != nil {
			n++
		}
	}
	return n
}

func (s *turtleTrendSimpStrategy) countShortSlots() int {
	n := 0
	for _, slot := range s.shortSlots {
		if slot != nil {
			n++
		}
	}
	return n
}

func (s *turtleTrendSimpStrategy) nextFreeLongSlot() int {
	for i, slot := range s.longSlots {
		if slot == nil {
			return i
		}
	}
	return -1
}

func (s *turtleTrendSimpStrategy) nextFreeShortSlot() int {
	for i, slot := range s.shortSlots {
		if slot == nil {
			return i
		}
	}
	return -1
}

func (s *turtleTrendSimpStrategy) applyDefaults() {
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
	if s.Direction == "" {
		s.Direction = DirectionBoth
	}
	if !s.Debug {
		s.Debug = parseEnvBool("TOKTIK_TURTLE_DEBUG")
	}
	if s.DebugEvery <= 0 {
		s.DebugEvery = parseEnvInt("TOKTIK_TURTLE_DEBUG_EVERY", 100)
	}
}

func (s *turtleTrendSimpStrategy) shouldDebugBar(barIndex int) bool {
	if !s.Debug {
		return false
	}
	if s.DebugEvery <= 0 {
		return true
	}
	return barIndex%s.DebugEvery == 0
}

func (s *turtleTrendSimpStrategy) debugf(format string, args ...interface{}) {
	_, _ = format, args
}

func (s *turtleTrendSimpStrategy) withDeltaReason(base string, _ float64) string {
	// implemented somewhere else
	return base
	// if math.IsNaN(delta) {
	// 	return base
	// }
	// return base + " | delta=" + strconv.FormatFloat(delta, 'f', 4, 64)
}

func parseEnvBool(key string) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return value
}

func parseEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func chainLen(chain *backtest.OptionsChain) int {
	if chain == nil {
		return 0
	}
	return chain.Len()
}

// rollingStdDev computes a rolling standard deviation over the given period.
func rollingStdDev(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 1 {
		return out
	}

	for i := 0; i < n; i++ {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		sum := 0.0
		sumSq := 0.0
		valid := 0
		for j := i - period + 1; j <= i; j++ {
			v := src[j]
			if math.IsNaN(v) {
				out[i] = math.NaN()
				break
			}
			sum += v
			sumSq += v * v
			valid++
		}
		if valid == period {
			mean := sum / float64(period)
			variance := sumSq/float64(period) - mean*mean
			if variance < 0 {
				variance = 0
			}
			out[i] = math.Sqrt(variance)
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}
