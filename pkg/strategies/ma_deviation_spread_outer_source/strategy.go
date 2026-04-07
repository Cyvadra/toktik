package madeviationspread

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/signals"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
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

	interval12h           = "12h"
	entrySignalTimeLayout = "2006/1/2 15:04"
	txtTimeLayout         = "Jan 2, 2006, 15:04"
	entrySignalPath       = "pkg/strategies/ma_deviation_spread_outer_source/SF18_RE_Bearish_Divergence_Only_BINANCE_BTCUSD_2026-03-30.csv"
	callTrancheCount      = 2
	positionGroupTag      = "ma-deviation-outer-source"
	positionGroupDecay    = 1.0

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
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			entryTimes, err := loadEntrySignalTimes(entrySignalPath)
			if err != nil {
				return nil, err
			}
			return &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				positionSize:     fixedPositionSize,
				direction:        cfg.Direction,
				highRSIDTE:       catalog.IntOrDefault(cfg.TargetExpiryDays, defaultHighRSIDTE),
				lowRSIDTE:        catalog.IntOrDefault(cfg.MinExpiryDays, defaultLowRSIDTE),
				entryTimes:       entryTimes,
				lowestSinceEntry: math.NaN(),
			}, nil
		},
	})
}

type strategy struct {
	optutil.PricingMixin
	optutil.GroupMixin

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

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "entry_signal", Label: "Entry Signal", Decimals: 0},
		{Source: "rsi_12h", Label: "RSI 200 12h", Decimals: 2},
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

	aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, "rsi_12h")
	if err != nil {
		return err
	}
	return ctx.Primary().SetColumn("rsi_12h", aligned)
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)
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

	targetDTE := s.targetExpiryDays(ctx.Ind("rsi_12h"))
	withinDTEs := expiryDTEsWithin(chain, ctx.Time(), s.lowRSIDTE, s.highRSIDTE)
	fmt.Printf("[%s] entry option selection: target DTE=%d, within DTEs=%s\n", ctx.Time().Format(time.RFC3339), targetDTE, formatDTEs(withinDTEs))
	shortCall := s.selectShortCall(chain)
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

	longPut := s.selectLongPut(chain, shortCall)
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

	groupID := s.OpenGroup(ctx, positionGroupTag, s.positionSize, positionGroupDecay)

	putSpreadID := s.OpenSpreadInGroup(ctx, []backtest.SpreadLeg{{
		Contract:   *longPut,
		Side:       backtest.Buy,
		Qty:        putQty,
		EntryPrice: putEntryPrice,
	}}, openNoteInitialPut, groupID)
	if putSpreadID <= 0 {
		s.CloseGroup(ctx, groupID)
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
		spreadID := s.OpenSpreadInGroup(ctx, []backtest.SpreadLeg{{
			Contract:   *shortCall,
			Side:       backtest.Sell,
			Qty:        qty,
			EntryPrice: shortEntryPrice,
		}}, tags[i], groupID)
		if spreadID <= 0 {
			s.rollbackOpenedStructure(ctx, openedCallIDs, putSpreadID, shortEntryPrice, putEntryPrice, groupID)
			return
		}
		openedCallIDs[i] = spreadID
	}

	for i := range s.callSpreadIDs {
		s.callSpreadIDs[i] = openedCallIDs[i]
	}
	s.putSpreadID = putSpreadID
	s.PositionGroupID = groupID

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
		contract := optutil.ResolveContract(leg.Contract, contractMap)
		markPrice := s.LegValuationPrice(leg, contractMap)
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
			closePrice := s.LegExitPrice(leg, contractMap)
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
			contract := optutil.ResolveContract(leg.Contract, contractMap)
			markPrice := s.LegValuationPrice(leg, contractMap)
			pnlPct := sp.LegUnrealizedPnLPct(0, markPrice)
			absDelta := math.Abs(contract.Delta)

			if contract.DaysToExpiry(now) <= 1 {
				closePrice := s.LegExitPrice(leg, contractMap)
				if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(s.putSpreadID, 0, closePrice, closeNoteExpiryPut) {
					s.putSpreadID = 0
				}
			} else if absDelta >= defaultPutRollDelta || (!math.IsNaN(pnlPct) && pnlPct >= defaultPutRollProfit) {
				closePrice := s.LegExitPrice(leg, contractMap)
				if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(s.putSpreadID, 0, closePrice, s.putRollReason(absDelta, pnlPct)) {
					s.putSpreadID = 0
					s.reopenPutLeg(ctx, chain)
				}
			}
		}
	}

	stillActive := active || s.hasOpenCallSpread(ctx) || s.hasOpenPutSpread(ctx)
	if !stillActive {
		s.CloseGroup(ctx, s.PositionGroupID)
	}
	return stillActive
}

