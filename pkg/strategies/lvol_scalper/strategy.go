package lvolscalper

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

const (
	strategyName  = "lvol-scalper"
	strategyAlias = "lvol_scalper"

	defaultTargetExpiryDays = 14
	defaultMinExpiryDays    = 7
	defaultMaxExpiryDays    = 20
	defaultSafetyFactor     = 2.25
	defaultPremiumCap       = 20.0
	defaultNotionalCap      = 500.0
	defaultMarginWarn       = 0.60
	defaultTakeProfitPct    = 0.175
	defaultStopLossBTC      = 20.0
	defaultDeltaTrigger     = 15.0
	defaultDeltaTarget      = 3.0
	defaultSessionDays      = 30
	defaultRVLookbackDays   = 7
	defaultMinPremium       = 0.001

	entryStartHour = 2
	entryEndHour   = 5
	exitHour       = 12
	exitMinute     = 30

	preloadColSessionSigma = "lvol_session_sigma"
	preloadColRVP20        = "lvol_rv7d_p20"
	preloadColMove5m       = "lvol_move_5m"
	preloadColRV7d         = "lvol_rv7d"

	yearSeconds = 365.0 * 24.0 * 60.0 * 60.0
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{strategyAlias},
		Groups:  []string{"options", "timed", "hedged"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeMaterial},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			strategy := &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				targetExpiryDays: catalog.IntOrDefault(cfg.TargetExpiryDays, defaultTargetExpiryDays),
				minExpiryDays:    catalog.IntOrDefault(cfg.MinExpiryDays, defaultMinExpiryDays),
				maxExpiryDays:    defaultMaxExpiryDays,
				safetyFactor:     defaultSafetyFactor,
				premiumCap:       defaultPremiumCap,
				notionalCap:      defaultNotionalCap,
				marginWarn:       defaultMarginWarn,
				takeProfitPct:    defaultTakeProfitPct,
				stopLossBTC:      defaultStopLossBTC,
				deltaTrigger:     defaultDeltaTrigger,
				deltaTarget:      defaultDeltaTarget,
				minPremium:       catalog.FloatOrDefault(cfg.MinPremium, defaultMinPremium),
			}
			strategy.ApplyPricingDefaults()
			return strategy, nil
		},
	})
}

type strategy struct {
	optutil.PricingMixin

	targetExpiryDays int
	minExpiryDays    int
	maxExpiryDays    int
	safetyFactor     float64
	premiumCap       float64
	notionalCap      float64
	marginWarn       float64
	takeProfitPct    float64
	stopLossBTC      float64
	deltaTrigger     float64
	deltaTarget      float64
	minPremium       float64

	activeSpreadID  int
	entryPremium    float64
	entryDayKey     int
	lastEntryDayKey int
}

type shortStrangleSelection struct {
	call      backtest.OptionContract
	put       backtest.OptionContract
	callPrice float64
	putPrice  float64
	qty       float64
}

