package strategies

import (
	"log"
	"math"
	"strconv"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func init() {
	Register(Registration{
		Name:    "btc-coin-enhanced",
		Aliases: []string{"btc_coin_enhanced", "btc-enhanced", "coin-enhanced"},
		Groups:  []string{"options", "multi-leg"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return &btcCoinEnhancedStrategy{
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
			}, nil
		},
	})
}

// btcCoinEnhancedStrategy implements the "BTC币本位增强策略".
//
// RSI(200) divides into bullish/bearish regimes:
//   - RSI > 50 → 12h/24h cycle, 40-day options
//   - RSI < 50 → 3h/6h cycle, 25-day options
//
// Entry: MACD top-divergence + volatility above 50th percentile + bearish candle
//   - Sell Call (Delta ≈ 0.3)
//   - Buy Put (Delta ≈ -0.25) with 70% of Call premium
//
// Management:
//   - Put: roll if |Delta| > 0.5 or profit > 50%
//   - Call: close half at 70% profit, close all at 88% profit
type btcCoinEnhancedStrategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode
	Debug              bool
	DebugEvery         int

	// Configurable parameters with defaults
	RSIPeriod         int     // default 200
	RSIThreshold      float64 // default 50
	DivergencePeriod  int     // default 30
	VolQuantilePeriod int     // default 100
	VolQuantileMin    float64 // default 0.50
	CallDelta         float64 // default 0.3
	PutDelta          float64 // default -0.25
	PutBudgetRatio    float64 // default 0.70
	PutRollDelta      float64 // default 0.5 (absolute)
	PutRollProfit     float64 // default 0.50
	CallHalfProfit    float64 // default 0.70
	CallFullProfit    float64 // default 0.88
	BullExpiryDays    int     // default 40
	BearExpiryDays    int     // default 25
	PositionBTC       float64 // default 10 BTC notional per trade

	// Internal state
	activeCall *enhancedSlot // tracked short call position
	activePut  *enhancedSlot // tracked long put position
	callHalved bool          // whether half of call has been closed
}

type enhancedSlot struct {
	spreadID   int
	entryPrice float64 // option entry price
	qty        float64 // quantity
}

func (s *btcCoinEnhancedStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

func (s *btcCoinEnhancedStrategy) Name() string { return "BTCCoinEnhanced" }

func (s *btcCoinEnhancedStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	// RSI(200) for regime detection
	ctx.Register("rsi200", backtest.RSI("close", s.RSIPeriod))

	// MACD(12,26,9) for divergence detection
	ctx.Register("macd", backtest.MACD("close", 12, 26, 9))

	// Rolling highest of high over divergence period
	ctx.Register("hh30", backtest.Highest("high", s.DivergencePeriod))

	// Volatility: StdDev(close, 20) → MA(StdDev, 20)
	ctx.Register("std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs["close"], 20)
		},
	))
	ctx.Register("ma_std20", backtest.SMA("std20", 20))

	// Quantile of ma_std20 over the volatility lookback period
	ctx.Register("vol_quantile", backtest.Quantile("ma_std20", s.VolQuantilePeriod, 0))

	s.debugf("init entry_mode=%v exit_mode=%v valuation_mode=%v rsi_period=%d bull_expiry=%d bear_expiry=%d",
		s.EntryPriceMode, s.ExitPriceMode, s.ValuationPriceMode, s.RSIPeriod, s.BullExpiryDays, s.BearExpiryDays)

	return nil
}

