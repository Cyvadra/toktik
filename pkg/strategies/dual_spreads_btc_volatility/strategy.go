package dualspreadsvol

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

const (
	strategyName  = "dual-spreads-btc-volatility"
	strategyAlias = "dual_spreads_btc_volatility"

	amountBase                    = 2.0
	highIVMinDTE                  = 20
	highIVTargetDTE               = 35
	highIVMaxDTE                  = 35
	lowIVMinDTE                   = 30
	lowIVTargetDTE                = 45
	lowIVMaxDTE                   = 45
	shortDTEIVPercentileCutoff    = 55.0
	hvPercentileLookback          = 150
	ivPercentileLookback          = 150
	minDVOLBarsForThreshold       = 200
	defaultVolThresholdPercentile = 66.0
	defaultMetricPercentile       = 60.0
	tvAnnualizationDays           = 365.0
	initDVOLFallbackMax           = 96.0
	rollProfitPct                 = 0.50
	rollDeltaIncrease             = 0.20
	decayFactor                   = 0.90
	minLongDelta                  = 0.20
	maxLongDelta                  = 0.80
	minShortDelta                 = 0.05
	maxShortDelta                 = 0.70
	interval12h                   = "12h"

	signalCSVPath       = "pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv"
	entryAddCountColumn = "entry_add_count"
	hvReturnColumn      = "log_ret_12h"
	hvValueColumn       = "hv_100_12h"
	hvPercentileColumn  = "hv_pr_100_12h"
	hvThresholdColumn   = "hv_q66_100_12h"
	dvolValueColumn     = "dvol_12h"
	ivPercentileColumn  = "iv_pr_200_12h"
	ivThresholdColumn   = "iv_q66_200_12h"
	dvolBarIndexColumn  = "dvol_12h_bar_index"
)

type signalType int

const (
	signalNone signalType = iota
	signalInit
	signalAdd
)

type signalEvent struct {
	time     time.Time
	sigType  signalType
	addCount int
}

var trailingDigitsPattern = regexp.MustCompile(`(\d+)\s*$`)

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{strategyAlias},
		Groups:  []string{"options", "spread", "timed"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			signals, err := loadSignals(signalCSVPath)
			if err != nil {
				return nil, fmt.Errorf("load signals: %w", err)
			}
			return &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				signals: signals,
			}, nil
		},
	})
}

type activeGroup struct {
	groupID  int
	spreadID int
}

type strategy struct {
	optutil.PricingMixin
	logf func(format string, args ...any)

	signals      []signalEvent
	ref12h       backtest.SecurityRef
	dvolRef      backtest.FactorRef
	activeGroups []activeGroup
}

type spreadSelection struct {
	long       backtest.OptionContract
	short      backtest.OptionContract
	longPrice  float64
	shortPrice float64
	spreadCost float64
	qty        float64
	expiry     time.Time
	dte        float64
}

type dteWindow struct {
	min    int
	target int
	max    int
}

