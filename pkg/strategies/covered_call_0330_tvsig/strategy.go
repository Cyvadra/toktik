package coveredcall0330tvsig

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	strategyName  = "covered-call-0330-tvsig"
	strategyAlias = "covered_call_0330_tvsig"

	signalSourceEnv = "COVERED_CALL_0330_TVSIG_SIGNAL_SOURCE"

	callAmountTotal = 10.0 // BTC – total for the 2 call-spread tranches
	putAmountInit   = 7.0  // BTC – initial amount for protective PUT
	putDecayFactor  = 0.80 // each roll multiplies protective amount by this

	callTargetDTE = 25 // target DTE for bear call spreads
	callBiasDTE   = 8  // ±8 days around callTargetDTE

	putTargetDTE = 35 // target DTE for protective PUT (and rolled CALL)
	putBiasDTE   = 10 // ±10 days around putTargetDTE

	sellCallTargetDelta = 0.35  // SELL call near this delta
	buyCallTargetDelta  = 0.10  // BUY call hedge near this delta
	buyPutTargetDelta   = -0.25 // BUY put near this delta (negative)

	// call-spread take-profit levels
	callTP1ProfitPct          = 0.60 // 60%  profit → close 40%-tranche
	callTP2ProfitPct          = 0.85 // 85%  profit → close 60%-tranche
	callStopLossPriceMultiple = 2.0  // sold call price >= 2x entry → close whole order group

	callTranche0Pct = 0.40 // tranche-0 fraction of total qty
	callTranche1Pct = 0.60 // tranche-1 fraction of total qty

	// protective-leg roll triggers
	protRollDeltaLimit  = 0.50 // abs(delta) ≥ 0.5
	protRollProfitLimit = 0.50 // unrealised profit ≥ 50%

	expiryCloseLeadDays = 1 // close 1 day before expiry

	signalPath12h = "pkg/strategies/covered_call_0330_tvsig/12h.txt"
	signalPath6h  = "pkg/strategies/covered_call_0330_tvsig/6h.txt"

	txtTimeLayout = "Jan 2, 2006, 15:04" // format used in signal txt files (UTC)

	positionGroupTag   = "covered-call-0330-tvsig"
	positionGroupDecay = 1.0

	openNoteCallTranche0  = "开仓 | 空call spread (40%仓)"
	openNoteCallTranche1  = "开仓 | 空call spread (60%仓)"
	openNoteProtPut       = "开仓 | 多put保护"
	openNoteRolledCall    = "换仓 | 多call"
	closeNoteExpiryCall   = "到期平仓 | 空call spread"
	closeNoteExpiryProt   = "到期平仓 | 保护仓"
	closeNoteTP1Call      = "止盈40% | 空call spread 60%盈利"
	closeNoteTP2Call      = "止盈全仓 | 空call spread 85%盈利"
	closeNoteStopLossCall = "止损全仓 | Sold CALL价格翻倍"
	closeNoteRollProt     = "换仓 | 平保护仓"
	closeNoteOpenRollback = "开仓回滚"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    strategyName,
		Aliases: []string{strategyAlias},
		Groups:  []string{"options", "spread", "timed"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &strategy{
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
			}, nil
		},
	})
}

// strategy implements the covered-call + protective-put/call spread logic.
//
//	Entry (triggered by external UTC signal):
//	  • 2× bear-call spread (sell delta~0.35 call, buy delta~0.10 call hedge)
//	    DTE ~25±8 days, total 10 BTC
//	    – tranche-0 (40% qty): take-profit at 60% spread profit
//	    – tranche-1 (60% qty): take-profit at 85% spread profit
//	  • 1× protective PUT  (buy delta~-0.25)
//	    DTE ~35±10 days, initial 7 BTC
//
//	Protective-leg roll: when abs(delta) ≥ 0.5 OR unrealised profit ≥ 50%
//	  close current leg, reopen as BUY CALL (same DTE/|delta| params), 80% amount.
//
//	Stop-loss: if any open sold CALL marks at >= 2x its entry price,
//	  close the whole order group.
//
//	expiry close: all legs are closed 1 day before expiry.
type strategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	entryTimes           map[int64]struct{}
	processedSignalTimes map[int64]struct{}

	// callSpreadIDs[0] = 40%-tranche, callSpreadIDs[1] = 60%-tranche
	callSpreadIDs [2]int

	// protective leg – starts as PUT, rolls to CALL
	protLegID      int
	protLegIsCall  bool
	protRollAmount float64 // decays with each roll

	positionGroupID int
}