func (s *btcCoinEnhancedStrategy) OnBar(ctx *backtest.BarContext) {
	barIndex := ctx.BarIndex()
	close := ctx.Close()
	if math.IsNaN(close) {
		return
	}

	chain := ctx.OptionsChain()
	var contractMap map[string]backtest.OptionContract
	if chain != nil {
		contractMap = make(map[string]backtest.OptionContract)
		for _, c := range chain.Contracts() {
			contractMap[c.Symbol] = c
		}
	}

	// --- Manage existing positions ---
	s.manageCallPosition(ctx, contractMap)
	s.managePutPosition(ctx, chain, contractMap)

	// --- Check entry signal ---
	rsi := ctx.Ind("rsi200")
	if math.IsNaN(rsi) {
		return
	}

	// Already have an active call → no new entry
	if s.activeCall != nil {
		return
	}

	// Determine expiry based on RSI regime
	expiryDays := s.BearExpiryDays
	if rsi > s.RSIThreshold {
		expiryDays = s.BullExpiryDays
	}

	// Check volatility condition: ma_std20 must be above 50th percentile
	if !s.checkVolCondition(ctx) {
		return
	}

	// Check divergence signal
	if !s.checkDivergence(ctx) {
		return
	}

	// Check bearish candle (close < open)
	open := ctx.Open()
	if math.IsNaN(open) || close >= open {
		if s.shouldDebugBar(barIndex) {
			s.debugf("bar=%d skip: not a bearish candle close=%.6f open=%.6f", barIndex, close, open)
		}
		return
	}

	// --- Entry: build the hedge combo ---
	if chain == nil || chain.Len() == 0 {
		return
	}

	s.debugf("bar=%d entry signal triggered rsi=%.2f expiry_days=%d close=%.6f", barIndex, rsi, expiryDays, close)

	// 1. Sell Call: Delta ≈ 0.3
	callSlot := s.openShortCall(ctx, chain, expiryDays)
	if callSlot == nil {
		return
	}
	s.activeCall = callSlot
	s.callHalved = false

	// 2. Buy Put: spend 70% of Call premium, Delta ≈ -0.25
	callPremium := callSlot.entryPrice * callSlot.qty
	putBudget := callPremium * s.PutBudgetRatio
	putSlot := s.openLongPut(ctx, chain, expiryDays, putBudget)
	if putSlot != nil {
		s.activePut = putSlot
	}

	s.debugf("bar=%d entry complete call_spread_id=%d call_premium=%.6f put_budget=%.6f put_spread_id=%d",
		barIndex, callSlot.spreadID, callPremium, putBudget, s.putSpreadID())
}

// manageCallPosition manages the short call position.
func (s *btcCoinEnhancedStrategy) manageCallPosition(ctx *backtest.BarContext, contractMap map[string]backtest.OptionContract) {
	if s.activeCall == nil {
		return
	}

	sp := ctx.Spreads().Get(s.activeCall.spreadID)
	if sp == nil || sp.IsFullyClosed() {
		s.activeCall = nil
		s.callHalved = false
		return
	}
	leg := &sp.Legs[0]
	if leg.Closed {
		s.activeCall = nil
		s.callHalved = false
		return
	}

	currentContract := s.currentContract(leg.Contract, contractMap)
	markPrice := s.ValuationPriceMode.ExitPrice(leg.Side, currentContract)

	// Close for expiry
	if currentContract.DaysToExpiry(ctx.Time()) <= 1 {
		exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
		ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice, "Call到期平仓")
		s.activeCall = nil
		s.callHalved = false
		return
	}

	// Profit-taking
	pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
	if math.IsNaN(pnlPct) {
		return
	}

	if pnlPct > s.CallFullProfit {
		// Close all remaining
		exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
		ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice,
			"Call全平：浮盈>"+strconv.FormatFloat(s.CallFullProfit*100, 'f', 0, 64)+"%")
		s.activeCall = nil
		s.callHalved = false
		s.debugf("bar=%d call full close pnl_pct=%.4f", ctx.BarIndex(), pnlPct)
		return
	}

	if !s.callHalved && pnlPct > s.CallHalfProfit {
		// Close half the position by closing this leg and reopening with half qty
		exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
		ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice,
			"Call半平：浮盈>"+strconv.FormatFloat(s.CallHalfProfit*100, 'f', 0, 64)+"%")

		// Reopen with half the original quantity
		halfQty := s.activeCall.qty / 2
		if halfQty > 0 {
			entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, currentContract)
			if optionPriceOK(entryPrice) {
				spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
					Contract:   currentContract,
					Side:       backtest.Sell,
					Qty:        halfQty,
					EntryPrice: entryPrice,
				}}, "币本位增强-Call半仓")
				if spreadID > 0 {
					s.activeCall = &enhancedSlot{
						spreadID:   spreadID,
						entryPrice: entryPrice,
						qty:        halfQty,
					}
					s.callHalved = true
					s.debugf("bar=%d call half close pnl_pct=%.4f new_spread_id=%d half_qty=%.6f", ctx.BarIndex(), pnlPct, spreadID, halfQty)
				}
			}
		}
	}
}

