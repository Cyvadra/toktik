// Package optutil provides composable building blocks for options strategy
// development, eliminating boilerplate that is otherwise copy-pasted across
// every strategy.
//
// Typical usage — embed PricingMixin and GroupMixin into your strategy struct:
//
//	type myStrategy struct {
//	    optutil.PricingMixin
//	    optutil.GroupMixin
//	    // ... strategy-specific fields
//	}
//
// The embedded methods cover spread pricing config, contract map lookups,
// per-leg exit/valuation pricing, and position-group lifecycle.
package optutil

import (
	"math"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// ContractMap maps option contract symbols to their latest snapshot.
type ContractMap = map[string]backtest.OptionContract

// PricingMixin provides the three canonical option price modes and helper
// methods that every options strategy needs. Embed it in your strategy struct
// to get SpreadPricingConfig(), ApplyPricingDefaults(), and per-leg pricing
// helpers for free.
type PricingMixin struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode
}

// SpreadPricingConfig implements backtest.SpreadPricingProvider.
func (p *PricingMixin) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     p.EntryPriceMode,
		ExitMode:      p.ExitPriceMode,
		ValuationMode: p.ValuationPriceMode,
	}.WithDefaults()
}

// ApplyPricingDefaults fills any unspecified price modes with the engine defaults.
func (p *PricingMixin) ApplyPricingDefaults() {
	defaults := backtest.DefaultSpreadPricingConfig()
	if p.EntryPriceMode == backtest.OptionPriceModeUnspecified {
		p.EntryPriceMode = defaults.EntryMode
	}
	if p.ExitPriceMode == backtest.OptionPriceModeUnspecified {
		p.ExitPriceMode = defaults.ExitMode
	}
	if p.ValuationPriceMode == backtest.OptionPriceModeUnspecified {
		p.ValuationPriceMode = defaults.ValuationMode
	}
}

// BuildContractMap creates a symbol→contract lookup from the current options chain.
// Returns nil when the chain is empty.
func BuildContractMap(chain *backtest.OptionsChain) ContractMap {
	if chain == nil || chain.Len() == 0 {
		return nil
	}
	contracts := chain.Contracts()
	cm := make(ContractMap, len(contracts))
	for _, c := range contracts {
		cm[c.Symbol] = c
	}
	return cm
}

// ResolveContract returns the latest snapshot of a contract from the map,
// falling back to the original when the map is nil or the symbol is absent.
func ResolveContract(contract backtest.OptionContract, cm ContractMap) backtest.OptionContract {
	if cm == nil {
		return contract
	}
	if updated, ok := cm[contract.Symbol]; ok {
		return updated
	}
	return contract
}

// LegExitPrice returns the exit price for a spread leg using the ExitPriceMode.
func (p *PricingMixin) LegExitPrice(leg backtest.SpreadLeg, cm ContractMap) float64 {
	contract := ResolveContract(leg.Contract, cm)
	return p.ExitPriceMode.ExitPrice(leg.Side, contract)
}

// LegValuationPrice returns the mark-to-market price for a spread leg.
func (p *PricingMixin) LegValuationPrice(leg backtest.SpreadLeg, cm ContractMap) float64 {
	contract := ResolveContract(leg.Contract, cm)
	return p.ValuationPriceMode.ExitPrice(leg.Side, contract)
}

// ValidEntryPrice returns the entry price and true if the price is usable
// (finite and positive). Returns (0, false) otherwise.
func (p *PricingMixin) ValidEntryPrice(side backtest.Side, contract backtest.OptionContract) (float64, bool) {
	price := p.EntryPriceMode.EntryPrice(side, contract)
	if IsValidPrice(price) {
		return price, true
	}
	return 0, false
}

// IsValidPrice reports whether a price is finite and positive.
func IsValidPrice(price float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0) && price > 0
}

// ShouldCloseForExpiry reports whether a contract is within daysThreshold
// days of expiration.
func ShouldCloseForExpiry(contract backtest.OptionContract, now time.Time, daysThreshold float64) bool {
	return contract.DaysToExpiry(now) <= daysThreshold
}

// CloseLeg closes a spread leg if the exit price is valid.
// Returns true if the close succeeded.
func CloseLeg(ctx *backtest.BarContext, spreadID, legIdx int, exitPrice float64, reason string) bool {
	if !IsValidPrice(exitPrice) {
		return false
	}
	return ctx.CloseSpreadLegWithReason(spreadID, legIdx, exitPrice, reason)
}