func (s *strategy) Name() string { return "CoveredCall0330TVSig" }

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
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()
	signalSource := mustGetSignalSource()
	entryTimes, err := loadSignalTimesForSource(signalSource)
	if err != nil {
		return fmt.Errorf("load signal times for %s=%q: %w", signalSourceEnv, signalSource, err)
	}
	s.entryTimes = entryTimes
	fmt.Printf("[%s] using %s=%q for external entry signals (allowed: 6h, 12h, both)\n", s.Name(), signalSourceEnv, signalSource)
	ctx.SetParam("call_amount_total", callAmountTotal)
	ctx.SetParam("put_amount_init", putAmountInit)
	ctx.SetParam("signal_count", float64(len(s.entryTimes)))
	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}
	entrySignal := make([]float64, primary.Len())
	for i, ts := range primary.Timestamps() {
		if _, ok := s.entryTimes[ts.UTC().Unix()]; ok {
			entrySignal[i] = 1
		}
	}
	return primary.SetColumn("entry_signal", entrySignal)
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	contractMap := buildContractMap(chain)

	hasOpen := s.managePositions(ctx, chain, contractMap)
	if hasOpen {
		return
	}

	if ctx.Ind("entry_signal") != 1 {
		return
	}

	signalTime := ctx.Time().UTC().Unix()
	if s.isSignalProcessed(signalTime) {
		return
	}
	s.markSignalProcessed(signalTime)

	s.tryOpenStructure(ctx, chain)
}

// tryOpenStructure opens the full 3-leg structure on a signal bar.
func (s *strategy) tryOpenStructure(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 {
		return
	}

	// Reset protective-leg amount for each new entry cycle.
	s.protRollAmount = putAmountInit
	s.protLegIsCall = false

	// ── Bear call spread ─────────────────────────────────────────────────────
	sellCall, sellCallPrice, buyCall, buyCallPrice, ok := s.selectCallSpread(ctx.Time(), chain)
	if !ok {
		return
	}

	spreadCredit := sellCallPrice - buyCallPrice
	if spreadCredit <= 0 {
		fmt.Printf("[%s] skip entry: non-positive call spread credit=%.6f\n",
			ctx.Time().Format(time.RFC3339), spreadCredit)
		return
	}

	totalQty := callAmountTotal / spreadCredit
	if totalQty <= 0 {
		return
	}

	qty0 := totalQty * callTranche0Pct // 40%
	qty1 := totalQty * callTranche1Pct // 60%

	// ── Protective PUT ───────────────────────────────────────────────────────
	protPut, protPutPrice, ok2 := s.selectProtectivePut(ctx.Time(), chain)
	if !ok2 {
		return
	}

	putQty := s.protRollAmount / protPutPrice
	if putQty <= 0 {
		return
	}

	groupID := s.openGroupID(ctx)

	// Open PUT first; roll back everything if any subsequent open fails.
	protID := s.openSpreadInGroup(ctx, []backtest.SpreadLeg{
		{Contract: *protPut, Side: backtest.Buy, Qty: putQty, EntryPrice: protPutPrice},
	}, openNoteProtPut, groupID)
	if protID <= 0 {
		s.closeGroup(ctx, groupID)
		return
	}

	callID0 := s.openSpreadInGroup(ctx, []backtest.SpreadLeg{
		{Contract: *sellCall, Side: backtest.Sell, Qty: qty0, EntryPrice: sellCallPrice},
		{Contract: *buyCall, Side: backtest.Buy, Qty: qty0, EntryPrice: buyCallPrice},
	}, openNoteCallTranche0, groupID)
	if callID0 <= 0 {
		s.rollbackSingleLeg(ctx, protID, protPutPrice)
		s.closeGroup(ctx, groupID)
		return
	}

	callID1 := s.openSpreadInGroup(ctx, []backtest.SpreadLeg{
		{Contract: *sellCall, Side: backtest.Sell, Qty: qty1, EntryPrice: sellCallPrice},
		{Contract: *buyCall, Side: backtest.Buy, Qty: qty1, EntryPrice: buyCallPrice},
	}, openNoteCallTranche1, groupID)
	if callID1 <= 0 {
		s.rollbackSingleLeg(ctx, protID, protPutPrice)
		s.rollbackCallSpread(ctx, callID0, sellCallPrice, buyCallPrice)
		s.closeGroup(ctx, groupID)
		return
	}

	s.callSpreadIDs[0] = callID0
	s.callSpreadIDs[1] = callID1
	s.protLegID = protID
	s.positionGroupID = groupID

	fmt.Printf("[%s] entry: sell=%s(d=%.4f)@%.6f buy=%s(d=%.4f)@%.6f credit=%.6f qty=[%.4f+%.4f]; put=%s(d=%.4f)@%.6f qty=%.4f\n",
		ctx.Time().Format(time.RFC3339),
		sellCall.Symbol, sellCall.Delta, sellCallPrice,
		buyCall.Symbol, buyCall.Delta, buyCallPrice,
		spreadCredit, qty0, qty1,
		protPut.Symbol, protPut.Delta, protPutPrice, putQty)
}

