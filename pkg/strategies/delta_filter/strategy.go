package deltafilter

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/validation"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/helpers"
)

const (
	defaultEntryTWAPBars = 1
	defaultPositionPct   = 0.5
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "delta-filter",
		Groups:  []string{"signal"},
		Profile: catalog.StrategyProfile{RegularTrade: catalog.RegularTradeMaterial},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &deltaFilterStrategy{
				entryTWAP:   catalog.IntOrDefault(cfg.EntryTWAPBars, defaultEntryTWAPBars),
				positionPct: helpers.ClampPositionPct(cfg.PositionSize, defaultPositionPct),
			}, nil
		},
	})
}

type deltaFilterStrategy struct {
	entryTWAP   int
	positionPct float64
}

func (s *deltaFilterStrategy) Name() string { return "DeltaFilter" }

func (s *deltaFilterStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.Register("ema20", backtest.EMA("close", 20))
	ctx.Register("rsi14", backtest.RSI("close", 14))
	ctx.Register("delta_ok", backtest.Custom(
		[]string{"delta"},
		func(inputs map[string][]float64) []float64 {
			deltaSeries := inputs["delta"]
			out := make([]float64, len(deltaSeries))
			for i, value := range deltaSeries {
				if validation.AllValid(value) && value > 0.3 && value < 0.7 {
					out[i] = 1
				}
			}
			return out
		},
	))
	return nil
}

func (s *deltaFilterStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	deltaOK := ctx.Ind("delta_ok")
	rsi := ctx.Ind("rsi14")

	if deltaOK == 1 && rsi < 30 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		qty := helpers.PositionSizeFromEquity(ctx.Cash(), ctx.Equity(), price, s.positionPct)
		if qty > 0 {
			// Use the new OrderBuilder pattern
			if s.entryTWAP > 1 {
				ctx.Order(primary).Buy().Qty(qty).TWAP(s.entryTWAP).Submit()
			} else {
				ctx.Order(primary).Buy().Qty(qty).Submit()
			}
		}
	}

	if (deltaOK == 0 || rsi > 70) && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}
