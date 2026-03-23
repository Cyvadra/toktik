// Package buyflashlow implements the "均线过滤版震荡下沿插针评分系统_V2" strategy.
//
// Entry logic (all conditions must hold):
//   - Current bar touches or dips into the lower boundary zone (lowest low of previous
//     lookback bars ± 0.7 × ATR) — "flash low / pin at support"
//   - Bullish pin bar: close is in the upper half of the bar's range
//   - Amplitude percentile rank > minAmpPr across last 100 bars
//   - Composite score (amplitude + volume) meets the threshold
//     (stricter threshold when MAs are in full bearish alignment)
//
// Exit logic:
//   - Trailing drawdown from highest-since-entry exceeds 2 × ATR
package buyflashlow

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultLookback       = 20
	defaultMinAmpPr       = 66.0
	defaultScoreThreshold = 3
	defaultStrictScore    = 5
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "buy-flash-low",
		Aliases: []string{"buy_flash_low"},
		Groups:  []string{"momentum", "single-leg"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &buyFlashLowStrategy{
				lookback:       intOrDefault(cfg.FastPeriod, defaultLookback),
				minAmpPr:       floatOrDefault(cfg.PThreshold, defaultMinAmpPr),
				scoreThreshold: intOrDefault(cfg.SlowPeriod, defaultScoreThreshold),
				strictScore:    defaultStrictScore,
			}, nil
		},
	})
}

type buyFlashLowStrategy struct {
	lookback       int
	minAmpPr       float64
	scoreThreshold int
	strictScore    int

	// runtime state
	highestSinceEntry float64
}

func (s *buyFlashLowStrategy) Name() string { return "BuyFlashLow" }

// ReportColumns implements backtest.ReportColumnProvider so the HTML report's
// data window shows key indicator values when hovering over the candlestick chart.
func (s *buyFlashLowStrategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "atr", Label: "ATR", Decimals: 2},
		{Source: "sma_20", Label: "SMA 20", Decimals: 2},
		{Source: "vol_norm", Label: "Volume", Decimals: 0},
		{Source: "vol_sma100", Label: "Vol SMA 100", Decimals: 0},
		{Source: "amp_pr100", Label: "Amp PR%", Decimals: 1},
		{Source: "amp_score", Label: "Amp Score", Decimals: 0},
		{Source: "vol_score", Label: "Vol Score", Decimals: 0},
		{Source: "l_prev", Label: "Support", Decimals: 2},
	}
}