// managePositions manages all open legs, returns true if any position is still open.
func (s *strategy) managePositions(ctx *backtest.BarContext, chain *backtest.OptionsChain, contractMap map[string]backtest.OptionContract) bool {
	now := ctx.Time()

	// ── Call spread tranches ────────────────────────────────────────────────
	for i := range s.callSpreadIDs {
		spreadID := s.callSpreadIDs[i]
		if spreadID <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(spreadID)
		if sp == nil || sp.IsFullyClosed() {
			s.callSpreadIDs[i] = 0
			continue
		}
		if len(sp.Legs) < 2 {
			continue
		}

		sellLeg := sp.Legs[0] // sell call
		buyLeg := sp.Legs[1]  // buy call hedge

		if sellLeg.Closed && buyLeg.Closed {
			s.callSpreadIDs[i] = 0
			continue
		}

		sellContract := resolveContract(sellLeg.Contract, contractMap)
		buyContract := resolveContract(buyLeg.Contract, contractMap)

		// Expiry close: 1 day before
		if !sellLeg.Closed && sellContract.DaysToExpiry(now) <= float64(expiryCloseLeadDays) {
			s.closeCallSpread(ctx, spreadID, contractMap, closeNoteExpiryCall)
			s.callSpreadIDs[i] = 0
			fmt.Printf("[%s] call tranche %d: expiry close\n", now.Format(time.RFC3339), i)
			continue
		}

		// Group stop-loss: if the short call doubles from its entry price,
		// close every remaining leg in the current order group.
		if !sellLeg.Closed {
			sellMark := s.ValuationPriceMode.ExitPrice(backtest.Sell, sellContract)
			if soldCallHitStopLoss(sellLeg.EntryPrice, sellMark) {
				fmt.Printf("[%s] call tranche %d stop-loss: sell=%s entry=%.6f mark=%.6f threshold=%.6f\n",
					now.Format(time.RFC3339), i, sellContract.Symbol, sellLeg.EntryPrice, sellMark,
					sellLeg.EntryPrice*callStopLossPriceMultiple)
				s.closeEntireOrderGroup(ctx, contractMap, closeNoteStopLossCall)
				return false
			}
		}

		// Take-profit check
		if !sellLeg.Closed && !buyLeg.Closed {
			sellMark := s.ValuationPriceMode.ExitPrice(backtest.Sell, sellContract)
			buyMark := s.ValuationPriceMode.ExitPrice(backtest.Buy, buyContract)
			if !math.IsNaN(sellMark) && !math.IsNaN(buyMark) {
				entryCredit := sellLeg.EntryPrice - buyLeg.EntryPrice
				currentCloseCost := sellMark - buyMark
				if entryCredit > 0 {
					pnlPct := (entryCredit - currentCloseCost) / entryCredit
					var shouldClose bool
					var closeReason string
					switch {
					case i == 0 && pnlPct >= callTP1ProfitPct:
						shouldClose = true
						closeReason = closeNoteTP1Call
					case i == 1 && pnlPct >= callTP2ProfitPct:
						shouldClose = true
						closeReason = closeNoteTP2Call
					}
					if shouldClose {
						s.closeCallSpread(ctx, spreadID, contractMap, closeReason)
						s.callSpreadIDs[i] = 0
						fmt.Printf("[%s] call tranche %d closed: pnl=%.1f%%, reason=%s\n",
							now.Format(time.RFC3339), i, pnlPct*100, closeReason)
					}
				}
			}
		}
	}

	// ── Protective leg ──────────────────────────────────────────────────────
	if s.protLegID > 0 {
		sp := ctx.Spreads().Get(s.protLegID)
		if sp == nil || sp.IsFullyClosed() || len(sp.Legs) == 0 || sp.Legs[0].Closed {
			s.protLegID = 0
		} else {
			leg := sp.Legs[0]
			contract := resolveContract(leg.Contract, contractMap)

			if contract.DaysToExpiry(now) <= float64(expiryCloseLeadDays) {
				closePrice := s.ExitPriceMode.ExitPrice(leg.Side, contract)
				if !math.IsNaN(closePrice) && closePrice > 0 {
					if ctx.CloseSpreadLegWithReason(s.protLegID, 0, closePrice, closeNoteExpiryProt) {
						s.protLegID = 0
					}
				}
			} else {
				absDelta := math.Abs(contract.Delta)
				markPrice := s.ValuationPriceMode.ExitPrice(leg.Side, contract)
				pnlPct := math.NaN()
				if !math.IsNaN(markPrice) && leg.EntryPrice > 0 {
					pnlPct = (markPrice - leg.EntryPrice) / leg.EntryPrice
				}

				if absDelta >= protRollDeltaLimit || (!math.IsNaN(pnlPct) && pnlPct >= protRollProfitLimit) {
					closePrice := s.ExitPriceMode.ExitPrice(leg.Side, contract)
					if !math.IsNaN(closePrice) && closePrice > 0 {
						rollReason := protRollReason(absDelta, pnlPct)
						if ctx.CloseSpreadLegWithReason(s.protLegID, 0, closePrice, rollReason) {
							s.protLegID = 0
							s.protRollAmount *= putDecayFactor
							s.protLegIsCall = true
							fmt.Printf("[%s] protective leg rolled: %s, newAmount=%.4f\n",
								now.Format(time.RFC3339), rollReason, s.protRollAmount)
							s.reopenProtectiveLeg(ctx, chain)
						}
					}
				}
			}
		}
	}

	stillActive := s.hasAnyOpen(ctx)
	if !stillActive {
		s.closeGroup(ctx, s.positionGroupID)
	}
	return stillActive
}