func (s *strategy) Name() string { return "DualSpreadsBTCVolatility" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "entry_signal", Label: "Entry Signal", Decimals: 0},
		{Source: hvValueColumn, Label: "HV 150 12H", Decimals: 2},
		{Source: dvolValueColumn, Label: "DVOL 12H", Decimals: 2},
		{Source: hvPercentileColumn, Label: "HV PR150 12H", Decimals: 1},
		{Source: ivPercentileColumn, Label: "DVOL PR150 12H", Decimals: 1},
		{Source: hvThresholdColumn, Label: "HV Q66 12H", Decimals: 2},
		{Source: ivThresholdColumn, Label: "IV Q66 12H", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	primary := ctx.PrimaryRef()
	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)

	ctx.RegisterOn(s.ref12h, hvReturnColumn, logReturnIndicator("close"))
	ctx.RegisterOn(s.ref12h, hvValueColumn, tradingViewHVIndicator(hvReturnColumn, 10))
	ctx.RegisterOn(s.ref12h, hvPercentileColumn, optutil.PercentileRank(hvValueColumn, hvPercentileLookback))

	s.dvolRef = ctx.AddFactor("dvol", interval12h)
	ctx.RegisterFactor(s.dvolRef, ivPercentileColumn, optutil.PercentileRank("close", ivPercentileLookback))

	ctx.SetWarmup(120 * 24 * time.Hour)
	ctx.SetParam("amount_base", amountBase)
	ctx.SetParam("signal_count", float64(len(s.signals)))
	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}
	htf := ctx.Security(s.ref12h)
	if htf == nil || htf.Len() == 0 {
		return fmt.Errorf("12h security unavailable")
	}
	dvol := ctx.Factor(s.dvolRef)
	if dvol == nil || dvol.Len() == 0 {
		return fmt.Errorf("12h dvol factor unavailable")
	}

	entrySignal12h := buildSignalColumn(htf.Timestamps(), s.signals)
	entrySignal := buildTriggeredAlignedSignalColumn(htf.AlignMap(), entrySignal12h, primary.Len())
	if err := primary.SetColumn("entry_signal", entrySignal); err != nil {
		return err
	}
	entryAddCount12h := buildSignalAddCountColumn(htf.Timestamps(), s.signals)
	if err := primary.SetColumn(entryAddCountColumn, buildTriggeredAlignedSignalColumn(htf.AlignMap(), entryAddCount12h, primary.Len())); err != nil {
		return err
	}

	if err := htf.Quantile(hvThresholdColumn, hvValueColumn, hvPercentileLookback, defaultVolThresholdPercentile/100); err != nil {
		return err
	}
	if err := dvol.Quantile(ivThresholdColumn, "close", ivPercentileLookback, defaultVolThresholdPercentile/100); err != nil {
		return err
	}

	ivPercentile12h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(ivPercentileColumn))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(ivPercentileColumn, ivPercentile12h); err != nil {
		return err
	}
	ivThreshold12h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(ivThresholdColumn))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(ivThresholdColumn, ivThreshold12h); err != nil {
		return err
	}

	for _, name := range []string{hvValueColumn, hvPercentileColumn, hvThresholdColumn} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
	}
	dvolValue, err := ctx.ColumnAlignedFactorToPrimary(s.dvolRef, "close")
	if err != nil {
		return err
	}
	if err := primary.SetColumn(dvolValueColumn, dvolValue); err != nil {
		return err
	}
	for _, name := range []string{ivPercentileColumn, ivThresholdColumn} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
	}

	return primary.SetColumn(dvolBarIndexColumn, buildAlignedIndexColumn(dvol.AlignMap(), primary.Len()))
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)

	s.manageGroups(ctx, chain, contractMap)

	sigType, ok := signalTypeFromIndicator(ctx.Ind("entry_signal"))
	if !ok {
		return
	}

	if sigType == signalInit && !s.initEntryAllowed(ctx) {
		return
	}

	s.openNewGroup(ctx, chain, amountBase)
}

func signalTypeFromIndicator(value float64) (signalType, bool) {
	if math.IsNaN(value) {
		return signalNone, false
	}

	switch value {
	case 1:
		return signalInit, true
	case 2:
		return signalAdd, true
	default:
		return signalNone, false
	}
}

func (s *strategy) initEntryAllowed(ctx *backtest.BarContext) bool {
	hv := s.currentHV(ctx)
	dvol := s.currentDVOL(ctx)
	hvThreshold := ctx.Ind(hvThresholdColumn)
	ivThreshold := ctx.Ind(ivThresholdColumn)
	return initEntryAllowedByMetrics(hv, dvol, hvThreshold, ivThreshold, s.currentDVOLBarCount(ctx))
}

func initEntryAllowedByMetrics(hv, dvol, hvThreshold, ivThreshold float64, dvolBarCount int) bool {
	if math.IsNaN(hv) || hv <= 0 || math.IsNaN(dvol) || dvol <= 0 {
		return false
	}
	if !math.IsNaN(hvThreshold) && hv <= hvThreshold {
		return true
	}
	if dvolBarCount < minDVOLBarsForThreshold {
		return dvol/hv <= initDVOLFallbackMax
	}
	if !math.IsNaN(ivThreshold) && dvol <= ivThreshold {
		return true
	}
	return false
}

func (s *strategy) logSelection(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
		return
	}
	fmt.Printf(format, args...)
}

