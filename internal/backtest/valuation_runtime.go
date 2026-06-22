package backtest

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	WarningValuationCarryForward = "valuation.carry_forward"
	WarningValuationEntrySeed    = "valuation.entry_seed"
	WarningOptionsExpiryClose    = "options.expiry_auto_close"
	WarningOptionsIntrinsicValue = "options.expiry_intrinsic_fallback"

	TradeCustomDataKeyCloseSource = "close_source"
)

type replayWarningSink struct {
	warnings []BacktestWarning
}

func (s *replayWarningSink) add(w BacktestWarning) {
	if w.Severity == "" {
		w.Severity = WarningSeverityWarning
	}
	s.warnings = append(s.warnings, w)
}

func (s *replayWarningSink) all() []BacktestWarning {
	if len(s.warnings) == 0 {
		return nil
	}
	out := make([]BacktestWarning, len(s.warnings))
	copy(out, s.warnings)
	return out
}

type spreadValuationState struct {
	policy ValuationMissingPolicy
	last   map[string]float64
	warns  *replayWarningSink
}

func newSpreadValuationState(policy ValuationMissingPolicy, warns *replayWarningSink) *spreadValuationState {
	return &spreadValuationState{
		policy: policy.normalized(),
		last:   make(map[string]float64),
		warns:  warns,
	}
}

func (s *spreadValuationState) markPrice(spreadID, legIndex int, leg SpreadLeg, contract OptionContract, mode OptionPriceMode, barIndex int, barTime time.Time) float64 {
	key := spreadLegValuationKey(spreadID, legIndex, leg)
	price := mode.ExitPrice(leg.Side, contract)
	if optionPriceValid(price) {
		s.last[key] = price
		return price
	}
	if carried, ok := s.last[key]; ok && optionPriceValid(carried) {
		s.warns.add(valuationWarning(WarningValuationCarryForward, "option mark missing; using last valid price", barIndex, barTime, spreadID, legIndex, contract.Symbol, string(s.policy), carried))
		return carried
	}
	if optionPriceValid(leg.EntryPrice) {
		s.last[key] = leg.EntryPrice
		s.warns.add(valuationWarning(WarningValuationEntrySeed, "option mark missing and no carried price exists; seeding valuation from entry price", barIndex, barTime, spreadID, legIndex, contract.Symbol, string(s.policy), leg.EntryPrice))
		return leg.EntryPrice
	}
	s.warns.add(valuationWarning(WarningValuationCarryForward, "option mark missing and no valid fallback exists", barIndex, barTime, spreadID, legIndex, contract.Symbol, string(s.policy), math.NaN()))
	return 0
}

func (s *spreadValuationState) expiryPrice(spreadID, legIndex int, leg SpreadLeg, contract OptionContract, mode OptionPriceMode, barIndex int, barTime time.Time) (float64, string) {
	price := mode.ExitPrice(leg.Side, contract)
	if optionPriceValid(price) {
		s.last[spreadLegValuationKey(spreadID, legIndex, leg)] = price
		return price, "quote"
	}
	if intrinsic := optionIntrinsicValue(contract); optionPriceValidOrZero(intrinsic) {
		s.warns.add(valuationWarning(WarningOptionsIntrinsicValue, "option expired without valid quote; using intrinsic value", barIndex, barTime, spreadID, legIndex, contract.Symbol, string(s.policy), intrinsic))
		s.last[spreadLegValuationKey(spreadID, legIndex, leg)] = intrinsic
		return intrinsic, "intrinsic"
	}
	return s.markPrice(spreadID, legIndex, leg, contract, mode, barIndex, barTime), "carry_forward"
}

func valuationWarning(code, message string, barIndex int, barTime time.Time, spreadID, legIndex int, symbol, policy string, price float64) BacktestWarning {
	idx := barIndex
	ts := barTime
	spID := spreadID
	legIdx := legIndex
	details := map[string]string{}
	if !math.IsNaN(price) && !math.IsInf(price, 0) {
		details["price"] = strconv.FormatFloat(price, 'f', -1, 64)
	}
	return BacktestWarning{
		Severity:  WarningSeverityWarning,
		Code:      code,
		Message:   message,
		BarIndex:  &idx,
		Timestamp: &ts,
		SpreadID:  &spID,
		LegIndex:  &legIdx,
		Symbol:    symbol,
		Policy:    policy,
		Details:   details,
	}
}

