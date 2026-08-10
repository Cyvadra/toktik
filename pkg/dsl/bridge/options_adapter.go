package bridge

import (
	"math"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/optionsanalytics"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
	"github.com/Cyvadra/toktik/pkg/feeds"
)

func (b *barContextBridge) OptionsChain() interface{} {
	ch := b.ctx.OptionsChain()
	if ch == nil || ch.Len() == 0 {
		return nil
	}
	return ch
}

func (b *barContextBridge) OptionsChainFor(market, underlying string) interface{} {
	ch := b.ctx.OptionsChainFor(market, underlying)
	if ch == nil || ch.Len() == 0 {
		return nil
	}
	return ch
}

func (b *barContextBridge) ChainCalls(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Calls()
	}
	return nil
}

func (b *barContextBridge) ChainPuts(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Puts()
	}
	return nil
}

func (b *barContextBridge) ChainExpiryNearest(chain interface{}, targetDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryNearest(targetDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryRange(chain interface{}, minDays, maxDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryRange(minDays, maxDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryMin(chain interface{}, minDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryMin(minDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryMax(chain interface{}, maxDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryMax(maxDays)
	}
	return nil
}

func (b *barContextBridge) ChainDeltaRange(chain interface{}, minDelta, maxDelta float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.DeltaRange(minDelta, maxDelta)
	}
	return nil
}

func (b *barContextBridge) ChainMinPremium(chain interface{}, minBid float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.MinPremium(minBid)
	}
	return nil
}

func (b *barContextBridge) ChainStrikeRange(chain interface{}, min, max float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.StrikeRange(min, max)
	}
	return nil
}

func (b *barContextBridge) ChainLen(chain interface{}) int {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Len()
	}
	return 0
}

func (b *barContextBridge) ChainBestSpread(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.BestSpread()
	}
	return nil
}

func (b *barContextBridge) ChainSortByDelta(chain interface{}, targetDelta float64) []interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		contracts := ch.SortByDelta(targetDelta)
		out := make([]interface{}, len(contracts))
		for i := range contracts {
			c := contracts[i]
			out[i] = &c
		}
		return out
	}
	return nil
}

func (b *barContextBridge) IVSmileSurface(chain interface{}, maxStrikeDistanceRatio float64) interface{} {
	ch, ok := chain.(*backtest.OptionsChain)
	if !ok || ch == nil {
		return nil
	}
	contracts := ch.Contracts()
	points := make([]optionsanalytics.IVPoint, 0, len(contracts))
	for _, contract := range contracts {
		points = append(points, optionsanalytics.IVPoint{
			Expiration:   contract.Expiration,
			OptionType:   string(contract.Type),
			Strike:       contract.StrikePrice,
			IV:           contract.IV,
			OpenInterest: contract.OpenInterest,
		})
	}
	surface, err := optionsanalytics.BuildIVSmileSurface(points, maxStrikeDistanceRatio)
	if err != nil || len(surface.Expirations) == 0 {
		return nil
	}
	return surface
}

func (b *barContextBridge) IVSmileExpirations(surface interface{}) []float64 {
	typed, ok := surface.(*optionsanalytics.IVSmileSurface)
	if !ok || typed == nil {
		return nil
	}
	values := make([]float64, len(typed.Expirations))
	for i, smile := range typed.Expirations {
		values[i] = float64(smile.Expiration.UTC().Unix())
	}
	return values
}

func (b *barContextBridge) IVSmile(surface interface{}, expiration float64) interface{} {
	typed, ok := surface.(*optionsanalytics.IVSmileSurface)
	if !ok || typed == nil || math.IsNaN(expiration) || math.IsInf(expiration, 0) {
		return nil
	}
	for i := range typed.Expirations {
		if float64(typed.Expirations[i].Expiration.UTC().Unix()) == expiration {
			return &typed.Expirations[i]
		}
	}
	return nil
}

func (b *barContextBridge) IVSmileExpiry(smile interface{}) float64 {
	if typed, ok := smile.(*optionsanalytics.ExpirationSmile); ok && typed != nil {
		return float64(typed.Expiration.UTC().Unix())
	}
	return math.NaN()
}