func (s *strategy) Name() string { return "LVolScalper" }

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetWarmup((defaultSessionDays + defaultRVLookbackDays + 2) * 24 * time.Hour)
	ctx.SetParam("target_expiry_days", s.targetExpiryDays)
	ctx.SetParam("min_expiry_days", s.minExpiryDays)
	ctx.SetParam("max_expiry_days", s.maxExpiryDays)
	ctx.SetParam("safety_factor", s.safetyFactor)
	ctx.SetParam("premium_cap", s.premiumCap)
	ctx.SetParam("notional_cap", s.notionalCap)
	ctx.SetParam("delta_trigger", s.deltaTrigger)
	ctx.SetParam("delta_target", s.deltaTarget)
	ctx.SetParam("take_profit_pct", s.takeProfitPct)
	ctx.SetParam("stop_loss_btc", s.stopLossBTC)
	return nil
}

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: preloadColSessionSigma, Label: "Session Sigma", Decimals: 4},
		{Source: preloadColRV7d, Label: "RV 7D", Decimals: 4},
		{Source: preloadColRVP20, Label: "RV P20", Decimals: 4},
		{Source: preloadColMove5m, Label: "Move 5M", Decimals: 4},
	}
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}

	timestamps := primary.Timestamps()
	closeCol, err := primary.RequireColumn("close")
	if err != nil {
		return err
	}

	returns := computeReturns(closeCol)
	sessionSigma := computeSessionSigmaSeries(timestamps, returns, defaultSessionDays)
	rv7d := computeAnnualizedRVSeries(timestamps, returns, defaultRVLookbackDays)
	rvP20 := computeDailyRollingPercentileSeries(timestamps, rv7d, defaultRVLookbackDays, 0.20)
	move5m := computeMoveWindowSeries(timestamps, closeCol, 5*time.Minute)

	if err := primary.SetColumn(preloadColSessionSigma, sessionSigma); err != nil {
		return err
	}
	if err := primary.SetColumn(preloadColRV7d, rv7d); err != nil {
		return err
	}
	if err := primary.SetColumn(preloadColRVP20, rvP20); err != nil {
		return err
	}
	if err := primary.SetColumn(preloadColMove5m, move5m); err != nil {
		return err
	}
	return nil
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)
	primary := ctx.PrimaryRef()
	now := ctx.Time().UTC()

	if s.activeSpreadID != 0 {
		if s.handleExit(ctx, contractMap, primary, now) {
			return
		}
		s.hedgeDelta(ctx, contractMap, primary)
		return
	}

	if pos := ctx.Position(primary); pos != 0 {
		s.flattenPrimaryNow(ctx, primary, "orphan_hedge_flatten")
	}

	dayKey := dateKey(now)
	if s.lastEntryDayKey == dayKey {
		return
	}
	if !withinEntryWindow(now) {
		return
	}

	sessionSigma := ctx.Field(preloadColSessionSigma)
	if !isFinitePositive(sessionSigma) {
		return
	}

	rvFloor := ctx.Field(preloadColRVP20)
	atmIV, ok := currentATMIV(chain, now, s.minExpiryDays, s.maxExpiryDays, s.targetExpiryDays)
	if !ok || (isFinitePositive(rvFloor) && atmIV < rvFloor) {
		return
	}

	selection, ok := s.selectContracts(chain, ctx.Close(), sessionSigma, now, ctx.Equity())
	if !ok {
		return
	}

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{
		{Contract: selection.call, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.callPrice},
		{Contract: selection.put, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.putPrice},
	}, fmt.Sprintf("short-strangle|sigma=%.4f|iv=%.4f|qty=%.4f", sessionSigma, atmIV, selection.qty))
	if spreadID <= 0 {
		return
	}

	s.activeSpreadID = spreadID
	s.entryPremium = selection.qty * (selection.callPrice + selection.putPrice)
	s.entryDayKey = dayKey
	s.lastEntryDayKey = dayKey
	s.hedgeDelta(ctx, contractMap, primary)
}

func (s *strategy) handleExit(ctx *backtest.BarContext, contractMap optutil.ContractMap, primary backtest.SecurityRef, now time.Time) bool {
	spread := ctx.Spreads().Get(s.activeSpreadID)
	if spread == nil || spread.IsFullyClosed() {
		s.resetPositionState()
		s.flattenPrimaryNow(ctx, primary, "post_close_flatten")
		return true
	}

	if forceExitTime(now) {
		s.closeAll(ctx, spread, contractMap, primary, "utc_1230_exit")
		return true
	}

	move5m := ctx.Field(preloadColMove5m)
	if !math.IsNaN(move5m) && math.Abs(move5m) >= 0.03 {
		s.closeAll(ctx, spread, contractMap, primary, "circuit_breaker_5m_3pct")
		return true
	}

	optionPnL, netDelta, grossNotional, ok := s.positionState(ctx, spread, contractMap, primary)
	if !ok {
		return false
	}

	if s.entryPremium > 0 && optionPnL >= s.entryPremium*s.takeProfitPct {
		s.closeAll(ctx, spread, contractMap, primary, "take_profit")
		return true
	}
	if optionPnL <= -s.stopLossBTC {
		s.closeAll(ctx, spread, contractMap, primary, "stop_loss")
		return true
	}
	if ctx.Equity() > 0 && grossNotional/ctx.Equity() >= 1.0/s.marginWarn {
		s.closeAll(ctx, spread, contractMap, primary, "risk_deleveraging")
		return true
	}
	if math.Abs(netDelta) >= 4*s.deltaTrigger {
		s.closeAll(ctx, spread, contractMap, primary, "delta_runaway")
		return true
	}

	return false
}

