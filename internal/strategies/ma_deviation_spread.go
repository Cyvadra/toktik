package strategies

import (
	"math"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// SpreadDirection determines whether the strategy trades bull or bear spreads.
type SpreadDirection int

const (
	BullSpread SpreadDirection = iota // Bull Put Spread (P > threshold)
	BearSpread                        // Bear Call Spread (P < -threshold)
)

// MADeviationSpreadStrategy implements the BTC Moving Average Deviation
// Spread Strategy from the v2 specification.
//
// Bull mode: when P > 0.15, opens a Bull Put Spread (sell put + buy put).
// Bear mode: when P < -0.15, opens a Bear Call Spread (sell call + buy call).
//
// Indicators are computed on the underlying asset's OHLC (the primary security).
// Options are selected dynamically via the OptionsChain API.
type MADeviationSpreadStrategy struct {
	Direction SpreadDirection

	// Indicator parameters
	MAPeriod int // SMA period (default 120)

	// Entry signal
	PThreshold float64 // deviation ratio threshold (default 0.15)

	// Option selection
	TargetExpiryDays int     // target days to expiry (default 15)
	MinExpiryDays    int     // minimum days to expiry (default 7)
	MinPremium       float64 // minimum bid price for short leg (default 0.025)

	// Short leg delta range (absolute values configured per direction)
	ShortDeltaMin float64 // e.g. 0.4
	ShortDeltaMax float64 // e.g. 0.5

	// Long leg delta range (absolute values configured per direction)
	LongDeltaMin float64 // e.g. 0.1
	LongDeltaMax float64 // e.g. 0.15

	// Position management
	ShortProfitPct float64       // close short leg at this unrealized profit % (default 0.88)
	LongProfitPct  float64       // close long leg immediately if profit reaches this (default 0.50)
	LongCloseDelay time.Duration // delay before closing long leg after short (default 24h)
	MaxHoldTime    time.Duration // maximum holding period (default 48h)

	// internal: tracks spread states during replay
	spreadStates map[int]*spreadState
}

// spreadState tracks per-spread lifecycle during bar replay.
type spreadState struct {
	spreadID       int
	shortLegClosed bool
	shortCloseTime time.Time
}

func (s *MADeviationSpreadStrategy) Name() string {
	if s.Direction == BullSpread {
		return "MADeviationBullPutSpread"
	}
	return "MADeviationBearCallSpread"
}

func (s *MADeviationSpreadStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()
	s.spreadStates = make(map[int]*spreadState)

	period := s.MAPeriod

	// SMA of the underlying close
	ctx.Register("ma", backtest.SMA("close", period))

	// Highest and Lowest for M calculation
	ctx.Register("highest_h", backtest.Highest("high", period))
	ctx.Register("lowest_c", backtest.Lowest("close", period))
	ctx.Register("highest_c", backtest.Highest("close", period))
	ctx.Register("lowest_l", backtest.Lowest("low", period))

	// M = max(Highest(H,n)-Lowest(C,n), Highest(C,n)-Lowest(L,n))
	ctx.Register("m_val", backtest.Custom(
		[]string{"highest_h", "lowest_c", "highest_c", "lowest_l"},
		func(inputs map[string][]float64) []float64 {
			hh := inputs["highest_h"]
			lc := inputs["lowest_c"]
			hc := inputs["highest_c"]
			ll := inputs["lowest_l"]
			n := len(hh)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(hh[i]) || math.IsNaN(lc[i]) || math.IsNaN(hc[i]) || math.IsNaN(ll[i]) {
					out[i] = math.NaN()
					continue
				}
				a := hh[i] - lc[i]
				b := hc[i] - ll[i]
				if a > b {
					out[i] = a
				} else {
					out[i] = b
				}
			}
			return out
		},
	))

	// P = (Close - ma) / M
	ctx.Register("p_ratio", backtest.Custom(
		[]string{"close", "ma", "m_val"},
		func(inputs map[string][]float64) []float64 {
			cls := inputs["close"]
			ma := inputs["ma"]
			m := inputs["m_val"]
			n := len(cls)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(cls[i]) || math.IsNaN(ma[i]) || math.IsNaN(m[i]) || m[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (cls[i] - ma[i]) / m[i]
			}
			return out
		},
	))

	return nil
}

