package sf31short

import (
"github.com/Cyvadra/toktik/internal/backtest"
"github.com/Cyvadra/toktik/pkg/strategies/catalog"
"github.com/Cyvadra/toktik/pkg/strategies/sf31"
)

func init() {
catalog.Register(catalog.Registration{
Name:    "sf31-short",
Aliases: []string{"sf31_short"},
Groups:  []string{"trend", "single-leg"},
Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
return sf31.New(sf31.Short, cfg), nil
},
})
}
