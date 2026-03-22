package strategies

import (
	"fmt"
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

	// Higher-timeframe state for bullish-regime signal evaluation.
	htfRef             backtest.SecurityRef
	htfInterval        string
	lastHTFSignalIndex int

	// Internal state
	activeCalls [2]*enhancedSlot // tracked short call tranches
	activePut   *enhancedSlot    // tracked long put position
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
	s.lastHTFSignalIndex = -1

	primary := ctx.PrimaryRef()
	htfInterval, err := s.resolveHigherTimeframe(primary.Interval)
	if err != nil {
		return err
	}
	s.htfInterval = htfInterval
	s.htfRef = ctx.AddSecurity(primary.Market, primary.Symbol, htfInterval)

	registerPrimary := func(name string, ind backtest.Indicator) {
		ctx.Register(name, ind)
	}
	registerHTF := func(name string, ind backtest.Indicator) {
		ctx.RegisterOn(s.htfRef, name, ind)
	}

	s.registerSignalIndicators(registerPrimary)
	s.registerSignalIndicators(registerHTF)

	s.debugf("init entry_mode=%v exit_mode=%v valuation_mode=%v primary_interval=%s htf_interval=%s rsi_period=%d bull_expiry=%d bear_expiry=%d",
		s.EntryPriceMode, s.ExitPriceMode, s.ValuationPriceMode, primary.Interval, s.htfInterval, s.RSIPeriod, s.BullExpiryDays, s.BearExpiryDays)

	return nil
}

func (s *btcCoinEnhancedStrategy) Preload(ctx *backtest.PreloadContext) error {
	htf := ctx.Security(s.htfRef)
	if htf == nil || htf.Len() == 0 {
		return nil
	}

	rsi, err := htf.RequireColumn("rsi200")
	if err != nil {
		return err
	}
	high, err := htf.RequireColumn("high")
	if err != nil {
		return err
	}
	hh30, err := htf.RequireColumn("hh30")
	if err != nil {
		return err
	}
	diff, err := htf.RequireColumn("macd")
	if err != nil {
		return err
	}
	open, err := htf.RequireColumn("open")
	if err != nil {
		return err
	}
	close, err := htf.RequireColumn("close")
	if err != nil {
		return err
	}
	stdma20, err := htf.RequireColumn("stdma20")
	if err != nil {
		return err
	}

	derived := map[string][]float64{
		"rsi200_prev":     shiftSeries(rsi),
		"divergence_prev": shiftSeries(s.divergenceFlags(high, hh30, diff)),
		"bearish_prev":    shiftSeries(s.bearishCandleFlags(open, close)),
		"vol_ok_prev":     shiftSeries(s.volatilityFlags(stdma20)),
	}
	for name, values := range derived {
		if err := htf.SetColumn(name, values); err != nil {
			return err
		}
	}

	alignNames := []string{"rsi200_prev", "divergence_prev", "bearish_prev", "vol_ok_prev"}
	for _, name := range alignNames {
		aligned, err := ctx.ColumnAlignedToPrimary(s.htfRef, name)
		if err != nil {
			return err
		}
		if err := ctx.Primary().SetColumn("htf_"+name, aligned); err != nil {
			return err
		}
	}

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
	return ctx.Primary().SetColumn("htf_signal_index", alignedHTFIndex)
}

