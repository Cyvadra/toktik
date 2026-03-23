package sf31long

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "sf31-long",
		Aliases: []string{"sf31_long"},
		Groups:  []string{"trend", "single-leg"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &sf31LongStrategy{
				BarNo:     intOrDefault(cfg.FastPeriod, 10),
				DnBand:    floatOrDefault(cfg.PThreshold, 10),
				TS:        3.0,
				Lots:      floatOrDefault(cfg.PositionSize, 1),
				StartPro1: 5.0,
				StopPro1:  10.0,
				StartPro2: 10.0,
				StopPro2:  10.0,
			}, nil
		},
	})
}

// sf31LongStrategy implements the SF31 long-only IMI momentum strategy.
//
// Entry: goes long when IMI crosses above DnBand from below.
// Exit:  chandelier trailing stop that tightens over holding time,
//
//	plus two profit-protection stop levels.
type sf31LongStrategy struct {
	// Parameters
	BarNo  int     // lookback period for IMI calculation
	DnBand float64 // IMI threshold for entry signal
	TS     float64 // trailing stop width factor (applied as TS/1000 * price)
	Lots   float64 // position size

	StartPro1 float64 // profit protection level 1: start (% above entry)
	StopPro1  float64 // profit protection level 1: trail (% of profit)
	StartPro2 float64 // profit protection level 2: start (% above entry)
	StopPro2  float64 // profit protection level 2: trail (% of profit)

	// Runtime state
	highAfterEntry float64
	lowAfterEntry  float64
	liQKA          float64
	dliqPoint      float64
	entryPrice     float64
	openBar        int
	hasPosition    bool
}

func (s *sf31LongStrategy) Name() string { return "SF31Long" }

func (s *sf31LongStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("bar_no", s.BarNo)
	ctx.SetParam("dn_band", s.DnBand)

	barNo := s.BarNo

	// IMI indicator: measures bullish momentum as a ratio.
	// When close > open, imi = sum(close-open) / sum(|close-open|) * 100 over BarNo bars.
	// Otherwise imi = 0.
	ctx.Register("imi", backtest.Custom(
		[]string{"open", "close"},
		func(inputs map[string][]float64) []float64 {
			opens := inputs["open"]
			closes := inputs["close"]
			n := len(opens)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < barNo {
					out[i] = math.NaN()
					continue
				}
				// pre_imi = sum(|close - open|) over [i-barNo, i-1)
				preIMI := 0.0
				for j := i - barNo; j < i; j++ {
					preIMI += math.Abs(closes[j] - opens[j])
				}
				if preIMI == 0 || math.IsNaN(closes[i]) || math.IsNaN(opens[i]) {
					out[i] = 0
					continue
				}
				if closes[i] > opens[i] {
					// sum of (close - open) over the lookback window
					bodySum := 0.0
					for j := i - barNo; j < i; j++ {
						bodySum += closes[j] - opens[j]
					}
					out[i] = (bodySum / preIMI) * 100
				} else {
					out[i] = 0
				}
			}
			return out
		},
	))

	return nil
}

func (s *sf31LongStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	pos := ctx.Position(primary)

	imi := ctx.Ind("imi")
	imiPrev := ctx.IndAt("imi", 1)

	// Update high after entry while in position
	if pos > 0 {
		if s.openBar == 0 {
			s.highAfterEntry = ctx.High()
		} else if s.openBar > 0 {
			s.highAfterEntry = math.Max(s.highAfterEntry, ctx.High())
		}
	}

	// Entry condition: IMI crosses above DnBand from below
	cdCon := !math.IsNaN(imiPrev) && !math.IsNaN(imi) &&
		imiPrev <= s.DnBand && imi > s.DnBand

	if cdCon && pos == 0 {
		price := ctx.Open()
		if price > 0 {
			ctx.Buy(primary, s.Lots)
			s.entryPrice = price
			s.lowAfterEntry = ctx.Low()
			s.hasPosition = true
			s.openBar = 0
			s.liQKA = 1
			s.highAfterEntry = ctx.High()
		}
	}

	// Trailing exit logic (only when in position)
	if pos > 0 {
		if s.openBar == 0 {
			s.lowAfterEntry = ctx.Low()
		} else if s.openBar > 0 {
			s.lowAfterEntry = math.Max(s.lowAfterEntry, ctx.Low())
		}
		s.openBar++
	}

	// Update liQKA adaptive parameter
	if pos == 0 {
		s.liQKA = 1
	} else {
		s.liQKA -= 0.1
		s.liQKA = math.Max(s.liQKA, 0.3)
	}

	// Calculate chandelier exit point for longs
	if pos > 0 && s.openBar >= 1 {
		s.dliqPoint = s.lowAfterEntry - (ctx.Open()*(s.TS/1000))*s.liQKA
	}

	// Exit method 1: chandelier trailing stop
	if pos > 0 && s.openBar >= 1 && ctx.Low() < s.dliqPoint {
		exitPrice := math.Min(s.dliqPoint, ctx.Low())
		_ = exitPrice // stop-price hint; framework uses market order
		ctx.ClosePosition(primary)
		s.resetState()
		return
	}

	// Exit method 2: profit protection level 2 (wider)
	if pos > 0 && s.openBar > 0 &&
		s.highAfterEntry >= s.entryPrice*(1+0.01*s.StartPro2) {
		trailLevel := s.highAfterEntry - (s.highAfterEntry-s.entryPrice)*0.01*s.StopPro2
		if ctx.Low() <= trailLevel {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}

	// Exit method 3: profit protection level 1 (tighter)
	if pos > 0 && s.openBar > 0 &&
		s.highAfterEntry >= s.entryPrice*(1+0.01*s.StartPro1) {
		trailLevel := s.highAfterEntry - (s.highAfterEntry-s.entryPrice)*0.01*s.StopPro1
		if ctx.Low() <= trailLevel {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}
}

func (s *sf31LongStrategy) resetState() {
	s.highAfterEntry = 0
	s.lowAfterEntry = 0
	s.dliqPoint = 0
	s.entryPrice = 0
	s.openBar = 0
	s.hasPosition = false
	s.liQKA = 1
}

func floatOrDefault(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func intOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
