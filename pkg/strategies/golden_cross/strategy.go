package goldencross

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/helpers"
)

const (
	defaultFastPeriod    = 10
	defaultSlowPeriod    = 50
	defaultEntryTWAPBars = 1
	defaultPositionPct   = 0.95
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "golden-cross",
		Aliases: []string{"golden_cross"},
		Groups:  []string{"trend"},
		Profile: catalog.StrategyProfile{RegularTrade: catalog.RegularTradeMaterial},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &goldenCrossStrategy{
				fastPeriod:  catalog.IntOrDefault(cfg.FastPeriod, defaultFastPeriod),
				slowPeriod:  catalog.IntOrDefault(cfg.SlowPeriod, defaultSlowPeriod),
				entryTWAP:   catalog.IntOrDefault(cfg.EntryTWAPBars, defaultEntryTWAPBars),
				positionPct: helpers.ClampPositionPct(cfg.PositionSize, defaultPositionPct),
			}, nil
		},
	})
}

type goldenCrossStrategy struct {
	fastPeriod  int
	slowPeriod  int
	entryTWAP   int
	positionPct float64
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
		qty := helpers.PositionSizeFromEquity(ctx.Cash(), ctx.Equity(), price, s.positionPct)
		if qty > 0 {
			// Use the OrderBuilder pattern for cleaner code
			if s.entryTWAP > 1 {
				ctx.Order(primary).Buy().Qty(qty).TWAP(s.entryTWAP).Submit()
			} else {
				ctx.Order(primary).Buy().Qty(qty).Submit()
			}
		}
	}

	if ctx.Ind("sell_signal") == 1 && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}