func (s *strategy) selectSpread(now time.Time, chain *backtest.OptionsChain, amount, hvPercentile, ivPercentile float64, scope string) (*spreadSelection, bool) {
	if chain == nil || chain.Len() == 0 {
		s.logSelection("[%s] %s: skip selection, empty options chain\n", now.Format(time.RFC3339), scope)
		return nil, false
	}

	window := selectDTEWindow(ivPercentile)
	eligibleCalls := chain.Calls().ExpiryRange(window.min, window.max)
	if eligibleCalls.Len() == 0 {
		s.logSelection("[%s] %s: skip selection, no call contracts with dte in [%d,%d]\n", now.Format(time.RFC3339), scope, window.min, window.max)
		return nil, false
	}

	contracts := eligibleCalls.Contracts()
	expiries := candidateExpiries(contracts, now, window.target)
	if len(expiries) == 0 {
		s.logSelection("[%s] %s: skip selection, no candidate expiries after filtering\n", now.Format(time.RFC3339), scope)
		return nil, false
	}

	longTargetDelta := dynamicLongDelta(hvPercentile, ivPercentile)

	for idx, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		dte := expiry.Sub(now).Hours() / 24
		s.logSelection("[%s] %s: try expiry %s (dte=%.2f, contracts=%d, rank=%d/%d)\n",
			now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), dte, len(expiryContracts), idx+1, len(expiries))

		expiryChain := backtest.NewOptionsChain(expiryContracts, now)
		longOpt, longPrice, longReason := s.pickOption(now, scope, "long", backtest.Buy, longTargetDelta, expiryChain.SortByDelta(longTargetDelta))
		if longOpt == nil {
			s.logSelection("[%s] %s: skip expiry %s, reason=%s\n", now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), longReason)
			continue
		}

		shortOpt, shortPrice, shortReason := s.pickShortOption(now, scope, *longOpt, expiryContracts)
		if shortOpt == nil {
			s.logSelection("[%s] %s: skip expiry %s, reason=%s\n", now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), shortReason)
			continue
		}

		spreadCost := longPrice - shortPrice
		if spreadCost <= 0 {
			s.logSelection("[%s] %s: skip expiry %s, reason=non-positive spread cost %.6f (buy %.6f - sell %.6f)\n",
				now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), spreadCost, longPrice, shortPrice)
			continue
		}

		qty := amount / spreadCost
		if qty <= 0 {
			s.logSelection("[%s] %s: skip expiry %s, reason=non-positive quantity %.6f for amount %.6f\n",
				now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), qty, amount)
			continue
		}

		s.logSelection("[%s] %s: selected expiry %s (dte=%.2f), buy=%s delta=%.4f price=%.6f, sell=%s delta=%.4f price=%.6f, cost=%.6f\n",
			now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), dte,
			longOpt.Symbol, longOpt.Delta, longPrice,
			shortOpt.Symbol, shortOpt.Delta, shortPrice,
			spreadCost)

		return &spreadSelection{
			long:       *longOpt,
			short:      *shortOpt,
			longPrice:  longPrice,
			shortPrice: shortPrice,
			spreadCost: spreadCost,
			qty:        qty,
			expiry:     expiry,
			dte:        dte,
		}, true
	}

	s.logSelection("[%s] %s: no usable expiry found within dte in [%d,%d]\n", now.Format(time.RFC3339), scope, window.min, window.max)
	return nil, false
}

func (s *strategy) pickOption(now time.Time, scope, leg string, side backtest.Side, target float64, candidates []backtest.OptionContract) (*backtest.OptionContract, float64, string) {
	if len(candidates) == 0 {
		return nil, 0, fmt.Sprintf("no %s candidates near delta %.2f", leg, target)
	}

	for idx := range candidates {
		price := s.EntryPriceMode.EntryPrice(side, candidates[idx])
		if !math.IsNaN(price) && price > 0 {
			selected := candidates[idx]
			return &selected, price, ""
		}

		reason := fmt.Sprintf("invalid entry price for %s candidate %s (delta=%.4f, target=%.2f, mark=%.6f, bid=%.6f, ask=%.6f)",
			leg,
			candidates[idx].Symbol,
			candidates[idx].Delta,
			target,
			candidates[idx].MarkPrice,
			candidates[idx].BidPrice,
			candidates[idx].AskPrice,
		)
		s.logSelection("[%s] %s: skip %s candidate #%d %s, reason=%s\n",
			now.Format(time.RFC3339), scope, leg, idx+1, candidates[idx].Symbol, reason)
	}

	return nil, 0, fmt.Sprintf("no valid %s contract near delta %.2f", leg, target)
}