func (s *strategy) hedgeDelta(ctx *backtest.BarContext, contractMap optutil.ContractMap, primary backtest.SecurityRef) {
	if s.activeSpreadID == 0 {
		return
	}
	spread := ctx.Spreads().Get(s.activeSpreadID)
	if spread == nil || spread.IsFullyClosed() {
		return
	}

	_, netDelta, _, ok := s.positionState(ctx, spread, contractMap, primary)
	if !ok {
		return
	}
	if math.Abs(netDelta) < s.deltaTrigger {
		return
	}

	targetNet := math.Copysign(s.deltaTarget, netDelta)
	adjustment := netDelta - targetNet
	if adjustment > 0 {
		ctx.SellNowWithNote(primary, adjustment, "delta_hedge_sell")
		return
	}
	ctx.BuyNowWithNote(primary, -adjustment, "delta_hedge_buy")
}

func (s *strategy) positionState(ctx *backtest.BarContext, spread *backtest.SpreadPosition, contractMap optutil.ContractMap, primary backtest.SecurityRef) (float64, float64, float64, bool) {
	if spread == nil {
		return 0, 0, 0, false
	}

	optionPnL := 0.0
	optionDelta := 0.0
	grossNotional := 0.0

	for i := range spread.Legs {
		leg := spread.Legs[i]
		if leg.Closed {
			continue
		}
		contract := optutil.ResolveContract(leg.Contract, contractMap)
		mark := s.ValuationPriceMode.ExitPrice(leg.Side, contract)
		if !isFinitePositive(mark) {
			return 0, 0, 0, false
		}
		optionPnL += leg.UnrealizedPnL(mark)
		signedQty := leg.Qty
		if leg.Side == backtest.Sell {
			signedQty = -signedQty
		}
		optionDelta += signedQty * contract.Delta
		grossNotional += math.Abs(leg.Qty * contract.UnderlyingPrice)
	}

	netDelta := optionDelta + ctx.Position(primary)
	return optionPnL, netDelta, grossNotional, true
}

func (s *strategy) closeAll(ctx *backtest.BarContext, spread *backtest.SpreadPosition, contractMap optutil.ContractMap, primary backtest.SecurityRef, reason string) {
	if spread != nil {
		for i := range spread.Legs {
			leg := spread.Legs[i]
			if leg.Closed {
				continue
			}
			exitPrice := s.LegExitPrice(leg, contractMap)
			if !isFinitePositive(exitPrice) {
				continue
			}
			ctx.CloseSpreadLegWithReason(spread.ID, i, exitPrice, reason)
		}
	}
	s.flattenPrimaryNow(ctx, primary, reason)
	s.resetPositionState()
}

func (s *strategy) flattenPrimaryNow(ctx *backtest.BarContext, primary backtest.SecurityRef, note string) {
	position := ctx.Position(primary)
	if position > 0 {
		ctx.SellNowWithNote(primary, position, note)
		return
	}
	if position < 0 {
		ctx.BuyNowWithNote(primary, -position, note)
	}
}

func (s *strategy) resetPositionState() {
	s.activeSpreadID = 0
	s.entryPremium = 0
	s.entryDayKey = 0
}

