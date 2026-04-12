package backtest

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	annualizationDaysPerYear  = 365.0
	annualizationHoursPerYear = annualizationDaysPerYear * 24.0
)

func computeResult(
	strategyName string,
	trades []Trade,
	equityCurve []float64,
	timestamps []time.Time,
	initialCapital float64,
	accountUnit string,
	series map[string][]float64,
	reportColumns []ReportColumn,
) *Result {
	n := len(equityCurve)
	r := &Result{
		StrategyName:   strategyName,
		BarsCount:      n,
		InitialCapital: initialCapital,
		AccountUnit:    strings.TrimSpace(accountUnit),
		Trades:         trades,
		EquityCurve:    equityCurve,
		Timestamps:     timestamps,
		Series:         series,
		ReportColumns:  normalizeReportColumns(reportColumns, series),
	}

	if n > 0 {
		r.StartTime = timestamps[0]
		r.EndTime = timestamps[n-1]
		r.FinalEquity = equityCurve[n-1]
	}

	ApplyDerivedPerformance(r)

	// Trade statistics
	r.TotalTrades = len(trades)
	if r.TotalTrades > 0 {
		applyRoundTripStats(r, ComputeTradePnL(trades))

		for _, t := range trades {
			r.TotalFees += t.Commission
		}
	}

	r.TradeOverview = ComputeTradeOverview(trades)
	r.EquityAnalysis = ComputeEquityAnalysis(equityCurve, timestamps)

	return r
}

// ApplyDerivedPerformance recomputes the account, asset, quote, and benchmark
// performance snapshots from the raw equity curve and close series.
func ApplyDerivedPerformance(r *Result) {
	if r == nil {
		return
	}
	r.AccountPerformance = ComputePerformanceSnapshot(r.EquityCurve, r.Timestamps, r.InitialCapital)
	applyPrimaryPerformance(r, r.AccountPerformance)

	closeSeries := []float64(nil)
	if r.Series != nil {
		closeSeries = r.Series["close"]
	}
	r.AssetPerformance = nil
	r.QuotePerformance = nil
	r.BuyHoldPerformance = nil
	if len(closeSeries) == 0 {
		return
	}

	assetCurve, assetTimes, assetInitial := buildAssetBasisCurve(r.EquityCurve, r.Timestamps, closeSeries, r.InitialCapital, r.AccountUnit)
	if len(assetCurve) > 0 {
		r.AssetPerformance = ComputePerformanceSnapshot(assetCurve, assetTimes, assetInitial)
	}

	quoteCurve, quoteTimes, quoteInitial := buildQuoteBasisCurve(r.EquityCurve, r.Timestamps, closeSeries, r.InitialCapital, r.AccountUnit)
	if len(quoteCurve) > 0 {
		r.QuotePerformance = ComputePerformanceSnapshot(quoteCurve, quoteTimes, quoteInitial)
	}

	buyHoldCurve, buyHoldTimes, buyHoldInitial := buildBuyHoldQuoteCurve(r.Timestamps, closeSeries, r.InitialCapital, r.AccountUnit)
	if len(buyHoldCurve) > 0 {
		r.BuyHoldPerformance = ComputePerformanceSnapshot(buyHoldCurve, buyHoldTimes, buyHoldInitial)
	}
}

func applyPrimaryPerformance(r *Result, snapshot *PerformanceSnapshot) {
	if r == nil || snapshot == nil {
		return
	}
	r.FinalEquity = snapshot.FinalValue
	r.TotalReturn = snapshot.TotalReturn
	r.AnnualizedReturn = snapshot.AnnualizedReturn
	r.AnnualizedVolatility = snapshot.AnnualizedVolatility
	r.SharpeRatio = snapshot.SharpeRatio
	r.CalmarRatio = snapshot.CalmarRatio
	r.MaxDrawdown = snapshot.MaxDrawdown
	r.MaxDrawdownStart = snapshot.MaxDrawdownStart
	r.MaxDrawdownEnd = snapshot.MaxDrawdownEnd
}