// reopenProtectiveLeg opens a fresh BUY CALL after rolling the protective leg.
func (s *strategy) reopenProtectiveLeg(ctx *backtest.BarContext, chain *backtest.OptionsChain) {
	if chain == nil || chain.Len() == 0 || s.protRollAmount <= 0 {
		return
	}

	opt, price, ok := s.selectProtectiveCall(ctx.Time(), chain)
	if !ok {
		fmt.Printf("[%s] protective roll: no suitable call found\n", ctx.Time().Format(time.RFC3339))
		return
	}

	qty := s.protRollAmount / price
	if qty <= 0 {
		return
	}

	spreadID := s.openSpreadInGroup(ctx, []backtest.SpreadLeg{
		{Contract: *opt, Side: backtest.Buy, Qty: qty, EntryPrice: price},
	}, openNoteRolledCall, s.positionGroupID)
	if spreadID <= 0 {
		return
	}

	s.protLegID = spreadID
	s.protLegIsCall = true
	fmt.Printf("[%s] protective call opened: %s delta=%.4f price=%.6f qty=%.4f amount=%.4f\n",
		ctx.Time().Format(time.RFC3339), opt.Symbol, opt.Delta, price, qty, s.protRollAmount)
}

// ── Option-selection helpers ──────────────────────────────────────────────────

