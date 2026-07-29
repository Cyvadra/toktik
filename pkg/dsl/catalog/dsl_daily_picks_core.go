package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	mustRegisterDSLFileWithMetadata(catalog.Registration{
		Name:    "daily-picks-core-dsl",
		Groups:  []string{"dsl", "options", "recommendation", "index"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, "daily-picks-core.toktik")
}