// managePutPosition manages the long put position with rolling.
func (s *btcCoinEnhancedStrategy) managePutPosition(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract) {
	if s.activePut == nil {
		return
	}

	sp := ctx.Spreads().Get(s.activePut.spreadID)
	if sp == nil || sp.IsFullyClosed() {
		s.activePut = nil
		return
	}
	leg := &sp.Legs[0]
	if leg.Closed {
		s.activePut = nil
		return
	}

	currentContract := s.currentContract(leg.Contract, contractMap)
	markPrice := s.ValuationPriceMode.ExitPrice(leg.Side, currentContract)

	// Close for expiry
	if currentContract.DaysToExpiry(ctx.Time()) <= 1 {
		exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
		ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice, "Put到期平仓")
		s.activePut = nil
		return
	}

	needsRoll := false
	var reason string

	// Check delta threshold: |Delta| > 0.5
	absDelta := math.Abs(currentContract.Delta)
	if absDelta > s.PutRollDelta {
		needsRoll = true
		reason = "Put换仓：Delta超标(>" + strconv.FormatFloat(s.PutRollDelta, 'f', 2, 64) + ")"
	}

	// Check profit threshold: > 50%
	pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
	if !math.IsNaN(pnlPct) && pnlPct > s.PutRollProfit {
		needsRoll = true
		if reason == "" {
			reason = "Put换仓：浮盈>" + strconv.FormatFloat(s.PutRollProfit*100, 'f', 0, 64) + "%"
		} else {
			reason += " & 浮盈>" + strconv.FormatFloat(s.PutRollProfit*100, 'f', 0, 64) + "%"
		}
	}

	if !needsRoll {
		return
	}

	s.debugf("bar=%d put roll triggered abs_delta=%.4f pnl_pct=%.4f reason=%s", ctx.BarIndex(), absDelta, pnlPct, reason)

	// Close current put
	exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
	ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice, reason)

	// Determine expiry from current RSI regime
	rsi := ctx.Ind("rsi200")
	expiryDays := s.BearExpiryDays
	if !math.IsNaN(rsi) && rsi > s.RSIThreshold {
		expiryDays = s.BullExpiryDays
	}

	// Reopen put with proceeds from the close
	proceeds := leg.Qty * exitPrice
	putSlot := s.openLongPut(ctx, chain, expiryDays, proceeds)
	if putSlot != nil {
		s.activePut = putSlot
	} else {
		s.activePut = nil
	}
}

// checkVolCondition verifies that MA(Std,20) is above the 50th percentile
// over the past VolQuantilePeriod bars.
func (s *btcCoinEnhancedStrategy) checkVolCondition(ctx *backtest.BarContext) bool {
	maStd := ctx.Ind("ma_std20")
	if math.IsNaN(maStd) {
		return false
	}

	// Compute percentile rank within the lookback window
	count := 0
	total := 0
	for k := 0; k < s.VolQuantilePeriod; k++ {
		v := ctx.IndAt("ma_std20", k)
		if math.IsNaN(v) {
			continue
		}
		total++
		if v < maStd {
			count++
		}
	}
	if total == 0 {
		return false
	}
	rank := float64(count) / float64(total)
	if s.shouldDebugBar(ctx.BarIndex()) {
		s.debugf("bar=%d vol_check rank=%.4f threshold=%.4f", ctx.BarIndex(), rank, s.VolQuantileMin)
	}
	return rank >= s.VolQuantileMin
}