func (s *MADeviationSpreadStrategy) OnBar(ctx *backtest.BarContext) {
	now := ctx.Time()

	// --- Manage existing spreads ---
	s.manageSpreads(ctx, now)

	// --- Check entry signal ---
	p := ctx.Ind("p_ratio")
	if math.IsNaN(p) {
		return
	}

	// Only enter if no open spreads exist
	if len(ctx.Spreads().OpenSpreads()) > 0 {
		return
	}

	entrySignal := false
	if s.Direction == BullSpread && p > s.PThreshold {
		entrySignal = true
	} else if s.Direction == BearSpread && p < -s.PThreshold {
		entrySignal = true
	}

	if !entrySignal {
		return
	}

	// --- Select options and open spread ---
	s.tryOpenSpread(ctx, now)
}

// manageSpreads handles profit-taking, scheduled closes, and max hold time.
func (s *MADeviationSpreadStrategy) manageSpreads(ctx *backtest.BarContext, now time.Time) {
	chain := ctx.OptionsChain()

	// Build a quick lookup of current mark prices from chain
	priceMap := make(map[string]float64)
	if chain != nil {
		for _, c := range chain.Contracts() {
			priceMap[c.Symbol] = c.MarkPrice
		}
	}

	markPrice := func(oc backtest.OptionContract) float64 {
		if p, ok := priceMap[oc.Symbol]; ok {
			return p
		}
		return oc.MarkPrice
	}

	for _, sp := range ctx.Spreads().OpenSpreads() {
		state := s.spreadStates[sp.ID]
		if state == nil {
			continue
		}

		// Check max hold time first
		if sp.TimeHeld(now) >= s.MaxHoldTime {
			ctx.CloseSpread(sp.ID, markPrice)
			continue
		}

		// Leg 0 = short leg, Leg 1 = long leg (by convention)
		shortLeg := &sp.Legs[0]
		longLeg := &sp.Legs[1]

		if !state.shortLegClosed && !shortLeg.Closed {
			// Check short leg profit: > 88% unrealized profit → close it
			shortMarkPrice := markPrice(shortLeg.Contract)
			pnlPct := sp.LegUnrealizedPnLPct(0, shortMarkPrice)
			if !math.IsNaN(pnlPct) && pnlPct > s.ShortProfitPct {
				ctx.CloseSpreadLeg(sp.ID, 0, shortMarkPrice)
				state.shortLegClosed = true
				state.shortCloseTime = now
			}
		}

		if state.shortLegClosed && !longLeg.Closed {
			// Short leg was closed. Check long leg conditions:
			// 1. Immediately if long leg profit >= 50%
			longMarkPrice := markPrice(longLeg.Contract)
			longPnlPct := sp.LegUnrealizedPnLPct(1, longMarkPrice)
			if !math.IsNaN(longPnlPct) && longPnlPct >= s.LongProfitPct {
				ctx.CloseSpreadLeg(sp.ID, 1, longMarkPrice)
				continue
			}
			// 2. After LongCloseDelay (24h) since short leg close
			if now.Sub(state.shortCloseTime) >= s.LongCloseDelay {
				ctx.CloseSpreadLeg(sp.ID, 1, longMarkPrice)
			}
		}
	}
}

