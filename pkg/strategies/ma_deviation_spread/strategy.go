package madeviationspread

import (
	"fmt"
	"math"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

const (
	defaultStrategyName  = "ma-deviation-spread"
	defaultAlias1        = "ma_deviation_spread"
	defaultAlias2        = "ma-spread"
	defaultPositionSize  = 10.0
	divergenceLookback   = 5
	atrPeriod            = 20
	volatilityPeriod     = 20
	volatilityMAPeriod   = 20
	volatilityWindow     = 100
	volatilityQuantile   = 0.5
	shortSignalBarsValid = 5
	rsiPeriod            = 200
	shortRSIThreshold    = 50.0
	longRSIThreshold     = 50.0
	trailATRMultiplier   = 3.0

	interval3h  = "3h"
	interval6h  = "6h"
	interval12h = "12h"
	interval24h = "1d"
)

func init() {
	catalog.Register(catalog.Registration{
		Name:    defaultStrategyName,
		Aliases: []string{defaultAlias1, defaultAlias2},
		Groups:  []string{"spot", "signal", "divergence"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &strategy{
				positionSize:       catalog.FloatOrDefault(cfg.PositionSize, defaultPositionSize),
				direction:          cfg.Direction,
				highestSinceEntry:  math.NaN(),
				lowestSinceEntry:   math.NaN(),
				wideTopBarsValid:   shortSignalBarsValid,
				trailATRMultiplier: trailATRMultiplier,
			}, nil
		},
	})
}

type strategy struct {
	positionSize       float64
	direction          catalog.TradeDirection
	highestSinceEntry  float64
	lowestSinceEntry   float64
	wideTopBarsValid   int
	trailATRMultiplier float64

	ref3h  backtest.SecurityRef
	ref6h  backtest.SecurityRef
	ref12h backtest.SecurityRef
	ref24h backtest.SecurityRef
}

func (s *strategy) Name() string { return "MADeviationSpreadSpot" }

func (s *strategy) ReportColumns() []backtest.ReportColumn {
	return []backtest.ReportColumn{
		{Source: "rsi_12h_prev", Label: "RSI 200 12h", Decimals: 2},
		{Source: "vol_ratio", Label: "Vol Ratio", Decimals: 4},
		{Source: "vol_threshold", Label: "Vol Median 100", Decimals: 4},
		{Source: "vol_condition", Label: "Vol OK", Decimals: 0},
		{Source: "div_base_top", Label: "Base Top Div", Decimals: 0},
		{Source: "div_base_bot", Label: "Base Bot Div", Decimals: 0},
		{Source: "div_wide_top", Label: "12h/24h Top Div", Decimals: 0},
		{Source: "div_quick_bot", Label: "3h/6h Bot Div", Decimals: 0},
		{Source: "bars_since_wide_top", Label: "Bars Since Wide Top", Decimals: 0},
		{Source: "cond_short", Label: "Short Signal", Decimals: 0},
		{Source: "cond_long", Label: "Long Signal", Decimals: 0},
		{Source: "atr20", Label: "ATR 20", Decimals: 2},
	}
}

func (s *strategy) Init(ctx *backtest.SetupContext) error {
	primary := ctx.PrimaryRef()
	s.ref3h = ctx.AddSecurity(primary.Market, primary.Symbol, interval3h)
	s.ref6h = ctx.AddSecurity(primary.Market, primary.Symbol, interval6h)
	s.ref12h = ctx.AddSecurity(primary.Market, primary.Symbol, interval12h)
	s.ref24h = ctx.AddSecurity(primary.Market, primary.Symbol, interval24h)

	ctx.SetParam("position_size", s.positionSize)
	ctx.SetParam("divergence_lookback", divergenceLookback)
	ctx.SetParam("wide_top_bars_valid", s.wideTopBarsValid)
	ctx.SetParam("trail_atr_multiplier", s.trailATRMultiplier)

	ctx.Register("atr20", backtest.ATR(atrPeriod))
	ctx.Register("std20", backtest.Custom(
		[]string{"close"},
		func(inputs map[string][]float64) []float64 {
			return rollingStdDev(inputs["close"], volatilityPeriod)
		},
	))
	ctx.Register("ma_std20", backtest.SMA("std20", volatilityMAPeriod))
	ctx.Register("vol_ratio", backtest.Custom(
		[]string{"std20", "ma_std20"},
		func(inputs map[string][]float64) []float64 {
			std20 := inputs["std20"]
			maStd20 := inputs["ma_std20"]
			out := make([]float64, len(std20))
			for i := range out {
				if i >= len(maStd20) || math.IsNaN(std20[i]) || math.IsNaN(maStd20[i]) || maStd20[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = std20[i] / maStd20[i]
			}
			return out
		},
	))
	ctx.Register("vol_threshold", backtest.Quantile("vol_ratio", volatilityWindow, volatilityQuantile))
	ctx.Register("vol_condition", backtest.Custom(
		[]string{"vol_ratio", "vol_threshold"},
		func(inputs map[string][]float64) []float64 {
			ratio := inputs["vol_ratio"]
			threshold := inputs["vol_threshold"]
			out := make([]float64, len(ratio))
			for i := range out {
				if i >= len(threshold) || math.IsNaN(ratio[i]) || math.IsNaN(threshold[i]) {
					out[i] = 0
					continue
				}
				if ratio[i] > threshold[i] {
					out[i] = 1
				}
			}
			return out
		},
	))
	ctx.Register("macd", backtest.MACD("close", 12, 26, 9))

	ctx.RegisterOn(s.ref3h, "macd", backtest.MACD("close", 12, 26, 9))
	ctx.RegisterOn(s.ref6h, "macd", backtest.MACD("close", 12, 26, 9))
	ctx.RegisterOn(s.ref12h, "macd", backtest.MACD("close", 12, 26, 9))
	ctx.RegisterOn(s.ref24h, "macd", backtest.MACD("close", 12, 26, 9))
	ctx.RegisterOn(s.ref12h, "rsi_12h", backtest.RSI("close", rsiPeriod))

	return nil
}

func (s *strategy) Preload(ctx *backtest.PreloadContext) error {
	primary := ctx.Primary()
	if primary == nil || primary.Len() == 0 {
		return nil
	}

	baseTop, baseBot := computeDivergenceSignals(
		primary.Column("high"),
		primary.Column("low"),
		primary.Column("macd"),
		primary.Column("macd_hist"),
		divergenceLookback,
	)
	if err := primary.SetColumn("div_base_top", baseTop); err != nil {
		return err
	}
	if err := primary.SetColumn("div_base_bot", baseBot); err != nil {
		return err
	}

	if err := s.setShiftedAlignedDivergenceColumns(ctx, s.ref3h, "div_3h"); err != nil {
		return err
	}
	if err := s.setShiftedAlignedDivergenceColumns(ctx, s.ref6h, "div_6h"); err != nil {
		return err
	}
	if err := s.setShiftedAlignedDivergenceColumns(ctx, s.ref12h, "div_12h"); err != nil {
		return err
	}
	if err := s.setShiftedAlignedDivergenceColumns(ctx, s.ref24h, "div_24h"); err != nil {
		return err
	}
	if err := s.setShiftedAlignedNumericColumn(ctx, s.ref12h, "rsi_12h", "rsi_12h_prev"); err != nil {
		return err
	}

	divWideTop := orBinarySeries(primary.Column("div_12h_top_prev"), primary.Column("div_24h_top_prev"))
	divQuickBot := orBinarySeries(primary.Column("div_3h_bot_prev"), primary.Column("div_6h_bot_prev"))
	if err := primary.SetColumn("div_wide_top", divWideTop); err != nil {
		return err
	}
	if err := primary.SetColumn("div_quick_bot", divQuickBot); err != nil {
		return err
	}

	barsSinceWideTop, condLong, condShort := buildEntrySignalColumns(
		primary.Timestamps(),
		primary.Column("open"),
		primary.Column("close"),
		divWideTop,
		divQuickBot,
		primary.Column("rsi_12h_prev"),
		primary.Column("vol_condition"),
		s.wideTopBarsValid,
	)
	if err := primary.SetColumn("bars_since_wide_top", barsSinceWideTop); err != nil {
		return err
	}
	if err := primary.SetColumn("cond_long", condLong); err != nil {
		return err
	}
	if err := primary.SetColumn("cond_short", condShort); err != nil {
		return err
	}

	return nil
}

func (s *strategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	position := ctx.Position(primary)

	if position == 0 {
		s.highestSinceEntry = math.NaN()
		s.lowestSinceEntry = math.NaN()
	}

	if s.handleExit(ctx, primary, position) {
		return
	}

	longSignal := ctx.Ind("cond_long") == 1 && s.allowsLong()
	shortSignal := ctx.Ind("cond_short") == 1 && s.allowsShort()
	if longSignal && shortSignal {
		return
	}

	qty := s.positionSize
	if qty <= 0 {
		return
	}

	switch {
	case longSignal && position <= 0:
		orderQty := qty
		note := "quick bullish divergence"
		if position < 0 {
			orderQty += -position
			note = "reverse to long on quick bullish divergence"
		}
		if ctx.BuyNowWithNote(primary, orderQty, note) {
			s.highestSinceEntry = ctx.High()
			s.lowestSinceEntry = math.NaN()
		}
	case shortSignal && position >= 0:
		orderQty := qty
		note := "wide bearish divergence"
		if position > 0 {
			orderQty += position
			note = "reverse to short on wide bearish divergence"
		}
		if ctx.SellNowWithNote(primary, orderQty, note) {
			s.lowestSinceEntry = ctx.Low()
			s.highestSinceEntry = math.NaN()
		}
	}
}

func (s *strategy) handleExit(ctx *backtest.BarContext, primary backtest.SecurityRef, position float64) bool {
	atr := ctx.Ind("atr20")
	if math.IsNaN(atr) || atr <= 0 || position == 0 {
		return false
	}

	if position > 0 {
		if math.IsNaN(s.highestSinceEntry) || ctx.High() > s.highestSinceEntry {
			s.highestSinceEntry = ctx.High()
		}
		if !math.IsNaN(s.highestSinceEntry) && s.highestSinceEntry-ctx.Low() > s.trailATRMultiplier*atr {
			if ctx.SellNowWithNote(primary, position, fmt.Sprintf("exit long: %.1fx ATR pullback", s.trailATRMultiplier)) {
				s.highestSinceEntry = math.NaN()
				s.lowestSinceEntry = math.NaN()
				return true
			}
		}
		return false
	}

	if math.IsNaN(s.lowestSinceEntry) || ctx.Low() < s.lowestSinceEntry {
		s.lowestSinceEntry = ctx.Low()
	}
	if !math.IsNaN(s.lowestSinceEntry) && ctx.High()-s.lowestSinceEntry > s.trailATRMultiplier*atr {
		if ctx.BuyNowWithNote(primary, -position, fmt.Sprintf("exit short: %.1fx ATR rebound", s.trailATRMultiplier)) {
			s.highestSinceEntry = math.NaN()
			s.lowestSinceEntry = math.NaN()
			return true
		}
	}
	return false
}

func (s *strategy) allowsLong() bool {
	return s.direction == "" || s.direction == catalog.DirectionBoth || s.direction == catalog.DirectionLongOnly
}

func (s *strategy) allowsShort() bool {
	return s.direction == "" || s.direction == catalog.DirectionBoth || s.direction == catalog.DirectionShortOnly
}

func (s *strategy) setShiftedAlignedDivergenceColumns(ctx *backtest.PreloadContext, ref backtest.SecurityRef, prefix string) error {
	sec := ctx.Security(ref)
	if sec == nil || sec.Len() == 0 {
		return nil
	}

	top, bot := computeDivergenceSignals(
		sec.Column("high"),
		sec.Column("low"),
		sec.Column("macd"),
		sec.Column("macd_hist"),
		divergenceLookback,
	)
	if err := sec.SetColumn("div_top_raw", top); err != nil {
		return err
	}
	if err := sec.SetColumn("div_bot_raw", bot); err != nil {
		return err
	}

	shiftedTop := shiftSeries(top, 0)
	shiftedBot := shiftSeries(bot, 0)
	if err := sec.SetColumn("div_top_prev", shiftedTop); err != nil {
		return err
	}
	if err := sec.SetColumn("div_bot_prev", shiftedBot); err != nil {
		return err
	}

	alignedTop, err := ctx.ColumnAlignedToPrimary(ref, "div_top_prev")
	if err != nil {
		return err
	}
	alignedBot, err := ctx.ColumnAlignedToPrimary(ref, "div_bot_prev")
	if err != nil {
		return err
	}
	if err := ctx.Primary().SetColumn(prefix+"_top_prev", alignedTop); err != nil {
		return err
	}
	if err := ctx.Primary().SetColumn(prefix+"_bot_prev", alignedBot); err != nil {
		return err
	}
	return nil
}

func (s *strategy) setShiftedAlignedNumericColumn(ctx *backtest.PreloadContext, ref backtest.SecurityRef, source, target string) error {
	sec := ctx.Security(ref)
	if sec == nil || sec.Len() == 0 {
		return nil
	}
	col, err := sec.RequireColumn(source)
	if err != nil {
		return err
	}
	shifted := shiftSeries(col, math.NaN())
	if err := sec.SetColumn(source+"_prev", shifted); err != nil {
		return err
	}
	aligned, err := ctx.ColumnAlignedToPrimary(ref, source+"_prev")
	if err != nil {
		return err
	}
	return ctx.Primary().SetColumn(target, aligned)
}

func buildEntrySignalColumns(
	timestamps []time.Time,
	open []float64,
	close []float64,
	divWideTop []float64,
	divQuickBot []float64,
	rsi12h []float64,
	volCondition []float64,
	wideTopBarsValid int,
) ([]float64, []float64, []float64) {
	barsSinceWideTop := barsSinceBinary(divWideTop)
	n := minLen(len(timestamps), len(open), len(close), len(divWideTop), len(divQuickBot), len(rsi12h), len(volCondition))
	condLong := make([]float64, n)
	condShort := make([]float64, n)

	for i := 0; i < n; i++ {
		if timestamps[i].Year() < 2023 {
			continue
		}
		if volCondition[i] != 1 || math.IsNaN(rsi12h[i]) {
			continue
		}

		if divQuickBot[i] == 1 && rsi12h[i] < longRSIThreshold {
			condLong[i] = 1
		}

		if !math.IsNaN(barsSinceWideTop[i]) && barsSinceWideTop[i] <= float64(wideTopBarsValid) && close[i] < open[i] && rsi12h[i] > shortRSIThreshold {
			condShort[i] = 1
		}
	}

	return barsSinceWideTop[:n], condLong, condShort
}

func computeDivergenceSignals(high, low, dif, hist []float64, lookback int) ([]float64, []float64) {
	n := minLen(len(high), len(low), len(dif), len(hist))
	top := make([]float64, n)
	bot := make([]float64, n)
	if lookback <= 0 {
		return top, bot
	}

	var topPriceBase, topHistBase, topDifBase float64 = math.NaN(), math.NaN(), math.NaN()
	var botPriceBase, botHistBase, botDifBase float64 = math.NaN(), math.NaN(), math.NaN()

	for i := 0; i < n; i++ {
		pivotHigh := isPivotHigh(high, i, lookback)
		if pivotHigh {
			pivotIdx := i - lookback
			aHist := hist[pivotIdx]
			aDif := dif[pivotIdx]
			curPrice := high[pivotIdx]
			if !math.IsNaN(topPriceBase) && curPrice > topPriceBase {
				histDiv := !math.IsNaN(aHist) && !math.IsNaN(topHistBase) && aHist < topHistBase-0.5*math.Abs(topHistBase)
				difDiv := !math.IsNaN(aDif) && !math.IsNaN(topDifBase) && aDif < topDifBase
				if histDiv || difDiv {
					top[i] = 1
				}
			}
			topPriceBase = curPrice
			topHistBase = aHist
			topDifBase = aDif
		}

		pivotLow := isPivotLow(low, i, lookback)
		if pivotLow {
			pivotIdx := i - lookback
			aHist := hist[pivotIdx]
			aDif := dif[pivotIdx]
			curPrice := low[pivotIdx]
			if !math.IsNaN(botPriceBase) && curPrice < botPriceBase {
				histDiv := !math.IsNaN(aHist) && !math.IsNaN(botHistBase) && aHist > botHistBase+0.5*math.Abs(botHistBase)
				difDiv := !math.IsNaN(aDif) && !math.IsNaN(botDifBase) && aDif > botDifBase
				if histDiv || difDiv {
					bot[i] = 1
				}
			}
			botPriceBase = curPrice
			botHistBase = aHist
			botDifBase = aDif
		}
	}

	return top, bot
}

func isPivotHigh(series []float64, currentIndex, lookback int) bool {
	pivotIdx := currentIndex - lookback
	if pivotIdx < lookback || pivotIdx < 0 || currentIndex >= len(series) {
		return false
	}
	if pivotIdx+lookback != currentIndex {
		return false
	}
	pivotVal := series[pivotIdx]
	if math.IsNaN(pivotVal) {
		return false
	}
	for i := pivotIdx - lookback; i <= pivotIdx+lookback; i++ {
		if i < 0 || i >= len(series) {
			return false
		}
		if i == pivotIdx {
			continue
		}
		if math.IsNaN(series[i]) || series[i] >= pivotVal {
			return false
		}
	}
	return true
}

func isPivotLow(series []float64, currentIndex, lookback int) bool {
	pivotIdx := currentIndex - lookback
	if pivotIdx < lookback || pivotIdx < 0 || currentIndex >= len(series) {
		return false
	}
	if pivotIdx+lookback != currentIndex {
		return false
	}
	pivotVal := series[pivotIdx]
	if math.IsNaN(pivotVal) {
		return false
	}
	for i := pivotIdx - lookback; i <= pivotIdx+lookback; i++ {
		if i < 0 || i >= len(series) {
			return false
		}
		if i == pivotIdx {
			continue
		}
		if math.IsNaN(series[i]) || series[i] <= pivotVal {
			return false
		}
	}
	return true
}

func barsSinceBinary(series []float64) []float64 {
	out := make([]float64, len(series))
	lastTrue := -1
	for i := range series {
		out[i] = math.NaN()
		if series[i] == 1 {
			lastTrue = i
			out[i] = 0
			continue
		}
		if lastTrue >= 0 {
			out[i] = float64(i - lastTrue)
		}
	}
	return out
}

func orBinarySeries(a, b []float64) []float64 {
	n := minLen(len(a), len(b))
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if a[i] == 1 || b[i] == 1 {
			out[i] = 1
		}
	}
	return out
}

func shiftSeries(src []float64, headValue float64) []float64 {
	out := make([]float64, len(src))
	if len(src) == 0 {
		return out
	}
	out[0] = headValue
	copy(out[1:], src[:len(src)-1])
	return out
}

func rollingStdDev(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 1 {
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
		sumSq := 0.0
		valid := 0
		for j := i - period + 1; j <= i; j++ {
			v := src[j]
			if math.IsNaN(v) {
				valid = 0
				break
			}
			sum += v
			sumSq += v * v
			valid++
		}
		if valid != period {
			out[i] = math.NaN()
			continue
		}
		mean := sum / float64(period)
		variance := sumSq/float64(period) - mean*mean
		if variance < 0 {
			variance = 0
		}
		out[i] = math.Sqrt(variance)
	}
	return out
}

func minLen(first int, rest ...int) int {
	minValue := first
	for _, value := range rest {
		if value < minValue {
			minValue = value
		}
	}
	return minValue
}
