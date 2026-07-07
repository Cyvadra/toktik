package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	mustRegisterDSLFileWithMetadata(catalog.Registration{
		Name:    "index-options-dsl",
		Groups:  []string{"dsl", "index", "options"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, "index-options.toktik")
}
