package retracementratioprotectivespread

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

const (
	strategyName  = "retracement-ratio-protective-spread"
	strategyAlias = "retracement_ratio_protective_spread"

	// Phase 1 parameters
	phase1SellNotional      = 5.0  // 5 BTC notional for sell leg
	phase1TargetDTE         = 70   // preferred DTE
	phase1MinDTE            = 55   // min DTE for phase 1
	phase1MaxDTE            = 85   // max DTE for phase 1
	phase1SellDeltaTarget   = 0.50 // target delta for sell leg
	phase1BuyDeltaTarget    = 0.30 // target delta for buy legs
	phase1PartialProfitPct  = 0.30 // 30% profit → partial close
	phase1FullProfitPct     = 0.50 // 50% profit → phase switch
	phase1StopATRMultiplier = 8.0  // stop loss ATR multiplier
	phase1RollMinDTE        = 20   // min remaining DTE for rolling
	phase1RollMaxDTE        = 40   // max remaining DTE for rolling
	phase1RollATRMultiplier = 2.0  // ATR threshold for rolling
	dvolPercentile95        = 95.0 // IV filter threshold

	// Phase 2 parameters
	phase2Amount             = 2.0 // 2 BTC for phase 2
	phase2MinDTE             = 25
	phase2TargetDTE          = 35
	phase2MaxDTE             = 40
	phase2MinDelta           = 0.10
	phase2MaxDelta           = 0.80
	phase2RollProfitPct      = 0.50 // 50% spread profit → roll
	phase2RollDeltaIncrease  = 0.20 // delta increase → roll
	phase2DecayFactor        = 0.90 // capital decay per roll
	phase2LongStrikeMultiple = 1.15 // KSell >= KBuy * 115% (long)
	phase2ShortStrikeFactor  = 0.80 // KSell <= KBuy * 80% (short)

	// Indicator parameters
	hvPercentileLookback = 100
	ivPercentileLookback = 200
	atrPeriod            = 14
	tvAnnualizationDays  = 365.0

	interval12h = "12h"

	// Column names
	hvReturnColumn     = "log_ret_12h"
	hvValueColumn      = "hv_100_12h"
	hvPercentileColumn = "hv_pr_100_12h"
	dvolValueColumn    = "dvol_12h"
	ivPercentileColumn = "iv_pr_200_12h"
	atrColumn          = "atr14"
	dvolBarIndexColumn = "dvol_12h_bar_index"

	signalLevelEnv = "SIGNAL_LEVEL"
	directionEnv   = "RRPS_DIRECTION"

	signalCSVDirPrefix = "pkg/strategies/retracement_ratio_protective_spread/"
)

type tradeDirection int

const (
	directionLong  tradeDirection = 1
	directionShort tradeDirection = 2
)

type phase int

const (
	phaseAmbush phase = 1 // Phase 1: ratio spread ambush
	phaseTrend  phase = 2 // Phase 2: trend following spread
)

type signalEvent struct {
	time time.Time
}

// activePosition tracks the current phase1 or phase2 position within a group.
type activePosition struct {
	groupID        int
	phase          phase
	spreadIDs      []int   // all spreads in this group (sell leg + buy legs combined)
	entryPrice     float64 // underlying price at entry
	entryATR       float64 // ATR at entry
	partialClosed  bool    // whether 30% partial close happened
	buyInitialCost float64 // total initial cost of buy legs
	// Phase 2 specific
	phase2BuyDelta float64 // buy leg delta at entry (for roll check)
	phase2Amount   float64 // current capital for this phase2 group
}

type strategy struct {
	optutil.PricingMixin

	direction tradeDirection
	signals   []signalEvent
	ref12h    backtest.SecurityRef
	dvolRef   backtest.FactorRef
	positions []activePosition
}

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{strategyAlias},
		Groups:  []string{"options", "spread", "timed"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			dir := resolveDirection()
			signalLevel := resolveSignalLevel()
			csvPath := buildCSVPath(signalLevel, dir)

			signals, err := loadSignals(csvPath)
			if err != nil {
				return nil, fmt.Errorf("load signals from %s: %w", csvPath, err)
			}
			fmt.Printf("[%s] direction=%s, signal_level=%s, csv=%s, signals=%d\n",
				strategyName, directionLabel(dir), signalLevel, csvPath, len(signals))

			return &strategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				direction: dir,
				signals:   signals,
			}, nil
		},
	})
}

func (s *strategy) Name() string { return "RetracementRatioProtectiveSpread" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "entry_signal", Label: "Entry Signal", Decimals: 0},
		{Source: hvValueColumn, Label: "HV 100 12H", Decimals: 2},
		{Source: dvolValueColumn, Label: "DVOL 12H", Decimals: 2},
		{Source: hvPercentileColumn, Label: "HV PR100 12H", Decimals: 1},
		{Source: ivPercentileColumn, Label: "DVOL PR200 12H", Decimals: 1},
		{Source: atrColumn, Label: "ATR14", Decimals: 2},
	}
}

