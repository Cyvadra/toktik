package backtest

import (
	"math"
	"sort"
	"strings"
	"time"
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

	// Total return
	if initialCapital > 0 {
		r.TotalReturn = (r.FinalEquity - initialCapital) / initialCapital
	}

	// Annualized return
	if n > 1 {
		years := r.EndTime.Sub(r.StartTime).Hours() / (365.25 * 24)
		if years > 0 && r.FinalEquity > 0 && initialCapital > 0 {
			r.AnnualizedReturn = math.Pow(r.FinalEquity/initialCapital, 1.0/years) - 1
		}
	}

	// Max drawdown
	r.MaxDrawdown, r.MaxDrawdownStart, r.MaxDrawdownEnd = ComputeMaxDrawdown(equityCurve, initialCapital)

	// Sharpe ratio – annualised using the actual bar interval inferred from timestamps
	r.SharpeRatio = ComputeSharpe(equityCurve, timestamps)

	// Trade statistics
	r.TotalTrades = len(trades)
	if r.TotalTrades > 0 {
		// Group trades into round trips to compute win/loss
		// Simple approach: pair consecutive buy/sell on same security
		pnlByTrade := ComputeTradePnL(trades)
		grossWins := 0.0
		grossLosses := 0.0
		for _, pnl := range pnlByTrade {
			if pnl > 0 {
				r.WinningTrades++
				grossWins += pnl
			} else if pnl < 0 {
				r.LosingTrades++
				grossLosses += -pnl
			}
		}

		if len(pnlByTrade) > 0 {
			r.WinRate = float64(r.WinningTrades) / float64(len(pnlByTrade))
		}
		if grossLosses > 0 {
			r.ProfitFactor = grossWins / grossLosses
		} else if grossWins > 0 {
			r.ProfitFactor = math.Inf(1)
		}
		if r.WinningTrades > 0 {
			r.AvgWin = grossWins / float64(r.WinningTrades)
		}
		if r.LosingTrades > 0 {
			r.AvgLoss = grossLosses / float64(r.LosingTrades)
		}

		for _, t := range trades {
			r.TotalFees += t.Commission
		}
	}

	r.TradeOverview = ComputeTradeOverview(trades)
	r.EquityAnalysis = ComputeEquityAnalysis(equityCurve, timestamps)

	return r
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
			entryCommission := entry.commission * (closeQty / entry.qty)
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
			entry.price = (entry.price*entry.qty + t.FillPrice*t.Qty) / totalQty
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
// Falls back to 252 (daily bars) when the series is too short to measure.
func inferBarsPerYear(timestamps []time.Time) float64 {
	if len(timestamps) < 2 {
		return 252
	}
	durs := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		h := timestamps[i].Sub(timestamps[i-1]).Hours()
		if h > 0 {
			durs = append(durs, h)
		}
	}
	if len(durs) == 0 {
		return 252
	}
	sort.Float64s(durs)
	mid := len(durs) / 2
	var medianHours float64
	if len(durs)%2 == 0 {
		medianHours = (durs[mid-1] + durs[mid]) / 2.0
	} else {
		medianHours = durs[mid]
	}
	const hoursPerYear = 365.25 * 24.0
	return hoursPerYear / medianHours
}

func buildSpreadPositionReports(tracker *SpreadTracker, endTime time.Time) []SpreadPositionReport {
	if tracker == nil || len(tracker.All()) == 0 {
		return nil
	}
	reports := make([]SpreadPositionReport, 0, len(tracker.All()))
	for _, spread := range tracker.All() {
		report := SpreadPositionReport{
			ID:          spread.ID,
			Tag:         spread.Tag,
			Status:      "open",
			OpenTime:    spread.OpenTime,
			NetPremium:  spreadNetPremium(spread),
			RealizedPnL: spread.TotalRealizedPnL(),
			Legs:        make([]SpreadLegReport, 0, len(spread.Legs)),
		}
		closeTime := latestSpreadCloseTime(spread)
		if closeTime != nil {
			report.CloseTime = closeTime
		}
		if spread.IsFullyClosed() {
			report.Status = "closed"
		}
		referenceEnd := endTime
		if closeTime != nil {
			referenceEnd = *closeTime
		}
		report.DaysHeld = referenceEnd.Sub(spread.OpenTime).Hours() / 24

		for _, leg := range spread.Legs {
			legReport := SpreadLegReport{
				Symbol:      leg.Contract.Symbol,
				Side:        leg.Side.String(),
				Type:        leg.Contract.Type,
				StrikePrice: leg.Contract.StrikePrice,
				Expiration:  leg.Contract.Expiration,
				Delta:       leg.Contract.Delta,
				Qty:         leg.Qty,
				EntryPrice:  leg.EntryPrice,
				EntryTime:   leg.EntryTime,
				Closed:      leg.Closed,
				CloseReason: leg.CloseReason,
				RealizedPnL: leg.RealizedPnL(),
			}
			if leg.Closed {
				closeAt := leg.CloseTime
				legReport.ClosePrice = leg.ClosePrice
				legReport.CloseTime = &closeAt
			}
			report.Legs = append(report.Legs, legReport)
		}
		reports = append(reports, report)
	}
	return reports
}

func latestSpreadCloseTime(spread *SpreadPosition) *time.Time {
	if spread == nil {
		return nil
	}
	var latest time.Time
	closed := false
	for _, leg := range spread.Legs {
		if leg.Closed && (!closed || leg.CloseTime.After(latest)) {
			latest = leg.CloseTime
			closed = true
		}
	}
	if !closed {
		return nil
	}
	return &latest
}

func spreadNetPremium(spread *SpreadPosition) float64 {
	if spread == nil {
		return 0
	}
	total := 0.0
	for _, leg := range spread.Legs {
		amount := leg.Qty * leg.EntryPrice
		if leg.Side == Sell {
			total += amount
		} else {
			total -= amount
		}
	}
	return total
}

// computeSpreadSummary aggregates metrics from the spread tracker.
func computeSpreadSummary(tracker *SpreadTracker) *SpreadSummary {
	if tracker == nil || len(tracker.All()) == 0 {
		return nil
	}

	s := &SpreadSummary{
		TotalSpreads: len(tracker.All()),
	}

	for _, sp := range tracker.All() {
		if sp.IsFullyClosed() {
			s.ClosedSpreads++
			pnl := sp.TotalRealizedPnL()
			s.TotalPnL += pnl
			if pnl > 0 {
				s.WinningSpreads++
			} else if pnl < 0 {
				s.LosingSpreads++
			}
		} else {
			s.OpenSpreads++
		}
	}

	if s.ClosedSpreads > 0 {
		s.WinRate = float64(s.WinningSpreads) / float64(s.ClosedSpreads)
	}

	return s
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
	n := len(equity)
	if n <= 1 {
		return 0
	}
	returns := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		prev := equity[i-1]
		if prev == 0 {
			continue
		}
		returns = append(returns, (equity[i]-prev)/prev)
	}
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

	pnls := ComputeTradePnL(r.Trades)
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
	if grossLosses > 0 {
		r.ProfitFactor = grossWins / grossLosses
	} else if grossWins > 0 {
		r.ProfitFactor = math.Inf(1)
	}
	if r.WinningTrades > 0 {
		r.AvgWin = grossWins / float64(r.WinningTrades)
	}
	if r.LosingTrades > 0 {
		r.AvgLoss = grossLosses / float64(r.LosingTrades)
	}
}
