package madeviationspread

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultStrategyName = "ma-deviation-spread-outer-source"
	defaultAlias1       = "ma_deviation_spread_outer_source"
	defaultAlias2       = "ma_spread_outer_source"

	fixedPositionSize      = 10.0
	defaultHighRSIDTE      = 40
	defaultLowRSIDTE       = 25
	defaultShortCallDelta  = 0.30
	defaultLongPutDelta    = -0.25
	defaultPutBudgetRatio  = 0.70
	defaultPutRollDelta    = 0.50
	defaultPutRollProfit   = 0.50
	defaultCallTakeProfit1 = 0.70
	defaultCallTakeProfit2 = 0.88
	atrPeriod              = 20
	rsiPeriod              = 200
	trailATRMultiplier     = 3.0

	interval12h       = "12h"
	entrySignalLayout = "Jan 02, 2006, 15:04"
	entrySignalPath   = "/home/jason89757/workspace/toktik/signal-list/tmp.txt"
	callTrancheCount  = 2

	openNoteInitialPut    = "首仓开仓 | 多put保护"
	openNoteInitialCall1  = "首仓开仓 | 空call分批止盈70%"
	openNoteInitialCall2  = "首仓开仓 | 空call分批止盈88%"
	openNoteRolledPut     = "换仓开仓 | 多put保护"
	closeNoteExpiryCall   = "到期平仓 | 空call"
	closeNoteTakeProfit70 = "止盈平仓 | 空call 70%"
	closeNoteTakeProfit88 = "止盈平仓 | 空call 88%"
	closeNoteExpiryPut    = "到期平仓 | 多put"
	closeNoteOpenRollback = "开仓回滚"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    defaultStrategyName,
		Aliases: []string{defaultAlias1, defaultAlias2},
		Groups:  []string{"options", "spread", "timed"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			entryTimes, err := loadEntrySignalTimes(entrySignalPath)
			if err != nil {
				return nil, err
			}
			return &strategy{
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				positionSize:       fixedPositionSize,
				direction:          cfg.Direction,
				highRSIDTE:         catalog.IntOrDefault(cfg.TargetExpiryDays, defaultHighRSIDTE),
				lowRSIDTE:          catalog.IntOrDefault(cfg.MinExpiryDays, defaultLowRSIDTE),
				entryTimes:         entryTimes,
				lowestSinceEntry:   math.NaN(),
			}, nil
		},
	})
}

type strategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	positionSize float64
	direction    catalog.TradeDirection
	highRSIDTE   int
	lowRSIDTE    int

	entryTimes map[int64]struct{}

	lowestSinceEntry float64
	callSpreadIDs    [callTrancheCount]int
	putSpreadID      int

	ref12h backtest.SecurityRef
}

func (s *strategy) Name() string { return "MADeviationSpreadOuterSource" }

func (s *strategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "entry_signal", Label: "Entry Signal", Decimals: 0},
		{Source: "rsi_12h_prev", Label: "RSI 200 12h", Decimals: 2},
		{Source: "atr20", Label: "ATR 20", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	primary := ctx.PrimaryRef()
	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)

	ctx.SetParam("position_size", s.positionSize)
	ctx.SetParam("entry_signal_path", entrySignalPath)
	ctx.SetParam("entry_signal_count", float64(len(s.entryTimes)))
	ctx.SetParam("high_rsi_dte", float64(s.highRSIDTE))
	ctx.SetParam("low_rsi_dte", float64(s.lowRSIDTE))
	ctx.SetParam("trail_atr_multiplier", trailATRMultiplier)
	ctx.SetWarmup(120 * 24 * time.Hour)

	ctx.Register("atr20", backtest.ATR(atrPeriod))
	ctx.RegisterOn(s.ref12h, "rsi_12h", backtest.RSI("close", rsiPeriod))

	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}

	entrySignal := buildEntrySignalSeries(primary.Timestamps(), s.entryTimes)
	if err := primary.SetColumn("entry_signal", entrySignal); err != nil {
		return err
	}

	return s.setShiftedAlignedNumericColumn(ctx, s.ref12h, "rsi_12h", "rsi_12h_prev")
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := s.buildContractMap(chain)
	hasOpenOptionPosition := s.manageOpenPositions(ctx, chain, contractMap)

	if hasOpenOptionPosition {
		if math.IsNaN(s.lowestSinceEntry) || ctx.Low() < s.lowestSinceEntry {
			s.lowestSinceEntry = ctx.Low()
		}
		// 仅依赖止盈和到期前平仓，去掉ATR止损
		// if s.handleATRExit(ctx, contractMap) {
		// 	return
		// }
	} else {
		s.lowestSinceEntry = math.NaN()
	}

	if ctx.Ind("entry_signal") != 1 || hasOpenOptionPosition || !s.allowsEntry() {
		return
	}

	s.tryOpenStructure(ctx, chain)
}

