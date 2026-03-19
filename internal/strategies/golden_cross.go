package strategies

import "github.com/Cyvadra/toktik/internal/backtest"

func init() {
	Register(Registration{
		Name:    "golden-cross",
		Aliases: []string{"golden_cross"},
		Groups:  []string{"trend"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return &goldenCrossStrategy{
				fastPeriod: intOrDefault(cfg.FastPeriod, defaultFastPeriod),
				slowPeriod: intOrDefault(cfg.SlowPeriod, defaultSlowPeriod),
				entryTWAP:  intOrDefault(cfg.EntryTWAPBars, defaultEntryTWAPBars),
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

func intOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
