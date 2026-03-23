package madeviationforumshortput

import (
	"math"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	forumShortPutDefaultMAPeriod         = 120
	forumShortPutDefaultPThreshold       = 0.15
	forumShortPutDefaultTargetExpiryDays = 14
	forumShortPutDefaultPositionSize     = 1
	forumShortPutDefaultHoldTime         = 24 * time.Hour
	forumShortPutDefaultStrikeOffset     = -1000
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    "forum-short-put",
		Aliases: []string{"ma-deviation-forum", "forum"},
		Groups:  []string{"single-leg"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &MADeviationForumShortPutStrategy{
				PositionSize:       cfg.PositionSize,
				EntryPriceMode:     cfg.EntryPriceMode,
				ExitPriceMode:      cfg.ExitPriceMode,
				ValuationPriceMode: cfg.ValuationPriceMode,
				MAPeriod:           cfg.MAPeriod,
				PThreshold:         cfg.PThreshold,
			}, nil
		},
	})
}

// MADeviationForumShortPutStrategy implements the single-leg short put strategy
// described in the forum research note.
type MADeviationForumShortPutStrategy struct {
	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	MAPeriod   int
	PThreshold float64

	TargetExpiryDays int
	StrikeOffset     float64
	MinPremium       float64

	PositionSize float64
	HoldTime     time.Duration
}

func (s *MADeviationForumShortPutStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

func (s *MADeviationForumShortPutStrategy) Name() string {
	return "MADeviationForumShortPut"
}

func (s *MADeviationForumShortPutStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	period := s.MAPeriod

	ctx.Register("ma", backtest.SMA("close", period))
	ctx.Register("highest_h", backtest.Highest("high", period))
	ctx.Register("lowest_c", backtest.Lowest("close", period))
	ctx.Register("highest_c", backtest.Highest("close", period))
	ctx.Register("lowest_l", backtest.Lowest("low", period))

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

func (s *MADeviationForumShortPutStrategy) OnBar(ctx *backtest.BarContext) {
	if len(ctx.Spreads().OpenSpreads()) > 0 {
		return
	}

	p := ctx.Ind("p_ratio")
	if math.IsNaN(p) || p <= s.PThreshold {
		return
	}

	s.tryOpenShortPut(ctx)
}

func (s *MADeviationForumShortPutStrategy) tryOpenShortPut(ctx *backtest.BarContext) {
	chain := ctx.OptionsChain()
	if chain == nil || chain.Len() == 0 {
		return
	}

	puts := chain.Puts()
	if puts.Len() == 0 {
		return
	}

	expiryFiltered := puts.ExpiryNearest(s.TargetExpiryDays)
	if expiryFiltered.Len() == 0 {
		return
	}

	shortLeg := s.selectContract(expiryFiltered, ctx.Close())
	if shortLeg == nil {
		return
	}

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Sell, *shortLeg)
	if math.IsNaN(entryPrice) || math.IsInf(entryPrice, 0) || entryPrice <= 0 {
		return
	}

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   *shortLeg,
		Side:       backtest.Sell,
		Qty:        s.PositionSize,
		EntryPrice: entryPrice,
	}}, "forum-short-put")
	if spreadID > 0 {
		ctx.ScheduleCloseAfter(s.HoldTime, spreadID)
	}
}

func (s *MADeviationForumShortPutStrategy) selectContract(chain *backtest.OptionsChain, underlyingClose float64) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	filtered := chain
	if s.MinPremium > 0 {
		filtered = filtered.MinPremium(s.MinPremium)
	}
	if filtered.Len() == 0 {
		return nil
	}

	targetStrike := s.targetStrike(filtered.Contracts(), underlyingClose)
	bestIndex := -1
	bestStrikeDiff := math.Inf(1)
	bestSpreadRatio := math.Inf(1)
	bestVolume := -1.0
	contracts := filtered.Contracts()

	for i := range contracts {
		strikeDiff := math.Abs(contracts[i].StrikePrice - targetStrike)
		spreadRatio := contracts[i].SpreadRatio()
		if strikeDiff < bestStrikeDiff ||
			(strikeDiff == bestStrikeDiff && spreadRatio < bestSpreadRatio) ||
			(strikeDiff == bestStrikeDiff && spreadRatio == bestSpreadRatio && contracts[i].Volume > bestVolume) {
			bestIndex = i
			bestStrikeDiff = strikeDiff
			bestSpreadRatio = spreadRatio
			bestVolume = contracts[i].Volume
		}
	}

	if bestIndex < 0 {
		return nil
	}

	contract := contracts[bestIndex]
	return &contract
}

func (s *MADeviationForumShortPutStrategy) targetStrike(contracts []backtest.OptionContract, fallbackUnderlying float64) float64 {
	underlying := fallbackUnderlying
	for _, contract := range contracts {
		if contract.UnderlyingPrice > 0 {
			underlying = contract.UnderlyingPrice
			break
		}
	}

	atmFloor := math.NaN()
	for _, contract := range contracts {
		if contract.StrikePrice <= underlying && (math.IsNaN(atmFloor) || contract.StrikePrice > atmFloor) {
			atmFloor = contract.StrikePrice
		}
	}
	if math.IsNaN(atmFloor) {
		best := math.NaN()
		bestDiff := math.Inf(1)
		for _, contract := range contracts {
			diff := math.Abs(contract.StrikePrice - underlying)
			if diff < bestDiff {
				best = contract.StrikePrice
				bestDiff = diff
			}
		}
		atmFloor = best
	}

	return atmFloor + s.StrikeOffset
}

func (s *MADeviationForumShortPutStrategy) applyDefaults() {
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
		s.MAPeriod = forumShortPutDefaultMAPeriod
	}
	if s.PThreshold == 0 {
		s.PThreshold = forumShortPutDefaultPThreshold
	}
	if s.TargetExpiryDays == 0 {
		s.TargetExpiryDays = forumShortPutDefaultTargetExpiryDays
	}
	if s.PositionSize == 0 {
		s.PositionSize = forumShortPutDefaultPositionSize
	}
	if s.HoldTime == 0 {
		s.HoldTime = forumShortPutDefaultHoldTime
	}
	if s.StrikeOffset == 0 {
		s.StrikeOffset = forumShortPutDefaultStrikeOffset
	}
}

// NewForumShortPutStrategy returns a forum-style single-leg short put strategy.
func NewForumShortPutStrategy() *MADeviationForumShortPutStrategy {
	return &MADeviationForumShortPutStrategy{}
}