// selectCallSpread returns (sellCall, sellPrice, buyCall, buyPrice, ok) for the
// bear call spread: SELL delta~0.35, BUY delta~0.10 hedge, DTE ~25±8.
func (s *strategy) selectCallSpread(now time.Time, chain *backtest.OptionsChain) (
	*backtest.OptionContract, float64,
	*backtest.OptionContract, float64,
	bool,
) {
	calls := chain.Calls().ExpiryRange(callTargetDTE-callBiasDTE, callTargetDTE+callBiasDTE)
	if calls.Len() == 0 {
		fmt.Printf("[%s] call spread: no contracts in DTE [%d,%d]\n",
			now.Format(time.RFC3339), callTargetDTE-callBiasDTE, callTargetDTE+callBiasDTE)
		return nil, 0, nil, 0, false
	}

	for _, expiry := range uniqueExpiriesNearest(calls.Contracts(), now, callTargetDTE) {
		ec := backtest.NewOptionsChain(contractsForExpiry(calls.Contracts(), expiry), now)

		sellCall, sellPrice := pickFirstWithPrice(ec.SortByDelta(sellCallTargetDelta), backtest.Sell, s.EntryPriceMode)
		if sellCall == nil {
			continue
		}
		buyCall, buyPrice := pickFirstWithPrice(ec.SortByDelta(buyCallTargetDelta), backtest.Buy, s.EntryPriceMode)
		if buyCall == nil {
			continue
		}
		if sellCall.Delta <= buyCall.Delta {
			continue
		}
		if sellPrice-buyPrice <= 0 {
			continue
		}

		fmt.Printf("[%s] call spread: expiry=%s sell=%s(d=%.4f)@%.6f buy=%s(d=%.4f)@%.6f credit=%.6f\n",
			now.Format(time.RFC3339), expiry.Format("2006-01-02"),
			sellCall.Symbol, sellCall.Delta, sellPrice,
			buyCall.Symbol, buyCall.Delta, buyPrice,
			sellPrice-buyPrice)
		return sellCall, sellPrice, buyCall, buyPrice, true
	}

	fmt.Printf("[%s] call spread: no suitable expiry found\n", now.Format(time.RFC3339))
	return nil, 0, nil, 0, false
}

// selectProtectivePut returns the best BUY PUT (delta~-0.25, DTE ~35±10).
func (s *strategy) selectProtectivePut(now time.Time, chain *backtest.OptionsChain) (*backtest.OptionContract, float64, bool) {
	puts := chain.Puts().ExpiryRange(putTargetDTE-putBiasDTE, putTargetDTE+putBiasDTE)
	if puts.Len() == 0 {
		fmt.Printf("[%s] protective put: no contracts in DTE [%d,%d]\n",
			now.Format(time.RFC3339), putTargetDTE-putBiasDTE, putTargetDTE+putBiasDTE)
		return nil, 0, false
	}

	for _, expiry := range uniqueExpiriesNearest(puts.Contracts(), now, putTargetDTE) {
		ec := backtest.NewOptionsChain(contractsForExpiry(puts.Contracts(), expiry), now)
		opt, price := pickFirstWithPrice(ec.SortByDelta(buyPutTargetDelta), backtest.Buy, s.EntryPriceMode)
		if opt == nil {
			continue
		}
		fmt.Printf("[%s] protective put: %s delta=%.4f price=%.6f expiry=%s\n",
			now.Format(time.RFC3339), opt.Symbol, opt.Delta, price, expiry.Format("2006-01-02"))
		return opt, price, true
	}

	fmt.Printf("[%s] protective put: no suitable expiry found\n", now.Format(time.RFC3339))
	return nil, 0, false
}

