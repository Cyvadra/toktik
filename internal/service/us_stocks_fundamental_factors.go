package service

import "strings"

type usStockFundamentalBinding struct {
	ResponseFactor string
	SourceFactor   string
	PriceDerived   bool
}

func resolveUSStockFundamentalBindings(symbol string, requestedFactors []string) []usStockFundamentalBinding {
	factors := normalizeRequestedFactors(requestedFactors)
	if len(factors) == 0 {
		return nil
	}
	bindings := make([]usStockFundamentalBinding, 0, len(factors))
	for _, factor := range factors {
		bindings = append(bindings, resolveUSStockFundamentalBinding(symbol, factor))
	}
	return bindings
}

func resolveUSStockFundamentalBinding(symbol, factor string) usStockFundamentalBinding {
	binding := usStockFundamentalBinding{
		ResponseFactor: factor,
		SourceFactor:   factor,
		PriceDerived:   defaultUSStockPriceDerivedFundamentalFactor(factor),
	}
	if isIndexPEProxyUSStockSymbol(symbol) && factor == "pe" {
		binding.SourceFactor = virtualFundamentalFactorPE10Live
		binding.PriceDerived = false
	}
	return binding
}

func uniqueUSStockFundamentalSourceFactors(bindings []usStockFundamentalBinding) []string {
	if len(bindings) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(bindings))
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if _, ok := seen[binding.SourceFactor]; ok {
			continue
		}
		seen[binding.SourceFactor] = struct{}{}
		out = append(out, binding.SourceFactor)
	}
	return out
}

func isPriceDerivedFundamentalFactor(binding usStockFundamentalBinding) bool {
	return binding.PriceDerived
}

func defaultUSStockPriceDerivedFundamentalFactor(factor string) bool {
	switch factor {
	case "pe", "pb":
		return true
	default:
		return false
	}
}

func isIndexPEProxyUSStockSymbol(symbol string) bool {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "SPY", "SPX", "QQQ", "NDX":
		return true
	default:
		return false
	}
}