func (s *btcCoinEnhancedStrategy) registerSignalIndicators(register func(name string, ind backtest.Indicator)) {
	register("rsi200", backtest.RSI("close", s.RSIPeriod))
	register("macd", backtest.MACD("close", 12, 26, 9))
	register("hh30", backtest.Highest("high", s.DivergencePeriod))
	register("std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs["close"], 20)
		},
	))
	register("ma_std20", backtest.SMA("std20", 20))
	register("stdma20", backtest.Custom(
		[]string{"std20", "ma_std20"},
		func(inputs map[string][]float64) []float64 {
			return divideSeries(inputs["std20"], inputs["ma_std20"])
		},
	))
	register("vol_quantile", backtest.Quantile("stdma20", s.VolQuantilePeriod, 0))
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
	s.manageCallPositions(ctx, contractMap)
	s.managePutPosition(ctx, chain, contractMap)

	// Do not stack a new combo while any leg from the previous one remains active.
	if s.hasOpenExposure() {
		return
	}

	// --- Check entry signal ---
	rsi := ctx.Field("htf_rsi200_prev")
	if math.IsNaN(rsi) {
		return
	}

	// Determine expiry based on RSI regime
	useHTFSignals := rsi > s.RSIThreshold
	expiryDays := s.BearExpiryDays
	if useHTFSignals {
		expiryDays = s.BullExpiryDays
	}

	if useHTFSignals {
		if !s.consumeHTFSignal(ctx.Field("htf_signal_index")) {
			return
		}
		if !s.fieldFlag(ctx, "htf_vol_ok_prev") {
			return
		}
		if !s.fieldFlag(ctx, "htf_divergence_prev") {
			return
		}
		if !s.fieldFlag(ctx, "htf_bearish_prev") {
			return
		}
	} else {
		if !s.checkVolCondition(ctx, "stdma20") {
			return
		}
		if !s.checkDivergence(ctx, "high", "hh30", "macd") {
			return
		}
		if !s.isBearishCandle(ctx, "open", "close") {
			if s.shouldDebugBar(barIndex) {
				s.debugf("bar=%d skip: not a bearish candle close=%.6f open=%.6f", barIndex, close, ctx.Open())
			}
			return
		}
	}

	// --- Entry: build the hedge combo ---
	if chain == nil || chain.Len() == 0 {
		return
	}

	s.debugf("bar=%d entry signal triggered rsi=%.2f expiry_days=%d close=%.6f", barIndex, rsi, expiryDays, close)

	// 1. Sell Call: Delta ≈ 0.3, split into two equal tranches so the
	// documented 70% partial profit-taking can be represented exactly.
	callSlots := s.openShortCallTranches(ctx, chain, expiryDays)
	callPremium := 0.0
	for i := range callSlots {
		if callSlots[i] == nil {
			continue
		}
		s.activeCalls[i] = callSlots[i]
		callPremium += callSlots[i].entryPrice * callSlots[i].qty
	}
	if callPremium <= 0 {
		return
	}

	// 2. Buy Put: spend 70% of Call premium, Delta ≈ -0.25
	putBudget := callPremium * s.PutBudgetRatio
	putSlot := s.openLongPut(ctx, chain, expiryDays, putBudget)
	if putSlot != nil {
		s.activePut = putSlot
	}

	s.debugf("bar=%d entry complete call_spread_ids=[%d,%d] call_premium=%.6f put_budget=%.6f put_spread_id=%d",
		barIndex, s.callSpreadID(0), s.callSpreadID(1), callPremium, putBudget, s.putSpreadID())
}

// manageCallPositions manages the short call tranches.
func (s *btcCoinEnhancedStrategy) manageCallPositions(ctx *backtest.BarContext, contractMap map[string]backtest.OptionContract) {
	for idx := range s.activeCalls {
		slot := s.activeCalls[idx]
		if slot == nil {
			continue
		}

		sp := ctx.Spreads().Get(slot.spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.activeCalls[idx] = nil
			continue
		}

		leg := &sp.Legs[0]
		currentContract := s.currentContract(leg.Contract, contractMap)

		if currentContract.DaysToExpiry(ctx.Time()) <= 1 {
			exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
			ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice, "Call到期平仓")
			s.activeCalls[idx] = nil
			continue
		}

		markPrice := s.ValuationPriceMode.ExitPrice(leg.Side, currentContract)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
		if math.IsNaN(pnlPct) {
			continue
		}

		if pnlPct > s.CallFullProfit {
			exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
			ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice,
				"Call全平：浮盈>"+strconv.FormatFloat(s.CallFullProfit*100, 'f', 0, 64)+"%")
			s.activeCalls[idx] = nil
			s.debugf("bar=%d call full close tranche=%d pnl_pct=%.4f", ctx.BarIndex(), idx, pnlPct)
		}
	}

	if s.countActiveCalls() != len(s.activeCalls) {
		return
	}

	for idx := range s.activeCalls {
		slot := s.activeCalls[idx]
		if slot == nil {
			continue
		}

		sp := ctx.Spreads().Get(slot.spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.activeCalls[idx] = nil
			continue
		}

		leg := &sp.Legs[0]
		currentContract := s.currentContract(leg.Contract, contractMap)
		markPrice := s.ValuationPriceMode.ExitPrice(leg.Side, currentContract)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
		if math.IsNaN(pnlPct) || pnlPct <= s.CallHalfProfit {
			continue
		}

		exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, currentContract)
		ctx.CloseSpreadLegWithReason(sp.ID, 0, exitPrice,
			"Call半平：浮盈>"+strconv.FormatFloat(s.CallHalfProfit*100, 'f', 0, 64)+"%")
		s.activeCalls[idx] = nil
		s.debugf("bar=%d call half close tranche=%d pnl_pct=%.4f", ctx.BarIndex(), idx, pnlPct)
		return
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
	rsi := ctx.Field("htf_rsi200_prev")
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