// selectProtectiveCall returns the best BUY CALL (delta~|buyPutTargetDelta|, DTE ~35±10).
func (s *strategy) selectProtectiveCall(now time.Time, chain *backtest.OptionsChain) (*backtest.OptionContract, float64, bool) {
	targetDelta := math.Abs(buyPutTargetDelta) // ~0.25
	calls := chain.Calls().ExpiryRange(putTargetDTE-putBiasDTE, putTargetDTE+putBiasDTE)
	if calls.Len() == 0 {
		return nil, 0, false
	}

	for _, expiry := range uniqueExpiriesNearest(calls.Contracts(), now, putTargetDTE) {
		ec := backtest.NewOptionsChain(contractsForExpiry(calls.Contracts(), expiry), now)
		opt, price := pickFirstWithPrice(ec.SortByDelta(targetDelta), backtest.Buy, s.EntryPriceMode)
		if opt == nil {
			continue
		}
		fmt.Printf("[%s] protective call (roll): %s delta=%.4f price=%.6f expiry=%s\n",
			now.Format(time.RFC3339), opt.Symbol, opt.Delta, price, expiry.Format("2006-01-02"))
		return opt, price, true
	}

	return nil, 0, false
}

// ── Position management helpers ───────────────────────────────────────────────

func (s *strategy) closeCallSpread(ctx *backtest.BarContext, spreadID int, contractMap map[string]backtest.OptionContract, reason string) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i, leg := range sp.Legs {
		if leg.Closed {
			continue
		}
		contract := resolveContract(leg.Contract, contractMap)
		closePrice := s.ExitPriceMode.ExitPrice(leg.Side, contract)
		if !math.IsNaN(closePrice) && closePrice > 0 {
			ctx.CloseSpreadLegWithReason(spreadID, i, closePrice, reason)
		}
	}
}

func (s *strategy) closeProtectiveLeg(ctx *backtest.BarContext, spreadID int, contractMap map[string]backtest.OptionContract, reason string) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil || len(sp.Legs) == 0 {
		return
	}
	leg := sp.Legs[0]
	if leg.Closed {
		return
	}
	contract := resolveContract(leg.Contract, contractMap)
	closePrice := s.ExitPriceMode.ExitPrice(leg.Side, contract)
	if !math.IsNaN(closePrice) && closePrice > 0 {
		ctx.CloseSpreadLegWithReason(spreadID, 0, closePrice, reason)
	}
}

func (s *strategy) closeEntireOrderGroup(ctx *backtest.BarContext, contractMap map[string]backtest.OptionContract, reason string) {
	for _, spreadID := range s.callSpreadIDs {
		if spreadID > 0 {
			s.closeCallSpread(ctx, spreadID, contractMap, reason)
		}
	}
	if s.protLegID > 0 {
		s.closeProtectiveLeg(ctx, s.protLegID, contractMap, reason)
	}
	if !s.hasAnyOpen(ctx) {
		s.closeGroup(ctx, s.positionGroupID)
	}
}

func (s *strategy) rollbackSingleLeg(ctx *backtest.BarContext, spreadID int, entryPrice float64) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i, leg := range sp.Legs {
		if !leg.Closed {
			ctx.CloseSpreadLegWithReason(spreadID, i, entryPrice, closeNoteOpenRollback)
		}
	}
}

func (s *strategy) rollbackCallSpread(ctx *backtest.BarContext, spreadID int, sellPrice, buyPrice float64) {
	sp := ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	prices := []float64{sellPrice, buyPrice}
	for i, leg := range sp.Legs {
		if !leg.Closed && i < len(prices) {
			ctx.CloseSpreadLegWithReason(spreadID, i, prices[i], closeNoteOpenRollback)
		}
	}
}

