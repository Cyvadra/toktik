package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	mustRegisterDSLFileWithMetadata(catalog.Registration{
		Name:    "strong-momentum-dsl",
		Groups:  []string{"dsl", "universe", "momentum"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, "strong-momentum.dsl")
}