func (s *strategy) reopenPutLeg(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	budget := s.remainingCallPremiumBudget(ctx)
	if budget <= 0 {
		return
	}

	shortCall := s.activeShortCallContract(ctx)
	longPut := s.selectLongPut(chain, shortCall)
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

	putSpreadID := s.OpenSpreadInGroup(ctx, []backtest.SpreadLeg{{
		Contract:   *longPut,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, openNoteRolledPut, s.PositionGroupID)
	if putSpreadID <= 0 {
		return
	}

	s.putSpreadID = putSpreadID
	if s.PositionGroupID > 0 && ctx.SpreadGroups() != nil {
		ctx.SpreadGroups().IncrementRoll(s.PositionGroupID)
	}
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
		closePrice := s.LegExitPrice(sp.Legs[0], contractMap)
		if !math.IsNaN(closePrice) && closePrice > 0 && ctx.CloseSpreadLegWithReason(spreadID, 0, closePrice, fmt.Sprintf("ATR反弹%.1fx平空call", trailATRMultiplier)) {
			closedAny = true
			s.callSpreadIDs[i] = 0
		}
	}

	if s.putSpreadID > 0 {
		sp := ctx.Spreads().Get(s.putSpreadID)
		if sp != nil && len(sp.Legs) > 0 && !sp.Legs[0].Closed {
			closePrice := s.LegExitPrice(sp.Legs[0], contractMap)
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

func (s *strategy) rollbackOpenedStructure(ctx *backtest.BarContext, callSpreadIDs [callTrancheCount]int, putSpreadID int, shortEntryPrice, putEntryPrice float64, groupID int) {
	for _, spreadID := range callSpreadIDs {
		if spreadID <= 0 {
			continue
		}
		ctx.CloseSpreadLegWithReason(spreadID, 0, shortEntryPrice, closeNoteOpenRollback)
	}
	if putSpreadID > 0 {
		ctx.CloseSpreadLegWithReason(putSpreadID, 0, putEntryPrice, closeNoteOpenRollback)
	}
	s.CloseGroup(ctx, groupID)
}

func (s *strategy) selectShortCall(chain *backtest.OptionsChain) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	filtered := chain.Calls().ExpiryRange(s.lowRSIDTE, s.highRSIDTE)
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

func (s *strategy) selectLongPut(chain *backtest.OptionsChain, refCall *backtest.OptionContract) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	filtered := chain.Puts().ExpiryRange(s.lowRSIDTE, s.highRSIDTE)
	if refCall != nil {
		matched := filtered.SameExpiry(refCall)
		if matched.Len() > 0 {
			filtered = matched
		}
	}
	if filtered.Len() == 0 {
		return nil
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
	s.ApplyPricingDefaults()
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
	return signals.BuildBinarySeries(timestamps, entryTimes)
}

func loadEntrySignalTimes(path string) (map[int64]struct{}, error) {
	return signals.LoadTimes(signals.Config{
		Paths:            []string{path},
		TimestampColumns: []string{"日期和时间"},
		TypeColumns:      []string{"类型"},
		SignalColumns:    []string{"信号"},
		TimeLayouts:      []string{entrySignalTimeLayout, txtTimeLayout},
		Location:         time.FixedZone("UTC+8", 8*3600),
		TextLocation:     time.UTC,
		EntryMatchers:    []string{"进场", "开仓", "entry", "open", "做空", "空头", "bearish", "divergence"},
	})
}

func resolveSignalFilePath(path string) (string, error) {
	resolvedPath, found, err := signals.ResolvePath(path)
	if err != nil {
		return "", err
	}
	if found {
		return resolvedPath, nil
	}
	return "", fmt.Errorf("entry signal file not found: %s", path)
}

func csvColumnIndex(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, name := range header {
		normalized := strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))
		columns[normalized] = index
	}
	return columns
}

func isEntrySignalRecord(record []string, typeIndex int, hasType bool, signalIndex int, hasSignal bool) bool {
	// First check the type column if available - exit types take precedence
	if hasType && typeIndex < len(record) {
		entryType := strings.TrimSpace(strings.ToLower(record[typeIndex]))
		// Explicitly check for exit types first - these should never be treated as entries
		if strings.Contains(entryType, "出场") || strings.Contains(entryType, "平仓") || strings.Contains(entryType, "exit") || strings.Contains(entryType, "close") {
			return false
		}
		// Check for entry types
		if strings.Contains(entryType, "进场") || strings.Contains(entryType, "开仓") || strings.Contains(entryType, "entry") || strings.Contains(entryType, "open") {
			return true
		}
	}

	// Fall back to signal column only if type column didn't determine the result
	if hasSignal && signalIndex < len(record) {
		signal := strings.TrimSpace(strings.ToLower(record[signalIndex]))
		if strings.Contains(signal, "做空") || strings.Contains(signal, "空头") || strings.Contains(signal, "bearish") || strings.Contains(signal, "divergence") {
			return true
		}
	}

	return false
}

func loadTextEntrySignalTimes(path string) (map[int64]struct{}, error) {
	return signals.LoadTimes(signals.Config{
		Paths:       []string{path},
		TimeLayouts: []string{txtTimeLayout},
		Location:    time.UTC,
	})
}

func expiryDTEsWithin(chain *backtest.OptionsChain, now time.Time, minDTE, maxDTE int) []float64 {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	type expiryInfo struct {
		expiration time.Time
		dte        float64
	}

	filtered := chain.ExpiryRange(minDTE, maxDTE)
	if filtered.Len() == 0 {
		return nil
	}

	unique := make(map[int64]expiryInfo)
	for _, contract := range filtered.Contracts() {
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
		if expiries[i].dte != expiries[j].dte {
			return expiries[i].dte < expiries[j].dte
		}
		return expiries[i].expiration.Before(expiries[j].expiration)
	})

	out := make([]float64, 0, len(expiries))
	for i := 0; i < len(expiries); i++ {
		out = append(out, expiries[i].dte)
	}
	return out
}

func formatDTEs(dtes []float64) string {
	if len(dtes) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(dtes))
	for _, dte := range dtes {
		parts = append(parts, fmt.Sprintf("%.2f", dte))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