// ---------------------------------------------------------------------------
// Init / Preload
// ---------------------------------------------------------------------------

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	primary := ctx.PrimaryRef()
	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)

	// indicators on 12h
	ctx.RegisterOn(s.ref12h, hvReturnColumn, logReturnIndicator("close"))
	ctx.RegisterOn(s.ref12h, hvValueColumn, tradingViewHVIndicator(hvReturnColumn, 10))
	ctx.RegisterOn(s.ref12h, hvPercentileColumn, optutil.PercentileRank(hvValueColumn, hvPercentileLookback))

	// DVOL factor on 12h
	s.dvolRef = ctx.AddFactor("dvol", interval12h)
	ctx.RegisterFactor(s.dvolRef, ivPercentileColumn, optutil.PercentileRank("close", ivPercentileLookback))

	// ATR on primary timeframe
	ctx.Register(atrColumn, backtest.ATR(atrPeriod))

	ctx.SetWarmup(120 * 24 * time.Hour)
	ctx.SetParam("direction", float64(s.direction))
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

	// Build and align signal column
	entrySignal12h := buildSignalColumn(htf.Timestamps(), s.signals)
	entrySignal := buildTriggeredAlignedSignalColumn(htf.AlignMap(), entrySignal12h, primary.Len())
	if err := primary.SetColumn("entry_signal", entrySignal); err != nil {
		return err
	}

	// Align HV columns from 12h to primary
	for _, name := range []string{hvValueColumn, hvPercentileColumn} {
		aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, name)
		if err != nil {
			return err
		}
		if err := primary.SetColumn(name, aligned); err != nil {
			return err
		}
	}

	// Align DVOL value
	dvolValue, err := ctx.ColumnAlignedFactorToPrimary(s.dvolRef, "close")
	if err != nil {
		return err
	}
	if err := primary.SetColumn(dvolValueColumn, dvolValue); err != nil {
		return err
	}

	// Align IV percentile from dvol factor → 12h → primary
	ivPercentile12h, err := alignSeriesValues(htf.Timestamps(), dvol.Timestamps(), dvol.Column(ivPercentileColumn))
	if err != nil {
		return err
	}
	if err := htf.SetColumn(ivPercentileColumn, ivPercentile12h); err != nil {
		return err
	}
	aligned, err := ctx.ColumnAlignedToPrimary(s.ref12h, ivPercentileColumn)
	if err != nil {
		return err
	}
	if err := primary.SetColumn(ivPercentileColumn, aligned); err != nil {
		return err
	}

	return primary.SetColumn(dvolBarIndexColumn, buildAlignedIndexColumn(dvol.AlignMap(), primary.Len()))
}

// ---------------------------------------------------------------------------
// OnBar
// ---------------------------------------------------------------------------

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := optutil.BuildContractMap(chain)

	// Manage existing positions (profit checks, expiry, rolling)
	s.managePositions(ctx, chain, contractMap)

	// Check for new entry signal
	sigVal := ctx.Ind("entry_signal")
	if math.IsNaN(sigVal) || sigVal != 1 {
		return
	}

	// DVOL percentile filter: < 95th percentile
	ivPR := ctx.Ind(ivPercentileColumn)
	if !math.IsNaN(ivPR) && ivPR >= dvolPercentile95 {
		fmt.Printf("[%s] skip signal: DVOL percentile %.1f >= %.1f\n",
			ctx.Time().Format(time.RFC3339), ivPR, dvolPercentile95)
		return
	}

	// On new signal: close ALL existing positions first
	s.closeAllPositions(ctx, contractMap, "新信号平仓")

	// Open phase 1 position
	if pos := s.openPhase1(ctx, chain); pos != nil {
		s.positions = append(s.positions, *pos)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Ratio Spread Ambush
// ---------------------------------------------------------------------------

func (s *strategy) openPhase1(ctx *backtest.BarContext, chain *backtest.OptionsChain) *activePosition {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	now := ctx.Time()
	optType := s.phase1OptionType()

	// Filter by type and DTE
	eligible := s.filterByType(chain, optType).ExpiryRange(phase1MinDTE, phase1MaxDTE)
	if eligible.Len() == 0 {
		fmt.Printf("[%s] phase1: no %s contracts with DTE in [%d,%d]\n",
			now.Format(time.RFC3339), optType, phase1MinDTE, phase1MaxDTE)
		return nil
	}

	contracts := eligible.Contracts()
	expiries := s.candidateExpiries(contracts, now, phase1TargetDTE)
	if len(expiries) == 0 {
		return nil
	}

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		// Select sell leg: delta closest to 0.5
		sellOpt, sellPrice := s.pickPhase1SellLeg(now, expiryChain, optType)
		if sellOpt == nil {
			continue
		}

		// n1 = 5 BTC / a
		n1 := phase1SellNotional / sellPrice

		// Select 2 buy legs: delta closest to 0.3
		buyOpt1, buyPrice1, buyOpt2, buyPrice2 := s.pickPhase1BuyLegs(now, expiryChain, optType, sellOpt)
		if buyOpt1 == nil || buyOpt2 == nil {
			continue
		}

		// n2 = 5 BTC / (b + c)
		n2 := phase1SellNotional / (buyPrice1 + buyPrice2)

		buyInitialCost := n2 * (buyPrice1 + buyPrice2)

		// Create order group
		groupTracker := ctx.SpreadGroups()
		atrVal := ctx.Ind(atrColumn)
		groupID := groupTracker.Open(
			fmt.Sprintf("phase1-%s|dte=%.0f", directionLabel(s.direction), expiry.Sub(now).Hours()/24),
			phase1SellNotional, 1.0, now,
		)

		// Open sell leg spread
		sellSide := s.sellSide()
		buySide := s.buySide()

		sellSpreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *sellOpt, Side: sellSide, Qty: n1, EntryPrice: sellPrice},
		}, fmt.Sprintf("P1卖出|d=%.2f|n=%.2f|K=%.0f", sellOpt.Delta, n1, sellOpt.StrikePrice), groupID)
		if sellSpreadID > 0 {
			groupTracker.AddSpread(groupID, sellSpreadID)
		}

		// Open buy leg 1
		buySpread1ID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt1, Side: buySide, Qty: n2, EntryPrice: buyPrice1},
		}, fmt.Sprintf("P1买入1|d=%.2f|n=%.2f|K=%.0f", buyOpt1.Delta, n2, buyOpt1.StrikePrice), groupID)
		if buySpread1ID > 0 {
			groupTracker.AddSpread(groupID, buySpread1ID)
		}

		// Open buy leg 2
		buySpread2ID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt2, Side: buySide, Qty: n2, EntryPrice: buyPrice2},
		}, fmt.Sprintf("P1买入2|d=%.2f|n=%.2f|K=%.0f", buyOpt2.Delta, n2, buyOpt2.StrikePrice), groupID)
		if buySpread2ID > 0 {
			groupTracker.AddSpread(groupID, buySpread2ID)
		}

		var spreadIDs []int
		if sellSpreadID > 0 {
			spreadIDs = append(spreadIDs, sellSpreadID)
		}
		if buySpread1ID > 0 {
			spreadIDs = append(spreadIDs, buySpread1ID)
		}
		if buySpread2ID > 0 {
			spreadIDs = append(spreadIDs, buySpread2ID)
		}

		if len(spreadIDs) == 0 {
			groupTracker.Close(groupID)
			continue
		}

		pos := &activePosition{
			groupID:        groupID,
			phase:          phaseAmbush,
			spreadIDs:      spreadIDs,
			entryPrice:     ctx.Close(),
			entryATR:       atrVal,
			buyInitialCost: buyInitialCost,
		}

		fmt.Printf("[%s] phase1 opened: group=%d, sell=%s d=%.2f n=%.2f, buy1=%s d=%.2f, buy2=%s d=%.2f n=%.2f\n",
			now.Format(time.RFC3339), groupID,
			sellOpt.Symbol, sellOpt.Delta, n1,
			buyOpt1.Symbol, buyOpt1.Delta,
			buyOpt2.Symbol, buyOpt2.Delta, n2)
		return pos // only open one group per signal
	}

	return nil
}