// ComputePerformanceSnapshot computes the requested five risk/return metrics on
// a sanitized curve: annual return, annualized volatility, max drawdown,
// Sharpe ratio, and Calmar ratio. Risk-free rate is fixed at 0%.
func ComputePerformanceSnapshot(curve []float64, timestamps []time.Time, initialValue float64) *PerformanceSnapshot {
	values, times := sanitizePerformanceSeries(curve, timestamps)
	if len(values) == 0 {
		return nil
	}
	if !performanceValueValid(initialValue) || initialValue == 0 {
		initialValue = values[0]
	}
	snapshot := &PerformanceSnapshot{
		InitialValue: initialValue,
		FinalValue:   values[len(values)-1],
	}
	if initialValue != 0 {
		snapshot.TotalReturn = (snapshot.FinalValue - initialValue) / initialValue
	}
	if len(times) > 1 {
		years := times[len(times)-1].Sub(times[0]).Hours() / annualizationHoursPerYear
		if years > 0 && snapshot.FinalValue > 0 && initialValue > 0 {
			snapshot.AnnualizedReturn = math.Pow(snapshot.FinalValue/initialValue, 1.0/years) - 1
		}
	}
	snapshot.MaxDrawdown, snapshot.MaxDrawdownStart, snapshot.MaxDrawdownEnd = ComputeMaxDrawdown(values, initialValue)
	snapshot.AnnualizedVolatility = ComputeAnnualizedVolatility(values, times)
	snapshot.SharpeRatio = ComputeSharpe(values, times)
	snapshot.CalmarRatio = ComputeCalmar(snapshot.AnnualizedReturn, snapshot.MaxDrawdown)
	return snapshot
}

func sanitizePerformanceSeries(curve []float64, timestamps []time.Time) ([]float64, []time.Time) {
	n := performanceMinInt(len(curve), len(timestamps))
	if n == 0 {
		return nil, nil
	}
	values := make([]float64, 0, n)
	times := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		value := curve[i]
		if !performanceValueValid(value) {
			continue
		}
		values = append(values, value)
		times = append(times, timestamps[i])
	}
	return values, times
}

func buildAssetBasisCurve(equity []float64, timestamps []time.Time, closeSeries []float64, initialCapital float64, accountUnit string) ([]float64, []time.Time, float64) {
	n := performanceMinInt(len(equity), performanceMinInt(len(timestamps), len(closeSeries)))
	if n == 0 {
		return nil, nil, 0
	}
	curve := make([]float64, 0, n)
	times := make([]time.Time, 0, n)
	initial := 0.0
	for i := 0; i < n; i++ {
		closeValue := closeSeries[i]
		equityValue := equity[i]
		if !performanceValueValid(closeValue) || closeValue <= 0 || !performanceValueValid(equityValue) {
			continue
		}
		value := equityValue
		if isUSDLikeUnit(accountUnit) || strings.TrimSpace(accountUnit) == "" {
			value = equityValue / closeValue
		}
		if initial == 0 {
			initial = initialCapital
			if isUSDLikeUnit(accountUnit) || strings.TrimSpace(accountUnit) == "" {
				initial = initialCapital / closeValue
			}
		}
		curve = append(curve, value)
		times = append(times, timestamps[i])
	}
	return curve, times, initial
}

func buildQuoteBasisCurve(equity []float64, timestamps []time.Time, closeSeries []float64, initialCapital float64, accountUnit string) ([]float64, []time.Time, float64) {
	n := performanceMinInt(len(equity), performanceMinInt(len(timestamps), len(closeSeries)))
	if n == 0 {
		return nil, nil, 0
	}
	curve := make([]float64, 0, n)
	times := make([]time.Time, 0, n)
	initial := initialCapital
	for i := 0; i < n; i++ {
		closeValue := closeSeries[i]
		equityValue := equity[i]
		if !performanceValueValid(equityValue) {
			continue
		}
		value := equityValue
		if !isUSDLikeUnit(accountUnit) && strings.TrimSpace(accountUnit) != "" {
			if !performanceValueValid(closeValue) || closeValue <= 0 {
				continue
			}
			value = equityValue * closeValue
			if len(curve) == 0 {
				initial = initialCapital * closeValue
			}
		}
		curve = append(curve, value)
		times = append(times, timestamps[i])
	}
	return curve, times, initial
}