func (b *barContextBridge) IVSmileTotalOI(smile interface{}) float64 {
	if typed, ok := smile.(*optionsanalytics.ExpirationSmile); ok && typed != nil {
		return typed.TotalOI
	}
	return math.NaN()
}

func (b *barContextBridge) IVSmileOICoverage(smile interface{}, optionType string) float64 {
	curve := smileCurve(smile, optionType)
	if len(curve.Points) == 0 {
		return math.NaN()
	}
	return float64(curve.PositiveOIPoints) / float64(len(curve.Points))
}

func (b *barContextBridge) IVSmileStrikes(smile interface{}, optionType string) []float64 {
	curve := smileCurve(smile, optionType)
	values := make([]float64, len(curve.Points))
	for i, point := range curve.Points {
		values[i] = point.Strike
	}
	return values
}

func (b *barContextBridge) IVSmileValues(smile interface{}, optionType string, smoothed bool) []float64 {
	curve := smileCurve(smile, optionType)
	values := make([]float64, len(curve.Points))
	for i, point := range curve.Points {
		values[i] = point.RawIV
		if smoothed {
			values[i] = point.SmoothedIV
		}
	}
	return values
}

func (b *barContextBridge) IVSmileOpenInterests(smile interface{}, optionType string) []float64 {
	curve := smileCurve(smile, optionType)
	values := make([]float64, len(curve.Points))
	for i, point := range curve.Points {
		values[i] = point.OpenInterest
	}
	return values
}

func (b *barContextBridge) IVSmileAt(smile interface{}, optionType string, strike float64, smoothed bool) float64 {
	curve := smileCurve(smile, optionType)
	if len(curve.Points) == 0 || !isFinite(strike) || strike < curve.Points[0].Strike || strike > curve.Points[len(curve.Points)-1].Strike {
		return math.NaN()
	}
	for i, point := range curve.Points {
		value := point.RawIV
		if smoothed {
			value = point.SmoothedIV
		}
		if point.Strike == strike {
			return value
		}
		if i > 0 && strike < point.Strike {
			previous := curve.Points[i-1]
			previousValue := previous.RawIV
			if smoothed {
				previousValue = previous.SmoothedIV
			}
			return previousValue + (value-previousValue)*(strike-previous.Strike)/(point.Strike-previous.Strike)
		}
	}
	return math.NaN()
}

func smileCurve(smile interface{}, optionType string) optionsanalytics.Curve {
	typed, ok := smile.(*optionsanalytics.ExpirationSmile)
	if !ok || typed == nil {
		return optionsanalytics.Curve{}
	}
	switch strings.ToLower(strings.TrimSpace(optionType)) {
	case "call", "c":
		return typed.Call
	case "put", "p":
		return typed.Put
	default:
		return optionsanalytics.Curve{}
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (b *barContextBridge) ContractSymbol(c interface{}) string {
	return contractString(c, func(oc *backtest.OptionContract) string { return oc.Symbol })
}

func (b *barContextBridge) ContractUnderlying(c interface{}) string {
	return contractString(c, func(oc *backtest.OptionContract) string { return oc.ChainUnderlying() })
}

func (b *barContextBridge) ContractMarket(c interface{}) string {
	return contractString(c, func(oc *backtest.OptionContract) string { return oc.ChainMarket() })
}

func (b *barContextBridge) ContractType(c interface{}) string {
	return contractString(c, func(oc *backtest.OptionContract) string { return string(oc.Type) })
}

func (b *barContextBridge) ContractStrike(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.StrikePrice })
}

func (b *barContextBridge) ContractExpiry(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return float64(oc.Expiration.UTC().Unix()) })
}

func (b *barContextBridge) ContractDTE(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.DaysToExpiry(b.ctx.Time()) })
}

func (b *barContextBridge) ContractDelta(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.Delta })
}

func (b *barContextBridge) ContractGamma(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.Gamma })
}

func (b *barContextBridge) ContractVega(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.Vega })
}

func (b *barContextBridge) ContractTheta(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.Theta })
}

func (b *barContextBridge) ContractIV(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.IV })
}

func (b *barContextBridge) ContractBid(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.BidPrice })
}

func (b *barContextBridge) ContractAsk(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.AskPrice })
}

func (b *barContextBridge) ContractMark(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 {
		return backtest.OptionPriceMarkClose.EntryPrice(backtest.Buy, *oc)
	})
}