func expiryCloseWarning(barIndex int, barTime time.Time, spreadID, legIndex int, symbol, source string, price float64) BacktestWarning {
	warning := valuationWarning(WarningOptionsExpiryClose, "option leg expired and was force-closed by the engine", barIndex, barTime, spreadID, legIndex, symbol, "expiry_auto_close", price)
	if warning.Details == nil {
		warning.Details = map[string]string{}
	}
	warning.Details["source"] = source
	return warning
}

func spreadLegValuationKey(spreadID, legIndex int, leg SpreadLeg) string {
	symbol := strings.TrimSpace(leg.Contract.Symbol)
	if symbol == "" {
		symbol = fmt.Sprintf("spread:%d:leg:%d", spreadID, legIndex)
	}
	return fmt.Sprintf("%d|%d|%s", spreadID, legIndex, symbol)
}

func optionIntrinsicValue(contract OptionContract) float64 {
	if !isValidPrice(contract.UnderlyingPrice) || !isValidPrice(contract.StrikePrice) {
		return math.NaN()
	}
	switch contract.Type {
	case Call:
		return math.Max(contract.UnderlyingPrice-contract.StrikePrice, 0)
	case Put:
		return math.Max(contract.StrikePrice-contract.UnderlyingPrice, 0)
	default:
		return math.NaN()
	}
}

func optionPriceValidOrZero(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func forceCloseExpiredOptionLegs(barCtx *BarContext, tracker *SpreadTracker, contractMap map[string]OptionContract, pricing SpreadPricingConfig, valuation *spreadValuationState, barIndex int, barTime time.Time) {
	if barCtx == nil || tracker == nil || valuation == nil {
		return
	}
	for _, sp := range tracker.OpenSpreads() {
		if sp == nil {
			continue
		}
		for legIndex := range sp.Legs {
			leg := sp.Legs[legIndex]
			if leg.Closed || leg.Contract.Expiration.IsZero() || barTime.Before(leg.Contract.Expiration) {
				continue
			}
			contract := resolveSpreadContract(leg.Contract, contractMap)
			price, source := valuation.expiryPrice(sp.ID, legIndex, leg, contract, pricing.ExitMode, barIndex, barTime)
			customData := upsertTradeCustomData(nil, TradeCustomDataKeyCloseTriggerTime, barTime.UTC().Format(time.RFC3339Nano))
			customData = upsertTradeCustomData(customData, TradeCustomDataKeyCloseSource, source)
			if barCtx.CloseSpreadLegWithReasonAndData(sp.ID, legIndex, price, "expiry_auto_close", customData) {
				valuation.warns.add(expiryCloseWarning(barIndex, barTime, sp.ID, legIndex, contract.Symbol, source, price))
			}
		}
	}
}

func calculateSpreadMarketValue(tracker *SpreadTracker, contractMap map[string]OptionContract, pricing SpreadPricingConfig, valuation *spreadValuationState, barIndex int, barTime time.Time) float64 {
	if tracker == nil || valuation == nil {
		return 0
	}
	spreadMarketValue := 0.0
	for _, sp := range tracker.OpenSpreads() {
		if sp == nil {
			continue
		}
		for legIndex, leg := range sp.Legs {
			if leg.Closed {
				continue
			}
			contract := resolveSpreadContract(leg.Contract, contractMap)
			markPrice := valuation.markPrice(sp.ID, legIndex, leg, contract, pricing.ValuationMode, barIndex, barTime)
			notional := leg.Qty * markPrice * contract.Multiplier()
			if leg.Side == Buy {
				spreadMarketValue += notional
			} else {
				spreadMarketValue -= notional
			}
		}
	}
	return spreadMarketValue
}
