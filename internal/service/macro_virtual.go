package service

import (
	"math"

	"github.com/Cyvadra/toktik/internal/dto"
)

type macroVirtualFactor struct {
	Code        string
	BaseFactor  string
	DisplayName string
	Description string
	ValueType   string
	Unit        string
	Transform   func(float64) (float64, bool)
}

type macroVirtualFactorProvider struct{}

func newMacroVirtualFactorProvider() *macroVirtualFactorProvider {
	return &macroVirtualFactorProvider{}
}

func (p *macroVirtualFactorProvider) appendCatalogEntries(dataset string, entries []dto.MacroFactorCatalogEntry) []dto.MacroFactorCatalogEntry {
	for _, factor := range p.factorMap(dataset) {
		entries = append(entries, dto.MacroFactorCatalogEntry{
			Dataset:            dataset,
			FactorCode:         factor.Code,
			DisplayName:        factor.DisplayName,
			Description:        factor.Description,
			ValueType:          factor.ValueType,
			Unit:               factor.Unit,
			PreferredFrequency: "intraday",
			FillPolicy:         "forward_fill",
			PointInTime:        true,
			Source:             macroVirtualFactorSource,
			ReferenceMarket:    defaultMacroReferenceMarket,
			RealtimeMode:       macroRealtimePriceScaled,
			Active:             true,
		})
	}
	return entries
}

func (p *macroVirtualFactorProvider) factorMap(dataset string) map[string]macroVirtualFactor {
	if dataset != "" {
		if _, ok := supportedMacroDatasets[dataset]; !ok {
			return map[string]macroVirtualFactor{}
		}
	} else {
		dataset = macroDatasetGurufocusShiller
	}
	if dataset == "" {
		return map[string]macroVirtualFactor{}
	}
	return map[string]macroVirtualFactor{
		"pe10_live": {
			Code:        "pe10_live",
			BaseFactor:  "pe10",
			DisplayName: "Shiller PE Live",
			Description: "Realtime-updated CAPE using the latest reference price ratio applied to the monthly Shiller PE anchor.",
			ValueType:   "ratio",
			Transform: func(value float64) (float64, bool) {
				return value, true
			},
		},
		"pe_reg_live": {
			Code:        "pe_reg_live",
			BaseFactor:  "pe_reg",
			DisplayName: "Regression PE Live",
			Description: "Realtime-updated regression PE using the latest reference price ratio applied to the monthly regression anchor.",
			ValueType:   "ratio",
			Transform: func(value float64) (float64, bool) {
				return value, true
			},
		},
		"cape_earnings_yield_live": {
			Code:        "cape_earnings_yield_live",
			BaseFactor:  "pe10",
			DisplayName: "CAPE Earnings Yield Live",
			Description: "Realtime earnings-yield view computed as 100 / pe10_live.",
			ValueType:   "percent",
			Unit:        "%",
			Transform: func(value float64) (float64, bool) {
				if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return 0, false
				}
				return 100 / value, true
			},
		},
		"regression_earnings_yield_live": {
			Code:        "regression_earnings_yield_live",
			BaseFactor:  "pe_reg",
			DisplayName: "Regression Earnings Yield Live",
			Description: "Realtime earnings-yield view computed as 100 / pe_reg_live.",
			ValueType:   "percent",
			Unit:        "%",
			Transform: func(value float64) (float64, bool) {
				if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return 0, false
				}
				return 100 / value, true
			},
		},
	}
}

func virtualMacroFactorsForDataset(dataset string, _ []dto.MacroFactorCatalogEntry) map[string]macroVirtualFactor {
	return newMacroVirtualFactorProvider().factorMap(dataset)
}