func (s *buyFlashLowStrategy) Init(ctx *backtest.SetupContext) error {
	s.highestSinceEntry = math.NaN()

	lkb := s.lookback
	ctx.SetParam("lookback", lkb)
	ctx.SetParam("min_amp_pr", s.minAmpPr)
	ctx.SetParam("score_threshold", s.scoreThreshold)

	ctx.Register("atr", backtest.ATR(lkb))
	ctx.Register("sma_2", backtest.SMA("close", 2))
	ctx.Register("sma_6", backtest.SMA("close", 6))
	ctx.Register("sma_10", backtest.SMA("close", 10))
	ctx.Register("sma_15", backtest.SMA("close", 15))
	ctx.Register("sma_20", backtest.SMA("close", 20))
	// vol_norm: unified volume series — prefers "volume", falls back to
	// "tick_count", and is all-NaN when neither column is present.
	ctx.Register("vol_norm", backtest.CustomOptional(
		[]string{},
		[]string{"volume", "tick_count"},
		func(inputs map[string][]float64) []float64 {
			if col, ok := inputs["volume"]; ok && !allNaN(col) {
				return col
			}
			if col, ok := inputs["tick_count"]; ok && !allNaN(col) {
				return col
			}
			// Neither available — return whatever we have (all-NaN) so
			// downstream indicators degrade gracefully.
			if col, ok := inputs["volume"]; ok {
				return col
			}
			if col, ok := inputs["tick_count"]; ok {
				return col
			}
			return make([]float64, 0)
		},
	))
	ctx.Register("vol_sma100", backtest.SMA("vol_norm", 100))

	// Lowest low of the previous lkb bars — ta.lowest(low, lkb)[1] in Pine Script.
	// At bar i this equals min(low[i-lkb], ..., low[i-1]).
	ctx.Register("l_prev", backtest.Custom(
		[]string{"low"},
		func(inputs map[string][]float64) []float64 {
			low := inputs["low"]
			n := len(low)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < lkb {
					out[i] = math.NaN()
					continue
				}
				minVal := math.Inf(1)
				for j := i - lkb; j < i; j++ {
					if !math.IsNaN(low[j]) && low[j] < minVal {
						minVal = low[j]
					}
				}
				if math.IsInf(minVal, 1) {
					out[i] = math.NaN()
				} else {
					out[i] = minVal
				}
			}
			return out
		},
	))

	// Amplitude = (high − low) / close
	ctx.Register("amp", backtest.Custom(
		[]string{"high", "low", "close"},
		func(inputs map[string][]float64) []float64 {
			high := inputs["high"]
			low := inputs["low"]
			cls := inputs["close"]
			n := len(high)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(cls[i]) || cls[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (high[i] - low[i]) / cls[i]
			}
			return out
		},
	))

	// Percentile rank of amplitude over the last 100 bars (ta.percentrank in Pine Script).
	// Returns the percentage of the 100 most-recent historical bars where amp < current amp.
	ctx.Register("amp_pr100", backtest.Custom(
		[]string{"amp"},
		func(inputs map[string][]float64) []float64 {
			amp := inputs["amp"]
			n := len(amp)
			out := make([]float64, n)
			const prPeriod = 100
			for i := 0; i < n; i++ {
				if i < prPeriod || math.IsNaN(amp[i]) {
					out[i] = math.NaN()
					continue
				}
				count := 0
				for j := i - prPeriod; j < i; j++ {
					if !math.IsNaN(amp[j]) && amp[j] < amp[i] {
						count++
					}
				}
				out[i] = float64(count) / float64(prPeriod) * 100
			}
			return out
		},
	))
	ctx.Register("amp_score", backtest.Custom(
		[]string{"amp_pr100"},
		func(inputs map[string][]float64) []float64 {
			ampPr100 := inputs["amp_pr100"]
			n := len(ampPr100)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				out[i] = float64(computeAmpScore(ampPr100[i]))
			}
			return out
		},
	))

	// Volume rank: 1 = highest volume in the window.
	// Mirrors Pine Script's get_vol_rank(len): rank = 1 + #{past bars where vol > current}.
	ctx.Register("vol_rank_10", backtest.Custom([]string{"vol_norm"}, makeVolRank(20)))
	ctx.Register("vol_rank_20", backtest.Custom([]string{"vol_norm"}, makeVolRank(60)))
	ctx.Register("vol_rank_100", backtest.Custom([]string{"vol_norm"}, makeVolRank(180)))
	ctx.Register("vol_score", backtest.Custom(
		[]string{"vol_rank_10", "vol_rank_20", "vol_rank_100", "vol_sma100", "vol_norm"},
		func(inputs map[string][]float64) []float64 {
			volRank10 := inputs["vol_rank_10"]
			volRank20 := inputs["vol_rank_20"]
			volRank100 := inputs["vol_rank_100"]
			volSMA100 := inputs["vol_sma100"]
			volNorm := inputs["vol_norm"]
			n := len(volNorm)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				out[i] = float64(computeVolScore(volRank10[i], volRank20[i], volRank100[i], volSMA100[i], volNorm[i]))
			}
			return out
		},
	))

	return nil
}

// makeVolRank returns a compute function that ranks the current bar's volume (1 = highest)
// against the previous `window` bars.
func makeVolRank(window int) func(inputs map[string][]float64) []float64 {
	return func(inputs map[string][]float64) []float64 {
		vol := inputs["vol_norm"]
		n := len(vol)
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			if math.IsNaN(vol[i]) {
				out[i] = math.NaN()
				continue
			}
			rank := 1
			start := i - window
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				if !math.IsNaN(vol[j]) && vol[j] > vol[i] {
					rank++
				}
			}
			out[i] = float64(rank)
		}
		return out
	}
}

