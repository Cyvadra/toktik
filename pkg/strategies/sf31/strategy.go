// Package sf31 provides the shared SF31 IMI momentum strategy implementation.
// Both the long-only and short-only variants delegate to the Strategy type here,
// configured via the Direction parameter.
package sf31

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// Direction specifies whether the strategy trades long or short.
type Direction int8

const (
	Long  Direction = 1
	Short Direction = -1
)

// Strategy implements the SF31 IMI momentum strategy for either direction.
//
// Entry: goes long when IMI crosses above Band (Long) or short when IMI crosses
// below Band (Short).
// Exit:  chandelier trailing stop that tightens over holding time, plus two
// profit-protection stop levels.
type Strategy struct {
	// Parameters
	Dir       Direction
	BarNo     int     // lookback period for IMI calculation
	Band      float64 // IMI threshold (DnBand for long, UpBand for short)
	TS        float64 // trailing stop width factor (applied as TS/1000 * price)
	Lots      float64 // position size
	StartPro1 float64 // profit protection level 1: start (% from entry)
	StopPro1  float64 // profit protection level 1: trail (% of profit)
	StartPro2 float64 // profit protection level 2: start (% from entry)
	StopPro2  float64 // profit protection level 2: trail (% of profit)

	// Runtime state
	profitExtreme float64 // max high (long) or min low (short) after entry
	stopRef       float64 // max low (long) or min high (short) — chandelier reference
	liqPoint      float64 // current chandelier exit level
	entryPrice    float64
	openBar       int
	hasPosition   bool
	liQKA         float64
}

// New returns a configured SF31 Strategy from catalog config.
func New(dir Direction, cfg catalog.Config) *Strategy {
	return &Strategy{
		Dir:       dir,
		BarNo:     catalog.IntOrDefault(cfg.FastPeriod, 10),
		Band:      catalog.FloatOrDefault(cfg.PThreshold, 10),
		TS:        3.0,
		Lots:      catalog.FloatOrDefault(cfg.PositionSize, 1),
		StartPro1: 5.0,
		StopPro1:  10.0,
		StartPro2: 10.0,
		StopPro2:  10.0,
	}
}

// Name returns the strategy identifier.
func (s *Strategy) Name() string {
	if s.Dir == Long {
		return "SF31Long"
	}
	return "SF31Short"
}