func (b *barContextBridge) ContractVolume(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.Volume })
}

func (b *barContextBridge) ContractOI(c interface{}) float64 {
	return contractFloat(c, func(oc *backtest.OptionContract) float64 { return oc.OpenInterest })
}

func contractString(c interface{}, read func(*backtest.OptionContract) string) string {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return read(oc)
	}
	return ""
}

func contractFloat(c interface{}, read func(*backtest.OptionContract) float64) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return read(oc)
	}
	return 0
}

func (b *barContextBridge) OpenSpread(legs []runtime.SpreadLegInput, tag string) int {
	btLegs := convertLegs(legs, b.spreadPricing.EntryMode)
	return b.ctx.OpenSpread(btLegs, tag)
}

func (b *barContextBridge) OpenSpreadInGroup(legs []runtime.SpreadLegInput, tag string, groupID int) int {
	btLegs := convertLegs(legs, b.spreadPricing.EntryMode)
	return b.ctx.OpenSpreadInGroup(btLegs, tag, groupID)
}

func (b *barContextBridge) CloseSpread(spreadID int) {
	b.CloseSpreadWithReason(spreadID, "")
}

func (b *barContextBridge) CloseSpreadWithReason(spreadID int, reason string) {
	sp := b.ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		price := b.spreadPricing.ExitMode.ExitPrice(sp.Legs[i].Side, sp.Legs[i].Contract)
		if reason != "" {
			b.ctx.CloseSpreadLegWithReason(spreadID, i, price, reason)
		} else {
			b.ctx.CloseSpreadLeg(spreadID, i, price)
		}
	}
}

func (b *barContextBridge) CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool {
	return b.ctx.CloseSpreadLeg(spreadID, legIndex, closePrice)
}

func (b *barContextBridge) SpreadGet(spreadID int) runtime.SpreadInfo {
	st := b.ctx.Spreads()
	if st == nil {
		return runtime.SpreadInfo{}
	}
	sp := st.Get(spreadID)
	if sp == nil {
		return runtime.SpreadInfo{}
	}
	return runtime.SpreadInfo{
		ID:          sp.ID,
		Tag:         sp.Tag,
		BarsHeld:    sp.BarsHeld(b.ctx.BarIndex()),
		RealizedPnL: sp.TotalRealizedPnL(),
		IsOpen:      !sp.IsFullyClosed(),
		LegCount:    len(sp.Legs),
	}
}

func (b *barContextBridge) OpenSpreads() []int {
	st := b.ctx.Spreads()
	if st == nil {
		return nil
	}
	open := st.OpenSpreads()
	ids := make([]int, len(open))
	for i, sp := range open {
		ids[i] = sp.ID
	}
	return ids
}

func (b *barContextBridge) SpreadPnL(spreadID int) float64 {
	st := b.ctx.Spreads()
	if st == nil {
		return 0
	}
	sp := st.Get(spreadID)
	if sp == nil {
		return 0
	}
	return sp.TotalUnrealizedPnL(func(oc backtest.OptionContract) float64 {
		return b.spreadPricing.ValuationMode.ExitPrice(backtest.Buy, oc)
	})
}

func (b *barContextBridge) SpreadLegContract(spreadID, legIndex int) interface{} {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return nil
	}
	contract := leg.Contract
	return &contract
}

func (b *barContextBridge) SpreadLegEntryPrice(spreadID, legIndex int) float64 {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return 0
	}
	return leg.EntryPrice
}

func (b *barContextBridge) SpreadLegQty(spreadID, legIndex int) float64 {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return 0
	}
	return leg.Qty
}

func (b *barContextBridge) SpreadLegSide(spreadID, legIndex int) string {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return ""
	}
	if leg.Side == backtest.Sell {
		return "sell"
	}
	return "buy"
}

func (b *barContextBridge) SpreadLegIsOpen(spreadID, legIndex int) bool {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return false
	}
	return !leg.Closed
}

func (b *barContextBridge) spreadLeg(spreadID, legIndex int) *backtest.SpreadLeg {
	st := b.ctx.Spreads()
	if st == nil {
		return nil
	}
	sp := st.Get(spreadID)
	if sp == nil || legIndex < 0 || legIndex >= len(sp.Legs) {
		return nil
	}
	return &sp.Legs[legIndex]
}