func (s *buyFlashLowStrategy) OnBar(ctx *backtest.BarContext) {
	// Skip the warmup period (mirrors Pine Script's bar_index >= 100 guard).
	if ctx.BarIndex() < 100 {
		return
	}

	primary := ctx.PrimaryRef()
	high := ctx.High()
	low := ctx.Low()
	cls := ctx.Close()
	vol := ctx.Ind("vol_norm")

	atr := ctx.Ind("atr")
	if math.IsNaN(atr) || atr <= 0 {
		return
	}

	// ── MA bearish-alignment check ─────────────────────────────────────────
	// is_bearish = ma2 < ma6+buf && ma6 < ma10+buf && ma10 < ma15+buf && ma15 < ma20+buf
	ma2 := ctx.Ind("sma_2")
	ma6 := ctx.Ind("sma_6")
	ma10 := ctx.Ind("sma_10")
	ma15 := ctx.Ind("sma_15")
	ma20 := ctx.Ind("sma_20")

	isBearish := false
	if !math.IsNaN(ma2) && !math.IsNaN(ma6) && !math.IsNaN(ma10) &&
		!math.IsNaN(ma15) && !math.IsNaN(ma20) {
		buf := 0.05 * atr
		isBearish = ma2 < ma6+buf && ma6 < ma10+buf && ma10 < ma15+buf && ma15 < ma20+buf
	}

	currentThreshold := s.scoreThreshold
	if isBearish {
		currentThreshold = s.strictScore
	}

	hasPendingOrder := func(side backtest.Side) bool {
		for _, order := range ctx.PendingOrders() {
			if order.Security == primary && order.Side == side {
				return true
			}
		}
		return false
	}

	positionQty := ctx.Position(primary)
	hasLongPosition := positionQty > 0
	hasPendingBuy := hasPendingOrder(backtest.Buy)
	hasPendingSell := hasPendingOrder(backtest.Sell)

	// ── Exit management ────────────────────────────────────────────────────
	// Mirror the Pine strategy's state machine: seed the trailing anchor from
	// the signal bar close, then trail against the highest value seen since entry.
	if hasLongPosition {
		if math.IsNaN(s.highestSinceEntry) {
			s.highestSinceEntry = cls
		} else {
			s.highestSinceEntry = math.Max(s.highestSinceEntry, high)
		}
		if !hasPendingSell && s.highestSinceEntry-cls > 2*atr {
			ctx.ClosePosition(primary)
			hasPendingSell = true
		}
	} else if !hasPendingBuy {
		s.highestSinceEntry = math.NaN()
	}

	// ── Entry signal evaluation ────────────────────────────────────────────
	if hasLongPosition || hasPendingBuy || hasPendingSell {
		return
	}

	lPrev := ctx.Ind("l_prev")
	if math.IsNaN(lPrev) {
		return
	}

	// in_bot: current bar touches or dips into the support zone
	inBot := low <= (lPrev+0.7*atr) && high >= lPrev

	// Shape: close in upper half of bar (bullish pin) with large relative range
	isPinShape := cls > 0.5*(high+low)
	ampPr100 := ctx.Ind("amp_pr100")
	shapeEntry := isPinShape && !math.IsNaN(ampPr100) && ampPr100 > s.minAmpPr

	if !inBot || !shapeEntry {
		return
	}

	// ── Score computation ──────────────────────────────────────────────────
	ampScore := computeAmpScore(ampPr100)

	volRank10 := ctx.Ind("vol_rank_10")
	volRank20 := ctx.Ind("vol_rank_20")
	volRank100 := ctx.Ind("vol_rank_100")
	volSMA100 := ctx.Ind("vol_sma100")

	volScore := computeVolScore(volRank10, volRank20, volRank100, volSMA100, vol)

	// When no volume data is available the maximum achievable score is 2 (amplitude
	// only). Clamp the threshold so high-quality pin bars can still trigger.
	const ampMax, volMax = 2, 3
	volDataPresent := !math.IsNaN(volRank10) || !math.IsNaN(volRank20) || !math.IsNaN(volRank100)
	maxPossible := ampMax
	if volDataPresent {
		maxPossible += volMax
	}
	if currentThreshold > maxPossible {
		currentThreshold = maxPossible
	}

	if ampScore+volScore < currentThreshold {
		return
	}

	// ── Entry ──────────────────────────────────────────────────────────────
	if cls > 0 {
		s.highestSinceEntry = cls
		qty := (ctx.Equity() * 0.95) / cls
		ctx.Buy(primary, qty)
	}
}

func intOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func floatOrDefault(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func computeAmpScore(ampPr100 float64) int {
	score := 0
	if ampPr100 > 77 {
		score++
	}
	if ampPr100 > 90 {
		score++
	}
	return score
}

func computeVolScore(volRank10, volRank20, volRank100, volSMA100, vol float64) int {
	score := 0
	if !math.IsNaN(volRank10) && volRank10 <= 3 {
		score++
	}
	if !math.IsNaN(volRank20) && volRank20 <= 6 {
		score++
	}
	if !math.IsNaN(volRank100) && volRank100 <= 10 &&
		!math.IsNaN(volSMA100) && volSMA100 > 0 && vol > 2*volSMA100 {
		score++
	}
	return score
}

// allNaN reports whether every element in a slice is NaN.
func allNaN(s []float64) bool {
	for _, v := range s {
		if !math.IsNaN(v) {
			return false
		}
	}
	return true
}