func buildBuyHoldQuoteCurve(timestamps []time.Time, closeSeries []float64, initialCapital float64, accountUnit string) ([]float64, []time.Time, float64) {
	n := performanceMinInt(len(timestamps), len(closeSeries))
	if n == 0 {
		return nil, nil, 0
	}
	entryIndex := -1
	entryClose := 0.0
	for i := 0; i < n; i++ {
		if performanceValueValid(closeSeries[i]) && closeSeries[i] > 0 {
			entryIndex = i
			entryClose = closeSeries[i]
			break
		}
	}
	if entryIndex < 0 {
		return nil, nil, 0
	}
	initialQuote := initialCapital
	if !isUSDLikeUnit(accountUnit) && strings.TrimSpace(accountUnit) != "" {
		initialQuote = initialCapital * entryClose
	}
	if !performanceValueValid(initialQuote) || initialQuote <= 0 {
		return nil, nil, 0
	}
	curve := make([]float64, 0, n-entryIndex)
	times := make([]time.Time, 0, n-entryIndex)
	for i := entryIndex; i < n; i++ {
		closeValue := closeSeries[i]
		if !performanceValueValid(closeValue) || closeValue <= 0 {
			continue
		}
		curve = append(curve, initialQuote*(closeValue/entryClose))
		times = append(times, timestamps[i])
	}
	return curve, times, initialQuote
}

func performanceValueValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func performanceMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isUSDLikeUnit(unit string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(unit))
	switch trimmed {
	case "", "USD", "USDT", "USDC", "BUSD", "FDUSD":
		return true
	default:
		return false
	}
}

// ComputeCalmar computes Calmar ratio from annualized return and max drawdown.
func ComputeCalmar(annualizedReturn, maxDrawdown float64) float64 {
	if math.IsNaN(annualizedReturn) || math.IsNaN(maxDrawdown) {
		return math.NaN()
	}
	if maxDrawdown == 0 {
		if annualizedReturn > 0 {
			return math.Inf(1)
		}
		if annualizedReturn < 0 {
			return math.Inf(-1)
		}
		return 0
	}
	return annualizedReturn / maxDrawdown
}

