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
	defaultVolumePeriod   = 20
	defaultVolumeRatio    = 1.2
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
				volumePeriod:      defaultVolumePeriod,
				volumeRatioMin:    defaultVolumeRatio,
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
	volumePeriod      int
	volumeRatioMin    float64
	positionPct       float64
	highestSinceEntry float64
}

func (s *strategy) Name() string { return "EMAATRSpot" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "ema_fast", Label: fmt.Sprintf("EMA %d", s.fastPeriod), Decimals: 2, Overlay: true},
		{Source: "ema_slow", Label: fmt.Sprintf("EMA %d", s.slowPeriod), Decimals: 2, Overlay: true},
		{Source: "atr", Label: "ATR", Decimals: 2},
		{Source: "volume_sma", Label: fmt.Sprintf("VOL SMA %d", s.volumePeriod), Decimals: 2},
		{Source: "volume_ratio", Label: "Vol Ratio", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("fast_period", s.fastPeriod)
	ctx.SetParam("slow_period", s.slowPeriod)
	ctx.SetParam("atr_period", s.atrPeriod)
	ctx.SetParam("atr_multiplier", s.atrMultiplier)
	ctx.SetParam("volume_period", s.volumePeriod)
	ctx.SetParam("volume_ratio_min", s.volumeRatioMin)
	ctx.SetParam("position_pct", s.positionPct)

	ctx.Register("ema_fast", backtest.EMA("close", s.fastPeriod))
	ctx.Register("ema_slow", backtest.EMA("close", s.slowPeriod))
	ctx.Register("atr", backtest.ATR(s.atrPeriod))
	ctx.Register("volume_value", backtest.CustomOptional([]string{"close"}, []string{"volume"}, func(inputs map[string][]float64) []float64 {
		return cloneSeries(inputs["volume"])
	}))
	ctx.Register("volume_sma", backtest.SMA("volume_value", s.volumePeriod))
	ctx.Register("volume_ratio", backtest.Custom([]string{"volume_value", "volume_sma"}, func(inputs map[string][]float64) []float64 {
		volume := inputs["volume_value"]
		volumeSMA := inputs["volume_sma"]
		out := make([]float64, len(volume))
		for i := range volume {
			if math.IsNaN(volume[i]) || math.IsNaN(volumeSMA[i]) || volumeSMA[i] <= 0 {
				out[i] = math.NaN()
				continue
			}
			out[i] = volume[i] / volumeSMA[i]
		}
		return out
	}))
	ctx.Register("buy_signal", backtest.Crossover("ema_fast", "ema_slow"))
	return nil
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	position := ctx.Position(primary)
	price := ctx.Close()
	emaFast := ctx.Ind("ema_fast")
	emaSlow := ctx.Ind("ema_slow")
	volumeRatio := ctx.Ind("volume_ratio")

	if position <= 0 {
		s.highestSinceEntry = math.NaN()
		if shouldEnterLong(ctx.Ind("buy_signal"), ctx.Open(), price, emaFast, emaSlow, volumeRatio, s.volumeRatioMin) {
			qty := positionSizeFromBudget(ctx.Cash(), ctx.Equity(), price, s.positionPct)
			if qty > 0 {
				ctx.BuyWithNote(primary, qty, fmt.Sprintf("ema trend with volume %.2fx", volumeRatio))
			}
		}
		return
	}

	if math.IsNaN(s.highestSinceEntry) || ctx.High() > s.highestSinceEntry {
		s.highestSinceEntry = ctx.High()
	}

	if shouldExitTrend(price, emaFast, emaSlow) {
		ctx.ClosePosition(primary)
		s.highestSinceEntry = math.NaN()
		return
	}

	atr := ctx.Ind("atr")
	if math.IsNaN(atr) || atr <= 0 || math.IsNaN(s.highestSinceEntry) {
		return
	}

	stopPrice := s.highestSinceEntry - s.atrMultiplier*atr
	if !math.IsNaN(stopPrice) && ctx.Low() <= stopPrice {
		ctx.ClosePosition(primary)
		s.highestSinceEntry = math.NaN()
	}
}

func shouldEnterLong(crossover, openPrice, closePrice, emaFast, emaSlow, volumeRatio, minVolumeRatio float64) bool {
	if crossover != 1 || closePrice <= 0 {
		return false
	}
	if math.IsNaN(openPrice) || math.IsNaN(closePrice) || math.IsNaN(emaFast) || math.IsNaN(emaSlow) || math.IsNaN(volumeRatio) {
		return false
	}
	if closePrice <= openPrice || closePrice <= emaFast || emaFast <= emaSlow {
		return false
	}
	return volumeRatio >= minVolumeRatio
}

func shouldExitTrend(closePrice, emaFast, emaSlow float64) bool {
	if math.IsNaN(closePrice) || math.IsNaN(emaFast) || math.IsNaN(emaSlow) {
		return false
	}
	return closePrice < emaFast && emaFast < emaSlow
}

func positionSizeFromBudget(cash, equity, price, positionPct float64) float64 {
	if cash <= 0 || equity <= 0 || price <= 0 || positionPct <= 0 {
		return 0
	}
	budget := math.Min(cash, equity*positionPct)
	if budget <= 0 {
		return 0
	}
	return budget / price
}

func cloneSeries(values []float64) []float64 {
	out := make([]float64, len(values))
	copy(out, values)
	return out
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