func (s *strategy) pickPhase1SellLeg(now time.Time, chain *backtest.OptionsChain, optType backtest.OptionType) (*backtest.OptionContract, float64) {
	targetDelta := phase1SellDeltaTarget
	if optType == backtest.Put {
		targetDelta = -phase1SellDeltaTarget
	}
	candidates := chain.SortByDelta(targetDelta)
	for i := range candidates {
		if candidates[i].Type != optType {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(s.sellSide(), candidates[i])
		if optutil.IsValidPrice(price) {
			c := candidates[i]
			return &c, price
		}
	}
	return nil, 0
}

func (s *strategy) pickPhase1BuyLegs(now time.Time, chain *backtest.OptionsChain, optType backtest.OptionType, sellOpt *backtest.OptionContract) (*backtest.OptionContract, float64, *backtest.OptionContract, float64) {
	targetDelta := phase1BuyDeltaTarget
	if optType == backtest.Put {
		targetDelta = -phase1BuyDeltaTarget
	}
	candidates := chain.SortByDelta(targetDelta)

	type optWithPrice struct {
		contract backtest.OptionContract
		price    float64
	}
	var valid []optWithPrice
	for i := range candidates {
		if candidates[i].Type != optType {
			continue
		}
		if candidates[i].Symbol == sellOpt.Symbol {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(s.buySide(), candidates[i])
		if !optutil.IsValidPrice(price) {
			continue
		}
		valid = append(valid, optWithPrice{contract: candidates[i], price: price})
		if len(valid) >= 2 {
			break
		}
	}

	if len(valid) < 2 {
		return nil, 0, nil, 0
	}

	// Sort by delta ascending (for puts, more negative is lower)
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].contract.Delta < valid[j].contract.Delta
	})

	return &valid[0].contract, valid[0].price, &valid[1].contract, valid[1].price
}

// ---------------------------------------------------------------------------
// Phase 2: Trend Following Spread
// ---------------------------------------------------------------------------

