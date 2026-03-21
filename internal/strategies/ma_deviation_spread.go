package strategies

import (
	"math"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func init() {
	Register(Registration{
		Name:    "ma-deviation-bull",
		Aliases: []string{"bull-put-spread", "bull"},
		Groups:  []string{"spread"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return &MADeviationSpreadStrategy{
				Direction:          BullSpread,
				PositionSize:       cfg.PositionSize,
				MaxHoldTime:        cfg.MaxHoldTime,
				TargetExpiryDays:   cfg.TargetExpiryDays,
				MinExpiryDays:      cfg.MinExpiryDays,
				MinPremium:         cfg.MinPremium,
				ShortDeltaMin:      cfg.ShortDeltaMin,
				ShortDeltaMax:      cfg.ShortDeltaMax,
				LongDeltaMin:       cfg.LongDeltaMin,
				LongDeltaMax:       cfg.LongDeltaMax,
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				MAPeriod:           cfg.MAPeriod,
				PThreshold:         cfg.PThreshold,
			}, nil
		},
	})

	Register(Registration{
		Name:    "ma-deviation-bear",
		Aliases: []string{"bear-call-spread", "bear"},
		Groups:  []string{"spread"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return &MADeviationSpreadStrategy{
				Direction:          BearSpread,
				PositionSize:       cfg.PositionSize,
				MaxHoldTime:        cfg.MaxHoldTime,
				TargetExpiryDays:   cfg.TargetExpiryDays,
				MinExpiryDays:      cfg.MinExpiryDays,
				MinPremium:         cfg.MinPremium,
				ShortDeltaMin:      cfg.ShortDeltaMin,
				ShortDeltaMax:      cfg.ShortDeltaMax,
				LongDeltaMin:       cfg.LongDeltaMin,
				LongDeltaMax:       cfg.LongDeltaMax,
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				MAPeriod:           cfg.MAPeriod,
				PThreshold:         cfg.PThreshold,
			}, nil
		},
	})
}

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

	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

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
	PositionSize   float64       // contracts per leg (default 1)
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

func (s *MADeviationSpreadStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
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
		[]string{"highest_h", "lowest_c", "highest_c", "lowest_l", "compat_fallback"},
		func(inputs map[string][]float64) []float64 {
			hh := inputs["highest_h"]
			lc := inputs["lowest_c"]
			hc := inputs["highest_c"]
			ll := inputs["lowest_l"]
			fallback := inputs["compat_fallback"]
			n := len(hh)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(hh[i]) || math.IsNaN(lc[i]) || math.IsNaN(hc[i]) || math.IsNaN(ll[i]) || math.IsNaN(fallback[i]) {
					out[i] = math.NaN()
					continue
				}
				if fallback[i] >= 0.5 {
					out[i] = hc[i] - lc[i]
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
			maSeries := inputs["ma"]
			m := inputs["m_val"]
			n := len(cls)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(cls[i]) || math.IsNaN(maSeries[i]) || math.IsNaN(m[i]) || m[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (cls[i] - maSeries[i]) / m[i]
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

	contractMap := make(map[string]backtest.OptionContract)
	if chain != nil {
		for _, c := range chain.Contracts() {
			contractMap[c.Symbol] = c
		}
	}

	for _, sp := range ctx.Spreads().OpenSpreads() {
		state := s.spreadStates[sp.ID]
		if state == nil {
			continue
		}

		if sp.TimeHeld(now) >= s.MaxHoldTime {
			s.closeSpread(ctx, sp, contractMap)
			continue
		}

		shortLeg := &sp.Legs[0]
		longLeg := &sp.Legs[1]

		if !state.shortLegClosed && !shortLeg.Closed {
			shortMarkPrice := s.valuationPrice(*shortLeg, contractMap)
			pnlPct := sp.LegUnrealizedPnLPct(0, shortMarkPrice)
			if !math.IsNaN(pnlPct) && pnlPct > s.ShortProfitPct {
				ctx.CloseSpreadLeg(sp.ID, 0, s.exitPrice(*shortLeg, contractMap))
				state.shortLegClosed = true
				state.shortCloseTime = now
			}
		}

		if state.shortLegClosed && !longLeg.Closed {
			longMarkPrice := s.valuationPrice(*longLeg, contractMap)
			longPnlPct := sp.LegUnrealizedPnLPct(1, longMarkPrice)
			if !math.IsNaN(longPnlPct) && longPnlPct >= s.LongProfitPct {
				ctx.CloseSpreadLeg(sp.ID, 1, s.exitPrice(*longLeg, contractMap))
				continue
			}
			if now.Sub(state.shortCloseTime) >= s.LongCloseDelay {
				ctx.CloseSpreadLeg(sp.ID, 1, s.exitPrice(*longLeg, contractMap))
			}
		}
	}
}

// tryOpenSpread selects options from the chain and opens a spread.
func (s *MADeviationSpreadStrategy) tryOpenSpread(ctx *backtest.BarContext, _ time.Time) {
	chain := ctx.OptionsChain()
	if chain == nil || chain.Len() == 0 {
		return
	}

	var typeChain *backtest.OptionsChain
	if s.Direction == BullSpread {
		typeChain = chain.Puts()
	} else {
		typeChain = chain.Calls()
	}
	if typeChain.Len() == 0 {
		return
	}

	expiryFiltered := s.selectTargetExpiryChain(typeChain)
	if expiryFiltered.Len() == 0 {
		return
	}

	var shortDeltaMin, shortDeltaMax float64
	if s.Direction == BullSpread {
		shortDeltaMin = -s.ShortDeltaMax
		shortDeltaMax = -s.ShortDeltaMin
	} else {
		shortDeltaMin = s.ShortDeltaMin
		shortDeltaMax = s.ShortDeltaMax
	}

	var longDeltaMin, longDeltaMax float64
	if s.Direction == BullSpread {
		longDeltaMin = -s.LongDeltaMax
		longDeltaMax = -s.LongDeltaMin
	} else {
		longDeltaMin = s.LongDeltaMin
		longDeltaMax = s.LongDeltaMax
	}

	shortCandidates := expiryFiltered.DeltaRange(shortDeltaMin, shortDeltaMax).MinPremium(s.MinPremium)
	if shortCandidates.Len() == 0 {
		return
	}

	shortLeg := shortCandidates.BestSpread()
	if shortLeg == nil {
		return
	}

	longCandidates := expiryFiltered.SameExpiry(shortLeg).DeltaRange(longDeltaMin, longDeltaMax)
	if longCandidates.Len() == 0 {
		return
	}

	longLegContract := longCandidates.BestSpread()
	if longLegContract == nil {
		return
	}

	tag := "bull-put-spread"
	if s.Direction == BearSpread {
		tag = "bear-call-spread"
	}

	legs := []backtest.SpreadLeg{
		{
			Contract:   *shortLeg,
			Side:       backtest.Sell,
			Qty:        s.PositionSize,
			EntryPrice: s.EntryPriceMode.EntryPrice(backtest.Sell, *shortLeg),
		},
		{
			Contract:   *longLegContract,
			Side:       backtest.Buy,
			Qty:        s.PositionSize,
			EntryPrice: s.EntryPriceMode.EntryPrice(backtest.Buy, *longLegContract),
		},
	}

	spreadID := ctx.OpenSpread(legs, tag)
	if spreadID > 0 {
		s.spreadStates[spreadID] = &spreadState{spreadID: spreadID}
	}
}

func (s *MADeviationSpreadStrategy) selectTargetExpiryChain(chain *backtest.OptionsChain) *backtest.OptionsChain {
	if chain == nil || chain.Len() == 0 {
		return chain
	}

	filtered := chain.ExpiryRange(s.MinExpiryDays, s.TargetExpiryDays)
	if filtered.Len() == 0 {
		return filtered
	}

	return filtered.ExpiryNearest(s.TargetExpiryDays)
}

func (s *MADeviationSpreadStrategy) applyDefaults() {
	pricingDefaults := backtest.DefaultSpreadPricingConfig()
	if s.EntryPriceMode == backtest.OptionPriceModeUnspecified {
		s.EntryPriceMode = pricingDefaults.EntryMode
	}
	if s.ExitPriceMode == backtest.OptionPriceModeUnspecified {
		s.ExitPriceMode = pricingDefaults.ExitMode
	}
	if s.ValuationPriceMode == backtest.OptionPriceModeUnspecified {
		s.ValuationPriceMode = pricingDefaults.ValuationMode
	}
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
	if s.PositionSize == 0 {
		s.PositionSize = 1
	}
	if s.ShortProfitPct == 0 {
		s.ShortProfitPct = 0.5 // 0.5; 0.18; 0.88
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

func (s *MADeviationSpreadStrategy) currentContract(contract backtest.OptionContract, contractMap map[string]backtest.OptionContract) backtest.OptionContract {
	if updated, ok := contractMap[contract.Symbol]; ok {
		return updated
	}
	return contract
}

func (s *MADeviationSpreadStrategy) exitPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ExitPriceMode.ExitPrice(leg.Side, contract)
}

func (s *MADeviationSpreadStrategy) valuationPrice(leg backtest.SpreadLeg, contractMap map[string]backtest.OptionContract) float64 {
	contract := s.currentContract(leg.Contract, contractMap)
	return s.ValuationPriceMode.ExitPrice(leg.Side, contract)
}

func (s *MADeviationSpreadStrategy) closeSpread(ctx *backtest.BarContext, sp *backtest.SpreadPosition, contractMap map[string]backtest.OptionContract) {
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		ctx.CloseSpreadLeg(sp.ID, i, s.exitPrice(sp.Legs[i], contractMap))
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