func normalizeReportColumns(columns []ReportColumn, series map[string][]float64) []ReportColumn {
	if len(columns) == 0 || len(series) == 0 {
		return nil
	}
	filtered := make([]ReportColumn, 0, len(columns))
	for _, column := range columns {
		source := strings.TrimSpace(column.Source)
		if source == "" {
			continue
		}
		if _, ok := series[source]; !ok {
			continue
		}
		label := strings.TrimSpace(column.Label)
		if label == "" {
			label = source
		}
		decimals := column.Decimals
		if decimals < 0 {
			decimals = 0
		}
		filtered = append(filtered, ReportColumn{
			Source:   source,
			Label:    label,
			Decimals: decimals,
			Overlay:  column.Overlay,
		})
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// ComputeTradeOverview aggregates raw fill and round-trip level trade metrics.
func ComputeTradeOverview(trades []Trade) *TradeOverview {
	overview := &TradeOverview{RawFills: len(trades)}
	if len(trades) == 0 {
		return overview
	}

	for _, trade := range trades {
		overview.TotalNotional += math.Abs(trade.Qty * trade.FillPrice)
		if trade.Side == Buy {
			overview.LongFills++
		} else {
			overview.ShortFills++
		}
		overview.AvgCommissionPerFill += trade.Commission
	}

	pnls := ComputeTradePnL(trades)
	overview.RoundTrips = len(pnls)
	for _, pnl := range pnls {
		overview.NetPnL += pnl
		if pnl > 0 {
			overview.GrossProfit += pnl
		} else if pnl < 0 {
			overview.GrossLoss += -pnl
		}
	}

	overview.AvgCommissionPerFill /= float64(len(trades))
	if len(pnls) > 0 {
		overview.AvgPnLPerRoundTrip = overview.NetPnL / float64(len(pnls))
	}

	return overview
}

// ComputeEquityAnalysis captures higher-level diagnostics on the equity curve.
func ComputeEquityAnalysis(equityCurve []float64, timestamps []time.Time) *EquityAnalysis {
	if len(equityCurve) == 0 {
		return nil
	}

	analysis := &EquityAnalysis{
		PeakEquity:     equityCurve[0],
		LowestEquity:   equityCurve[0],
		BestBarReturn:  math.Inf(-1),
		WorstBarReturn: math.Inf(1),
	}
	if len(timestamps) > 0 {
		analysis.PeakTime = timestamps[0]
		analysis.LowestTime = timestamps[0]
	}

	peakIndex := 0
	currentDrawdownStart := -1
	for i, eq := range equityCurve {
		if eq > analysis.PeakEquity {
			if currentDrawdownStart >= 0 {
				durationBars := i - currentDrawdownStart
				if durationBars > analysis.MaxDrawdownDurationBars {
					analysis.MaxDrawdownDurationBars = durationBars
					analysis.MaxDrawdownDuration = durationHours(timestamps, currentDrawdownStart, i)
				}
				currentDrawdownStart = -1
			}
			analysis.PeakEquity = eq
			peakIndex = i
			if i < len(timestamps) {
				analysis.PeakTime = timestamps[i]
			}
		}
		if eq < analysis.LowestEquity {
			analysis.LowestEquity = eq
			if i < len(timestamps) {
				analysis.LowestTime = timestamps[i]
			}
		}
		if eq < analysis.PeakEquity && currentDrawdownStart < 0 {
			currentDrawdownStart = peakIndex
		}
	}
	if currentDrawdownStart >= 0 {
		durationBars := len(equityCurve) - 1 - currentDrawdownStart
		if durationBars > analysis.MaxDrawdownDurationBars {
			analysis.MaxDrawdownDurationBars = durationBars
			analysis.MaxDrawdownDuration = durationHours(timestamps, currentDrawdownStart, len(equityCurve)-1)
		}
	}

	if len(equityCurve) == 1 {
		analysis.BestBarReturn = 0
		analysis.WorstBarReturn = 0
		return analysis
	}

	returns := make([]float64, 0, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		prev := equityCurve[i-1]
		if prev == 0 {
			continue
		}
		ret := (equityCurve[i] - prev) / prev
		returns = append(returns, ret)
		if ret > analysis.BestBarReturn {
			analysis.BestBarReturn = ret
		}
		if ret < analysis.WorstBarReturn {
			analysis.WorstBarReturn = ret
		}
		switch {
		case ret > 0:
			analysis.PositiveBars++
		case ret < 0:
			analysis.NegativeBars++
		default:
			analysis.FlatBars++
		}
	}
	if len(returns) == 0 {
		analysis.BestBarReturn = 0
		analysis.WorstBarReturn = 0
		return analysis
	}
	_, analysis.BarReturnVolatility = meanStd(returns)
	return analysis
}

func durationHours(timestamps []time.Time, start, end int) float64 {
	if start < 0 || end < 0 || start >= len(timestamps) || end >= len(timestamps) || end < start {
		return 0
	}
	return timestamps[end].Sub(timestamps[start]).Hours()
}

// ComputeTradePnL pairs entries and exits to compute per-round-trip PnL.
// Entry-side commissions are tracked and subtracted proportionally when a
// position is partially or fully closed.
func ComputeTradePnL(trades []Trade) []float64 {
	type openEntry struct {
		side       Side
		qty        float64
		price      float64
		commission float64 // accumulated entry-side commission
	}

	// Track pending entries per security
	pending := make(map[SecurityRef]*openEntry)
	var pnls []float64

	for _, t := range trades {
		// Skip zero or negative-quantity fills – they carry no position delta
		// and dividing by their qty below would produce NaN/Inf.
		if t.Qty <= 0 {
			continue
		}
		entry, hasPending := pending[t.Security]
		if !hasPending {
			// New entry
			pending[t.Security] = &openEntry{side: t.Side, qty: t.Qty, price: t.FillPrice, commission: t.Commission}
			continue
		}

		if entry.side != t.Side {
			// Closing trade
			var pnl float64
			closeQty := t.Qty
			if closeQty > entry.qty {
				closeQty = entry.qty
			}
			if entry.side == Buy {
				pnl = closeQty * (t.FillPrice - entry.price)
			} else {
				pnl = closeQty * (entry.price - t.FillPrice)
			}
			// Subtract only the close-side commission attributable to the portion
			// that actually offsets the existing position, plus proportional entry commission.
			entryCommission := 0.0
			if entry.qty > 0 {
				entryCommission = entry.commission * (closeQty / entry.qty)
			}
			closeCommission := t.Commission
			if t.Qty > 0 && closeQty < t.Qty {
				closeCommission = t.Commission * (closeQty / t.Qty)
			}
			pnl -= closeCommission + entryCommission
			pnls = append(pnls, pnl)

			remaining := entry.qty - closeQty
			if remaining > 0 {
				entry.qty = remaining
				entry.commission -= entryCommission
			} else {
				excess := t.Qty - closeQty
				if excess > 0 {
					entryCommissionRemainder := t.Commission - closeCommission
					pending[t.Security] = &openEntry{side: t.Side, qty: excess, price: t.FillPrice, commission: entryCommissionRemainder}
				} else {
					delete(pending, t.Security)
				}
			}
		} else {
			// Adding to position
			totalQty := entry.qty + t.Qty
			if totalQty > 0 {
				entry.price = (entry.price*entry.qty + t.FillPrice*t.Qty) / totalQty
			}
			entry.commission += t.Commission
			entry.qty = totalQty
		}
	}

	return pnls
}

func meanStd(data []float64) (float64, float64) {
	n := len(data)
	if n == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(n)

	if n < 2 {
		return mean, 0
	}

	sumSq := 0.0
	for _, v := range data {
		d := v - mean
		sumSq += d * d
	}
	// Use sample standard deviation (÷N-1) per financial convention
	variance := sumSq / float64(n-1)
	return mean, math.Sqrt(variance)
}

// inferBarsPerYear estimates how many bars occur in one calendar year by
// computing the median inter-bar duration from the timestamp series.
// Falls back to 365 (daily bars) when the series is too short to measure.
func inferBarsPerYear(timestamps []time.Time) float64 {
	if len(timestamps) < 2 {
		return annualizationDaysPerYear
	}
	durs := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		h := timestamps[i].Sub(timestamps[i-1]).Hours()
		if h > 0 {
			durs = append(durs, h)
		}
	}
	if len(durs) == 0 {
		return annualizationDaysPerYear
	}
	sort.Float64s(durs)
	mid := len(durs) / 2
	var medianHours float64
	if len(durs)%2 == 0 {
		medianHours = (durs[mid-1] + durs[mid]) / 2.0
	} else {
		medianHours = durs[mid]
	}
	return annualizationHoursPerYear / medianHours
}

// ComputeMaxDrawdown computes max drawdown, start index, and end index from an
// equity curve. initialCapital is used as the initial high-water mark; if <= 0,
// the first equity value is used instead.
func ComputeMaxDrawdown(equity []float64, initialCapital float64) (float64, int, int) {
	if len(equity) == 0 {
		return 0, 0, 0
	}
	peak := initialCapital
	if peak <= 0 {
		peak = equity[0]
	}
	ddStart := 0
	maxDD := 0.0
	maxDDStart, maxDDEnd := 0, 0
	for i, eq := range equity {
		if eq > peak {
			peak = eq
			ddStart = i
		}
		if peak <= 0 {
			continue
		}
		dd := (peak - eq) / peak
		if dd > maxDD {
			maxDD = dd
			maxDDStart = ddStart
			maxDDEnd = i
		}
	}
	return maxDD, maxDDStart, maxDDEnd
}

// ComputeSharpe computes the annualized Sharpe ratio from an equity curve.
// The bar interval is inferred from timestamps for accurate annualisation.
func ComputeSharpe(equity []float64, timestamps []time.Time) float64 {
	returns := computeReturnSeries(equity)
	return computeSharpeFromReturns(returns, timestamps)
}

// ComputeAnnualizedVolatility computes annualized return volatility from an
// equity curve using the actual bar interval inferred from timestamps.
func ComputeAnnualizedVolatility(equity []float64, timestamps []time.Time) float64 {
	returns := computeReturnSeries(equity)
	if len(returns) == 0 {
		return 0
	}
	_, stddev := meanStd(returns)
	if stddev == 0 {
		return 0
	}
	return stddev * math.Sqrt(inferBarsPerYear(timestamps))
}

func computeSharpeFromReturns(returns []float64, timestamps []time.Time) float64 {
	if len(returns) == 0 {
		return 0
	}
	mean, stddev := meanStd(returns)
	if stddev == 0 {
		return 0
	}
	barsPerYear := inferBarsPerYear(timestamps)
	return (mean / stddev) * math.Sqrt(barsPerYear)
}

func computeReturnSeries(equity []float64) []float64 {
	n := len(equity)
	if n <= 1 {
		return nil
	}
	returns := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		prev := equity[i-1]
		if prev == 0 {
			continue
		}
		returns = append(returns, (equity[i]-prev)/prev)
	}
	return returns
}