func (s *strategy) openPhase2(ctx *backtest.BarContext, chain *backtest.OptionsChain, amount float64) *activePosition {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	now := ctx.Time()
	optType := s.phase2OptionType()

	// Compute dynamic delta from HV + DVOL percentiles
	hvPR := ctx.Ind(hvPercentileColumn)
	if math.IsNaN(hvPR) {
		hvPR = 50
	}
	ivPR := ctx.Ind(ivPercentileColumn)
	if math.IsNaN(ivPR) {
		ivPR = 50
	}
	buyDelta := clamp(phase2MinDelta, phase2MaxDelta, (2*hvPR+ivPR)/300.0)

	// Filter eligible options
	eligible := s.filterByType(chain, optType).ExpiryRange(phase2MinDTE, phase2MaxDTE)
	if eligible.Len() == 0 {
		fmt.Printf("[%s] phase2: no %s contracts with DTE in [%d,%d]\n",
			now.Format(time.RFC3339), optType, phase2MinDTE, phase2MaxDTE)
		return nil
	}

	contracts := eligible.Contracts()
	expiries := s.candidateExpiries(contracts, now, phase2TargetDTE)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		// Pick buy leg by dynamic delta
		targetDelta := buyDelta
		if optType == backtest.Put {
			targetDelta = -buyDelta
		}
		buyOpt, buyPrice := s.pickPhase2BuyLeg(now, expiryChain, optType, targetDelta)
		if buyOpt == nil {
			continue
		}

		// Pick sell leg based on strike constraint
		sellOpt, sellPrice := s.pickPhase2SellLeg(now, expiryContracts, optType, buyOpt)
		if sellOpt == nil {
			continue
		}

		spreadCost := buyPrice - sellPrice
		if spreadCost <= 0 {
			continue
		}

		qty := amount / spreadCost
		if qty <= 0 {
			continue
		}

		// Create order group
		groupTracker := ctx.SpreadGroups()
		groupID := groupTracker.Open(
			fmt.Sprintf("phase2-%s|amt=%.4f", directionLabel(s.direction), amount),
			amount, phase2DecayFactor, now,
		)

		buySide := s.buySide()
		sellSide := s.sellSide()

		spreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt, Side: buySide, Qty: qty, EntryPrice: buyPrice},
			{Contract: *sellOpt, Side: sellSide, Qty: qty, EntryPrice: sellPrice},
		}, fmt.Sprintf("P2开仓|d=%.2f/%.2f|K=%.0f/%.0f|n=%.2f",
			buyOpt.Delta, sellOpt.Delta, buyOpt.StrikePrice, sellOpt.StrikePrice, qty), groupID)

		if spreadID <= 0 {
			groupTracker.Close(groupID)
			continue
		}

		groupTracker.AddSpread(groupID, spreadID)

		pos := &activePosition{
			groupID:        groupID,
			phase:          phaseTrend,
			spreadIDs:      []int{spreadID},
			entryPrice:     ctx.Close(),
			entryATR:       ctx.Ind(atrColumn),
			phase2BuyDelta: buyOpt.Delta,
			phase2Amount:   amount,
		}

		fmt.Printf("[%s] phase2 opened: group=%d, spread=%d, buy=%s d=%.2f@%.6f, sell=%s d=%.2f@%.6f, cost=%.6f, n=%.4f\n",
			now.Format(time.RFC3339), groupID, spreadID,
			buyOpt.Symbol, buyOpt.Delta, buyPrice,
			sellOpt.Symbol, sellOpt.Delta, sellPrice,
			spreadCost, qty)
		return pos // one group per entry
	}

	return nil
}

func (s *strategy) pickPhase2BuyLeg(now time.Time, chain *backtest.OptionsChain, optType backtest.OptionType, targetDelta float64) (*backtest.OptionContract, float64) {
	candidates := chain.SortByDelta(targetDelta)
	for i := range candidates {
		if candidates[i].Type != optType {
			continue
		}
		absDelta := math.Abs(candidates[i].Delta)
		if absDelta < phase2MinDelta || absDelta > phase2MaxDelta {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(s.buySide(), candidates[i])
		if optutil.IsValidPrice(price) {
			c := candidates[i]
			return &c, price
		}
	}
	return nil, 0
}

func (s *strategy) pickPhase2SellLeg(now time.Time, contracts []backtest.OptionContract, optType backtest.OptionType, buyOpt *backtest.OptionContract) (*backtest.OptionContract, float64) {
	var targetStrike float64
	var filter func(strike float64) bool

	if s.direction == directionLong {
		// Long: KSell >= KBuy * 115%
		targetStrike = buyOpt.StrikePrice * phase2LongStrikeMultiple
		filter = func(strike float64) bool { return strike >= targetStrike }
	} else {
		// Short: KSell <= KBuy * 80%
		targetStrike = buyOpt.StrikePrice * phase2ShortStrikeFactor
		filter = func(strike float64) bool { return strike <= targetStrike }
	}

	type candidate struct {
		contract backtest.OptionContract
		price    float64
		dist     float64
	}
	var candidates []candidate

	for _, c := range contracts {
		if c.Type != optType || c.Symbol == buyOpt.Symbol {
			continue
		}
		if !filter(c.StrikePrice) {
			continue
		}
		absDelta := math.Abs(c.Delta)
		if absDelta < phase2MinDelta || absDelta > phase2MaxDelta {
			continue
		}
		price := s.EntryPriceMode.EntryPrice(s.sellSide(), c)
		if !optutil.IsValidPrice(price) {
			continue
		}
		candidates = append(candidates, candidate{
			contract: c,
			price:    price,
			dist:     math.Abs(c.StrikePrice - targetStrike),
		})
	}

	if len(candidates) == 0 {
		return nil, 0
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist != candidates[j].dist {
			return candidates[i].dist < candidates[j].dist
		}
		return candidates[i].contract.SpreadRatio() < candidates[j].contract.SpreadRatio()
	})

	return &candidates[0].contract, candidates[0].price
}

// ---------------------------------------------------------------------------
// Position Management
// ---------------------------------------------------------------------------

