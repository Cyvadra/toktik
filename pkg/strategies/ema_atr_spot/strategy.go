package emaatrspot

import (
	"fmt"
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultFastPeriod     = 20
	defaultSlowPeriod     = 50
	defaultATRPeriod      = 14
	defaultATRMultiplier  = 2.0
	defaultPositionPct    = 0.95
	defaultStrategyName   = "ema-atr-spot"
	defaultStrategyAlias1 = "ema_atr_spot"
	defaultStrategyAlias2 = "ema_spot"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    defaultStrategyName,
		Aliases: []string{defaultStrategyAlias1, defaultStrategyAlias2},
		Groups:  []string{"trend", "spot"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			fastPeriod := catalog.IntOrDefault(cfg.FastPeriod, defaultFastPeriod)
			slowPeriod := catalog.IntOrDefault(cfg.SlowPeriod, defaultSlowPeriod)
			if fastPeriod >= slowPeriod {
				return nil, fmt.Errorf("fast period must be smaller than slow period, got %d >= %d", fastPeriod, slowPeriod)
			}
			return &strategy{
				fastPeriod:        fastPeriod,
				slowPeriod:        slowPeriod,
				atrPeriod:         defaultATRPeriod,
				atrMultiplier:     defaultATRMultiplier,
				positionPct:       positionPctOrDefault(cfg.PositionSize),
				highestSinceEntry: math.NaN(),
			}, nil
		},
	})
}

type strategy struct {
	fastPeriod        int
	slowPeriod        int
	atrPeriod         int
	atrMultiplier     float64
	positionPct       float64
	highestSinceEntry float64
}

func (s *strategy) Name() string { return "EMAATRSpot" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "ema_fast", Label: fmt.Sprintf("EMA %d", s.fastPeriod), Decimals: 2, Overlay: true},
		{Source: "ema_slow", Label: fmt.Sprintf("EMA %d", s.slowPeriod), Decimals: 2, Overlay: true},
		{Source: "atr", Label: "ATR", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("fast_period", s.fastPeriod)
	ctx.SetParam("slow_period", s.slowPeriod)
	ctx.SetParam("atr_period", s.atrPeriod)
	ctx.SetParam("atr_multiplier", s.atrMultiplier)
	ctx.SetParam("position_pct", s.positionPct)

	ctx.Register("ema_fast", backtest.EMA("close", s.fastPeriod))
	ctx.Register("ema_slow", backtest.EMA("close", s.slowPeriod))
	ctx.Register("atr", backtest.ATR(s.atrPeriod))
	ctx.Register("buy_signal", backtest.Crossover("ema_fast", "ema_slow"))
	return nil
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	position := ctx.Position(primary)
	price := ctx.Close()

	if position <= 0 {
		s.highestSinceEntry = math.NaN()
		if ctx.Ind("buy_signal") == 1 && price > 0 {
			qty := 1.0
			if qty > 0 {
				ctx.BuyWithNote(primary, qty, "ema20 cross above ema50")
			}
		}
		return
	}

	if math.IsNaN(s.highestSinceEntry) || ctx.High() > s.highestSinceEntry {
		s.highestSinceEntry = ctx.High()
	}

	atr := ctx.Ind("atr")
	if math.IsNaN(atr) || atr <= 0 || math.IsNaN(s.highestSinceEntry) {
		return
	}

	stopPrice := s.highestSinceEntry - s.atrMultiplier*atr
	if !math.IsNaN(stopPrice) && ctx.Low() <= stopPrice {
		ctx.ClosePosition(primary)
	}
}

func positionPctOrDefault(raw float64) float64 {
	if raw <= 0 {
		return defaultPositionPct
	}
	if raw > 1 {
		return 1
	}
	return raw
}