func (s *strategy) selectContracts(chain *backtest.OptionsChain, spot, sessionSigma float64, now time.Time, equity float64) (*shortStrangleSelection, bool) {
	if chain == nil || chain.Len() == 0 || !isFinitePositive(spot) {
		return nil, false
	}
	if equity <= 0 {
		return nil, false
	}

	filtered := chain.ExpiryRange(s.minExpiryDays, s.maxExpiryDays)
	if filtered.Len() == 0 {
		return nil, false
	}

	expiry, ok := nearestExpiry(filtered.Contracts(), now, s.targetExpiryDays)
	if !ok {
		return nil, false
	}

	var calls []backtest.OptionContract
	var puts []backtest.OptionContract
	for _, contract := range filtered.Contracts() {
		if !contract.Expiration.Equal(expiry) {
			continue
		}
		switch contract.Type {
		case backtest.Call:
			calls = append(calls, contract)
		case backtest.Put:
			puts = append(puts, contract)
		}
	}
	if len(calls) == 0 || len(puts) == 0 {
		return nil, false
	}

	targetCallStrike := spot * (1 + s.safetyFactor*sessionSigma)
	targetPutStrike := spot * (1 - s.safetyFactor*sessionSigma)
	call, callPrice, ok := pickShortCall(calls, targetCallStrike, s.EntryPriceMode, s.minPremium)
	if !ok {
		return nil, false
	}
	put, putPrice, ok := pickShortPut(puts, targetPutStrike, s.EntryPriceMode, s.minPremium)
	if !ok {
		return nil, false
	}

	premiumPerSet := callPrice + putPrice
	if !isFinitePositive(premiumPerSet) {
		return nil, false
	}

	premiumQty := s.premiumCap / premiumPerSet
	notionalQty := s.notionalCap / (2 * spot)
	marginQty := (equity * s.marginWarn) / (2 * spot)
	qty := math.Min(premiumQty, math.Min(notionalQty, marginQty))
	if !isFinitePositive(qty) {
		return nil, false
	}

	return &shortStrangleSelection{
		call:      call,
		put:       put,
		callPrice: callPrice,
		putPrice:  putPrice,
		qty:       qty,
	}, true
}

func nearestExpiry(contracts []backtest.OptionContract, now time.Time, targetDays int) (time.Time, bool) {
	if len(contracts) == 0 {
		return time.Time{}, false
	}
	seen := make(map[int64]time.Time)
	for _, contract := range contracts {
		seen[contract.Expiration.Unix()] = contract.Expiration
	}
	bestDiff := math.Inf(1)
	bestExpiry := time.Time{}
	for _, expiry := range seen {
		diff := math.Abs(expiry.Sub(now).Hours()/24 - float64(targetDays))
		if diff < bestDiff {
			bestDiff = diff
			bestExpiry = expiry
		}
	}
	return bestExpiry, !bestExpiry.IsZero()
}

func pickShortCall(contracts []backtest.OptionContract, targetStrike float64, mode backtest.OptionPriceMode, minPremium float64) (backtest.OptionContract, float64, bool) {
	sorted := append([]backtest.OptionContract(nil), contracts...)
	sort.Slice(sorted, func(i, j int) bool {
		left := sorted[i].StrikePrice
		right := sorted[j].StrikePrice
		if left >= targetStrike && right < targetStrike {
			return true
		}
		if left < targetStrike && right >= targetStrike {
			return false
		}
		di := math.Abs(left - targetStrike)
		dj := math.Abs(right - targetStrike)
		if di != dj {
			return di < dj
		}
		return sorted[i].SpreadRatio() < sorted[j].SpreadRatio()
	})
	for _, contract := range sorted {
		price := mode.EntryPrice(backtest.Sell, contract)
		if isFinitePositive(price) && price >= minPremium {
			return contract, price, true
		}
	}
	return backtest.OptionContract{}, 0, false
}

func pickShortPut(contracts []backtest.OptionContract, targetStrike float64, mode backtest.OptionPriceMode, minPremium float64) (backtest.OptionContract, float64, bool) {
	sorted := append([]backtest.OptionContract(nil), contracts...)
	sort.Slice(sorted, func(i, j int) bool {
		left := sorted[i].StrikePrice
		right := sorted[j].StrikePrice
		if left <= targetStrike && right > targetStrike {
			return true
		}
		if left > targetStrike && right <= targetStrike {
			return false
		}
		di := math.Abs(left - targetStrike)
		dj := math.Abs(right - targetStrike)
		if di != dj {
			return di < dj
		}
		return sorted[i].SpreadRatio() < sorted[j].SpreadRatio()
	})
	for _, contract := range sorted {
		price := mode.EntryPrice(backtest.Sell, contract)
		if isFinitePositive(price) && price >= minPremium {
			return contract, price, true
		}
	}
	return backtest.OptionContract{}, 0, false
}

