package strategies

import (
	"math"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func init() {
	Register(Registration{
		Name:    "option-trend-replacement-long",
		Aliases: []string{"trend-replacement", "option-trend-replacement", "otr-long"},
		Groups:  []string{"single-leg"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return newOptionTrendReplacementStrategy(optionTrendLongCall, cfg), nil
		},
	})

	Register(Registration{
		Name:    "option-trend-replacement-short-put",
		Aliases: []string{"otr-short", "trend-replacement-short"},
		Groups:  []string{"single-leg"},
		Factory: func(cfg Config) (backtest.Strategy, error) {
			return newOptionTrendReplacementStrategy(optionTrendShortPut, cfg), nil
		},
	})
}

type optionTrendMode int

const (
	optionTrendLongCall optionTrendMode = iota
	optionTrendShortPut
)

type optionTrendReplacementStrategy struct {
	mode optionTrendMode

	EntryPriceMode     backtest.OptionPriceMode
	ExitPriceMode      backtest.OptionPriceMode
	ValuationPriceMode backtest.OptionPriceMode

	TargetDTEDays int
	BaseOrderBTC  float64

	MaxAdds int

	activeCycle  bool
	addCount     int
	lastAddPrice float64
	ivHistory    []float64
}

func newOptionTrendReplacementStrategy(mode optionTrendMode, cfg Config) *optionTrendReplacementStrategy {
	maxAdds := 2
	if mode == optionTrendShortPut {
		maxAdds = 1
	}
	return &optionTrendReplacementStrategy{
		mode:               mode,
		EntryPriceMode:     cfg.EntryPriceMode,
		ExitPriceMode:      cfg.ExitPriceMode,
		ValuationPriceMode: cfg.ValuationPriceMode,
		TargetDTEDays:      35,
		BaseOrderBTC:       1.0,
		MaxAdds:            maxAdds,
		ivHistory:          make([]float64, 0, 256),
	}
}

func (s *optionTrendReplacementStrategy) Name() string {
	if s.mode == optionTrendLongCall {
		return "OptionTrendReplacementLongCall"
	}
	return "OptionTrendReplacementShortPut"
}

func (s *optionTrendReplacementStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return backtest.SpreadPricingConfig{
		EntryMode:     s.EntryPriceMode,
		ExitMode:      s.ExitPriceMode,
		ValuationMode: s.ValuationPriceMode,
	}.WithDefaults()
}

func (s *optionTrendReplacementStrategy) Init(ctx *backtest.SetupContext) error {
	s.applyDefaults()

	ctx.Register("atr20", backtest.ATR(20))
	ctx.Register("donchian_upper20", backtest.Highest("high", 20))
	ctx.Register("donchian_lower20", backtest.Lowest("low", 20))

	ctx.Register("std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStd(inputs["close"], 20)
		},
	))
	ctx.Register("std20_ma20", backtest.SMA("std20", 20))
	ctx.Register("std20_ma20_q35_120", backtest.Quantile("std20_ma20", 120, 0.35))

	ctx.Register("std_ratio", backtest.Custom(
		[]string{"std20", "std20_ma20"},
		func(inputs map[string][]float64) []float64 {
			std := inputs["std20"]
			ma := inputs["std20_ma20"]
			n := len(std)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(std[i]) || math.IsNaN(ma[i]) || ma[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = std[i] / ma[i]
			}
			return out
		},
	))
	ctx.Register("std_ratio_q35_120", backtest.Quantile("std_ratio", 120, 0.35))

	ctx.Register("close_ma20", backtest.SMA("close", 20))
	ctx.Register("boll_upper20", backtest.Custom(
		[]string{"close_ma20", "std20"},
		func(inputs map[string][]float64) []float64 {
			ma := inputs["close_ma20"]
			std := inputs["std20"]
			n := len(ma)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(ma[i]) || math.IsNaN(std[i]) {
					out[i] = math.NaN()
					continue
				}
				out[i] = ma[i] + 2*std[i]
			}
			return out
		},
	))
	ctx.Register("boll_lower20", backtest.Custom(
		[]string{"close_ma20", "std20"},
		func(inputs map[string][]float64) []float64 {
			ma := inputs["close_ma20"]
			std := inputs["std20"]
			n := len(ma)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(ma[i]) || math.IsNaN(std[i]) {
					out[i] = math.NaN()
					continue
				}
				out[i] = ma[i] - 2*std[i]
			}
			return out
		},
	))

	ctx.Register("upper_trigger", backtest.Custom(
		[]string{"donchian_upper20", "boll_upper20"},
		func(inputs map[string][]float64) []float64 {
			donch := inputs["donchian_upper20"]
			boll := inputs["boll_upper20"]
			n := len(donch)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(donch[i]) || math.IsNaN(boll[i]) {
					out[i] = math.NaN()
					continue
				}
				out[i] = math.Max(donch[i], boll[i])
			}
			return out
		},
	))
	ctx.Register("lower_trigger", backtest.Custom(
		[]string{"donchian_lower20", "boll_lower20", "atr20"},
		func(inputs map[string][]float64) []float64 {
			donch := inputs["donchian_lower20"]
			boll := inputs["boll_lower20"]
			atr := inputs["atr20"]
			n := len(donch)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(donch[i]) || math.IsNaN(boll[i]) || math.IsNaN(atr[i]) {
					out[i] = math.NaN()
					continue
				}
				out[i] = math.Min(donch[i], boll[i]) - 0.5*atr[i]
			}
			return out
		},
	))

	return nil
}