// ApplyTradeSummary recomputes win/loss trade statistics on a Result from its
// own Trades slice. This is used when trades from multiple results are merged
// and the summary metrics need to be recalculated from scratch.
func ApplyTradeSummary(r *Result) {
	if r == nil {
		return
	}
	r.TotalTrades = len(r.Trades)
	r.WinningTrades = 0
	r.LosingTrades = 0
	r.ProfitFactor = 0
	r.AvgWin = 0
	r.AvgLoss = 0
	r.WinRate = 0

	applyRoundTripStats(r, ComputeTradePnL(r.Trades))
}

// applyRoundTripStats populates win/loss metrics on r from a pre-computed
// slice of per-round-trip PnL values. When there are completed round trips but
// neither side has any gain or loss (all break-even), ProfitFactor is set to
// 1.0 rather than the zero value to avoid misleading comparisons.
func applyRoundTripStats(r *Result, pnls []float64) {
	grossWins := 0.0
	grossLosses := 0.0
	for _, pnl := range pnls {
		if pnl > 0 {
			r.WinningTrades++
			grossWins += pnl
		} else if pnl < 0 {
			r.LosingTrades++
			grossLosses += -pnl
		}
	}
	if len(pnls) > 0 {
		r.WinRate = float64(r.WinningTrades) / float64(len(pnls))
	}
	switch {
	case grossLosses > 0:
		r.ProfitFactor = grossWins / grossLosses
	case grossWins > 0:
		r.ProfitFactor = math.Inf(1)
	case len(pnls) > 0:
		// All round trips broke even: profit factor is exactly 1.
		r.ProfitFactor = 1.0
	}
	if r.WinningTrades > 0 {
		r.AvgWin = grossWins / float64(r.WinningTrades)
	}
	if r.LosingTrades > 0 {
		r.AvgLoss = grossLosses / float64(r.LosingTrades)
	}
}
