package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	mustRegisterDSLFileWithMetadata(catalog.Registration{
		Name:    "us-option-min-iv-strike-percentiles",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeMaterial},
	}, "us-option-min-iv-strike-percentiles.toktik")
}