func (s *optionTrendReplacementStrategy) OnBar(ctx *backtest.BarContext) {
	now := ctx.Time()
	chain := ctx.OptionsChain()
	if chain == nil || chain.Len() == 0 {
		return
	}

	if ivValue := s.currentIVIndex(chain); optionFinite(ivValue) {
		s.ivHistory = append(s.ivHistory, ivValue)
	}

	contractMap := map[string]backtest.OptionContract{}
	for _, c := range chain.Contracts() {
		contractMap[c.Symbol] = c
	}

	s.manageOpenPositions(ctx, now, contractMap)
	openCount := len(ctx.Spreads().OpenSpreads())
	if openCount == 0 {
		s.activeCycle = false
		s.addCount = 0
		s.lastAddPrice = 0
	}

	atr := ctx.Ind("atr20")
	if optionFinite(atr) && s.activeCycle && s.addCount < s.MaxAdds && openCount < s.maxSlots() {
		if s.shouldAdd(ctx.Close(), atr) {
			if s.executeStandardOrder(ctx, chain, "add") {
				s.addCount++
				s.lastAddPrice = ctx.Close()
				openCount++
			}
		}
	}

	if openCount >= s.maxSlots() {
		return
	}

	if s.entrySignal(ctx) && s.executeStandardOrder(ctx, chain, "entry") {
		s.activeCycle = true
		s.addCount = 0
		s.lastAddPrice = ctx.Close()
	}
}

func (s *optionTrendReplacementStrategy) manageOpenPositions(ctx *backtest.BarContext, now time.Time, contractMap map[string]backtest.OptionContract) {
	for _, sp := range ctx.Spreads().OpenSpreads() {
		if len(sp.Legs) == 0 {
			continue
		}
		leg := sp.Legs[0]
		if leg.Closed {
			continue
		}

		current := leg.Contract
		if updated, ok := contractMap[leg.Contract.Symbol]; ok {
			current = updated
		}

		mark := s.ValuationPriceMode.ExitPrice(leg.Side, current)
		if !optionFinite(mark) || leg.EntryPrice <= 0 {
			continue
		}

		pnlPct := (mark - leg.EntryPrice) / leg.EntryPrice
		if pnlPct <= -0.80 {
			ctx.CloseSpreadLeg(sp.ID, 0, s.ExitPriceMode.ExitPrice(leg.Side, current))
			continue
		}

		if math.Abs(current.Delta) > 0.55 || pnlPct > 0.66 {
			exitPrice := s.ExitPriceMode.ExitPrice(leg.Side, current)
			if !optionFinite(exitPrice) {
				continue
			}
			if !ctx.CloseSpreadLeg(sp.ID, 0, exitPrice) {
				continue
			}
			// Rolling keeps the directional exposure by reopening immediately.
			s.executeStandardOrder(ctx, ctx.OptionsChain(), "roll")
		}
	}
	_ = now
}