func (s *strategy) managePositions(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap optutil.ContractMap) {
	now := ctx.Time()
	var remaining []activePosition

	for i := range s.positions {
		pos := &s.positions[i]

		switch pos.phase {
		case phaseAmbush:
			action := s.managePhase1(ctx, pos, chain, contractMap)
			switch action {
			case actionRemove:
				s.closePositionSpreads(ctx, pos, contractMap, "P1退出")
				ctx.SpreadGroups().Close(pos.groupID)
				continue
			case actionRollPhase1:
				s.closePositionSpreads(ctx, pos, contractMap, "DTE滚动")
				ctx.SpreadGroups().Close(pos.groupID)
				if newPos := s.openPhase1(ctx, chain); newPos != nil {
					remaining = append(remaining, *newPos)
				}
				continue
			case actionSwitchPhase2:
				s.closePositionSpreads(ctx, pos, contractMap, "P1→P2切换")
				ctx.SpreadGroups().Close(pos.groupID)
				if newPos := s.openPhase2(ctx, chain, phase2Amount); newPos != nil {
					remaining = append(remaining, *newPos)
				}
				continue
			}

		case phaseTrend:
			action := s.managePhase2(ctx, pos, chain, contractMap)
			switch action {
			case actionRemove:
				s.closePositionSpreads(ctx, pos, contractMap, "P2退出")
				ctx.SpreadGroups().Close(pos.groupID)
				continue
			case actionRoll:
				s.closePositionSpreads(ctx, pos, contractMap, "P2换仓")
				ctx.SpreadGroups().IncrementRoll(pos.groupID)
				group := ctx.SpreadGroups().Get(pos.groupID)
				if group != nil {
					newAmount := group.CurrentAmount()
					if newPos := s.rollPhase2InGroup(ctx, chain, pos.groupID, newAmount); newPos != nil {
						remaining = append(remaining, *newPos)
					} else {
						ctx.SpreadGroups().Close(pos.groupID)
					}
				} else {
					ctx.SpreadGroups().Close(pos.groupID)
				}
				continue
			}
		}

		// Check if all spreads are fully closed
		allClosed := true
		for _, sid := range pos.spreadIDs {
			sp := ctx.Spreads().Get(sid)
			if sp != nil && !sp.IsFullyClosed() {
				allClosed = false
				break
			}
		}
		if allClosed {
			ctx.SpreadGroups().Close(pos.groupID)
			continue
		}

		// Check expiry close
		needExpiryClose := false
		for _, sid := range pos.spreadIDs {
			sp := ctx.Spreads().Get(sid)
			if sp == nil {
				continue
			}
			for _, leg := range sp.Legs {
				if !leg.Closed {
					contract := optutil.ResolveContract(leg.Contract, contractMap)
					if contract.DaysToExpiry(now) <= 1 {
						needExpiryClose = true
						break
					}
				}
			}
			if needExpiryClose {
				break
			}
		}

		if needExpiryClose {
			s.closePositionSpreads(ctx, pos, contractMap, "到期前平仓")
			ctx.SpreadGroups().Close(pos.groupID)
			continue
		}

		remaining = append(remaining, s.positions[i])
	}
	s.positions = remaining
}

type posAction int

const (
	actionKeep         posAction = 0
	actionRemove       posAction = 1
	actionSwitchPhase2 posAction = 2
	actionRoll         posAction = 3
	actionRollPhase1   posAction = 4
)

func (s *strategy) managePhase1(ctx *backtest.BarContext, pos *activePosition, chain *backtest.OptionsChain, contractMap optutil.ContractMap) posAction {
	now := ctx.Time()
	atrVal := ctx.Ind(atrColumn)

	// Stop loss: price moves against us by phase1StopATRMultiplier ATRs
	if !math.IsNaN(pos.entryATR) && pos.entryATR > 0 && !math.IsNaN(atrVal) {
		priceDiff := ctx.Close() - pos.entryPrice
		if s.direction == directionShort {
			priceDiff = -priceDiff // for short, price going up is bad
		}
		if priceDiff < -phase1StopATRMultiplier*pos.entryATR {
			fmt.Printf("[%s] phase1 stop loss: ATR=%.2f, price moved %.2f ATRs\n",
				now.Format(time.RFC3339), pos.entryATR, priceDiff/pos.entryATR)
			return actionRemove
		}
	}

	// DTE rolling check: 20 < DTE < 40 and price within 2 ATR of entry → roll
	if !math.IsNaN(pos.entryATR) && pos.entryATR > 0 {
		for _, sid := range pos.spreadIDs {
			sp := ctx.Spreads().Get(sid)
			if sp == nil {
				continue
			}
			for _, leg := range sp.Legs {
				if leg.Closed {
					continue
				}
				contract := optutil.ResolveContract(leg.Contract, contractMap)
				dte := contract.DaysToExpiry(now)
				if dte > float64(phase1RollMinDTE) && dte < float64(phase1RollMaxDTE) {
					priceDiff := math.Abs(ctx.Close() - pos.entryPrice)
					if priceDiff <= phase1RollATRMultiplier*pos.entryATR {
						fmt.Printf("[%s] phase1 DTE roll: dte=%.1f, price within %.1f ATRs\n",
							now.Format(time.RFC3339), dte, priceDiff/pos.entryATR)
						return actionRollPhase1
					}
				}
			}
			break // only need to check one spread for DTE
		}
	}

	// Calculate combined PnL
	combinedPnL := s.calculateCombinedPnL(ctx, pos, contractMap)
	buyInitialCost := pos.buyInitialCost

	if buyInitialCost <= 0 {
		return actionKeep
	}

	// 50% profit → switch to phase 2
	if combinedPnL >= phase1FullProfitPct*buyInitialCost {
		fmt.Printf("[%s] phase1 full profit: PnL=%.6f >= 50%% of %.6f\n",
			now.Format(time.RFC3339), combinedPnL, buyInitialCost)
		return actionSwitchPhase2
	}

	// 30% profit → partial close + rebuild buy legs
	if !pos.partialClosed && combinedPnL >= phase1PartialProfitPct*buyInitialCost {
		fmt.Printf("[%s] phase1 partial profit: PnL=%.6f >= 30%% of %.6f\n",
			now.Format(time.RFC3339), combinedPnL, buyInitialCost)
		s.phase1PartialClose(ctx, pos, chain, contractMap)
	}

	return actionKeep
}

func (s *strategy) calculateCombinedPnL(ctx *backtest.BarContext, pos *activePosition, contractMap optutil.ContractMap) float64 {
	totalPnL := 0.0
	for _, sid := range pos.spreadIDs {
		sp := ctx.Spreads().Get(sid)
		if sp == nil {
			continue
		}
		totalPnL += sp.TotalRealizedPnL()
		totalPnL += sp.TotalUnrealizedPnL(func(c backtest.OptionContract) float64 {
			resolved := optutil.ResolveContract(c, contractMap)
			return resolved.MarkPrice
		})
	}
	return totalPnL
}

