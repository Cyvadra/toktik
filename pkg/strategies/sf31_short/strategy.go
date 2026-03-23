package sf31short

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "sf31-short",
		Aliases: []string{"sf31_short"},
		Groups:  []string{"trend", "single-leg"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &sf31ShortStrategy{
				BarNo:     intOrDefault(cfg.FastPeriod, 10),
				UpBand:    floatOrDefault(cfg.PThreshold, 10),
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

// sf31ShortStrategy implements the SF31 short-only IMI momentum strategy.
//
// Entry: goes short when IMI crosses below UpBand from above.
// Exit:  chandelier trailing stop that tightens over holding time,
//
//	plus two profit-protection stop levels.
type sf31ShortStrategy struct {
	// Parameters
	BarNo  int     // lookback period for IMI calculation
	UpBand float64 // IMI threshold for entry signal (short when crossing below)
	TS     float64 // trailing stop width factor (applied as TS/1000 * price)
	Lots   float64 // position size

	StartPro1 float64 // profit protection level 1: start (% below entry)
	StopPro1  float64 // profit protection level 1: trail (% of profit)
	StartPro2 float64 // profit protection level 2: start (% below entry)
	StopPro2  float64 // profit protection level 2: trail (% of profit)

	// Runtime state
	highAfterEntry float64
	lowAfterEntry  float64
	liQKA          float64
	kliqPoint      float64
	entryPrice     float64
	openBar        int
	hasPosition    bool
}

func (s *sf31ShortStrategy) Name() string { return "SF31Short" }

func (s *sf31ShortStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("bar_no", s.BarNo)
	ctx.SetParam("up_band", s.UpBand)

	barNo := s.BarNo

	// IMI indicator: same computation as the long version.
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
				preIMI := 0.0
				for j := i - barNo; j < i; j++ {
					preIMI += math.Abs(closes[j] - opens[j])
				}
				if preIMI == 0 || math.IsNaN(closes[i]) || math.IsNaN(opens[i]) {
					out[i] = 0
					continue
				}
				if closes[i] > opens[i] {
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

func (s *sf31ShortStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	pos := ctx.Position(primary)

	imi := ctx.Ind("imi")
	imiPrev := ctx.IndAt("imi", 1)

	// Update low after entry while in short position
	if pos < 0 {
		if s.openBar == 0 {
			s.lowAfterEntry = ctx.Low()
		} else if s.openBar > 0 {
			s.lowAfterEntry = math.Min(s.lowAfterEntry, ctx.Low())
		}
	}

	// Entry condition: IMI crosses below UpBand from above
	cdCon := !math.IsNaN(imiPrev) && !math.IsNaN(imi) &&
		imiPrev >= s.UpBand && imi < s.UpBand

	if cdCon && pos == 0 {
		price := ctx.Open()
		if price > 0 {
			ctx.Sell(primary, s.Lots)
			s.entryPrice = price
			s.highAfterEntry = ctx.High()
			s.hasPosition = true
			s.openBar = 0
			s.liQKA = 1
			s.lowAfterEntry = ctx.Low()
		}
	}

	// Trailing exit logic (only when in position)
	if pos < 0 {
		if s.openBar == 0 {
			s.highAfterEntry = ctx.High()
		} else if s.openBar > 0 {
			s.highAfterEntry = math.Min(s.highAfterEntry, ctx.High())
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

	// Calculate chandelier exit point for shorts
	if pos < 0 && s.openBar >= 1 {
		s.kliqPoint = s.highAfterEntry + (ctx.Open()*(s.TS/1000))*s.liQKA
	}

	// Exit method 1: chandelier trailing stop (short cover)
	if pos < 0 && s.openBar >= 1 && ctx.High() > s.kliqPoint {
		ctx.ClosePosition(primary)
		s.resetState()
		return
	}

	// Exit method 2: profit protection level 2 (wider)
	if pos < 0 && s.openBar > 0 &&
		s.lowAfterEntry <= s.entryPrice*(1-0.01*s.StartPro2) {
		trailLevel := s.lowAfterEntry + (s.entryPrice-s.lowAfterEntry)*0.01*s.StopPro2
		if ctx.High() >= trailLevel {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}

	// Exit method 3: profit protection level 1 (tighter)
	if pos < 0 && s.openBar > 0 &&
		s.lowAfterEntry <= s.entryPrice*(1-0.01*s.StartPro1) {
		trailLevel := s.lowAfterEntry + (s.entryPrice-s.lowAfterEntry)*0.01*s.StopPro1
		if ctx.High() >= trailLevel {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}
}

func (s *sf31ShortStrategy) resetState() {
	s.highAfterEntry = 0
	s.lowAfterEntry = 0
	s.kliqPoint = 0
	s.entryPrice = 0
	s.openBar = 0
	s.hasPosition = false
	s.liQKA = 1
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