// checkDivergence checks for MACD top-divergence:
// Price makes new 30-bar high but MACD DIFF is lower than at the previous 30-bar high.
func (s *btcCoinEnhancedStrategy) checkDivergence(ctx *backtest.BarContext) bool {
	barIndex := ctx.BarIndex()
	high := ctx.High()
	hh30 := ctx.Ind("hh30")
	diff := ctx.Ind("macd")

	if math.IsNaN(high) || math.IsNaN(hh30) || math.IsNaN(diff) {
		return false
	}

	// Current bar must be making a new 30-bar high
	if high != hh30 {
		return false
	}

	// Find the previous 30-bar high (the one before the current)
	prevHH := math.NaN()
	prevDiff := math.NaN()

	for barsAgo := 1; barsAgo < s.DivergencePeriod*3 && barsAgo <= barIndex; barsAgo++ {
		pastHigh := ctx.FieldAt("high", barsAgo)
		pastHH30 := ctx.IndAt("hh30", barsAgo)
		if math.IsNaN(pastHigh) || math.IsNaN(pastHH30) {
			continue
		}
		// This bar was a local 30-bar high point
		if pastHigh == pastHH30 && pastHigh < high {
			prevHH = pastHigh
			prevDiff = ctx.IndAt("macd", barsAgo)
			break
		}
	}

	if math.IsNaN(prevHH) || math.IsNaN(prevDiff) {
		return false
	}

	// Divergence: price higher but DIFF lower
	diverged := high > prevHH && diff < prevDiff
	if diverged && s.shouldDebugBar(barIndex) {
		s.debugf("bar=%d divergence detected high=%.6f prev_hh=%.6f diff=%.6f prev_diff=%.6f",
			barIndex, high, prevHH, diff, prevDiff)
	}
	return diverged
}

// openShortCall sells a call option with Delta ≈ 0.3 and the given expiry.
func (s *btcCoinEnhancedStrategy) openShortCall(ctx *backtest.BarContext, chain *backtest.OptionsChain, expiryDays int) *enhancedSlot {
	calls := chain.Calls().ExpiryNearest(expiryDays)
	if calls.Len() == 0 {
		s.debugf("bar=%d openShortCall: no calls near %d DTE", ctx.BarIndex(), expiryDays)
		return nil
	}

	sorted := calls.SortByDelta(s.CallDelta)
	if len(sorted) == 0 {
		return nil
	}
	contract := sorted[0]

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, contract)
	if !optionPriceOK(entryPrice) {
		s.debugf("bar=%d openShortCall: invalid entry price=%.6f symbol=%s", ctx.BarIndex(), entryPrice, contract.Symbol)
		return nil
	}

	// Size: notional 10 BTC → qty = PositionBTC / underlying_price * entryPrice normalization
	// For BTC options on Deribit, qty is in contracts (1 contract = 1 BTC notional).
	qty := s.PositionBTC

	s.debugf("bar=%d openShortCall symbol=%s delta=%.4f iv=%.4f dte=%.1f entry=%.6f qty=%.4f",
		ctx.BarIndex(), contract.Symbol, contract.Delta, contract.IV, contract.DaysToExpiry(ctx.Time()), entryPrice, qty)

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   contract,
		Side:       backtest.Sell,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, "币本位增强-SellCall")

	if spreadID <= 0 {
		return nil
	}

	return &enhancedSlot{
		spreadID:   spreadID,
		entryPrice: entryPrice,
		qty:        qty,
	}
}