// tryOpenSpread selects options from the chain and opens a spread.
func (s *MADeviationSpreadStrategy) tryOpenSpread(ctx *backtest.BarContext, now time.Time) {
	chain := ctx.OptionsChain()
	if chain == nil || chain.Len() == 0 {
		return
	}

	// Step 1: Filter by type (puts for bull, calls for bear)
	var typeChain *backtest.OptionsChain
	if s.Direction == BullSpread {
		typeChain = chain.Puts()
	} else {
		typeChain = chain.Calls()
	}

	if typeChain.Len() == 0 {
		return
	}

	// Step 2: Filter by minimum expiry, then find nearest to target
	expiryFiltered := typeChain.ExpiryMin(s.MinExpiryDays)
	if expiryFiltered.Len() == 0 {
		return
	}
	expiryChain := expiryFiltered.ExpiryNearest(s.TargetExpiryDays)
	if expiryChain.Len() == 0 {
		return
	}

	// Step 3: Find short leg candidates
	var shortDeltaMin, shortDeltaMax float64
	if s.Direction == BullSpread {
		// Sell put: delta between -0.5 and -0.4
		shortDeltaMin = -s.ShortDeltaMax // -0.5
		shortDeltaMax = -s.ShortDeltaMin // -0.4
	} else {
		// Sell call: delta between 0.4 and 0.5
		shortDeltaMin = s.ShortDeltaMin // 0.4
		shortDeltaMax = s.ShortDeltaMax // 0.5
	}

	shortCandidates := expiryChain.DeltaRange(shortDeltaMin, shortDeltaMax).MinPremium(s.MinPremium)
	if shortCandidates.Len() == 0 {
		return
	}

	// Select best short leg by spread quality
	shortLeg := shortCandidates.BestSpread()
	if shortLeg == nil {
		return
	}

	// Step 4: Find long leg candidates (same expiry as short leg)
	var longDeltaMin, longDeltaMax float64
	if s.Direction == BullSpread {
		// Buy put: delta between -0.15 and -0.1
		longDeltaMin = -s.LongDeltaMax // -0.15
		longDeltaMax = -s.LongDeltaMin // -0.1
	} else {
		// Buy call: delta between 0.1 and 0.15
		longDeltaMin = s.LongDeltaMin // 0.1
		longDeltaMax = s.LongDeltaMax // 0.15
	}

	longCandidates := expiryChain.SameExpiry(shortLeg).DeltaRange(longDeltaMin, longDeltaMax)
	if longCandidates.Len() == 0 {
		return
	}

	longLegContract := longCandidates.BestSpread()
	if longLegContract == nil {
		return
	}

	// Step 5: Open the spread (1 contract each)
	// Short leg: sell at bid price
	// Long leg: buy at ask price
	tag := "bull-put-spread"
	if s.Direction == BearSpread {
		tag = "bear-call-spread"
	}

	legs := []backtest.SpreadLeg{
		{
			Contract:   *shortLeg,
			Side:       backtest.Sell,
			Qty:        1,
			EntryPrice: shortLeg.BidPrice,
		},
		{
			Contract:   *longLegContract,
			Side:       backtest.Buy,
			Qty:        1,
			EntryPrice: longLegContract.AskPrice,
		},
	}

	spreadID := ctx.OpenSpread(legs, tag)
	if spreadID > 0 {
		s.spreadStates[spreadID] = &spreadState{spreadID: spreadID}

		// Schedule max hold time close as a safety net
		ctx.ScheduleCloseAfter(s.MaxHoldTime, spreadID)
	}
}

func (s *MADeviationSpreadStrategy) applyDefaults() {
	if s.MAPeriod == 0 {
		s.MAPeriod = 120
	}
	if s.PThreshold == 0 {
		s.PThreshold = 0.15
	}
	if s.TargetExpiryDays == 0 {
		s.TargetExpiryDays = 15
	}
	if s.MinExpiryDays == 0 {
		s.MinExpiryDays = 7
	}
	if s.MinPremium == 0 {
		s.MinPremium = 0.025
	}
	if s.ShortDeltaMin == 0 {
		s.ShortDeltaMin = 0.4
	}
	if s.ShortDeltaMax == 0 {
		s.ShortDeltaMax = 0.5
	}
	if s.LongDeltaMin == 0 {
		s.LongDeltaMin = 0.1
	}
	if s.LongDeltaMax == 0 {
		s.LongDeltaMax = 0.15
	}
	if s.ShortProfitPct == 0 {
		s.ShortProfitPct = 0.88
	}
	if s.LongProfitPct == 0 {
		s.LongProfitPct = 0.50
	}
	if s.LongCloseDelay == 0 {
		s.LongCloseDelay = 24 * time.Hour
	}
	if s.MaxHoldTime == 0 {
		s.MaxHoldTime = 48 * time.Hour
	}
}

// NewBullPutSpreadStrategy returns a pre-configured Bull Put Spread strategy
// matching the v2 specification defaults.
func NewBullPutSpreadStrategy() *MADeviationSpreadStrategy {
	return &MADeviationSpreadStrategy{Direction: BullSpread}
}

// NewBearCallSpreadStrategy returns a pre-configured Bear Call Spread strategy
// matching the v2 specification defaults.
func NewBearCallSpreadStrategy() *MADeviationSpreadStrategy {
	return &MADeviationSpreadStrategy{Direction: BearSpread}
}
