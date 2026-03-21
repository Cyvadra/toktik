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

// htfInterval is the higher-timeframe bar interval used for signal generation.
// The primary bar interval (e.g. 5m) controls execution granularity.
const htfInterval = "8h"

// turtleTrendSimpStrategy implements the "期权趋势替代" strategy.
//
// The strategy runs on a low-timeframe primary (e.g. 5m) for fine-grained
// position management and exit scanning, while entry signals are derived
// from an 8h higher-timeframe security registered during Init.
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

	// Higher-timeframe (8h) security for signal indicators.
	htfRef             backtest.SecurityRef
	lastHTFSignalIndex int // tracks the latest aligned 8h bar index already consumed

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
	longSpotOpen        bool    // whether a long spot position is currently open
	shortSpotOpen       bool    // whether a short spot position is currently open
	longSpotEntryPrice  float64 // long spot initial entry anchor for add-on signal
	shortSpotEntryPrice float64 // short spot initial entry anchor for add-on signal
	longSpotAddCount    int     // number of executed long spot add-ons
	shortSpotAddCount   int     // number of executed short spot add-ons
	longSpotHigh        float64 // BTC_max: highest price since long spot entry
	shortSpotLow        float64 // BTC_min: lowest price since short spot entry
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

	// Register an 8h higher-timeframe security for signal indicators.
	primary := ctx.PrimaryRef()
	s.htfRef = ctx.AddSecurity(primary.Market, primary.Symbol, htfInterval)

	// All signal indicators are computed on the 8h security.
	// Donchian Channel (20): upper = Highest(high,20), lower = Lowest(low,20)
	ctx.RegisterOn(s.htfRef, "dc20", backtest.Donchian("high", "low", 20))

	// Bollinger Bands (20, 2σ)
	ctx.RegisterOn(s.htfRef, "bb20", backtest.Bollinger("close", 20, 2))

	// ATR(20) for add-on spacing and short entry offset
	ctx.RegisterOn(s.htfRef, "atr20", backtest.ATR(20))

	// Rolling StdDev of close over 20 bars, then MA(StdDev, 20)
	ctx.RegisterOn(s.htfRef, "std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs["close"], 20)
		},
	))
	ctx.RegisterOn(s.htfRef, "ma_std20", backtest.SMA("std20", 20))
	ctx.RegisterOn(s.htfRef, "stdma20", backtest.Custom(
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

	return nil
}

// Preload shifts 8h indicator columns by 1 bar (to avoid look-ahead into the
// current incomplete 8h bar) and aligns them onto the primary (5m) timeline.
// It also precomputes the low-volatility entry filter in 8h space.
func (s *turtleTrendSimpStrategy) Preload(ctx *backtest.PreloadContext) error {
	htf := ctx.Security(s.htfRef)
	if htf == nil || htf.Len() == 0 {
		return nil
	}
	n := htf.Len()

	// 1. Create _prev columns: shift each 8h indicator by 1 bar so that at
	//    every 5m bar we only see the most recently COMPLETED 8h bar's value.
	shiftNames := []string{"dc20_upper", "dc20_lower", "bb20_upper", "bb20_lower", "atr20", "close"}
	for _, name := range shiftNames {
		col := htf.Column(name)
		if col == nil {
			continue
		}
		shifted := make([]float64, n)
		shifted[0] = math.NaN()
		copy(shifted[1:], col[:n-1])
		if err := htf.SetColumn(name+"_prev", shifted); err != nil {
			return err
		}
	}

	// 2. Compute low-vol flag in 8h space.
	//    For each 8h bar i, check if any of bars i, i-1, i-2 has
	//    stdma20 percentile rank < 0.35 over 120 trailing bars.
	//    Then shift by 1 so it's safe to read during the next 8h period.
	stdma20 := htf.Column("stdma20")
	if stdma20 != nil {
		raw := make([]float64, n)
		for i := range raw {
			raw[i] = 0
		}
		for i := 0; i < n; i++ {
			for offset := 0; offset <= 2; offset++ {
				idx := i - offset
				if idx < 0 {
					continue
				}
				val := stdma20[idx]
				if math.IsNaN(val) {
					continue
				}
				count, total := 0, 0
				for k := 0; k < 120; k++ {
					bi := idx - k
					if bi < 0 {
						break
					}
					v := stdma20[bi]
					if math.IsNaN(v) {
						continue
					}
					total++
					if v < val {
						count++
					}
				}
				if total > 0 && float64(count)/float64(total) < 0.35 {
					raw[i] = 1
					break
				}
			}
		}
		// Shift by 1 to avoid look-ahead
		shifted := make([]float64, n)
		shifted[0] = 0
		copy(shifted[1:], raw[:n-1])
		if err := htf.SetColumn("lowvol_ok_prev", shifted); err != nil {
			return err
		}
	}

	// 3. Align all _prev columns onto the primary (5m) timeline.
	alignNames := []string{"dc20_upper_prev", "dc20_lower_prev", "bb20_upper_prev", "bb20_lower_prev", "atr20_prev", "close_prev", "lowvol_ok_prev"}
	for _, name := range alignNames {
		if htf.Column(name) == nil {
			continue
		}
		aligned, err := ctx.ColumnAlignedToPrimary(s.htfRef, name)
		if err != nil {
			continue
		}
		if err := ctx.Primary().SetColumn("htf_"+name, aligned); err != nil {
			return err
		}
	}

	// 4. Project the actual aligned 8h bar index onto the primary timeline.
	//    Signal evaluation should advance only when this index changes, not when
	//    the primary timestamp happens to cross a wall-clock 8h boundary.
	alignedHTFIndex := make([]float64, ctx.Primary().Len())
	for i := range alignedHTFIndex {
		alignedHTFIndex[i] = math.NaN()
	}
	if alignMap := htf.AlignMap(); len(alignMap) > 0 {
		for i := 0; i < len(alignedHTFIndex) && i < len(alignMap); i++ {
			if alignMap[i] < 0 {
				continue
			}
			alignedHTFIndex[i] = float64(alignMap[i])
		}
	}
	if err := ctx.Primary().SetColumn("htf_signal_index", alignedHTFIndex); err != nil {
		return err
	}

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

	// ATR from the completed 8h bar, aligned to the 5m primary timeline.
	atr := ctx.Field("htf_atr20_prev")

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

	// Spot signals should only advance when a new aligned 8h bar becomes available.
	if !s.consumeHTFSignal(ctx.Field("htf_signal_index")) {
		return
	}

	primary := ctx.PrimaryRef()

	// --- Spot trailing-stop management (runs on aligned 8h signal updates) ---
	if s.longSpotOpen {
		if close > s.longSpotHigh {
			s.longSpotHigh = close
		}
		if !math.IsNaN(atr) && close < s.longSpotHigh-3*atr {
			if qty := ctx.Position(primary); qty > 0 {
				ctx.SellWithNote(primary, qty, "海龟简易-Spot-Long：止损平仓(跌破BTC_max-3ATR)")
			}
			s.longSpotOpen = false
			s.longSpotEntryPrice = 0
			s.longSpotAddCount = 0
			s.longSpotHigh = 0
		}
	}
	if s.shortSpotOpen {
		if close < s.shortSpotLow {
			s.shortSpotLow = close
		}
		if !math.IsNaN(atr) && close > s.shortSpotLow+3*atr {
			if qty := ctx.Position(primary); qty < 0 {
				ctx.BuyWithNote(primary, -qty, "海龟简易-Spot-Short：止损平仓(突破BTC_min+3ATR)")
			}
			s.shortSpotOpen = false
			s.shortSpotEntryPrice = 0
			s.shortSpotAddCount = 0
			s.shortSpotLow = 0
		}
	}

	// --- Entry signals: gated to actual 8h bar availability ---

	// Read preloaded (shifted-by-1, aligned) 8h indicator values.
	lowVolOK := ctx.Field("htf_lowvol_ok_prev")
	allowInitialEntry := s.allowInitialEntry(lowVolOK)

	dcUpper := ctx.Field("htf_dc20_upper_prev")
	dcLower := ctx.Field("htf_dc20_lower_prev")
	bbUpper := ctx.Field("htf_bb20_upper_prev")
	bbLower := ctx.Field("htf_bb20_lower_prev")
	atrSignal := ctx.Field("htf_atr20_prev")
	htfClose := ctx.Field("htf_close_prev")

	if math.IsNaN(dcUpper) || math.IsNaN(bbUpper) || math.IsNaN(dcLower) || math.IsNaN(bbLower) || math.IsNaN(atrSignal) || math.IsNaN(htfClose) {
		return
	}

	// --- Long entry ---
	// Breakout above prior-bar Max(Donchian upper 20, Bollinger upper 20)
	longBreakout := math.Max(dcUpper, bbUpper)
	shortBreakout := math.Min(dcLower, bbLower) - 0.5*atrSignal

	if allowInitialEntry && close > longBreakout && s.Direction != DirectionShortOnly {
		if !s.longSpotOpen {
			// Spot system: open a $100 reference long position.
			spotQty := 100.0 / close
			ctx.BuyWithNote(primary, spotQty, "海龟简易-Spot-Long：首仓")
			s.longSpotOpen = true
			s.longSpotEntryPrice = close
			s.longSpotAddCount = 0
			s.longSpotHigh = close

			// Option system: open primary Call position alongside spot entry.
			if s.countLongSlots() == 0 {
				s.longAddCount = 0
				if slot := s.openCallOption(ctx, chain, close, "active-long-0", "首仓"); slot != nil {
					s.longSlots[0] = slot
					s.lastLongEntryPrice = close
				}
			}
		}
	}

	// Long spot add-on signal: once spot first entry is open, add signal fires
	// for each +0.75*ATR favorable move from the initial spot entry price.
	longSpotTargetAddCount := 0
	if s.longSpotOpen && !math.IsNaN(atr) && atr > 0 && s.longSpotEntryPrice > 0 {
		longSpotTargetAddCount = int(math.Floor((close - s.longSpotEntryPrice) / (0.75 * atr)))
		if longSpotTargetAddCount < 0 {
			longSpotTargetAddCount = 0
		}
	}

	// Long spot add-ons follow the same 0.75*ATR ladder from initial spot entry.
	if s.longSpotOpen && !math.IsNaN(atr) && atr > 0 {
		targetSpotAdds := longSpotTargetAddCount
		if targetSpotAdds > 2 {
			targetSpotAdds = 2
		}
		for s.longSpotAddCount < targetSpotAdds {
			spotQty := 100.0 / close
			note := "海龟简易-Spot-Long：加仓" + strconv.Itoa(s.longSpotAddCount+1)
			ctx.BuyWithNote(primary, spotQty, note)
			s.longSpotAddCount++
		}
	}

	// Long option add-ons: remove self-referential option ladder and only follow
	// spot add-on signal, with an extra guard that at least one active long slot exists.
	if s.Direction != DirectionShortOnly && s.longAddCount < 2 && longSpotTargetAddCount > 0 && s.countLongSlots() > 0 {
		if !math.IsNaN(atr) && atr > 0 {
			targetAddCount := longSpotTargetAddCount
			if targetAddCount > 2 {
				targetAddCount = 2
			}
			for s.longAddCount < targetAddCount {
				slotIdx := s.nextFreeLongSlot()
				if slotIdx < 0 {
					break
				}
				slot := s.openCallOption(ctx, chain, close, "active-long-add", "加仓")
				if slot == nil {
					break
				}
				s.longSlots[slotIdx] = slot
				s.longAddCount++
			}
		}
	}

	// --- Short entry ---
	// Breakout below prior-bar Min(Donchian lower 20, Bollinger lower 20) - 0.5*ATR
	if allowInitialEntry && close < shortBreakout && s.Direction != DirectionLongOnly {
		if !s.shortSpotOpen {
			// Spot system: open a $100 reference short position.
			spotQty := 100.0 / close
			ctx.SellWithNote(primary, spotQty, "海龟简易-Spot-Short：首仓")
			s.shortSpotOpen = true
			s.shortSpotEntryPrice = close
			s.shortSpotAddCount = 0
			s.shortSpotLow = close

			// Option system: open primary Put position alongside spot entry.
			if s.countShortSlots() == 0 {
				s.shortAddCount = 0
				if slot := s.openPutOption(ctx, chain, close, "active-short-0", "首仓"); slot != nil {
					s.shortSlots[0] = slot
					s.lastShortEntryPrice = close
				}
			}
		}
	}

	// Short spot add-on signal: once spot first entry is open, add signal fires
	// for each -0.75*ATR favorable move from the initial spot entry price.
	shortSpotTargetAddCount := 0
	if s.shortSpotOpen && !math.IsNaN(atr) && atr > 0 && s.shortSpotEntryPrice > 0 {
		shortSpotTargetAddCount = int(math.Floor((s.shortSpotEntryPrice - close) / (0.75 * atr)))
		if shortSpotTargetAddCount < 0 {
			shortSpotTargetAddCount = 0
		}
	}

	// Short spot add-ons follow the same 0.75*ATR ladder from initial spot entry.
	if s.shortSpotOpen && !math.IsNaN(atr) && atr > 0 {
		targetSpotAdds := shortSpotTargetAddCount
		if targetSpotAdds > 1 {
			targetSpotAdds = 1
		}
		for s.shortSpotAddCount < targetSpotAdds {
			spotQty := 100.0 / close
			note := "海龟简易-Spot-Short：加仓" + strconv.Itoa(s.shortSpotAddCount+1)
			ctx.SellWithNote(primary, spotQty, note)
			s.shortSpotAddCount++
		}
	}

	// Short option add-ons: only follow spot add-on signal, with an extra
	// guard that at least one active short slot exists.
	if s.Direction != DirectionLongOnly && s.shortAddCount < 1 && shortSpotTargetAddCount > 0 && s.countShortSlots() > 0 {
		targetAddCount := shortSpotTargetAddCount
		if targetAddCount > 1 {
			targetAddCount = 1
		}
		for s.shortAddCount < targetAddCount {
			slotIdx := s.nextFreeShortSlot()
			if slotIdx < 0 {
				break
			}
			slot := s.openPutOption(ctx, chain, close, "active-short-add", "加仓")
			if slot == nil {
				break
			}
			s.shortSlots[slotIdx] = slot
			s.shortAddCount++
		}
	}
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

func (s *turtleTrendSimpStrategy) consumeHTFSignal(signalIndex float64) bool {
	if math.IsNaN(signalIndex) {
		return false
	}
	idx := int(signalIndex)
	if idx == s.lastHTFSignalIndex {
		return false
	}
	s.lastHTFSignalIndex = idx
	return true
}

func (s *turtleTrendSimpStrategy) allowInitialEntry(lowVolOK float64) bool {
	return lowVolOK == 1
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
	s.lastHTFSignalIndex = -1
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