func (s *strategy) hasAnyOpen(ctx *backtest.BarContext) bool {
	for i, id := range s.callSpreadIDs {
		if id <= 0 {
			continue
		}
		sp := ctx.Spreads().Get(id)
		if sp == nil || sp.IsFullyClosed() {
			s.callSpreadIDs[i] = 0
			continue
		}
		return true
	}
	if s.protLegID > 0 {
		sp := ctx.Spreads().Get(s.protLegID)
		if sp == nil || sp.IsFullyClosed() {
			s.protLegID = 0
		} else {
			return true
		}
	}
	return false
}

func (s *strategy) openGroupID(ctx *backtest.BarContext) int {
	if ctx.SpreadGroups() == nil {
		return 0
	}
	return ctx.SpreadGroups().Open(positionGroupTag, callAmountTotal, positionGroupDecay, ctx.Time())
}

func (s *strategy) closeGroup(ctx *backtest.BarContext, groupID int) {
	if groupID <= 0 {
		return
	}
	if ctx.SpreadGroups() != nil {
		ctx.SpreadGroups().Close(groupID)
	}
	if s.positionGroupID == groupID {
		s.positionGroupID = 0
	}
}

func (s *strategy) openSpreadInGroup(ctx *backtest.BarContext, legs []backtest.SpreadLeg, tag string, groupID int) int {
	if groupID > 0 && ctx.SpreadGroups() != nil {
		spreadID := ctx.OpenSpreadInGroup(legs, tag, groupID)
		if spreadID > 0 {
			ctx.SpreadGroups().AddSpread(groupID, spreadID)
		}
		return spreadID
	}
	return ctx.OpenSpread(legs, tag)
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
	if s.entryTimes == nil {
		s.entryTimes = map[int64]struct{}{}
	}
	if s.processedSignalTimes == nil {
		s.processedSignalTimes = map[int64]struct{}{}
	}
	s.protRollAmount = putAmountInit
}

func (s *strategy) isSignalProcessed(ts int64) bool {
	_, ok := s.processedSignalTimes[ts]
	return ok
}

func (s *strategy) markSignalProcessed(ts int64) {
	if s.processedSignalTimes == nil {
		s.processedSignalTimes = map[int64]struct{}{}
	}
	s.processedSignalTimes[ts] = struct{}{}
}

func mustGetSignalSource() string {
	raw := strings.TrimSpace(os.Getenv(signalSourceEnv))
	if raw == "" {
		fmt.Printf("[%s] set environment variable %s to one of: 6h, 12h, both\n", strategyName, signalSourceEnv)
		panic(fmt.Sprintf("missing required environment variable %s", signalSourceEnv))
	}

	source := strings.ToLower(raw)
	switch source {
	case "6h", "12h", "both":
		return source
	default:
		fmt.Printf("[%s] invalid %s=%q; expected one of: 6h, 12h, both\n", strategyName, signalSourceEnv, raw)
		panic(fmt.Sprintf("invalid environment variable %s=%q", signalSourceEnv, raw))
	}
}

// ── Stand-alone helpers ────────────────────────────────────────────────────────

func protRollReason(absDelta, pnlPct float64) string {
	hasDelta := absDelta >= protRollDeltaLimit
	hasProfit := !math.IsNaN(pnlPct) && pnlPct >= protRollProfitLimit
	switch {
	case hasDelta && hasProfit:
		return fmt.Sprintf("保护仓换仓 delta=%.2f pnl=%.0f%%", absDelta, pnlPct*100)
	case hasDelta:
		return fmt.Sprintf("保护仓换仓 delta=%.2f", absDelta)
	default:
		return fmt.Sprintf("保护仓换仓 pnl=%.0f%%", pnlPct*100)
	}
}