func (s *strategy) pickShortOption(now time.Time, scope string, longOpt backtest.OptionContract, candidates []backtest.OptionContract) (*backtest.OptionContract, float64, string) {
	targetStrikeMultiple := shortStrikeMinMultipleForLongDelta(longOpt.Delta)
	targetStrike := longOpt.StrikePrice * targetStrikeMultiple
	filtered := make([]backtest.OptionContract, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Symbol == longOpt.Symbol {
			continue
		}
		if candidate.StrikePrice < targetStrike {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, 0, fmt.Sprintf("no short candidates with strike >= %.2f (buy strike %.2f, multiple %.2f)", targetStrike, longOpt.StrikePrice, targetStrikeMultiple)
	}

	sort.Slice(filtered, func(i, j int) bool {
		di := math.Abs(filtered[i].StrikePrice - targetStrike)
		dj := math.Abs(filtered[j].StrikePrice - targetStrike)
		if di != dj {
			return di < dj
		}
		return filtered[i].SpreadRatio() < filtered[j].SpreadRatio()
	})

	var fallback *backtest.OptionContract
	var fallbackPrice float64
	bestBoundaryDistance := math.Inf(1)

	for idx := range filtered {
		candidate := filtered[idx]
		price := s.EntryPriceMode.EntryPrice(backtest.Sell, candidate)
		if math.IsNaN(price) || price <= 0 {
			s.logSelection("[%s] %s: skip short candidate #%d %s, reason=invalid entry price (delta=%.4f, strike=%.2f, mark=%.6f, bid=%.6f, ask=%.6f)\n",
				now.Format(time.RFC3339), scope, idx+1, candidate.Symbol, candidate.Delta, candidate.StrikePrice, candidate.MarkPrice, candidate.BidPrice, candidate.AskPrice)
			continue
		}

		if candidate.Delta >= minShortDelta && candidate.Delta <= maxShortDelta {
			selected := candidate
			return &selected, price, ""
		}

		boundaryDistance := deltaBoundaryDistance(candidate.Delta, minShortDelta, maxShortDelta)
		if boundaryDistance < bestBoundaryDistance {
			bestBoundaryDistance = boundaryDistance
			selected := candidate
			fallback = &selected
			fallbackPrice = price
		}
	}

	if fallback != nil {
		return fallback, fallbackPrice, fmt.Sprintf("fallback to nearest delta boundary with strike >= %.2f", targetStrike)
	}

	return nil, 0, fmt.Sprintf("no valid short contract with strike >= %.2f", targetStrike)
}

func candidateExpiries(contracts []backtest.OptionContract, now time.Time, targetDTE int) []time.Time {
	seen := make(map[time.Time]struct{}, len(contracts))
	expiries := make([]time.Time, 0, len(contracts))
	for _, contract := range contracts {
		if _, ok := seen[contract.Expiration]; ok {
			continue
		}
		seen[contract.Expiration] = struct{}{}
		expiries = append(expiries, contract.Expiration)
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

func selectDTEWindow(ivPercentile float64) dteWindow {
	if ivPercentile > shortDTEIVPercentileCutoff {
		return dteWindow{min: highIVMinDTE, target: highIVTargetDTE, max: highIVMaxDTE}
	}
	return dteWindow{min: lowIVMinDTE, target: lowIVTargetDTE, max: lowIVMaxDTE}
}

func shortStrikeMinMultipleForLongDelta(longDelta float64) float64 {
	if longDelta < 0.4 {
		return 1.20
	}
	if longDelta <= 0.6 {
		return 1.15
	}
	return 1.10
}

func contractsForExpiry(contracts []backtest.OptionContract, expiry time.Time) []backtest.OptionContract {
	filtered := make([]backtest.OptionContract, 0, len(contracts))
	for _, contract := range contracts {
		if contract.Expiration.Equal(expiry) {
			filtered = append(filtered, contract)
		}
	}
	return filtered
}

func (s *strategy) openNewGroup(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64) {
	hvPercentile := s.currentHVPercentile(ctx)
	ivPercentile := s.currentIVPercentile(ctx)
	selection, ok := s.selectSpread(ctx.Time(), chain, amount, hvPercentile, ivPercentile, "entry")
	if !ok {
		return
	}

	groupTracker := ctx.SpreadGroups()
	groupID := groupTracker.Open(
		fmt.Sprintf("bull-call|amt=%.4f", amount),
		amount, decayFactor, ctx.Time(),
	)

	spreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
		{Contract: selection.long, Side: backtest.Buy, Qty: selection.qty, EntryPrice: selection.longPrice},
		{Contract: selection.short, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.shortPrice},
	}, fmt.Sprintf("开仓|A=%.1f|B=%.1f|d%.2f/d%.2f|n=%.2f", hvPercentile, ivPercentile, selection.long.Delta, selection.short.Delta, selection.qty), groupID)

	if spreadID <= 0 {
		return
	}

	groupTracker.AddSpread(groupID, spreadID)
	s.activeGroups = append(s.activeGroups, activeGroup{groupID: groupID, spreadID: spreadID})

	fmt.Printf("[%s] group %d: spread %d, n=%.4f, buyD=%.4f@%.6f, sellD=%.4f@%.6f, cost=%.6f\n",
		ctx.Time().Format(time.RFC3339), groupID, spreadID, selection.qty,
		selection.long.Delta, selection.longPrice, selection.short.Delta, selection.shortPrice, selection.spreadCost)
}

func (s *strategy) manageGroups(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract) {
	now := ctx.Time()
	var remaining []activeGroup

	for _, ag := range s.activeGroups {
		sp := ctx.Spreads().Get(ag.spreadID)
		if sp == nil || sp.IsFullyClosed() {
			continue
		}

		needExpiryClose := false
		for _, leg := range sp.Legs {
			if leg.Closed {
				continue
			}
			contract := optutil.ResolveContract(leg.Contract, contractMap)
			if contract.DaysToExpiry(now) <= 1 {
				needExpiryClose = true
				break
			}
		}

		if needExpiryClose {
			s.closeGroupSpread(ctx, ag.spreadID, contractMap, "到期前平仓")
			ctx.SpreadGroups().Close(ag.groupID)
			fmt.Printf("[%s] group %d: expiry close\n", now.Format(time.RFC3339), ag.groupID)
			continue
		}

		shouldRoll, rollReason := s.checkRollConditions(sp, contractMap)
		if shouldRoll {
			s.closeGroupSpread(ctx, ag.spreadID, contractMap, rollReason)
			group := ctx.SpreadGroups().Get(ag.groupID)
			if group == nil {
				continue
			}
			ctx.SpreadGroups().IncrementRoll(ag.groupID)
			newAmount := group.CurrentAmount()

			fmt.Printf("[%s] group %d: roll #%d, reason=%s, newAmount=%.4f\n",
				now.Format(time.RFC3339), ag.groupID, group.RollCount, rollReason, newAmount)

			newSpreadID := s.openRollSpread(ctx, chain, newAmount, ag.groupID)
			if newSpreadID > 0 {
				ctx.SpreadGroups().AddSpread(ag.groupID, newSpreadID)
				remaining = append(remaining, activeGroup{groupID: ag.groupID, spreadID: newSpreadID})
			} else {
				ctx.SpreadGroups().Close(ag.groupID)
			}
			continue
		}

		remaining = append(remaining, ag)
	}

	s.activeGroups = remaining
}

func (s *strategy) checkRollConditions(sp *backtest.SpreadPosition, contractMap map[string]backtest.OptionContract) (bool, string) {
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

	initialSpreadCost := longLeg.EntryPrice - shortLeg.EntryPrice
	currentSpreadValue := longMark - shortMark
	if initialSpreadCost > 0 {
		pnlPct := (currentSpreadValue - initialSpreadCost) / initialSpreadCost
		if pnlPct >= rollProfitPct {
			return true, fmt.Sprintf("换仓|spread涨%.0f%%", pnlPct*100)
		}
	}

	if longContract.Delta-longLeg.Contract.Delta >= rollDeltaIncrease {
		return true, fmt.Sprintf("换仓|多头D增加%.4f", longContract.Delta-longLeg.Contract.Delta)
	}

	return false, ""
}

func (s *strategy) closeGroupSpread(ctx *backtest.BarContext, spreadID int, contractMap map[string]backtest.OptionContract, reason string) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		contract := optutil.ResolveContract(sp.Legs[i].Contract, contractMap)
		closePrice := s.ExitPriceMode.ExitPrice(sp.Legs[i].Side, contract)
		if !math.IsNaN(closePrice) && closePrice > 0 {
			ctx.CloseSpreadLegWithReason(spreadID, i, closePrice, reason)
		}
	}
}

