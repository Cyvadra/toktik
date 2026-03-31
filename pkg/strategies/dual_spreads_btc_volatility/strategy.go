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

	amountBase      = 2.0
	targetDTE       = 33
	biasDTE         = 10
	longDelta       = 0.33
	shortDelta      = 0.1
	rollProfitPct   = 0.50
	rollDeltaLimit  = 0.50
	decayFactor     = 0.80
	dvolPercentile  = 0.50
	dvolLookback90  = 90
	dvolLookback365 = 365

	signalCSVPath         = "pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv"
	dvolQuantile90Column  = "dvol_p50_90d"
	dvolQuantile365Column = "dvol_p50_365d"
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
		{Source: "factor.dvol.1d.close", Label: "DVOL", Decimals: 2},
		{Source: "factor.dvol.1d." + dvolQuantile90Column, Label: "DVOL P50 90D", Decimals: 2},
		{Source: "factor.dvol.1d." + dvolQuantile365Column, Label: "DVOL P50 365D", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	if s.processedSignalTimes == nil {
		s.processedSignalTimes = make(map[int64]struct{}, len(s.signals))
	}

	s.dvolRef = ctx.AddFactor("dvol", "1d")
	ctx.RegisterFactor(s.dvolRef, dvolQuantile90Column, backtest.Quantile("close", dvolLookback90, dvolPercentile))
	ctx.RegisterFactor(s.dvolRef, dvolQuantile365Column, backtest.Quantile("close", dvolLookback365, dvolPercentile))
	ctx.SetWarmup((dvolLookback365 + 5) * 24 * time.Hour)
	ctx.SetParam("amount_base", amountBase)
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
	dvolNow := ctx.Factor(s.dvolRef).Field("close")
	if math.IsNaN(dvolNow) {
		return false
	}

	p50_90d := ctx.Factor(s.dvolRef).Field(dvolQuantile90Column)
	p50_365d := ctx.Factor(s.dvolRef).Field(dvolQuantile365Column)

	passes90d := !math.IsNaN(p50_90d) && dvolNow <= p50_90d
	passes365d := !math.IsNaN(p50_365d) && dvolNow <= p50_365d

	return passes90d || passes365d
}

func (s *strategy) logSelection(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
		return
	}
	fmt.Printf(format, args...)
}

func (s *strategy) selectSpread(now time.Time, chain *backtest.OptionsChain, amount float64, scope string) (*spreadSelection, bool) {
	if chain == nil || chain.Len() == 0 {
		s.logSelection("[%s] %s: skip selection, empty options chain\n", now.Format(time.RFC3339), scope)
		return nil, false
	}

	eligibleCalls := chain.Calls().ExpiryMax(targetDTE + biasDTE).ExpiryMin(targetDTE - biasDTE).ExpiryNearest(targetDTE)
	if eligibleCalls.Len() == 0 {
		s.logSelection("[%s] %s: skip selection, no call contracts with dte within [%d, %d]\n", now.Format(time.RFC3339), scope, targetDTE-biasDTE, targetDTE+biasDTE)
		return nil, false
	}

	contracts := eligibleCalls.Contracts()
	expiries := candidateExpiries(contracts, now)
	if len(expiries) == 0 {
		s.logSelection("[%s] %s: skip selection, no candidate expiries after filtering\n", now.Format(time.RFC3339), scope)
		return nil, false
	}

	for idx, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		dte := expiry.Sub(now).Hours() / 24
		s.logSelection("[%s] %s: try expiry %s (dte=%.2f, contracts=%d, rank=%d/%d)\n",
			now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), dte, len(expiryContracts), idx+1, len(expiries))

		expiryChain := backtest.NewOptionsChain(expiryContracts, now)
		longOpt, longPrice, longReason := s.pickOption(now, scope, "long", backtest.Buy, longDelta, expiryChain.SortByDelta(longDelta))
		if longOpt == nil {
			s.logSelection("[%s] %s: skip expiry %s, reason=%s\n", now.Format(time.RFC3339), scope, expiry.Format("2006-01-02"), longReason)
			continue
		}

		shortOpt, shortPrice, shortReason := s.pickOption(now, scope, "short", backtest.Sell, shortDelta, expiryChain.SortByDelta(shortDelta))
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

	s.logSelection("[%s] %s: no usable expiry found within dte [%d, %d]\n", now.Format(time.RFC3339), scope, targetDTE-biasDTE, targetDTE+biasDTE)
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
	selection, ok := s.selectSpread(ctx.Time(), chain, amount, "entry")
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
	}, fmt.Sprintf("开仓|d%.2f/d%.2f|n=%.2f", selection.long.Delta, selection.short.Delta, selection.qty), groupID)

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

	if longContract.Delta >= rollDeltaLimit {
		return true, fmt.Sprintf("换仓|多头D=%.4f", longContract.Delta)
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
	selection, ok := s.selectSpread(ctx.Time(), chain, amount, fmt.Sprintf("roll group %d", groupID))
	if !ok {
		return 0
	}

	spreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
		{Contract: selection.long, Side: backtest.Buy, Qty: selection.qty, EntryPrice: selection.longPrice},
		{Contract: selection.short, Side: backtest.Sell, Qty: selection.qty, EntryPrice: selection.shortPrice},
	}, fmt.Sprintf("换仓|d%.2f/d%.2f|n=%.2f", selection.long.Delta, selection.short.Delta, selection.qty), groupID)

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