func currentATMIV(chain *backtest.OptionsChain, now time.Time, minExpiryDays, maxExpiryDays, targetExpiryDays int) (float64, bool) {
	if chain == nil || chain.Len() == 0 {
		return 0, false
	}
	filtered := chain.ExpiryRange(minExpiryDays, maxExpiryDays)
	if filtered.Len() == 0 {
		return 0, false
	}
	expiry, ok := nearestExpiry(filtered.Contracts(), now, targetExpiryDays)
	if !ok {
		return 0, false
	}
	spot := math.NaN()
	for _, contract := range filtered.Contracts() {
		if contract.Expiration.Equal(expiry) && isFinitePositive(contract.UnderlyingPrice) {
			spot = contract.UnderlyingPrice
			break
		}
	}
	if !isFinitePositive(spot) {
		return 0, false
	}

	bestCallDist := math.Inf(1)
	bestPutDist := math.Inf(1)
	callIV := math.NaN()
	putIV := math.NaN()
	for _, contract := range filtered.Contracts() {
		if !contract.Expiration.Equal(expiry) || !isFinitePositive(contract.IV) {
			continue
		}
		dist := math.Abs(contract.StrikePrice - spot)
		switch contract.Type {
		case backtest.Call:
			if dist < bestCallDist {
				bestCallDist = dist
				callIV = contract.IV
			}
		case backtest.Put:
			if dist < bestPutDist {
				bestPutDist = dist
				putIV = contract.IV
			}
		}
	}
	if isFinitePositive(callIV) && isFinitePositive(putIV) {
		return (callIV + putIV) / 2, true
	}
	if isFinitePositive(callIV) {
		return callIV, true
	}
	if isFinitePositive(putIV) {
		return putIV, true
	}
	return 0, false
}

func withinEntryWindow(now time.Time) bool {
	hour := now.Hour()
	return hour >= entryStartHour && hour < entryEndHour
}

func forceExitTime(now time.Time) bool {
	if now.Hour() > exitHour {
		return true
	}
	return now.Hour() == exitHour && now.Minute() >= exitMinute
}

func computeReturns(closeCol []float64) []float64 {
	returns := make([]float64, len(closeCol))
	for i := range returns {
		returns[i] = math.NaN()
	}
	for i := 1; i < len(closeCol); i++ {
		prev := closeCol[i-1]
		curr := closeCol[i]
		if !isFinitePositive(prev) || !isFinitePositive(curr) {
			continue
		}
		returns[i] = math.Log(curr / prev)
	}
	return returns
}

func computeMoveWindowSeries(timestamps []time.Time, closeCol []float64, window time.Duration) []float64 {
	out := make([]float64, len(closeCol))
	for i := range out {
		out[i] = math.NaN()
	}
	for i := range timestamps {
		target := timestamps[i].Add(-window)
		idx := lastIndexAtOrBefore(timestamps, target)
		if idx < 0 || idx >= i {
			continue
		}
		if !isFinitePositive(closeCol[idx]) || !isFinitePositive(closeCol[i]) {
			continue
		}
		out[i] = closeCol[i]/closeCol[idx] - 1
	}
	return out
}

