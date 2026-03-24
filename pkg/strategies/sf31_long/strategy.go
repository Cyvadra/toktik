package sf31long

import (
"github.com/Cyvadra/toktik/internal/backtest"
"github.com/Cyvadra/toktik/pkg/strategies/catalog"
"github.com/Cyvadra/toktik/pkg/strategies/sf31"
)

func init() {
catalog.Register(catalog.Registration{
Name:    "sf31-long",
Aliases: []string{"sf31_long"},
Groups:  []string{"trend", "single-leg"},
Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
return sf31.New(sf31.Long, cfg), nil
},
})
}