func (b *barContextBridge) GroupOpen(tag string, initAmount, decayFactor float64) int {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return 0
	}
	return gt.Open(tag, initAmount, decayFactor, b.ctx.Time())
}

func (b *barContextBridge) GroupClose(groupID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.Close(groupID)
}

func (b *barContextBridge) GroupGet(groupID int) runtime.GroupInfo {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return runtime.GroupInfo{}
	}
	g := gt.Get(groupID)
	if g == nil {
		return runtime.GroupInfo{}
	}
	return runtime.GroupInfo{
		ID:          g.ID,
		Tag:         g.Tag,
		Amount:      g.CurrentAmount(),
		RollCount:   g.RollCount,
		IsClosed:    g.Closed,
		SpreadIDs:   g.SpreadIDs,
		SpreadCount: len(g.SpreadIDs),
	}
}

func (b *barContextBridge) GroupAddSpread(groupID, spreadID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.AddSpread(groupID, spreadID)
}

func (b *barContextBridge) GroupIncrementRoll(groupID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.IncrementRoll(groupID)
}

func (b *barContextBridge) OpenGroups() []int {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return nil
	}
	groups := gt.OpenGroups()
	ids := make([]int, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

func (b *barContextBridge) ScheduleCloseSpread(triggerBarOffset int, spreadID int) {
	b.ctx.ScheduleCloseSpreadAfterBars(triggerBarOffset, spreadID, "")
}

func (b *barContextBridge) ScheduleCloseSpreadWithReason(triggerBarOffset int, spreadID int, reason string) {
	b.ctx.ScheduleCloseSpreadAfterBars(triggerBarOffset, spreadID, reason)
}

func (b *barContextBridge) ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int) {
	b.ctx.ScheduleCloseLegAfterBars(triggerBarOffset, spreadID, legIndex)
}

func (b *barContextBridge) ScheduleCloseGroup(triggerBarOffset int, groupID int) {
	info := b.GroupGet(groupID)
	if info.ID == 0 {
		return
	}
	for _, spreadID := range info.SpreadIDs {
		if spreadID > 0 {
			b.ScheduleCloseSpread(triggerBarOffset, spreadID)
		}
	}
}

func (b *barContextBridge) barOffsetDuration(triggerBarOffset int) time.Duration {
	if triggerBarOffset <= 0 {
		return 0
	}
	return time.Duration(triggerBarOffset) * b.barSpacing()
}

// barSpacing estimates the wall-clock duration of one primary bar. It prefers
// the gap to the next bar, then the gap from the previous bar, and only
// falls back to parsing the primary security's interval label (or, failing
// that, one hour) when both neighboring bars are unavailable — e.g. on a
// single-bar replay. Using a hardcoded hour whenever NextBarTime() is zero
// previously caused schedule.close_* to fire at the wrong wall-clock time on
// non-hourly intervals, most visibly on the last bar of a daily strategy.
func (b *barContextBridge) barSpacing() time.Duration {
	now := b.ctx.Time()
	if next := b.ctx.NextBarTime(); !next.IsZero() && next.After(now) {
		return next.Sub(now)
	}
	if prev := b.ctx.PrevBarTime(); !prev.IsZero() && now.After(prev) {
		return now.Sub(prev)
	}
	if interval := strings.TrimSpace(b.ctx.PrimaryRef().Interval); interval != "" {
		if w, err := feeds.ParseWindow(interval); err == nil && w.Duration > 0 {
			return w.Duration
		}
	}
	return time.Hour
}

func convertLegs(legs []runtime.SpreadLegInput, entryMode backtest.OptionPriceMode) []backtest.SpreadLeg {
	out := make([]backtest.SpreadLeg, 0, len(legs))
	for _, leg := range legs {
		oc, ok := leg.Contract.(*backtest.OptionContract)
		if !ok {
			continue
		}
		side := backtest.Buy
		if leg.Side == "sell" {
			side = backtest.Sell
		}
		entryPrice := entryMode.EntryPrice(side, *oc)
		out = append(out, backtest.SpreadLeg{
			Contract:   *oc,
			Side:       side,
			Qty:        leg.Qty,
			EntryPrice: entryPrice,
		})
	}
	return out
}