func (s *optionTrendReplacementStrategy) shouldAdd(price, atr float64) bool {
	if !optionFinite(price) || !optionFinite(atr) || atr <= 0 {
		return false
	}
	step := 0.75 * atr
	if s.mode == optionTrendLongCall {
		return price >= s.lastAddPrice+step
	}
	return price <= s.lastAddPrice-step
}

func (s *optionTrendReplacementStrategy) entrySignal(ctx *backtest.BarContext) bool {
	if !s.volatilityCompressedRecent3(ctx) {
		return false
	}

	closeNow := ctx.Close()
	closePrev := ctx.FieldAt("close", 1)
	if !optionFinite(closeNow) || !optionFinite(closePrev) {
		return false
	}

	if s.mode == optionTrendLongCall {
		triggerPrev := ctx.IndAt("upper_trigger", 1)
		if !optionFinite(triggerPrev) {
			return false
		}
		return closeNow > triggerPrev && closePrev <= triggerPrev
	}

	triggerPrev := ctx.IndAt("lower_trigger", 1)
	if !optionFinite(triggerPrev) {
		return false
	}
	return closeNow < triggerPrev && closePrev >= triggerPrev
}

func (s *optionTrendReplacementStrategy) volatilityCompressedRecent3(ctx *backtest.BarContext) bool {
	for i := 0; i < 3; i++ {
		if s.mode == optionTrendLongCall {
			v := ctx.IndAt("std20_ma20", i)
			q := ctx.IndAt("std20_ma20_q35_120", i)
			if optionFinite(v) && optionFinite(q) && v <= q {
				return true
			}
			continue
		}

		v := ctx.IndAt("std_ratio", i)
		q := ctx.IndAt("std_ratio_q35_120", i)
		if optionFinite(v) && optionFinite(q) && v <= q {
			return true
		}
	}
	return false
}

func (s *optionTrendReplacementStrategy) executeStandardOrder(ctx *backtest.BarContext, chain *backtest.OptionsChain, tagSuffix string) bool {
	contract := s.selectContract(chain)
	if contract == nil {
		return false
	}

	entryPrice := s.EntryPriceMode.EntryPrice(backtest.Buy, *contract)
	if !optionFinite(entryPrice) {
		return false
	}

	ivPercentile := rollingPercentileRank(s.ivHistory, 100, contract.IV)
	multiplier := s.ivMultiplier(ivPercentile)
	notional := multiplier * s.BaseOrderBTC
	if notional <= 0 {
		return false
	}
	qty := notional / entryPrice
	if !optionFinite(qty) || qty <= 0 {
		return false
	}

	tag := "trend-replacement-long-call"
	if s.mode == optionTrendShortPut {
		tag = "trend-replacement-short-put"
	}
	if tagSuffix != "" {
		tag = tag + "-" + tagSuffix
	}

	spreadID := ctx.OpenSpread([]backtest.SpreadLeg{{
		Contract:   *contract,
		Side:       backtest.Buy,
		Qty:        qty,
		EntryPrice: entryPrice,
	}}, tag)
	return spreadID > 0
}