func (s *strategy) openRollSpread(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64, groupID int) int {
	hvPercentile := s.currentHVPercentile(ctx)
	ivPercentile := s.currentIVPercentile(ctx)
	selection, ok := s.selectSpread(ctx.Time(), chain, amount, hvPercentile, ivPercentile, fmt.Sprintf("roll group %d", groupID))
	if !ok {
		return 0
	}

	spreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
		{Contract: selection.long, Side: backtest.Buy, Qty: selection.qty, EntryPrice: selection.longPrice},
		{Contract: selection.short, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.shortPrice},
	}, fmt.Sprintf("换仓|A=%.1f|B=%.1f|d%.2f/d%.2f|n=%.2f", hvPercentile, ivPercentile, selection.long.Delta, selection.short.Delta, selection.qty), groupID)

	if spreadID > 0 {
		fmt.Printf("[%s] group %d: roll spread %d, n=%.4f, buyD=%.4f@%.6f, sellD=%.4f@%.6f\n",
			ctx.Time().Format(time.RFC3339), groupID, spreadID, selection.qty,
			selection.long.Delta, selection.longPrice, selection.short.Delta, selection.shortPrice)
	}
	return spreadID
}

func loadSignals(relPath string) ([]signalEvent, error) {
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

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV %s: %w", path, err)
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	events := make([]signalEvent, 0, len(records))

	for i, record := range records {
		if i == 0 {
			continue
		}
		if len(record) < 4 {
			continue
		}

		sigType, addCount, ok := parseSignalMetadata(record[3])
		if !ok {
			continue
		}

		dateStr := strings.TrimSpace(record[2]) // "日期和时间" column
		// Try multiple layouts for date parsing
		var ts time.Time
		layouts := []string{"2006/1/2 15:04", "2006-01-02 15:04", "2006/01/02 15:04"}
		parsed := false
		for _, layout := range layouts {
			if t, err := time.ParseInLocation(layout, dateStr, utc8); err == nil {
				ts = t
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		events = append(events, signalEvent{time: ts.UTC(), sigType: sigType, addCount: addCount})
	}

	sort.Slice(events, func(i, j int) bool {
		if !events[i].time.Equal(events[j].time) {
			return events[i].time.Before(events[j].time)
		}
		return events[i].sigType < events[j].sigType
	})
	return events, nil
}

func parseSignalMetadata(raw string) (signalType, int, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(lower, "爆发"), strings.Contains(lower, "顺势"), strings.Contains(lower, "首仓"), strings.Contains(lower, "init"):
		return signalInit, 0, true
	case strings.Contains(lower, "加仓"), strings.Contains(lower, "add"):
		return signalAdd, extractTrailingInt(lower, 1), true
	default:
		return signalNone, 0, false
	}
}

func extractTrailingInt(value string, fallback int) int {
	match := trailingDigitsPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return fallback
	}
	parsed, err := strconv.Atoi(match[1])
	if err != nil {
		return fallback
	}
	return parsed
}

