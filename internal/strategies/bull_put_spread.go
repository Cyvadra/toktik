package strategies

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// bullPutSpreadStrategy is a bullish options strategy that buys a position
// when the underlying BTC price is trading above its moving average (positive
// deviation P) and the option delta falls within the target put range.
//
// Entry signal (all conditions must hold):
//   - Deviation P = (underlying_price_close - SMA) / SMA > +bullThreshold
//   - Option delta in [minDelta, maxDelta]  (e.g. −0.40 … −0.15 for OTM puts)
//
// Exit signal (any condition triggers):
//   - Deviation P < −bearThreshold  (underlying turns bearish)
//   - Option delta drifts outside the acceptable put range
type bullPutSpreadStrategy struct {
	smaPeriod int
	entryTWAP int
}

const (
	bullPutBullThreshold = 0.01  // +1 % above SMA to enter
	bullPutBearThreshold = 0.01  // −1 % below SMA to exit
	bullPutMinDelta      = -0.40 // lower bound of target put delta
	bullPutMaxDelta      = -0.15 // upper bound of target put delta
	bullPutExitMinDelta  = -0.50 // hard floor — delta too deep ITM → exit
	bullPutExitMaxDelta  = -0.05 // hard ceiling — delta too far OTM → exit
)

func (s *bullPutSpreadStrategy) Name() string { return "BullPutSpread" }

func (s *bullPutSpreadStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("sma_period", s.smaPeriod)

	// Simple moving average of the underlying BTC spot price.
	ctx.Register("underlying_sma", backtest.SMA("underlying_price_close", s.smaPeriod))

	// Deviation P = (underlying − SMA) / SMA — positive means price is above average.
	ctx.Register("deviation_p", backtest.Custom(
		[]string{"underlying_price_close", "underlying_sma"},
		func(inputs map[string][]float64) []float64 {
			underlying := inputs["underlying_price_close"]
			sma := inputs["underlying_sma"]
			n := len(underlying)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(sma[i]) || sma[i] == 0 {
					out[i] = math.NaN()
				} else {
					out[i] = (underlying[i] - sma[i]) / sma[i]
				}
			}
			return out
		},
	))

	return nil
}

func (s *bullPutSpreadStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	delta := ctx.Field("delta")
	deviationP := ctx.Ind("deviation_p")

	if math.IsNaN(deviationP) || math.IsNaN(delta) {
		return
	}

	// Entry: bullish underlying AND put delta in the target OTM range.
	inEntryDeltaRange := delta >= bullPutMinDelta && delta <= bullPutMaxDelta
	bullish := deviationP > bullPutBullThreshold

	if bullish && inEntryDeltaRange && ctx.Position(primary) == 0 {
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

	// Exit: underlying turns bearish OR delta drifts outside the acceptable range.
	bearish := deviationP < -bullPutBearThreshold
	deltaOutOfRange := delta < bullPutExitMinDelta || delta > bullPutExitMaxDelta

	if (bearish || deltaOutOfRange) && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}
