package goldencross

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultFastPeriod    = 10
	defaultSlowPeriod    = 50
	defaultEntryTWAPBars = 1
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "golden-cross",
		Aliases: []string{"golden_cross"},
		Groups:  []string{"trend"},
		Profile: catalog.StrategyProfile{RegularTrade: catalog.RegularTradeMaterial},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &goldenCrossStrategy{
				fastPeriod: catalog.IntOrDefault(cfg.FastPeriod, defaultFastPeriod),
				slowPeriod: catalog.IntOrDefault(cfg.SlowPeriod, defaultSlowPeriod),
				entryTWAP:  catalog.IntOrDefault(cfg.EntryTWAPBars, defaultEntryTWAPBars),
			}, nil
		},
	})
}

type goldenCrossStrategy struct {
	fastPeriod int
	slowPeriod int
	entryTWAP  int
}

func (s *goldenCrossStrategy) Name() string { return "GoldenCross" }

func (s *goldenCrossStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("fast_period", s.fastPeriod)
	ctx.SetParam("slow_period", s.slowPeriod)

	ctx.Register("sma_fast", backtest.SMA("close", s.fastPeriod))
	ctx.Register("sma_slow", backtest.SMA("close", s.slowPeriod))
	ctx.Register("buy_signal", backtest.Crossover("sma_fast", "sma_slow"))
	ctx.Register("sell_signal", backtest.Crossunder("sma_fast", "sma_slow"))
	return nil
}

func (s *goldenCrossStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	if ctx.Ind("buy_signal") == 1 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		if price > 0 {
			qty := (ctx.Equity() * 0.95) / price
			if s.entryTWAP > 1 {
				ctx.BuyTWAP(primary, qty, s.entryTWAP)
			} else {
				ctx.Buy(primary, qty)
			}
		}
	}

	if ctx.Ind("sell_signal") == 1 && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}