func (s *optionTrendReplacementStrategy) selectContract(chain *backtest.OptionsChain) *backtest.OptionContract {
	if chain == nil || chain.Len() == 0 {
		return nil
	}

	typed := chain.Calls()
	targetDelta := 0.33
	if s.mode == optionTrendShortPut {
		typed = chain.Puts()
		targetDelta = -0.33
	}
	if typed.Len() == 0 {
		return nil
	}

	expiry := typed.ExpiryNearest(s.TargetDTEDays)
	if expiry.Len() == 0 {
		return nil
	}

	contracts := expiry.Contracts()
	type candidate struct {
		contract backtest.OptionContract
		deltaGap float64
		spread   float64
	}
	candidates := make([]candidate, 0, len(contracts))
	for _, c := range contracts {
		price := s.EntryPriceMode.EntryPrice(backtest.Buy, c)
		if !optionFinite(price) {
			continue
		}
		if !optionFinite(c.Delta) {
			continue
		}
		candidates = append(candidates, candidate{
			contract: c,
			deltaGap: math.Abs(c.Delta - targetDelta),
			spread:   c.SpreadRatio(),
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].deltaGap != candidates[j].deltaGap {
			return candidates[i].deltaGap < candidates[j].deltaGap
		}
		if candidates[i].spread != candidates[j].spread {
			return candidates[i].spread < candidates[j].spread
		}
		return candidates[i].contract.Volume > candidates[j].contract.Volume
	})

	selected := candidates[0].contract
	return &selected
}

func (s *optionTrendReplacementStrategy) currentIVIndex(chain *backtest.OptionsChain) float64 {
	if chain == nil || chain.Len() == 0 {
		return math.NaN()
	}

	typed := chain.Calls()
	if s.mode == optionTrendShortPut {
		typed = chain.Puts()
	}
	if typed.Len() == 0 {
		return math.NaN()
	}

	expiry := typed.ExpiryNearest(s.TargetDTEDays)
	if expiry.Len() == 0 {
		return math.NaN()
	}

	vals := make([]float64, 0, expiry.Len())
	for _, c := range expiry.Contracts() {
		if optionFinite(c.IV) {
			vals = append(vals, c.IV)
		}
	}
	if len(vals) == 0 {
		return math.NaN()
	}
	sort.Float64s(vals)
	return vals[len(vals)/2]
}

func (s *optionTrendReplacementStrategy) ivMultiplier(percentile float64) float64 {
	if s.mode == optionTrendShortPut {
		switch {
		case percentile < 35:
			return 1.2
		case percentile < 60:
			return 1.0
		case percentile < 85:
			return 0.7
		default:
			return 0.5
		}
	}

	switch {
	case percentile < 35:
		return 1.2
	case percentile < 60:
		return 1.0
	default:
		return 0.7
	}
}

func (s *optionTrendReplacementStrategy) maxSlots() int {
	return 1 + s.MaxAdds
}

func (s *optionTrendReplacementStrategy) applyDefaults() {
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
	if s.TargetDTEDays <= 0 {
		s.TargetDTEDays = 35
	}
	if s.BaseOrderBTC <= 0 {
		s.BaseOrderBTC = 1.0
	}
	if s.MaxAdds < 0 {
		s.MaxAdds = 0
	}
}

func rollingStd(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	for i := 0; i < n; i++ {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		sum := 0.0
		valid := true
		for j := i - period + 1; j <= i; j++ {
			if math.IsNaN(src[j]) {
				valid = false
				break
			}
			sum += src[j]
		}
		if !valid {
			out[i] = math.NaN()
			continue
		}
		mean := sum / float64(period)
		var accum float64
		for j := i - period + 1; j <= i; j++ {
			d := src[j] - mean
			accum += d * d
		}
		out[i] = math.Sqrt(accum / float64(period))
	}
	return out
}

func rollingPercentileRank(history []float64, window int, value float64) float64 {
	if !optionFinite(value) {
		return 50
	}
	if window <= 0 {
		window = len(history)
	}
	start := len(history) - window
	if start < 0 {
		start = 0
	}

	vals := make([]float64, 0, len(history)-start)
	for i := start; i < len(history); i++ {
		if optionFinite(history[i]) {
			vals = append(vals, history[i])
		}
	}
	if len(vals) == 0 {
		return 50
	}

	count := 0
	for _, v := range vals {
		if v <= value {
			count++
		}
	}
	return 100 * float64(count) / float64(len(vals))
}

func optionFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