func (s *strategy) phase1PartialClose(ctx *backtest.BarContext, pos *activePosition, chain *backtest.OptionsChain, contractMap optutil.ContractMap) {
	now := ctx.Time()

	// Close 1/3 of sell leg quantity
	if len(pos.spreadIDs) > 0 {
		sellSpreadID := pos.spreadIDs[0] // first spread is the sell leg
		sp := ctx.Spreads().Get(sellSpreadID)
		if sp != nil && !sp.IsFullyClosed() {
			for li := range sp.Legs {
				if sp.Legs[li].Closed {
					continue
				}
				contract := optutil.ResolveContract(sp.Legs[li].Contract, contractMap)
				closePrice := s.ExitPriceMode.ExitPrice(sp.Legs[li].Side, contract)
				if optutil.IsValidPrice(closePrice) {
					ctx.CloseSpreadLegWithReason(sellSpreadID, li, closePrice, "P1减仓1/3卖方")
				}
			}

			// Reopen sell leg at 2/3 quantity
			sellOpt, sellPrice := s.pickPhase1SellLeg(now, s.filterByType(chain, s.phase1OptionType()).ExpiryRange(phase1MinDTE, phase1MaxDTE), s.phase1OptionType())
			if sellOpt != nil {
				originalN1 := phase1SellNotional / sellPrice
				newN1 := originalN1 * 2.0 / 3.0
				newSellID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
					{Contract: *sellOpt, Side: s.sellSide(), Qty: newN1, EntryPrice: sellPrice},
				}, fmt.Sprintf("P1减仓卖方重建|d=%.2f|n=%.2f", sellOpt.Delta, newN1), pos.groupID)
				if newSellID > 0 {
					ctx.SpreadGroups().AddSpread(pos.groupID, newSellID)
					pos.spreadIDs[0] = newSellID
				}
			}
		}
	}

	// Close all buy legs
	for i := 1; i < len(pos.spreadIDs); i++ {
		sid := pos.spreadIDs[i]
		sp := ctx.Spreads().Get(sid)
		if sp == nil || sp.IsFullyClosed() {
			continue
		}
		s.closeSpread(ctx, sid, contractMap, "P1减仓平买方")
	}

	// Rebuild buy legs at 2/3 position
	optType := s.phase1OptionType()
	eligible := s.filterByType(chain, optType).ExpiryRange(phase1MinDTE, phase1MaxDTE)
	if eligible.Len() == 0 {
		pos.partialClosed = true
		return
	}

	contracts := eligible.Contracts()
	expiries := s.candidateExpiries(contracts, now, phase1TargetDTE)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		sellOpt, _ := s.pickPhase1SellLeg(now, expiryChain, optType)
		if sellOpt == nil {
			continue
		}

		buyOpt1, buyPrice1, buyOpt2, buyPrice2 := s.pickPhase1BuyLegs(now, expiryChain, optType, sellOpt)
		if buyOpt1 == nil || buyOpt2 == nil {
			continue
		}

		// 2/3 of original quantity
		n2Full := phase1SellNotional / (buyPrice1 + buyPrice2)
		n2 := n2Full * 2.0 / 3.0

		var newBuyIDs []int

		buySpread1ID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt1, Side: s.buySide(), Qty: n2, EntryPrice: buyPrice1},
		}, fmt.Sprintf("P1重建买入1|d=%.2f|n=%.2f", buyOpt1.Delta, n2), pos.groupID)
		if buySpread1ID > 0 {
			ctx.SpreadGroups().AddSpread(pos.groupID, buySpread1ID)
			newBuyIDs = append(newBuyIDs, buySpread1ID)
		}

		buySpread2ID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt2, Side: s.buySide(), Qty: n2, EntryPrice: buyPrice2},
		}, fmt.Sprintf("P1重建买入2|d=%.2f|n=%.2f", buyOpt2.Delta, n2), pos.groupID)
		if buySpread2ID > 0 {
			ctx.SpreadGroups().AddSpread(pos.groupID, buySpread2ID)
			newBuyIDs = append(newBuyIDs, buySpread2ID)
		}

		// Update position's spread IDs: keep sell (idx 0), replace buys
		pos.spreadIDs = append(pos.spreadIDs[:1], newBuyIDs...)
		pos.buyInitialCost = n2 * (buyPrice1 + buyPrice2)
		pos.partialClosed = true

		fmt.Printf("[%s] phase1 partial: rebuilt buy legs in group %d\n",
			now.Format(time.RFC3339), pos.groupID)
		return
	}

	pos.partialClosed = true
}

