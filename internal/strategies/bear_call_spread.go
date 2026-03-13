package strategies

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// bearCallSpreadStrategy is a bearish options strategy that buys a position
// when the underlying BTC price is trading below its moving average (negative
// deviation P) and the option delta falls within the target call range.
//
// Entry signal (all conditions must hold):
//   - Deviation P = (underlying_price_close - SMA) / SMA < −bearThreshold
//   - Option delta in [minDelta, maxDelta]  (e.g. +0.15 … +0.40 for OTM calls)
//
// Exit signal (any condition triggers):
//   - Deviation P > +bullThreshold  (underlying turns bullish)
//   - Option delta drifts outside the acceptable call range
type bearCallSpreadStrategy struct {
	smaPeriod int
	entryTWAP int
}

const (
	bearCallBearThreshold = 0.01  // −1 % below SMA to enter
	bearCallBullThreshold = 0.01  // +1 % above SMA to exit
	bearCallMinDelta      = 0.15  // lower bound of target call delta
	bearCallMaxDelta      = 0.40  // upper bound of target call delta
	bearCallExitMinDelta  = 0.05  // hard floor — delta too far OTM → exit
	bearCallExitMaxDelta  = 0.50  // hard ceiling — delta too deep ITM → exit
)

func (s *bearCallSpreadStrategy) Name() string { return "BearCallSpread" }

func (s *bearCallSpreadStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("sma_period", s.smaPeriod)

	// Simple moving average of the underlying BTC spot price.
	ctx.Register("underlying_sma", backtest.SMA("underlying_price_close", s.smaPeriod))

	// Deviation P = (underlying − SMA) / SMA — negative means price is below average.
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

func (s *bearCallSpreadStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	delta := ctx.Field("delta")
	deviationP := ctx.Ind("deviation_p")

	if math.IsNaN(deviationP) || math.IsNaN(delta) {
		return
	}

	// Entry: bearish underlying AND call delta in the target OTM range.
	inEntryDeltaRange := delta >= bearCallMinDelta && delta <= bearCallMaxDelta
	bearish := deviationP < -bearCallBearThreshold

	if bearish && inEntryDeltaRange && ctx.Position(primary) == 0 {
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

	// Exit: underlying turns bullish OR delta drifts outside the acceptable range.
	bullish := deviationP > bearCallBullThreshold
	deltaOutOfRange := delta < bearCallExitMinDelta || delta > bearCallExitMaxDelta

	if (bullish || deltaOutOfRange) && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}