func (s *strategy) tryOpenStructure(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	targetDTE := s.targetExpiryDays(ctx.Ind("rsi_12h_prev"))
	nearestDTEs := nearestExpiryDTEs(chain, ctx.Time(), targetDTE, 2)
	fmt.Printf("[%s] entry option selection: target DTE=%d, nearest DTEs=%s\n", ctx.Time().Format(time.RFC3339), targetDTE, formatNearestDTEs(nearestDTEs))
	shortCall := s.selectShortCall(chain, targetDTE)
	if shortCall == nil {
		return
	}

	shortEntryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, *shortCall)
	if math.IsNaN(shortEntryPrice) || shortEntryPrice <= 0 {
		return
	}

	if s.positionSize <= 0 {
		return
	}

	shortQtyTotal := s.positionSize / shortEntryPrice
	if shortQtyTotal <= 0 {
		return
	}

	longPut := s.selectLongPut(chain, shortCall, targetDTE)
	if longPut == nil {
		return
	}

	putEntryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, *longPut)
	if math.IsNaN(putEntryPrice) || putEntryPrice <= 0 {
		return
	}

	putBudget := shortQtyTotal * shortEntryPrice * defaultPutBudgetRatio
	putQty := putBudget / putEntryPrice
	if putQty <= 0 {
		return
	}

	putSpreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   *longPut,
		Side:       backtest.Buy,
		Qty:        putQty,
		EntryPrice: putEntryPrice,
	}}, openNoteInitialPut)
	if putSpreadID <= 0 {
		return
	}

	firstQty := shortQtyTotal / callTrancheCount
	secondQty := shortQtyTotal - firstQty
	callQty := [callTrancheCount]float64{firstQty, secondQty}
	tags := [callTrancheCount]string{openNoteInitialCall1, openNoteInitialCall2}
	openedCallIDs := [callTrancheCount]int{}

	for i, qty := range callQty {
		if qty <= 0 {
			continue
		}
		spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
			Contract:   *shortCall,
			Side:       backtest.Sell,
			Qty:        qty,
			EntryPrice: shortEntryPrice,
		}}, tags[i])
		if spreadID <= 0 {
			s.rollbackOpenedStructure(ctx, openedCallIDs, putSpreadID, shortEntryPrice, putEntryPrice)
			return
		}
		openedCallIDs[i] = spreadID
	}

	for i := range s.callSpreadIDs {
		s.callSpreadIDs[i] = openedCallIDs[i]
	}
	s.putSpreadID = putSpreadID

	s.lowestSinceEntry = ctx.Low()
}

