package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	mustRegisterDSLFileWithMetadata(catalog.Registration{
		Name:    "value-allocation-dsl",
		Groups:  []string{"dsl", "universe", "value"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, "value-allocation.toktik")
}