func soldCallHitStopLoss(entryPrice, markPrice float64) bool {
	if entryPrice <= 0 || math.IsNaN(markPrice) || math.IsInf(markPrice, 0) {
		return false
	}
	return markPrice >= entryPrice*callStopLossPriceMultiple
}

// pickFirstWithPrice returns the first contract in candidates that has a valid
// entry price for the given side.
func pickFirstWithPrice(candidates []backtest.OptionContract, side backtest.Side, mode backtest.OptionPriceMode) (*backtest.OptionContract, float64) {
	for i := range candidates {
		p := mode.EntryPrice(side, candidates[i])
		if !math.IsNaN(p) && p > 0 {
			return &candidates[i], p
		}
	}
	return nil, 0
}

// uniqueExpiriesNearest returns the unique expiry dates within the provided
// contract slice, sorted by closeness to targetDTE (nearest first).
func uniqueExpiriesNearest(contracts []backtest.OptionContract, now time.Time, targetDTE int) []time.Time {
	seen := make(map[int64]time.Time)
	for _, c := range contracts {
		key := c.Expiration.UTC().Unix()
		seen[key] = c.Expiration
	}
	expiries := make([]time.Time, 0, len(seen))
	for _, exp := range seen {
		expiries = append(expiries, exp)
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

// contractsForExpiry filters contracts to a single expiry date (UTC comparison).
func contractsForExpiry(contracts []backtest.OptionContract, expiry time.Time) []backtest.OptionContract {
	var out []backtest.OptionContract
	exp := expiry.UTC()
	for _, c := range contracts {
		if c.Expiration.UTC().Equal(exp) {
			out = append(out, c)
		}
	}
	return out
}

func resolveContract(c backtest.OptionContract, m map[string]backtest.OptionContract) backtest.OptionContract {
	if m == nil {
		return c
	}
	if updated, ok := m[c.Symbol]; ok {
		return updated
	}
	return c
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

// ── Signal file loading ────────────────────────────────────────────────────────

func loadSignalTimesForSource(source string) (map[int64]struct{}, error) {
	switch source {
	case "6h":
		return loadSignalTimesFromMultiple(signalPath6h)
	case "12h":
		return loadSignalTimesFromMultiple(signalPath12h)
	case "both":
		return loadSignalTimesFromMultiple(signalPath12h, signalPath6h)
	default:
		return nil, fmt.Errorf("unsupported signal source %q", source)
	}
}

// loadSignalTimesFromMultiple merges signal times from all provided file paths.
// Missing files are silently skipped.
func loadSignalTimesFromMultiple(paths ...string) (map[int64]struct{}, error) {
	result := make(map[int64]struct{})
	for _, path := range paths {
		times, err := loadSignalTimesFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		for ts := range times {
			result[ts] = struct{}{}
		}
	}
	return result, nil
}

// loadSignalTimesFromFile reads a txt signal file where each line is a UTC
// timestamp in the format "Jan 2, 2006, 15:04".  Lines may optionally start
// with a numeric index ("1 Jan 2, 2006, 15:04") which is stripped.
func loadSignalTimesFromFile(relPath string) (map[int64]struct{}, error) {
	resolved := resolveSignalPath(relPath)
	if resolved == "" {
		return map[int64]struct{}{}, nil // file not found → empty, not an error
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", resolved, err)
	}
	defer f.Close()

	result := make(map[int64]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\ufeff") // strip BOM
		if line == "" {
			continue
		}

		// Strip optional leading numeric index.
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if _, convErr := strconv.Atoi(fields[0]); convErr == nil {
				line = strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
			}
		}

		t, parseErr := time.Parse(txtTimeLayout, strings.TrimSpace(line))
		if parseErr != nil {
			continue // skip unrecognised lines
		}
		result[t.UTC().Unix()] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", resolved, err)
	}
	return result, nil
}

func resolveSignalPath(relPath string) string {
	if _, err := os.Stat(relPath); err == nil {
		return relPath
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	p := filepath.Join(wd, relPath)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}
