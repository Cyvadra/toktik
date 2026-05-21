package service

import (
	"strings"
	"time"
)

type usStockFundamentalBinding struct {
	ResponseFactor string
	SourceFactor   string
	PriceDerived   bool
	SeriesMode     string
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
		SeriesMode:     fundamentalSeriesModeEvent,
	}
	if factor == virtualFundamentalFactorPE10Live {
		binding.SeriesMode = fundamentalSeriesModeFilled
	}
	if factor == virtualFundamentalFactorPE {
		if _, ok := resolveVirtualFundamentalMacroTarget("us-stocks", symbol, factor); ok {
			binding.PriceDerived = false
			binding.SeriesMode = fundamentalSeriesModeFilled
		}
	}
	if factor == virtualFundamentalFactorPE10Live {
		binding.SeriesMode = fundamentalSeriesModeFilled
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

func usStockFundamentalKnownAtCutoff(barTS time.Time, interval string) time.Time {
	if strings.EqualFold(strings.TrimSpace(interval), "1d") {
		day := barTS.UTC()
		return time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, time.UTC)
	}
	return barTS.UTC()
}