func (s *strategy) manageOpenPositions(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract) bool {
	now := ctx.Time()
	active := false

	for i, spreadID := range s.callSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.callSpreadIDs[i] = 0
			continue
		}

		active = true
		leg := sp.Legs[0]
		contract := s.currentContract(leg.Contract, contractMap)
		markPrice := s.valuationPrice(leg, contractMap)
		pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)

		shouldClose := false
		closeReason := ""
		if contract.DaysToExpiry(now) <= 1 {
			shouldClose = true
			closeReason = closeNoteExpiryCall
		} else if i == 0 && !math.IsNaN(pnlPct) && pnlPct >= defaultCallTakeProfit1 {
			shouldClose = true
			closeReason = closeNoteTakeProfit70
		} else if i == 1 && !math.IsNaN(pnlPct) && pnlPct >= defaultCallTakeProfit2 {
			shouldClose = true
			closeReason = closeNoteTakeProfit88
		}

		if shouldClose {
			closePrice := s.exitPrice(leg, contractMap)
			if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(spreadID, 0, closePrice, closeReason) {
				s.callSpreadIDs[i] = 0
			}
		}
	}

	if s.putSpreadID > 0 {
		sp := ctx.Spreads().Get(s.putSpreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.putSpreadID = 0
		} else {
			active = true
			leg := sp.Legs[0]
			contract := s.currentContract(leg.Contract, contractMap)
			markPrice := s.valuationPrice(leg, contractMap)
			pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
			absDelta := math.Abs(contract.Delta)

			if contract.DaysToExpiry(now) <= 1 {
				closePrice := s.exitPrice(leg, contractMap)
				if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(s.putSpreadID, 0, closePrice, closeNoteExpiryPut) {
					s.putSpreadID = 0
				}
			} else if absDelta >= defaultPutRollDelta || (!math.IsNaN(pnlPct) && pnlPct >= defaultPutRollProfit) {
				closePrice := s.exitPrice(leg, contractMap)
				if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(s.putSpreadID, 0, closePrice, s.putRollReason(absDelta, pnlPct)) {
					s.putSpreadID = 0
					s.reopenPutLeg(ctx, chain)
				}
			}
		}
	}

	return active || s.hasOpenCallSpread(ctx) || s.hasOpenPutSpread(ctx)
}

func (s *strategy) reopenPutLeg(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	budget := s.remainingCallPremiumBudget(ctx)
	if budget <= 0 {
		return
	}

	targetDTE := s.targetExpiryDays(ctx.Ind("rsi_12h_prev"))
	shortCall := s.activeShortCallContract(ctx)
	longPut := s.selectLongPut(chain, shortCall, targetDTE)
	if longPut == nil {
		return
	}

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, *longPut)
	if math.IsNaN(entryPrice) || entryPrice <= 0 {
		return
	}

	qty := budget / entryPrice
	if qty <= 0 {
		return
	}

	s.putSpreadID = ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   *longPut,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, openNoteRolledPut)
}

func (s *strategy) handleATRExit(ctx *backtest.BarContext, contractMap map[string]backtest.OptionContract) bool {
	atr := ctx.Ind("atr20")
	if math.IsNaN(atr) || atr <= 0 || math.IsNaN(s.lowestSinceEntry) {
		return false
	}
	if ctx.High()-s.lowestSinceEntry <= trailATRMultiplier*atr {
		return false
	}

	closedAny := false
	for i, spreadID := range s.callSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.callSpreadIDs[i] = 0
			continue
		}
		closePrice := s.exitPrice(sp.Legs[0], contractMap)
		if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(spreadID, 0, closePrice, fmt.Sprintf("ATR反弹%.1fx平空call", trailATRMultiplier)) {
			closedAny = true
			s.callSpreadIDs[i] = 0
		}
	}

	if s.putSpreadID > 0 {
		sp := ctx.Spreads().Get(s.putSpreadID)
		if sp != nil && len(sp.Legs) > 0 && !sp.Legs[0].Closed {
			closePrice := s.exitPrice(sp.Legs[0], contractMap)
			if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(s.putSpreadID, 0, closePrice, fmt.Sprintf("ATR反弹%.1fx平多put", trailATRMultiplier)) {
				closedAny = true
			}
		}
		s.putSpreadID = 0
	}

	if closedAny {
		s.lowestSinceEntry = math.NaN()
	}
	return closedAny
}