func (s *strategy) currentHVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Ind(hvPercentileColumn)
	if math.IsNaN(value) {
		return defaultMetricPercentile
	}
	return value
}

func (s *strategy) currentIVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Ind(ivPercentileColumn)
	if math.IsNaN(value) {
		return defaultMetricPercentile
	}
	return value
}

func (s *strategy) currentHV(ctx *backtest.BarContext) float64 {
	return ctx.Ind(hvValueColumn)
}

func (s *strategy) currentDVOL(ctx *backtest.BarContext) float64 {
	return ctx.Ind(dvolValueColumn)
}

func (s *strategy) currentDVOLBarCount(ctx *backtest.BarContext) int {
	value := ctx.Ind(dvolBarIndexColumn)
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	return int(value) + 1
}

func dynamicLongDelta(hvPercentile, ivPercentile float64) float64 {
	value := ((2 * hvPercentile) + ivPercentile) / 300.0
	value -= 0.05
	if value < minLongDelta {
		return minLongDelta
	}
	if value > maxLongDelta {
		return maxLongDelta
	}
	return value
}

func deltaBoundaryDistance(delta, minDelta, maxDelta float64) float64 {
	if delta < minDelta {
		return minDelta - delta
	}
	if delta > maxDelta {
		return delta - maxDelta
	}
	return 0
}

func logReturnIndicator(source string) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			series := inputs[source]
			out := make([]float64, len(series))
			for i := range out {
				out[i] = math.NaN()
			}
			for i := 1; i < len(series); i++ {
				prev := series[i-1]
				curr := series[i]
				if math.IsNaN(prev) || math.IsNaN(curr) || prev <= 0 || curr <= 0 {
					continue
				}
				out[i] = math.Log(curr / prev)
			}
			return out
		},
	)
}