// openLongPut buys a put option with Delta ≈ -0.25 using the given budget.
func (s *btcCoinEnhancedStrategy) openLongPut(ctx *backtest.BarContext, chain *backtest.OptionsChain, expiryDays int, budget float64) *enhancedSlot {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	puts := chain.Puts().ExpiryNearest(expiryDays)
	if puts.Len() == 0 {
		s.debugf("bar=%d openLongPut: no puts near %d DTE", ctx.BarIndex(), expiryDays)
		return nil
	}

	sorted := puts.SortByDelta(s.PutDelta)
	if len(sorted) == 0 {
		return nil
	}
	contract := sorted[0]

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, contract)
	if !optionPriceOK(entryPrice) {
		s.debugf("bar=%d openLongPut: invalid entry price=%.6f symbol=%s", ctx.BarIndex(), entryPrice, contract.Symbol)
		return nil
	}

	// Size: spend the budget
	qty := budget / entryPrice
	if qty <= 0 {
		return nil
	}

	s.debugf("bar=%d openLongPut symbol=%s delta=%.4f iv=%.4f dte=%.1f entry=%.6f qty=%.4f budget=%.6f",
		ctx.BarIndex(), contract.Symbol, contract.Delta, contract.IV, contract.DaysToExpiry(ctx.Time()), entryPrice, qty, budget)

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   contract,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, "币本位增强-BuyPut")

	if spreadID <= 0 {
		return nil
	}

	return &enhancedSlot{
		spreadID:   spreadID,
		entryPrice: entryPrice,
		qty:        qty,
	}
}

func (s *btcCoinEnhancedStrategy) currentContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if contractMap == nil {
		return contract
	}
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func (s *btcCoinEnhancedStrategy) putSpreadID() int {
	if s.activePut == nil {
		return 0
	}
	return s.activePut.spreadID
}

func optionPriceOK(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

func (s *btcCoinEnhancedStrategy) applyDefaults() {
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
	if s.RSIPeriod <= 0 {
		s.RSIPeriod = 200
	}
	if s.RSIThreshold <= 0 {
		s.RSIThreshold = 50
	}
	if s.DivergencePeriod <= 0 {
		s.DivergencePeriod = 30
	}
	if s.VolQuantilePeriod <= 0 {
		s.VolQuantilePeriod = 100
	}
	if s.VolQuantileMin <= 0 {
		s.VolQuantileMin = 0.50
	}
	if s.CallDelta <= 0 {
		s.CallDelta = 0.3
	}
	if s.PutDelta == 0 {
		s.PutDelta = -0.25
	}
	if s.PutBudgetRatio <= 0 {
		s.PutBudgetRatio = 0.70
	}
	if s.PutRollDelta <= 0 {
		s.PutRollDelta = 0.5
	}
	if s.PutRollProfit <= 0 {
		s.PutRollProfit = 0.50
	}
	if s.CallHalfProfit <= 0 {
		s.CallHalfProfit = 0.70
	}
	if s.CallFullProfit <= 0 {
		s.CallFullProfit = 0.88
	}
	if s.BullExpiryDays <= 0 {
		s.BullExpiryDays = 40
	}
	if s.BearExpiryDays <= 0 {
		s.BearExpiryDays = 25
	}
	if s.PositionBTC <= 0 {
		s.PositionBTC = 10
	}
	if !s.Debug {
		s.Debug = parseEnvBool("TOKTIK_BTC_ENHANCED_DEBUG")
	}
	if s.DebugEvery <= 0 {
		s.DebugEvery = parseEnvInt("TOKTIK_BTC_ENHANCED_DEBUG_EVERY", 100)
	}
}

func (s *btcCoinEnhancedStrategy) shouldDebugBar(barIndex int) bool {
	if !s.Debug {
		return false
	}
	if s.DebugEvery <= 0 {
		return true
	}
	return barIndex%s.DebugEvery == 0
}

func (s *btcCoinEnhancedStrategy) debugf(format string, args ...interface{}) {
	if !s.Debug {
		return
	}
	log.Printf("[btc-coin-enhanced] "+format, args...)
}