// Init registers the IMI indicator and strategy parameters.
func (s *Strategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("bar_no", s.BarNo)
	if s.Dir == Long {
		ctx.SetParam("dn_band", s.Band)
	} else {
		ctx.SetParam("up_band", s.Band)
	}

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
				// preIMI = sum(|close - open|) over [i-barNo, i)
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

// OnBar executes the per-bar trading logic.
func (s *Strategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	pos := ctx.Position(primary)

	imi := ctx.Ind("imi")
	imiPrev := ctx.IndAt("imi", 1)

	inPosition := float64(s.Dir)*pos > 0

	// Update the profit extreme while in position (tracks highest high for long,
	// lowest low for short).
	if inPosition {
		if s.openBar == 0 {
			s.profitExtreme = s.extremeBarPrice(ctx)
		} else {
			s.profitExtreme = s.updateExtreme(s.profitExtreme, ctx)
		}
	}

	// Entry condition: IMI crosses the band in the direction of the trade.
	if s.imiBandCross(imiPrev, imi) && pos == 0 {
		price := ctx.Open()
		if price > 0 {
			if s.Dir == Long {
				ctx.Buy(primary, s.Lots)
			} else {
				ctx.Sell(primary, s.Lots)
			}
			s.entryPrice = price
			s.profitExtreme = s.extremeBarPrice(ctx)
			s.stopRef = s.stopRefBarPrice(ctx)
			s.hasPosition = true
			s.openBar = 0
			s.liQKA = 1
		}
	}

	// Update the stop reference while in position (tracks highest low for long,
	// lowest high for short — i.e., the trailing support / resistance).
	if inPosition {
		if s.openBar == 0 {
			s.stopRef = s.stopRefBarPrice(ctx)
		} else {
			s.stopRef = s.updateStopRef(s.stopRef, ctx)
		}
		s.openBar++
	}

	// Update liQKA adaptive tightening parameter.
	if pos == 0 {
		s.liQKA = 1
	} else {
		s.liQKA -= 0.1
		s.liQKA = math.Max(s.liQKA, 0.3)
	}

	// Compute chandelier exit level.
	if inPosition && s.openBar >= 1 {
		cushion := ctx.Open() * (s.TS / 1000) * s.liQKA
		if s.Dir == Long {
			s.liqPoint = s.stopRef - cushion
		} else {
			s.liqPoint = s.stopRef + cushion
		}
	}

	// Exit method 1: chandelier trailing stop.
	if inPosition && s.openBar >= 1 && s.liqExitTriggered(ctx) {
		ctx.ClosePosition(primary)
		s.resetState()
		return
	}

	// Exit method 2: profit protection level 2 (wider).
	if inPosition && s.openBar > 0 && s.profitThresholdReached(s.StartPro2) {
		trailLevel := s.profitExtreme - (s.profitExtreme-s.entryPrice)*0.01*s.StopPro2
		if s.profitExitTriggered(ctx, trailLevel) {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}

	// Exit method 3: profit protection level 1 (tighter).
	if inPosition && s.openBar > 0 && s.profitThresholdReached(s.StartPro1) {
		trailLevel := s.profitExtreme - (s.profitExtreme-s.entryPrice)*0.01*s.StopPro1
		if s.profitExitTriggered(ctx, trailLevel) {
			ctx.ClosePosition(primary)
			s.resetState()
			return
		}
	}
}

// imiBandCross returns true when the IMI crosses the band in the trade direction.
func (s *Strategy) imiBandCross(imiPrev, imi float64) bool {
	if math.IsNaN(imiPrev) || math.IsNaN(imi) {
		return false
	}
	if s.Dir == Long {
		return imiPrev <= s.Band && imi > s.Band
	}
	return imiPrev >= s.Band && imi < s.Band
}

// extremeBarPrice returns the bar price used to track the most favourable price
// after entry (High for long, Low for short).
func (s *Strategy) extremeBarPrice(ctx *backtest.BarContext) float64 {
	if s.Dir == Long {
		return ctx.High()
	}
	return ctx.Low()
}

// stopRefBarPrice returns the bar price used to track the chandelier stop
// reference (Low for long, High for short).
func (s *Strategy) stopRefBarPrice(ctx *backtest.BarContext) float64 {
	if s.Dir == Long {
		return ctx.Low()
	}
	return ctx.High()
}

// updateExtreme updates the profit extreme with the current bar's extreme price.
func (s *Strategy) updateExtreme(current float64, ctx *backtest.BarContext) float64 {
	if s.Dir == Long {
		return math.Max(current, ctx.High())
	}
	return math.Min(current, ctx.Low())
}

// updateStopRef updates the chandelier stop reference with the current bar.
func (s *Strategy) updateStopRef(current float64, ctx *backtest.BarContext) float64 {
	if s.Dir == Long {
		return math.Max(current, ctx.Low())
	}
	return math.Min(current, ctx.High())
}

// liqExitTriggered returns true when the chandelier stop has been hit.
func (s *Strategy) liqExitTriggered(ctx *backtest.BarContext) bool {
	if s.Dir == Long {
		return ctx.Low() < s.liqPoint
	}
	return ctx.High() > s.liqPoint
}

// profitThresholdReached returns true when the profit extreme has moved far
// enough from entry to activate a profit-protection stop.
func (s *Strategy) profitThresholdReached(startPro float64) bool {
	if s.Dir == Long {
		return s.profitExtreme >= s.entryPrice*(1+0.01*startPro)
	}
	return s.profitExtreme <= s.entryPrice*(1-0.01*startPro)
}

// profitExitTriggered returns true when the current bar has breached the
// profit-protection trail level.
func (s *Strategy) profitExitTriggered(ctx *backtest.BarContext, trailLevel float64) bool {
	if s.Dir == Long {
		return ctx.Low() <= trailLevel
	}
	return ctx.High() >= trailLevel
}

func (s *Strategy) resetState() {
	s.profitExtreme = 0
	s.stopRef = 0
	s.liqPoint = 0
	s.entryPrice = 0
	s.openBar = 0
	s.hasPosition = false
	s.liQKA = 1
}