func tradingViewHVIndicator(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			stddev := optutil.RollingStdDev(inputs[source], period)
			out := make([]float64, len(stddev))
			factor := math.Sqrt(tvAnnualizationDays) * 100.0
			for i, value := range stddev {
				if math.IsNaN(value) {
					out[i] = math.NaN()
					continue
				}
				out[i] = value * factor
			}
			return out
		},
	)
}

func buildAlignedIndexColumn(alignMap []int, length int) []float64 {
	values := make([]float64, length)
	for i := range values {
		values[i] = math.NaN()
	}
	for i := 0; i < length && i < len(alignMap); i++ {
		if alignMap[i] < 0 {
			continue
		}
		values[i] = float64(alignMap[i])
	}
	return values
}

func buildSignalColumn(primaryTimestamps []time.Time, events []signalEvent) []float64 {
	values := make([]float64, len(primaryTimestamps))
	if len(primaryTimestamps) == 0 || len(events) == 0 {
		return values
	}

	assigned := make(map[int]signalEvent)
	for _, event := range events {
		idx := primaryBarIndexForSignal(primaryTimestamps, event.time)
		if idx < 0 {
			continue
		}
		existing, ok := assigned[idx]
		if !ok || event.time.Before(existing.time) || (event.time.Equal(existing.time) && event.sigType == signalInit && existing.sigType != signalInit) {
			assigned[idx] = event
		}
	}

	for idx, event := range assigned {
		switch event.sigType {
		case signalInit:
			values[idx] = 1
		case signalAdd:
			values[idx] = 2
		}
	}
	return values
}

func buildSignalAddCountColumn(primaryTimestamps []time.Time, events []signalEvent) []float64 {
	values := make([]float64, len(primaryTimestamps))
	if len(primaryTimestamps) == 0 || len(events) == 0 {
		return values
	}

	assigned := make(map[int]signalEvent)
	for _, event := range events {
		idx := primaryBarIndexForSignal(primaryTimestamps, event.time)
		if idx < 0 {
			continue
		}
		existing, ok := assigned[idx]
		if !ok || event.time.Before(existing.time) || (event.time.Equal(existing.time) && event.sigType == signalInit && existing.sigType != signalInit) {
			assigned[idx] = event
		}
	}

	for idx, event := range assigned {
		values[idx] = float64(event.addCount)
	}
	return values
}

func buildTriggeredAlignedSignalColumn(alignMap []int, values []float64, length int) []float64 {
	out := make([]float64, length)
	prevMapped := -1
	for i := 0; i < length; i++ {
		mapped := -1
		if i < len(alignMap) {
			mapped = alignMap[i]
		}
		if mapped < 0 || mapped >= len(values) {
			prevMapped = mapped
			continue
		}
		if mapped != prevMapped && values[mapped] != 0 {
			out[i] = values[mapped]
		}
		prevMapped = mapped
	}
	return out
}

func primaryBarIndexForSignal(primaryTimestamps []time.Time, eventTime time.Time) int {
	if len(primaryTimestamps) == 0 || eventTime.IsZero() {
		return -1
	}
	idx := sort.Search(len(primaryTimestamps), func(i int) bool {
		return primaryTimestamps[i].After(eventTime)
	})
	if idx == 0 {
		if primaryTimestamps[0].After(eventTime) {
			return -1
		}
		return 0
	}
	if idx >= len(primaryTimestamps) {
		return len(primaryTimestamps) - 1
	}
	return idx - 1
}

func alignSeriesValues(targetTimes, sourceTimes []time.Time, sourceValues []float64) ([]float64, error) {
	if len(sourceTimes) != len(sourceValues) {
		return nil, fmt.Errorf("timestamp/value length mismatch: %d vs %d", len(sourceTimes), len(sourceValues))
	}
	out := make([]float64, len(targetTimes))
	for i := range out {
		out[i] = math.NaN()
	}
	if len(targetTimes) == 0 || len(sourceTimes) == 0 {
		return out, nil
	}
	for i, ts := range targetTimes {
		idx := sort.Search(len(sourceTimes), func(j int) bool {
			return sourceTimes[j].After(ts)
		}) - 1
		if idx < 0 || idx >= len(sourceValues) {
			continue
		}
		out[i] = sourceValues[idx]
	}
	return out, nil
}