func (s *strategy) managePhase2(ctx *backtest.BarContext, pos *activePosition, chain *backtest.OptionsChain, contractMap optutil.ContractMap) posAction {
	now := ctx.Time()

	if len(pos.spreadIDs) == 0 {
		return actionRemove
	}

	latestSpreadID := pos.spreadIDs[len(pos.spreadIDs)-1]
	sp := ctx.Spreads().Get(latestSpreadID)
	if sp == nil || sp.IsFullyClosed() {
		return actionRemove
	}

	if len(sp.Legs) < 2 {
		return actionKeep
	}

	buyLeg := sp.Legs[0]
	sellLeg := sp.Legs[1]
	if buyLeg.Closed || sellLeg.Closed {
		return actionKeep
	}

	// Check roll conditions:
	// Condition 1: current bar is opposite candle (long: bearish, short: bullish)
	barIsCounter := false
	if s.direction == directionLong {
		barIsCounter = ctx.Close() < ctx.Open() // bearish candle for long
	} else {
		barIsCounter = ctx.Close() > ctx.Open() // bullish candle for short
	}

	if !barIsCounter {
		return actionKeep
	}

	// Condition 2a: spread profit >= 50%
	buyContract := optutil.ResolveContract(buyLeg.Contract, contractMap)
	sellContract := optutil.ResolveContract(sellLeg.Contract, contractMap)
	buyMark := s.ValuationPriceMode.ExitPrice(buyLeg.Side, buyContract)
	sellMark := s.ValuationPriceMode.ExitPrice(sellLeg.Side, sellContract)

	if !math.IsNaN(buyMark) && !math.IsNaN(sellMark) {
		initialCost := buyLeg.EntryPrice - sellLeg.EntryPrice
		if initialCost > 0 {
			currentValue := buyMark - sellMark
			pnlPct := (currentValue - initialCost) / initialCost
			if pnlPct >= phase2RollProfitPct {
				fmt.Printf("[%s] phase2 roll: spread profit %.0f%%\n", now.Format(time.RFC3339), pnlPct*100)
				return actionRoll
			}
		}
	}

	// Condition 2b: buy leg delta increased by 0.2
	if math.Abs(buyContract.Delta)-math.Abs(pos.phase2BuyDelta) >= phase2RollDeltaIncrease {
		fmt.Printf("[%s] phase2 roll: delta increase %.4f → %.4f\n",
			now.Format(time.RFC3339), pos.phase2BuyDelta, buyContract.Delta)
		return actionRoll
	}

	return actionKeep
}

func (s *strategy) rollPhase2InGroup(ctx *backtest.BarContext, chain *backtest.OptionsChain, groupID int, amount float64) *activePosition {
	if chain == nil || chain.Len() == 0 {
		ctx.SpreadGroups().Close(groupID)
		return nil
	}

	now := ctx.Time()
	optType := s.phase2OptionType()

	hvPR := ctx.Ind(hvPercentileColumn)
	if math.IsNaN(hvPR) {
		hvPR = 50
	}
	ivPR := ctx.Ind(ivPercentileColumn)
	if math.IsNaN(ivPR) {
		ivPR = 50
	}
	buyDelta := clamp(phase2MinDelta, phase2MaxDelta, (2*hvPR+ivPR)/300.0)

	eligible := s.filterByType(chain, optType).ExpiryRange(phase2MinDTE, phase2MaxDTE)
	if eligible.Len() == 0 {
		ctx.SpreadGroups().Close(groupID)
		return nil
	}

	contracts := eligible.Contracts()
	expiries := s.candidateExpiries(contracts, now, phase2TargetDTE)

	for _, expiry := range expiries {
		expiryContracts := contractsForExpiry(contracts, expiry)
		expiryChain := backtest.NewOptionsChain(expiryContracts, now)

		targetDelta := buyDelta
		if optType == backtest.Put {
			targetDelta = -buyDelta
		}
		buyOpt, buyPrice := s.pickPhase2BuyLeg(now, expiryChain, optType, targetDelta)
		if buyOpt == nil {
			continue
		}

		sellOpt, sellPrice := s.pickPhase2SellLeg(now, expiryContracts, optType, buyOpt)
		if sellOpt == nil {
			continue
		}

		spreadCost := buyPrice - sellPrice
		if spreadCost <= 0 {
			continue
		}

		qty := amount / spreadCost
		if qty <= 0 {
			continue
		}

		spreadID := ctx.OpenSpreadInGroup([]backtest.SpreadLeg{
			{Contract: *buyOpt, Side: s.buySide(), Qty: qty, EntryPrice: buyPrice},
			{Contract: *sellOpt, Side: s.sellSide(), Qty: qty, EntryPrice: sellPrice},
		}, fmt.Sprintf("P2换仓|d=%.2f/%.2f|n=%.2f", buyOpt.Delta, sellOpt.Delta, qty), groupID)

		if spreadID <= 0 {
			continue
		}

		ctx.SpreadGroups().AddSpread(groupID, spreadID)

		pos := &activePosition{
			groupID:        groupID,
			phase:          phaseTrend,
			spreadIDs:      []int{spreadID},
			entryPrice:     ctx.Close(),
			entryATR:       ctx.Ind(atrColumn),
			phase2BuyDelta: buyOpt.Delta,
			phase2Amount:   amount,
		}

		fmt.Printf("[%s] phase2 rolled in group %d: spread=%d, buy=%s d=%.2f, sell=%s d=%.2f, n=%.4f\n",
			now.Format(time.RFC3339), groupID, spreadID,
			buyOpt.Symbol, buyOpt.Delta, sellOpt.Symbol, sellOpt.Delta, qty)
		return pos
	}

	ctx.SpreadGroups().Close(groupID)
	return nil
}

// ---------------------------------------------------------------------------
// Close helpers
// ---------------------------------------------------------------------------

func (s *strategy) closeAllPositions(ctx *backtest.BarContext, contractMap optutil.ContractMap, reason string) {
	for i := range s.positions {
		s.closePositionSpreads(ctx, &s.positions[i], contractMap, reason)
		ctx.SpreadGroups().Close(s.positions[i].groupID)
	}
	s.positions = nil
}

func (s *strategy) closePositionSpreads(ctx *backtest.BarContext, pos *activePosition, contractMap optutil.ContractMap, reason string) {
	for _, sid := range pos.spreadIDs {
		s.closeSpread(ctx, sid, contractMap, reason)
	}
}