// checkVolCondition verifies that Std(20)/MA(Std(20),20) is above the configured
// percentile threshold over the past VolQuantilePeriod bars.
func (s *btcCoinEnhancedStrategy) checkVolCondition(ctx *backtest.BarContext, fieldName string) bool {
	value := ctx.Field(fieldName)
	if math.IsNaN(value) {
		return false
	}

	// Compute percentile rank within the lookback window.
	count := 0
	total := 0
	for k := 0; k < s.VolQuantilePeriod; k++ {
		v := ctx.FieldAt(fieldName, k)
		if math.IsNaN(v) {
			continue
		}
		total++
		if v < value {
			count++
		}
	}
	if total == 0 {
		return false
	}
	rank := float64(count) / float64(total)
	if s.shouldDebugBar(ctx.BarIndex()) {
		s.debugf("bar=%d vol_check field=%s value=%.6f rank=%.4f threshold=%.4f", ctx.BarIndex(), fieldName, value, rank, s.VolQuantileMin)
	}
	return rank >= s.VolQuantileMin
}

// checkDivergence checks for MACD top-divergence using the documented formula:
//
//	HH30 = HHV(HIGH, 30)
//	PREV_HH = REF(HH30, 1)
//	SELL_SIGNAL = HIGH == HH30 && HIGH > PREV_HH && DIFF < PREV_DIFF_HH
func (s *btcCoinEnhancedStrategy) checkDivergence(ctx *backtest.BarContext, highField, hhField, diffField string) bool {
	barIndex := ctx.BarIndex()
	high := ctx.Field(highField)
	hh30 := ctx.Field(hhField)
	diff := ctx.Field(diffField)

	if math.IsNaN(high) || math.IsNaN(hh30) || math.IsNaN(diff) {
		return false
	}

	// Current bar must be making a new 30-bar high
	if high != hh30 {
		return false
	}

	prevHH := ctx.FieldAt(hhField, 1)
	if math.IsNaN(prevHH) || high <= prevHH {
		return false
	}

	prevDiff := math.NaN()

	for barsAgo := 1; barsAgo < s.DivergencePeriod*3 && barsAgo <= barIndex; barsAgo++ {
		pastHigh := ctx.FieldAt(highField, barsAgo)
		if math.IsNaN(pastHigh) {
			continue
		}
		if pastHigh == prevHH {
			prevDiff = ctx.FieldAt(diffField, barsAgo)
			break
		}
	}

	if math.IsNaN(prevDiff) {
		return false
	}

	diverged := diff < prevDiff
	if diverged && s.shouldDebugBar(barIndex) {
		s.debugf("bar=%d divergence detected high=%.6f prev_hh=%.6f diff=%.6f prev_diff=%.6f",
			barIndex, high, prevHH, diff, prevDiff)
	}
	return diverged
}

func (s *btcCoinEnhancedStrategy) fieldFlag(ctx *backtest.BarContext, fieldName string) bool {
	v := ctx.Field(fieldName)
	return !math.IsNaN(v) && v > 0
}

func (s *btcCoinEnhancedStrategy) isBearishCandle(ctx *backtest.BarContext, openField, closeField string) bool {
	open := ctx.Field(openField)
	close := ctx.Field(closeField)
	return !math.IsNaN(open) && !math.IsNaN(close) && close < open
}

// openShortCallTranches sells a call option with Delta ≈ 0.3 split across two tranches.
func (s *btcCoinEnhancedStrategy) openShortCallTranches(ctx *backtest.BarContext, chain *backtest.OptionsChain, expiryDays int) [2]*enhancedSlot {
	var slots [2]*enhancedSlot

	calls := chain.Calls().ExpiryNearest(expiryDays)
	if calls.Len() == 0 {
		s.debugf("bar=%d openShortCall: no calls near %d DTE", ctx.BarIndex(), expiryDays)
		return slots
	}

	sorted := calls.SortByDelta(s.CallDelta)
	if len(sorted) == 0 {
		return slots
	}
	contract := sorted[0]

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, contract)
	if !optionPriceOK(entryPrice) {
		s.debugf("bar=%d openShortCall: invalid entry price=%.6f symbol=%s", ctx.BarIndex(), entryPrice, contract.Symbol)
		return slots
	}

	qtyPerTranche := s.PositionBTC / float64(len(slots))
	if qtyPerTranche <= 0 {
		return slots
	}

	s.debugf("bar=%d openShortCall symbol=%s delta=%.4f iv=%.4f dte=%.1f entry=%.6f qty_per_tranche=%.4f",
		ctx.BarIndex(), contract.Symbol, contract.Delta, contract.IV, contract.DaysToExpiry(ctx.Time()), entryPrice, qtyPerTranche)

	for i := range slots {
		spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
			Contract:   contract,
			Side:       backtest.Sell,
			Qty:        qtyPerTranche,
			EntryPrice: entryPrice,
		}}, "币本位增强-SellCall")
		if spreadID <= 0 {
			return slots
		}
		slots[i] = &enhancedSlot{
			spreadID:   spreadID,
			entryPrice: entryPrice,
			qty:        qtyPerTranche,
		}
	}

	return slots
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

