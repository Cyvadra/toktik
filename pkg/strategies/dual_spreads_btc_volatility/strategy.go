package dualspreadsvol

import (
	"encoding/csv"
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
	strategyName  = "dual-spreads-btc-volatility"
	strategyAlias = "dual_spreads_btc_volatility"

	amountBase             = 2.0
	targetDTE              = 40
	maxDTE                 = 40
	volLookbackBars        = 100
	defaultVolPercentile   = 60.0
	entryIVPercentileMax   = 66.0
	rollProfitPct          = 0.50
	rollDeltaIncrease      = 0.20
	decayFactor            = 0.90
	minLongDelta           = 0.20
	maxLongDelta           = 0.80
	minShortDelta          = 0.10
	maxShortDelta          = 0.80
	shortStrikeMinMultiple = 1.20
	strategyRunInterval    = "1h"
	dvolInterval           = "12h"

	signalCSVPath        = "pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv"
	hvPercentileColumn   = "hv_pr_100_12h"
	dvolPercentileColumn = "dvol_pr_100_12h"
)

type signalType int

const (
	signalNone signalType = iota
	signalInit
	signalAdd
)

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
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				signals:            signals,
			}, nil
		},
	})
}

type activeGroup struct {
	groupID  int
	spreadID int
}

type strategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode
	logf               func(format string, args ...any)

	signals              map[int64]signalType
	processedSignalTimes map[int64]struct{}
	dvolRef              backtest.FactorRef
	activeGroups         []activeGroup
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

func (s *strategy) Name() string { return "DualSpreadsBTCVolatility" }

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
		{Source: hvPercentileColumn, Label: "HV PR100 12H", Decimals: 1},
		{Source: "factor.dvol." + dvolInterval + ".close", Label: "DVOL 12H", Decimals: 2},
		{Source: "factor.dvol." + dvolInterval + "." + dvolPercentileColumn, Label: "IV PR100 12H", Decimals: 1},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	if s.processedSignalTimes == nil {
		s.processedSignalTimes = make(map[int64]struct{}, len(s.signals))
	}

	ctx.Register("ret_12h", percentChange("close"))
	ctx.Register("hv_100_12h", rollingStdDevIndicator("ret_12h", volLookbackBars))
	ctx.Register(hvPercentileColumn, percentileRankIndicator("hv_100_12h", volLookbackBars))

	s.dvolRef = ctx.AddFactor("dvol", dvolInterval)
	ctx.RegisterFactor(s.dvolRef, dvolPercentileColumn, percentileRankIndicator("close", volLookbackBars))
	ctx.SetWarmup(120 * 24 * time.Hour)
	ctx.SetParam("amount_base", amountBase)
	ctx.SetParam("strategy_interval", strategyRunInterval)
	ctx.SetParam("signal_count", float64(len(s.signals)))
	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}

	entrySignal := make([]float64, primary.Len())
	for i, ts := range primary.Timestamps() {
		utcUnix := ts.UTC().Unix()
		if sigType, ok := s.signals[utcUnix]; ok {
			switch sigType {
			case signalInit:
				entrySignal[i] = 1
			case signalAdd:
				entrySignal[i] = 2
			}
		}
	}
	return primary.SetColumn("entry_signal", entrySignal)
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := buildContractMap(chain)

	s.manageGroups(ctx, chain, contractMap)

	sigType, ok := signalTypeFromIndicator(ctx.Ind("entry_signal"))
	if !ok {
		return
	}

	signalTime := ctx.Time().UTC().Unix()
	if s.signalProcessed(signalTime) {
		return
	}

	if sigType == signalInit && !s.dvolFilter(ctx) {
		s.markSignalProcessed(signalTime)
		return
	}

	s.markSignalProcessed(signalTime)
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

func (s *strategy) signalProcessed(signalTime int64) bool {
	if s.processedSignalTimes == nil {
		return false
	}
	_, ok := s.processedSignalTimes[signalTime]
	return ok
}

func (s *strategy) markSignalProcessed(signalTime int64) {
	if s.processedSignalTimes == nil {
		s.processedSignalTimes = make(map[int64]struct{})
	}
	s.processedSignalTimes[signalTime] = struct{}{}
}