func computeSessionSigmaSeries(timestamps []time.Time, returns []float64, emaDays int) []float64 {
	out := make([]float64, len(returns))
	for i := range out {
		out[i] = math.NaN()
	}

	daySigmas := make(map[int]float64)
	for i := 1; i < len(timestamps); i++ {
		if math.IsNaN(returns[i]) {
			continue
		}
		day := dateKey(timestamps[i].UTC())
		if !sameDay(timestamps[i-1].UTC(), timestamps[i].UTC()) {
			continue
		}
		if !withinSessionSigmaWindow(timestamps[i].UTC()) {
			continue
		}
		daySigmas[day] = math.NaN()
	}

	dayReturns := make(map[int][]float64)
	for i := 1; i < len(timestamps); i++ {
		if math.IsNaN(returns[i]) {
			continue
		}
		current := timestamps[i].UTC()
		prev := timestamps[i-1].UTC()
		if !sameDay(prev, current) || !withinSessionSigmaWindow(current) {
			continue
		}
		dayReturns[dateKey(current)] = append(dayReturns[dateKey(current)], returns[i])
	}

	for day, values := range dayReturns {
		if len(values) < 2 {
			continue
		}
		baseStd := stddev(values)
		if math.IsNaN(baseStd) {
			continue
		}
		daySigmas[day] = baseStd * math.Sqrt(float64(len(values)))
	}

	days := sortedIntKeys(daySigmas)
	alpha := 2.0 / (float64(emaDays) + 1)
	prevEMAByDay := make(map[int]float64, len(days))
	ema := math.NaN()
	for _, day := range days {
		prevEMAByDay[day] = ema
		sigma := daySigmas[day]
		if math.IsNaN(sigma) {
			continue
		}
		if math.IsNaN(ema) {
			ema = sigma
		} else {
			ema = alpha*sigma + (1-alpha)*ema
		}
	}

	for i, ts := range timestamps {
		if value, ok := prevEMAByDay[dateKey(ts.UTC())]; ok {
			out[i] = value
		}
	}
	return out
}

func computeAnnualizedRVSeries(timestamps []time.Time, returns []float64, lookbackDays int) []float64 {
	out := make([]float64, len(returns))
	for i := range out {
		out[i] = math.NaN()
	}
	if len(returns) == 0 {
		return out
	}

	window := time.Duration(lookbackDays) * 24 * time.Hour
	start := 1
	sumSquares := 0.0
	validCount := 0
	for i := 1; i < len(returns); i++ {
		if !math.IsNaN(returns[i]) {
			sumSquares += returns[i] * returns[i]
			validCount++
		}
		cutoff := timestamps[i].Add(-window)
		for start < i && timestamps[start].Before(cutoff) {
			if !math.IsNaN(returns[start]) {
				sumSquares -= returns[start] * returns[start]
				validCount--
			}
			start++
		}
		if validCount < 2 {
			continue
		}
		elapsedSeconds := timestamps[i].Sub(timestamps[start]).Seconds()
		if elapsedSeconds <= 0 {
			continue
		}
		out[i] = math.Sqrt(sumSquares * yearSeconds / elapsedSeconds)
	}
	return out
}

func computeDailyRollingPercentileSeries(timestamps []time.Time, series []float64, lookbackDays int, q float64) []float64 {
	out := make([]float64, len(series))
	for i := range out {
		out[i] = math.NaN()
	}
	dailyValues := make(map[int]float64)
	for i, ts := range timestamps {
		if math.IsNaN(series[i]) {
			continue
		}
		dailyValues[dateKey(ts.UTC())] = series[i]
	}
	days := sortedIntKeys(dailyValues)
	percentileByDay := make(map[int]float64, len(days))
	for idx, day := range days {
		start := idx - lookbackDays
		if start < 0 {
			start = 0
		}
		values := make([]float64, 0, lookbackDays)
		for j := start; j < idx; j++ {
			value := dailyValues[days[j]]
			if !math.IsNaN(value) {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			percentileByDay[day] = quantile(values, q)
		}
	}
	for i, ts := range timestamps {
		if value, ok := percentileByDay[dateKey(ts.UTC())]; ok {
			out[i] = value
		}
	}
	return out
}

func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	position := q * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return math.NaN()
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	variance /= float64(len(values))
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func withinSessionSigmaWindow(now time.Time) bool {
	hour := now.Hour()
	return hour >= entryStartHour && hour < exitHour
}

func lastIndexAtOrBefore(timestamps []time.Time, target time.Time) int {
	idx := sort.Search(len(timestamps), func(i int) bool {
		return !timestamps[i].Before(target)
	})
	if idx < len(timestamps) && timestamps[idx].Equal(target) {
		return idx
	}
	return idx - 1
}

func sameDay(left, right time.Time) bool {
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}

func dateKey(now time.Time) int {
	year, month, day := now.Date()
	return year*10000 + int(month)*100 + day
}

func sortedIntKeys(values map[int]float64) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