func (s *strategy) rollbackOpenedStructure(ctx *backtest.BarContext, callSpreadIDs [callTrancheCount]int, putSpreadID int, shortEntryPrice, putEntryPrice float64) {
	for _, spreadID := range callSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		ctx.CloseSpreadLegWithReason(spreadID, 0, shortEntryPrice, closeNoteOpenRollback)
	}
	if putSpreadID > 0 {
		ctx.CloseSpreadLegWithReason(putSpreadID, 0, putEntryPrice, closeNoteOpenRollback)
	}
}

func (s *strategy) selectShortCall(chain *backtest.OptionsChain, targetDTE int) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	filtered := chain.Calls().ExpiryNearest(targetDTE)
	if filtered.Len() == 0 {
		return nil
	}
	sorted := filtered.SortByDelta(defaultShortCallDelta)
	for i := range sorted {
		contract := sorted[i]
		entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, contract)
		if !math.IsNaN(entryPrice) && entryPrice > 0 {
			return &contract
		}
	}
	return nil
}

func (s *strategy) selectLongPut(chain *backtest.OptionsChain, refCall *backtest.OptionContract, targetDTE int) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	filtered := chain.Puts()
	if refCall != nil {
		matched := filtered.SameExpiry(refCall)
		if matched.Len() > 0 {
			filtered = matched
		}
	}
	if filtered.Len() == 0 {
		filtered = chain.Puts().ExpiryNearest(targetDTE)
		if filtered.Len() == 0 {
			return nil
		}
	}
	sorted := filtered.SortByDelta(defaultLongPutDelta)
	for i := range sorted {
		contract := sorted[i]
		entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, contract)
		if !math.IsNaN(entryPrice) && entryPrice > 0 {
			return &contract
		}
	}
	return nil
}

func (s *strategy) targetExpiryDays(rsi12h float64) int {
	if !math.IsNaN(rsi12h) && rsi12h > 50 {
		return s.highRSIDTE
	}
	return s.lowRSIDTE
}

func (s *strategy) applyDefaults() {
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
	if s.highRSIDTE <= 0 {
		s.highRSIDTE = defaultHighRSIDTE
	}
	if s.lowRSIDTE <= 0 {
		s.lowRSIDTE = defaultLowRSIDTE
	}
	s.positionSize = fixedPositionSize
	if s.entryTimes == nil {
		s.entryTimes = map[int64]struct{}{}
	}
}

func (s *strategy) setShiftedAlignedNumericColumn(ctx *backtest.PreloadContext, ref backtest.SecurityRef, source, target string) error {
	sec := ctx.Security(ref)
	if sec == nil || sec.Len() == 0 {
		return nil
	}
	col, err := sec.RequireColumn(source)
	if err != nil {
		return err
	}
	shifted := shiftSeries(col, math.NaN())
	if err := sec.SetColumn(source+"_prev", shifted); err != nil {
		return err
	}
	aligned, err := ctx.ColumnAlignedToPrimary(ref, source+"_prev")
	if err != nil {
		return err
	}
	return ctx.Primary().SetColumn(target, aligned)
}

func (s *strategy) buildContractMap(chain *backtest.OptionsChain) map[string]backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	contractMap := make(map[string]backtest.OptionContract, chain.Len())
	for _, contract := range chain.Contracts() {
		contractMap[contract.Symbol] = contract
	}
	return contractMap
}

func (s *strategy) currentContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if contractMap == nil {
		return contract
	}
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func (s *strategy) exitPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ExitPriceMode.ExitPrice(leg.Side, contract)
}

func (s *strategy) valuationPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ValuationPriceMode.ExitPrice(leg.Side, contract)
}

func (s *strategy) hasOpenCallSpread(ctx *backtest.BarContext) bool {
	for i, spreadID := range s.callSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() {
			s.callSpreadIDs[i] = 0
			continue
		}
		return true
	}
	return false
}

func (s *strategy) hasOpenPutSpread(ctx *backtest.BarContext) bool {
	if s.putSpreadID <= 0 {
		return false
	}
	sp := ctx.Spreads().Get(s.putSpreadID)
	if sp == nil || sp.IsFullyClosed() {
		s.putSpreadID = 0
		return false
	}
	return true
}