func (s *strategy) dvolFilter(ctx *backtest.BarContext) bool {
	return s.currentIVPercentile(ctx) <= entryIVPercentileMax
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

	eligibleCalls := chain.Calls().ExpiryMax(maxDTE)
	if eligibleCalls.Len() == 0 {
		s.logSelection("[%s] %s: skip selection, no call contracts with dte <= %d\n", now.Format(time.RFC3339), scope, maxDTE)
		return nil, false
	}

	contracts := eligibleCalls.Contracts()
	expiries := candidateExpiries(contracts, now)
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

	s.logSelection("[%s] %s: no usable expiry found within dte <= %d\n", now.Format(time.RFC3339), scope, maxDTE)
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
	targetStrike := longOpt.StrikePrice * shortStrikeMinMultiple
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
		return nil, 0, fmt.Sprintf("no short candidates with strike >= %.2f (buy strike %.2f)", targetStrike, longOpt.StrikePrice)
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

func candidateExpiries(contracts []backtest.OptionContract, now time.Time) []time.Time {
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
			contract := resolveContract(leg.Contract, contractMap)
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

	longContract := resolveContract(longLeg.Contract, contractMap)
	shortContract := resolveContract(shortLeg.Contract, contractMap)

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
		contract := resolveContract(sp.Legs[i].Contract, contractMap)
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

func resolveContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func buildContractMap(chain *backtest.OptionsChain) map[string]backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	contracts := chain.Contracts()
	cm := make(map[string]backtest.OptionContract, len(contracts))
	for _, c := range contracts {
		cm[c.Symbol] = c
	}
	return cm
}

func loadSignals(relPath string) (map[int64]signalType, error) {
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
	signals := make(map[int64]signalType)

	for i, record := range records {
		if i == 0 {
			continue
		}
		if len(record) < 4 {
			continue
		}

		sigStr := strings.TrimSpace(record[3])
		lower := strings.ToLower(sigStr)

		var sigType signalType
		if strings.Contains(lower, "init") {
			sigType = signalInit
		} else if strings.Contains(lower, "add") {
			sigType = signalAdd
		} else {
			continue
		}

		dateStr := strings.TrimSpace(record[2])
		ts, err := time.ParseInLocation("2006-01-02 15:04", dateStr, utc8)
		if err != nil {
			continue
		}

		utcUnix := ts.UTC().Unix()
		if existing, ok := signals[utcUnix]; ok && existing == signalInit {
			continue
		}
		signals[utcUnix] = sigType
	}

	return signals, nil
}

func (s *strategy) currentHVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Ind(hvPercentileColumn)
	if math.IsNaN(value) {
		return defaultVolPercentile
	}
	return value
}

func (s *strategy) currentIVPercentile(ctx *backtest.BarContext) float64 {
	value := ctx.Factor(s.dvolRef).Ind(dvolPercentileColumn)
	if math.IsNaN(value) {
		return defaultVolPercentile
	}
	return value
}

func dynamicLongDelta(hvPercentile, ivPercentile float64) float64 {
	value := ((2 * hvPercentile) + ivPercentile) / 300.0
	value -= 0.1
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

func percentChange(source string) backtest.Indicator {
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
				if math.IsNaN(prev) || math.IsNaN(curr) || prev == 0 {
					continue
				}
				out[i] = curr/prev - 1
			}
			return out
		},
	)
}

func rollingStdDevIndicator(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs[source], period)
		},
	)
}

func rollingStdDev(series []float64, period int) []float64 {
	out := make([]float64, len(series))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 {
		return out
	}
	for i := period - 1; i < len(series); i++ {
		sum := 0.0
		count := 0
		valid := true
		for j := i - period + 1; j <= i; j++ {
			if math.IsNaN(series[j]) {
				valid = false
				break
			}
			sum += series[j]
			count++
		}
		if !valid || count == 0 {
			continue
		}
		mean := sum / float64(count)
		variance := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := series[j] - mean
			variance += diff * diff
		}
		out[i] = math.Sqrt(variance / float64(count))
	}
	return out
}

func percentileRankIndicator(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			series := inputs[source]
			out := make([]float64, len(series))
			for i := range out {
				out[i] = math.NaN()
			}
			for i := 0; i < len(series); i++ {
				if i < period || math.IsNaN(series[i]) {
					continue
				}
				count := 0
				valid := 0
				for j := i - period; j < i; j++ {
					if math.IsNaN(series[j]) {
						continue
					}
					valid++
					if series[j] < series[i] {
						count++
					}
				}
				if valid == 0 {
					continue
				}
				out[i] = float64(count) / float64(valid) * 100
			}
			return out
		},
	)
}