func (s *strategy) closeSpread(ctx *backtest.BarContext, spreadID int, contractMap optutil.ContractMap, reason string) {
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
		if optutil.IsValidPrice(closePrice) {
			ctx.CloseSpreadLegWithReason(spreadID, i, closePrice, reason)
		}
	}
}

// ---------------------------------------------------------------------------
// Direction helpers
// ---------------------------------------------------------------------------

func (s *strategy) phase1OptionType() backtest.OptionType {
	if s.direction == directionLong {
		return backtest.Call
	}
	return backtest.Put
}

func (s *strategy) phase2OptionType() backtest.OptionType {
	if s.direction == directionLong {
		return backtest.Call
	}
	return backtest.Put
}

func (s *strategy) sellSide() backtest.Side { return backtest.Sell }
func (s *strategy) buySide() backtest.Side  { return backtest.Buy }

func (s *strategy) filterByType(chain *backtest.OptionsChain, optType backtest.OptionType) *backtest.OptionsChain {
	if optType == backtest.Call {
		return chain.Calls()
	}
	return chain.Puts()
}

// ---------------------------------------------------------------------------
// Shared option selection helpers
// ---------------------------------------------------------------------------

func (s *strategy) candidateExpiries(contracts []backtest.OptionContract, now time.Time, targetDTE int) []time.Time {
	seen := make(map[time.Time]struct{})
	var expiries []time.Time
	for _, c := range contracts {
		if _, ok := seen[c.Expiration]; ok {
			continue
		}
		seen[c.Expiration] = struct{}{}
		expiries = append(expiries, c.Expiration)
	}

	target := float64(targetDTE)
	sort.Slice(expiries, func(i, j int) bool {
		di := math.Abs(expiries[i].Sub(now).Hours()/24 - target)
		dj := math.Abs(expiries[j].Sub(now).Hours()/24 - target)
		if di != dj {
			return di < dj
		}
		return expiries[i].Before(expiries[j])
	})
	return expiries
}

func contractsForExpiry(contracts []backtest.OptionContract, expiry time.Time) []backtest.OptionContract {
	var filtered []backtest.OptionContract
	for _, c := range contracts {
		if c.Expiration.Equal(expiry) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// Signal loading
// ---------------------------------------------------------------------------

func loadSignals(relPath string) ([]signalEvent, error) {
	path := relPath
	if _, err := os.Stat(path); err != nil {
		wd, _ := os.Getwd()
		path = wd + "/" + relPath
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("signal file not found: %s (tried cwd=%s)", relPath, wd)
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
	var events []signalEvent

	for i, record := range records {
		if i == 0 {
			continue // skip header
		}
		if len(record) < 4 {
			continue
		}
		sigStr := strings.ToLower(strings.TrimSpace(record[3]))
		if !strings.Contains(sigStr, "init") {
			continue
		}
		dateStr := strings.TrimSpace(record[2])
		var ts time.Time
		layouts := []string{"2006-01-02 15:04", "2006/1/2 15:04", "2006/01/02 15:04"}
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
		events = append(events, signalEvent{time: ts.UTC()})
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].time.Before(events[j].time)
	})
	return events, nil
}

func buildSignalColumn(timestamps []time.Time, events []signalEvent) []float64 {
	values := make([]float64, len(timestamps))
	if len(timestamps) == 0 || len(events) == 0 {
		return values
	}

	for _, event := range events {
		idx := primaryBarIndexForSignal(timestamps, event.time)
		if idx >= 0 {
			values[idx] = 1
		}
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

func primaryBarIndexForSignal(timestamps []time.Time, eventTime time.Time) int {
	if len(timestamps) == 0 || eventTime.IsZero() {
		return -1
	}
	idx := sort.Search(len(timestamps), func(i int) bool {
		return timestamps[i].After(eventTime)
	})
	if idx == 0 {
		if timestamps[0].After(eventTime) {
			return -1
		}
		return 0
	}
	if idx >= len(timestamps) {
		return len(timestamps) - 1
	}
	return idx - 1
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

func alignSeriesValues(targetTimes, sourceTimes []time.Time, sourceValues []float64) ([]float64, error) {
	if len(sourceTimes) != len(sourceValues) {
		return nil, fmt.Errorf("timestamp/value length mismatch: %d vs %d", len(sourceTimes), len(sourceValues))
	}
	out := make([]float64, len(targetTimes))
	for i := range out {
		out[i] = math.NaN()
	}
	for i, ts := range targetTimes {
		idx := sort.Search(len(sourceTimes), func(j int) bool {
			return sourceTimes[j].After(ts)
		}) - 1
		if idx >= 0 && idx < len(sourceValues) {
			out[i] = sourceValues[idx]
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Indicator helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func clamp(low, high, value float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func resolveDirection() tradeDirection {
	raw := strings.TrimSpace(os.Getenv(directionEnv))
	switch strings.ToLower(raw) {
	case "short", "s", "put":
		return directionShort
	default:
		return directionLong
	}
}

func resolveSignalLevel() string {
	raw := strings.TrimSpace(os.Getenv(signalLevelEnv))
	switch strings.ToLower(raw) {
	case "1d":
		return "1d"
	default:
		return "12h"
	}
}

func buildCSVPath(level string, dir tradeDirection) string {
	suffix := "_long.csv"
	if dir == directionShort {
		suffix = "_short.csv"
	}
	return signalCSVDirPrefix + level + suffix
}

func directionLabel(dir tradeDirection) string {
	if dir == directionShort {
		return "short"
	}
	return "long"
}

// suppress unused import warnings
var _ = strconv.Atoi