func (s *strategy) activeShortCallContract(ctx *backtest.BarContext) *backtest.OptionContract {
	for i := range s.callSpreadIDs {
		spreadID := s.callSpreadIDs[i]
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			continue
		}
		contract := sp.Legs[0].Contract
		return &contract
	}
	return nil
}

func (s *strategy) remainingCallPremiumBudget(ctx *backtest.BarContext) float64 {
	budget := 0.0
	for i := range s.callSpreadIDs {
		spreadID := s.callSpreadIDs[i]
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			continue
		}
		leg := sp.Legs[0]
		budget += leg.Qty * leg.EntryPrice * defaultPutBudgetRatio
	}
	return budget
}

func (s *strategy) putRollReason(absDelta, pnlPct float64) string {
	if absDelta >= defaultPutRollDelta && !math.IsNaN(pnlPct) && pnlPct >= defaultPutRollProfit {
		return fmt.Sprintf("long put换仓 delta=%.2f pnl=%.0f%%", absDelta, pnlPct*100)
	}
	if absDelta >= defaultPutRollDelta {
		return fmt.Sprintf("long put换仓 delta=%.2f", absDelta)
	}
	return fmt.Sprintf("long put换仓 pnl=%.0f%%", pnlPct*100)
}

func (s *strategy) allowsEntry() bool {
	return s.direction == "" || s.direction == catalog.DirectionBoth || s.direction == catalog.DirectionShortOnly
}

func buildEntrySignalSeries(timestamps []time.Time, entryTimes map[int64]struct{}) []float64 {
	out := make([]float64, len(timestamps))
	for i, ts := range timestamps {
		if _, ok := entryTimes[ts.UTC().Unix()]; ok {
			out[i] = 1
		}
	}
	return out
}

func loadEntrySignalTimes(path string) (map[int64]struct{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read entry signal file %s: %w", path, err)
	}

	entryTimes := make(map[int64]struct{})
	for lineNo, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ts, err := time.ParseInLocation(entrySignalLayout, line, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("parse entry signal line %d (%q): %w", lineNo+1, line, err)
		}
		entryTimes[ts.Unix()] = struct{}{}
	}

	return entryTimes, nil
}

func shiftSeries(src []float64, headValue float64) []float64 {
	out := make([]float64, len(src))
	if len(src) == 0 {
		return out
	}
	out[0] = headValue
	copy(out[1:], src[:len(src)-1])
	return out
}

func nearestExpiryDTEs(chain *backtest.OptionsChain, now time.Time, targetDTE, limit int) []float64 {
	if chain == nil || chain.Len() == 0 || limit <= 0 {
		return nil
	}

	type expiryInfo struct {
		expiration time.Time
		dte        float64
	}

	unique := make(map[int64]expiryInfo)
	for _, contract := range chain.Contracts() {
		key := contract.Expiration.UTC().Unix()
		if _, exists := unique[key]; exists {
			continue
		}
		unique[key] = expiryInfo{
			expiration: contract.Expiration,
			dte:        contract.DaysToExpiry(now),
		}
	}

	expiries := make([]expiryInfo, 0, len(unique))
	for _, info := range unique {
		expiries = append(expiries, info)
	}

	sort.Slice(expiries, func(i, j int) bool {
		diffI := math.Abs(expiries[i].dte - float64(targetDTE))
		diffJ := math.Abs(expiries[j].dte - float64(targetDTE))
		if diffI != diffJ {
			return diffI < diffJ
		}
		return expiries[i].expiration.Before(expiries[j].expiration)
	})

	if limit > len(expiries) {
		limit = len(expiries)
	}

	out := make([]float64, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, expiries[i].dte)
	}
	return out
}

func formatNearestDTEs(dtes []float64) string {
	if len(dtes) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(dtes))
	for _, dte := range dtes {
		parts = append(parts, fmt.Sprintf("%.2f", dte))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