func (s *btcCoinEnhancedStrategy) countActiveCalls() int {
	count := 0
	for i := range s.activeCalls {
		if s.activeCalls[i] != nil {
			count++
		}
	}
	return count
}

func (s *btcCoinEnhancedStrategy) hasOpenExposure() bool {
	return s.countActiveCalls() > 0 || s.activePut != nil
}

func (s *btcCoinEnhancedStrategy) callSpreadID(idx int) int {
	if idx < 0 || idx >= len(s.activeCalls) || s.activeCalls[idx] == nil {
		return 0
	}
	return s.activeCalls[idx].spreadID
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

func divideSeries(num, denom []float64) []float64 {
	out := make([]float64, len(num))
	for i := range out {
		if i >= len(denom) || math.IsNaN(num[i]) || math.IsNaN(denom[i]) || denom[i] == 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = num[i] / denom[i]
	}
	return out
}

func shiftSeries(src []float64) []float64 {
	out := make([]float64, len(src))
	if len(src) == 0 {
		return out
	}
	out[0] = math.NaN()
	copy(out[1:], src[:len(src)-1])
	return out
}

func (s *btcCoinEnhancedStrategy) volatilityFlags(series []float64) []float64 {
	out := make([]float64, len(series))
	for i := range out {
		out[i] = 0
		current := series[i]
		if math.IsNaN(current) {
			continue
		}
		count := 0
		total := 0
		for k := 0; k < s.VolQuantilePeriod; k++ {
			idx := i - k
			if idx < 0 {
				break
			}
			v := series[idx]
			if math.IsNaN(v) {
				continue
			}
			total++
			if v < current {
				count++
			}
		}
		if total > 0 && float64(count)/float64(total) >= s.VolQuantileMin {
			out[i] = 1
		}
	}
	return out
}

func (s *btcCoinEnhancedStrategy) bearishCandleFlags(open, close []float64) []float64 {
	out := make([]float64, len(open))
	for i := range out {
		out[i] = 0
		if i >= len(close) || math.IsNaN(open[i]) || math.IsNaN(close[i]) {
			continue
		}
		if close[i] < open[i] {
			out[i] = 1
		}
	}
	return out
}

func (s *btcCoinEnhancedStrategy) divergenceFlags(high, hh30, diff []float64) []float64 {
	out := make([]float64, len(high))
	for i := range out {
		out[i] = 0
		if i >= len(hh30) || i >= len(diff) {
			continue
		}
		if math.IsNaN(high[i]) || math.IsNaN(hh30[i]) || math.IsNaN(diff[i]) || high[i] != hh30[i] {
			continue
		}
		if i == 0 || math.IsNaN(hh30[i-1]) || high[i] <= hh30[i-1] {
			continue
		}
		prevHH := hh30[i-1]
		prevDiff := math.NaN()
		for barsAgo := 1; barsAgo < s.DivergencePeriod*3 && barsAgo <= i; barsAgo++ {
			idx := i - barsAgo
			if math.IsNaN(high[idx]) {
				continue
			}
			if high[idx] == prevHH {
				prevDiff = diff[idx]
				break
			}
		}
		if !math.IsNaN(prevDiff) && diff[i] < prevDiff {
			out[i] = 1
		}
	}
	return out
}

func (s *btcCoinEnhancedStrategy) consumeHTFSignal(signalIndex float64) bool {
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

func (s *btcCoinEnhancedStrategy) resolveHigherTimeframe(primaryInterval string) (string, error) {
	switch primaryInterval {
	case "3h":
		return "12h", nil
	case "6h":
		return "1d", nil
	default:
		return "", fmt.Errorf("btc-coin-enhanced expects a small-cycle primary interval of 3h or 6h, got %q", primaryInterval)
	}
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
