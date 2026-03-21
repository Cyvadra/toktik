package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// HTMLMeta carries extra presentation metadata for a generated backtest report.
type HTMLMeta struct {
	Asset       string
	Interval    string
	GeneratedAt time.Time
}

type htmlReportView struct {
	Title                string
	StrategyName         string
	Asset                string
	Interval             string
	Period               string
	GeneratedAt          string
	InitialCapital       string
	FinalEquity          string
	NetPnL               string
	TotalReturn          string
	AnnualizedReturn     string
	SharpeRatio          string
	MaxDrawdown          string
	TotalFees            string
	BarsCount            int
	TradesCount          int
	SpreadsCount         int
	TradeMarkerCount     int
	SpreadEventCount     int
	EquityMin            string
	EquityMax            string
	DrawdownMax          string
	HasUnderlyingChart   bool
	UnderlyingPriceMin   string
	UnderlyingPriceMax   string
	UnderlyingChartNote  string
	UnderlyingCandleData template.JS
	UnderlyingMarkerData template.JS
	EquitySeriesData     template.JS
	DrawdownSeriesData   template.JS
	PnLUSDSeriesData     template.JS
	ActiveTimeData       template.JS
	HasPnLUSD            bool
	PnLUSDMin            string
	PnLUSDMax            string
	EquityAnalysis       equityAnalysisView
	TradeOverview        tradeOverviewView
	SpreadSummary        *spreadSummaryView
	Trades               []tradeRowView
	Spreads              []spreadRowView
	NoTradeRows          bool
	NoSpreadRows         bool
	Notes                []string
}

type combinedHTMLReportView struct {
	Title       string
	Asset       string
	Interval    string
	Period      string
	GeneratedAt string
	Strategies  []combinedHTMLStrategyView
}

type combinedHTMLStrategyView struct {
	AnchorID string
	Report   htmlReportView
}

type tradeOverviewView struct {
	RawFills             string
	RoundTrips           string
	LongFills            string
	ShortFills           string
	TotalNotional        string
	GrossProfit          string
	GrossLoss            string
	NetPnL               string
	AvgPnLPerRoundTrip   string
	AvgCommissionPerFill string
}

type equityAnalysisView struct {
	PeakEquity              string
	PeakTime                string
	LowestEquity            string
	LowestTime              string
	BestBarReturn           string
	WorstBarReturn          string
	BarReturnVolatility     string
	PositiveBars            string
	NegativeBars            string
	FlatBars                string
	MaxDrawdownDurationBars string
	MaxDrawdownDuration     string
}

type spreadSummaryView struct {
	TotalSpreads   string
	ClosedSpreads  string
	OpenSpreads    string
	WinningSpreads string
	LosingSpreads  string
	WinRate        string
	TotalPnL       string
}

type tradeRowView struct {
	Timestamp  string
	Security   string
	Side       string
	Reason     string
	Qty        string
	FillPrice  string
	Commission string
	Slippage   string
	NetAmount  string
	SideClass  string
}

type spreadRowView struct {
	ID          int
	Tag         string
	Status      string
	OpenTime    string
	CloseTime   string
	DaysHeld    string
	NetPremium  string
	RealizedPnL string
	StatusClass string
	Legs        []spreadLegRowView
}

type spreadLegRowView struct {
	Symbol      string
	Side        string
	Type        string
	StrikePrice string
	Expiration  string
	OpenSelect  string
	Qty         string
	EntryPrice  string
	EntryAmount string
	EntryTime   string
	ClosePrice  string
	CloseTime   string
	CloseReason string
	RealizedPnL string
	SideClass   string
}

type chartCandlePoint struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

type chartLinePoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

type chartMarker struct {
	Time     int64  `json:"time"`
	Position string `json:"position"`
	Color    string `json:"color"`
	Shape    string `json:"shape"`
	Text     string `json:"text"`
}

type markerKey struct {
	Time     int64
	Position string
	Color    string
	Shape    string
}

var (
	_ = combinedHTMLReportView{}
	_ = combinedHTMLStrategyView{}
	_ = buildCombinedHTMLView
	_ = slugToken
	_ = currency4
	_ = signedCurrency
	_ = nullableCurrency4
	_ = combinedHTMLTemplate
)

// WriteBacktestHTML renders a self-contained static HTML report for a backtest result.
func WriteBacktestHTML(path string, result *backtest.Result, meta HTMLMeta) error {
	view := buildHTMLView(result, meta)
	tmpl, err := template.New("backtest-report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse report template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return fmt.Errorf("render report template: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write report html: %w", err)
	}
	return nil
}

// WriteCombinedBacktestHTML renders multiple backtest results into one self-contained HTML report.
func WriteCombinedBacktestHTML(path string, results []*backtest.Result, meta HTMLMeta) error {
	if len(results) == 0 {
		return nil
	}
	merged := mergeBacktestResults(results)
	if merged == nil {
		return nil
	}
	return WriteBacktestHTML(path, merged, meta)
}

func mergeBacktestResults(results []*backtest.Result) *backtest.Result {
	valid := make([]*backtest.Result, 0, len(results))
	for _, result := range results {
		if result != nil {
			valid = append(valid, result)
		}
	}
	if len(valid) == 0 {
		return nil
	}

	merged := &backtest.Result{
		StrategyName: "Combined",
		Series:       make(map[string][]float64),
	}

	strategyNames := make([]string, 0, len(valid))
	for idx, result := range valid {
		name := strings.TrimSpace(result.StrategyName)
		if name == "" {
			name = fmt.Sprintf("strategy-%d", idx+1)
		}
		strategyNames = append(strategyNames, name)

		if merged.StartTime.IsZero() || (!result.StartTime.IsZero() && result.StartTime.Before(merged.StartTime)) {
			merged.StartTime = result.StartTime
		}
		if merged.EndTime.IsZero() || result.EndTime.After(merged.EndTime) {
			merged.EndTime = result.EndTime
		}
		if merged.AccountUnit == "" {
			merged.AccountUnit = result.AccountUnit
		} else if result.AccountUnit != "" && merged.AccountUnit != result.AccountUnit {
			merged.AccountUnit = ""
		}

		merged.InitialCapital += result.InitialCapital
		merged.FinalEquity += result.FinalEquity
		merged.TotalFees += result.TotalFees
		if result.BarsCount > merged.BarsCount {
			merged.BarsCount = result.BarsCount
		}

		merged.Trades = append(merged.Trades, result.Trades...)

		for _, spread := range result.SpreadPositions {
			spreadCopy := spread
			merged.SpreadPositions = append(merged.SpreadPositions, spreadCopy)
		}

		if len(merged.Series) == 0 {
			for key, series := range result.Series {
				copied := make([]float64, len(series))
				copy(copied, series)
				merged.Series[key] = copied
			}
		}
	}

	merged.StrategyName = "Combined: " + strings.Join(strategyNames, " + ")

	sort.Slice(merged.Trades, func(i, j int) bool {
		if merged.Trades[i].Timestamp.Equal(merged.Trades[j].Timestamp) {
			if merged.Trades[i].Security.Symbol != merged.Trades[j].Security.Symbol {
				return merged.Trades[i].Security.Symbol < merged.Trades[j].Security.Symbol
			}
			return merged.Trades[i].ID < merged.Trades[j].ID
		}
		return merged.Trades[i].Timestamp.Before(merged.Trades[j].Timestamp)
	})

	sort.Slice(merged.SpreadPositions, func(i, j int) bool {
		if merged.SpreadPositions[i].OpenTime.Equal(merged.SpreadPositions[j].OpenTime) {
			return merged.SpreadPositions[i].ID < merged.SpreadPositions[j].ID
		}
		return merged.SpreadPositions[i].OpenTime.Before(merged.SpreadPositions[j].OpenTime)
	})
	for i := range merged.SpreadPositions {
		merged.SpreadPositions[i].ID = i + 1
	}

	referenceTimes := selectReferenceTimeline(valid)
	merged.Timestamps, merged.EquityCurve = mergeEquityCurve(valid, referenceTimes)
	if len(merged.Timestamps) > 0 {
		merged.StartTime = merged.Timestamps[0]
		merged.EndTime = merged.Timestamps[len(merged.Timestamps)-1]
		merged.FinalEquity = merged.EquityCurve[len(merged.EquityCurve)-1]
		merged.BarsCount = len(merged.Timestamps)
	}

	if merged.InitialCapital > 0 {
		merged.TotalReturn = (merged.FinalEquity - merged.InitialCapital) / merged.InitialCapital
		years := merged.EndTime.Sub(merged.StartTime).Hours() / (365.25 * 24)
		if years > 0 && merged.FinalEquity > 0 {
			merged.AnnualizedReturn = math.Pow(merged.FinalEquity/merged.InitialCapital, 1.0/years) - 1
		}
	}

	merged.MaxDrawdown, merged.MaxDrawdownStart, merged.MaxDrawdownEnd = computeMaxDrawdown(merged.EquityCurve, merged.InitialCapital)
	merged.SharpeRatio = computeSharpe(merged.EquityCurve)

	merged.TradeOverview = computeTradeOverviewFromTrades(merged.Trades)
	merged.EquityAnalysis = computeEquityAnalysisFromSeries(merged.EquityCurve, merged.Timestamps)
	applyTradeSummary(merged)
	merged.SpreadSummary = computeSpreadSummaryFromReports(merged.SpreadPositions)

	return merged
}

func selectReferenceTimeline(results []*backtest.Result) []time.Time {
	for _, result := range results {
		if result == nil || len(result.Timestamps) == 0 {
			continue
		}
		times := make([]time.Time, len(result.Timestamps))
		copy(times, result.Timestamps)
		sort.Slice(times, func(i, j int) bool {
			return times[i].Before(times[j])
		})
		return times
	}
	return nil
}

func mergeEquityCurve(results []*backtest.Result, referenceTimes []time.Time) ([]time.Time, []float64) {
	if len(referenceTimes) == 0 {
		return nil, nil
	}

	type equityPoint struct {
		time  time.Time
		value float64
	}

	pointsByResult := make([][]equityPoint, 0, len(results))
	for _, result := range results {
		n := minInt(len(result.Timestamps), len(result.EquityCurve))
		points := make([]equityPoint, 0, n)
		for i := 0; i < n; i++ {
			if !chartValueValid(result.EquityCurve[i]) {
				continue
			}
			points = append(points, equityPoint{time: result.Timestamps[i], value: result.EquityCurve[i]})
		}
		sort.Slice(points, func(i, j int) bool {
			return points[i].time.Before(points[j].time)
		})
		pointsByResult = append(pointsByResult, points)
	}

	indices := make([]int, len(pointsByResult))
	last := make([]float64, len(pointsByResult))
	for i, result := range results {
		last[i] = result.InitialCapital
	}

	times := make([]time.Time, 0, len(referenceTimes))
	equity := make([]float64, 0, len(referenceTimes))
	for _, ts := range referenceTimes {
		total := 0.0
		for i := range pointsByResult {
			points := pointsByResult[i]
			for indices[i] < len(points) && !points[indices[i]].time.After(ts) {
				last[i] = points[indices[i]].value
				indices[i]++
			}
			total += last[i]
		}
		times = append(times, ts)
		equity = append(equity, total)
	}

	return times, equity
}

func computeMaxDrawdown(equity []float64, initialCapital float64) (float64, int, int) {
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

func computeSharpe(equity []float64) float64 {
	if len(equity) <= 1 {
		return 0
	}
	returns := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
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
	return (mean / stddev) * math.Sqrt(252)
}

func computeTradeOverviewFromTrades(trades []backtest.Trade) *backtest.TradeOverview {
	overview := &backtest.TradeOverview{RawFills: len(trades)}
	if len(trades) == 0 {
		return overview
	}

	for _, trade := range trades {
		overview.TotalNotional += math.Abs(trade.Qty * trade.FillPrice)
		if trade.Side == backtest.Buy {
			overview.LongFills++
		} else {
			overview.ShortFills++
		}
		overview.AvgCommissionPerFill += trade.Commission
	}

	pnls := computeTradePnLFromTrades(trades)
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

func applyTradeSummary(result *backtest.Result) {
	if result == nil {
		return
	}
	result.TotalTrades = len(result.Trades)
	result.WinningTrades = 0
	result.LosingTrades = 0
	result.ProfitFactor = 0
	result.AvgWin = 0
	result.AvgLoss = 0
	result.WinRate = 0

	pnls := computeTradePnLFromTrades(result.Trades)
	grossWins := 0.0
	grossLosses := 0.0
	for _, pnl := range pnls {
		if pnl > 0 {
			result.WinningTrades++
			grossWins += pnl
		} else if pnl < 0 {
			result.LosingTrades++
			grossLosses += -pnl
		}
	}

	if len(pnls) > 0 {
		result.WinRate = float64(result.WinningTrades) / float64(len(pnls))
	}
	if grossLosses > 0 {
		result.ProfitFactor = grossWins / grossLosses
	} else if grossWins > 0 {
		result.ProfitFactor = math.Inf(1)
	}
	if result.WinningTrades > 0 {
		result.AvgWin = grossWins / float64(result.WinningTrades)
	}
	if result.LosingTrades > 0 {
		result.AvgLoss = grossLosses / float64(result.LosingTrades)
	}
}

func computeTradePnLFromTrades(trades []backtest.Trade) []float64 {
	type openEntry struct {
		side  backtest.Side
		qty   float64
		price float64
	}

	pending := make(map[backtest.SecurityRef]*openEntry)
	pnls := make([]float64, 0)

	for _, trade := range trades {
		entry, hasPending := pending[trade.Security]
		if !hasPending {
			pending[trade.Security] = &openEntry{side: trade.Side, qty: trade.Qty, price: trade.FillPrice}
			continue
		}

		if entry.side != trade.Side {
			closeQty := trade.Qty
			if closeQty > entry.qty {
				closeQty = entry.qty
			}

			pnl := 0.0
			if entry.side == backtest.Buy {
				pnl = closeQty * (trade.FillPrice - entry.price)
			} else {
				pnl = closeQty * (entry.price - trade.FillPrice)
			}
			pnl -= trade.Commission
			pnls = append(pnls, pnl)

			remaining := entry.qty - closeQty
			if remaining > 0 {
				entry.qty = remaining
			} else {
				excess := trade.Qty - closeQty
				if excess > 0 {
					pending[trade.Security] = &openEntry{side: trade.Side, qty: excess, price: trade.FillPrice}
				} else {
					delete(pending, trade.Security)
				}
			}
			continue
		}

		totalQty := entry.qty + trade.Qty
		entry.price = (entry.price*entry.qty + trade.FillPrice*trade.Qty) / totalQty
		entry.qty = totalQty
	}

	return pnls
}

func computeEquityAnalysisFromSeries(equityCurve []float64, timestamps []time.Time) *backtest.EquityAnalysis {
	if len(equityCurve) == 0 {
		return nil
	}

	analysis := &backtest.EquityAnalysis{
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

func meanStd(data []float64) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, value := range data {
		sum += value
	}
	mean := sum / float64(len(data))

	sumSquares := 0.0
	for _, value := range data {
		delta := value - mean
		sumSquares += delta * delta
	}
	return mean, math.Sqrt(sumSquares / float64(len(data)))
}

func computeSpreadSummaryFromReports(spreads []backtest.SpreadPositionReport) *backtest.SpreadSummary {
	if len(spreads) == 0 {
		return nil
	}

	summary := &backtest.SpreadSummary{TotalSpreads: len(spreads)}
	for _, spread := range spreads {
		if spread.CloseTime != nil {
			summary.ClosedSpreads++
			summary.TotalPnL += spread.RealizedPnL
			if spread.RealizedPnL > 0 {
				summary.WinningSpreads++
			} else if spread.RealizedPnL < 0 {
				summary.LosingSpreads++
			}
		} else {
			summary.OpenSpreads++
		}
	}
	if summary.ClosedSpreads > 0 {
		summary.WinRate = float64(summary.WinningSpreads) / float64(summary.ClosedSpreads)
	}
	return summary
}

func buildHTMLView(result *backtest.Result, meta HTMLMeta) htmlReportView {
	if meta.GeneratedAt.IsZero() {
		meta.GeneratedAt = time.Now()
	}

	drawdown := drawdownSeries(result.EquityCurve)
	view := htmlReportView{
		Title:                fmt.Sprintf("%s Backtest Report", result.StrategyName),
		StrategyName:         result.StrategyName,
		Asset:                meta.Asset,
		Interval:             meta.Interval,
		Period:               fmt.Sprintf("%s to %s", formatDate(result.StartTime), formatDate(result.EndTime)),
		GeneratedAt:          meta.GeneratedAt.Format("2006-01-02 15:04:05"),
		InitialCapital:       amount(result.InitialCapital, result.AccountUnit),
		FinalEquity:          amount(result.FinalEquity, result.AccountUnit),
		NetPnL:               signedAmount(result.FinalEquity-result.InitialCapital, result.AccountUnit),
		TotalReturn:          pct(result.TotalReturn),
		AnnualizedReturn:     pct(result.AnnualizedReturn),
		SharpeRatio:          decimal(result.SharpeRatio),
		MaxDrawdown:          pct(result.MaxDrawdown),
		TotalFees:            amount(result.TotalFees, result.AccountUnit),
		BarsCount:            result.BarsCount,
		TradesCount:          len(result.Trades),
		SpreadsCount:         len(result.SpreadPositions),
		NoTradeRows:          len(result.Trades) == 0,
		NoSpreadRows:         len(result.SpreadPositions) == 0,
		UnderlyingCandleData: template.JS("[]"),
		UnderlyingMarkerData: template.JS("[]"),
		PnLUSDSeriesData:     template.JS("[]"),
		ActiveTimeData:       template.JS("[]"),
		EquitySeriesData:     marshalJS(buildLineSeries(result.Timestamps, result.EquityCurve)),
		DrawdownSeriesData:   marshalJS(buildLineSeries(result.Timestamps, drawdown)),
	}

	minEq, maxEq := minMax(result.EquityCurve)
	view.EquityMin = amount(minEq, result.AccountUnit)
	view.EquityMax = amount(maxEq, result.AccountUnit)
	view.DrawdownMax = pct(maxValue(drawdown))

	if closeSeries, ok := result.Series["close"]; ok && len(closeSeries) > 0 {
		n := minInt(len(result.Timestamps), minInt(len(result.EquityCurve), len(closeSeries)))
		pnlUSD := make([]float64, n)
		for i := 0; i < n; i++ {
			pnlUSD[i] = (result.EquityCurve[i] - result.InitialCapital) * closeSeries[i]
		}
		view.HasPnLUSD = true
		view.PnLUSDSeriesData = marshalJS(buildLineSeries(result.Timestamps[:n], pnlUSD))
		minPnL, maxPnL := minMax(pnlUSD)
		view.PnLUSDMin = currency(minPnL)
		view.PnLUSDMax = currency(maxPnL)
	}

	view.TradeOverview = buildTradeOverviewView(result.TradeOverview, result.AccountUnit)
	view.EquityAnalysis = buildEquityAnalysisView(result.EquityAnalysis, result.AccountUnit)
	view.Trades = buildTradeRows(result.Trades, result.AccountUnit)
	view.Spreads = buildSpreadRows(result.SpreadPositions, result.AccountUnit)

	if result.SpreadSummary != nil {
		s := result.SpreadSummary
		view.SpreadSummary = &spreadSummaryView{
			TotalSpreads:   integer(s.TotalSpreads),
			ClosedSpreads:  integer(s.ClosedSpreads),
			OpenSpreads:    integer(s.OpenSpreads),
			WinningSpreads: integer(s.WinningSpreads),
			LosingSpreads:  integer(s.LosingSpreads),
			WinRate:        pct(s.WinRate),
			TotalPnL:       signedAmount(s.TotalPnL, result.AccountUnit),
		}
	}

	candles, candleFallback := buildUnderlyingCandles(result)
	if len(candles) > 0 {
		view.HasUnderlyingChart = true
		view.UnderlyingCandleData = marshalJS(candles)
		minUnderlying, maxUnderlying := candleRange(candles)
		view.UnderlyingPriceMin = currency(minUnderlying)
		view.UnderlyingPriceMax = currency(maxUnderlying)
		if candleFallback {
			view.UnderlyingChartNote = "OHLC was not fully available in the result series, so candle bodies were reconstructed from close data for visualization."
		}
	}

	markers, tradeMarkerCount, spreadEventCount := buildUnderlyingMarkers(result)
	view.UnderlyingMarkerData = marshalJS(markers)
	view.ActiveTimeData = marshalJS(buildActiveTimes(result))
	view.TradeMarkerCount = tradeMarkerCount
	view.SpreadEventCount = spreadEventCount
	view.Notes = buildNotes(result, candleFallback)

	return view
}

func buildCombinedHTMLView(results []*backtest.Result, meta HTMLMeta) combinedHTMLReportView {
	if meta.GeneratedAt.IsZero() {
		meta.GeneratedAt = time.Now()
	}
	periodStart := results[0].StartTime
	periodEnd := results[0].EndTime
	strategies := make([]combinedHTMLStrategyView, 0, len(results))
	for index, result := range results {
		if result.StartTime.Before(periodStart) {
			periodStart = result.StartTime
		}
		if result.EndTime.After(periodEnd) {
			periodEnd = result.EndTime
		}
		strategies = append(strategies, combinedHTMLStrategyView{
			AnchorID: fmt.Sprintf("strategy-%d-%s", index+1, slugToken(result.StrategyName)),
			Report:   buildHTMLView(result, meta),
		})
	}
	return combinedHTMLReportView{
		Title:       fmt.Sprintf("%s Backtest Report", meta.Asset),
		Asset:       meta.Asset,
		Interval:    meta.Interval,
		Period:      fmt.Sprintf("%s to %s", formatDate(periodStart), formatDate(periodEnd)),
		GeneratedAt: meta.GeneratedAt.Format("2006-01-02 15:04:05"),
		Strategies:  strategies,
	}
}

func buildTradeOverviewView(overview *backtest.TradeOverview, unit string) tradeOverviewView {
	if overview == nil {
		return tradeOverviewView{}
	}
	return tradeOverviewView{
		RawFills:             integer(overview.RawFills),
		RoundTrips:           integer(overview.RoundTrips),
		LongFills:            integer(overview.LongFills),
		ShortFills:           integer(overview.ShortFills),
		TotalNotional:        amount(overview.TotalNotional, unit),
		GrossProfit:          amount(overview.GrossProfit, unit),
		GrossLoss:            amount(overview.GrossLoss, unit),
		NetPnL:               signedAmount(overview.NetPnL, unit),
		AvgPnLPerRoundTrip:   signedAmount(overview.AvgPnLPerRoundTrip, unit),
		AvgCommissionPerFill: amount(overview.AvgCommissionPerFill, unit),
	}
}

func buildEquityAnalysisView(analysis *backtest.EquityAnalysis, unit string) equityAnalysisView {
	if analysis == nil {
		return equityAnalysisView{}
	}
	return equityAnalysisView{
		PeakEquity:              amount(analysis.PeakEquity, unit),
		PeakTime:                formatDateTime(analysis.PeakTime),
		LowestEquity:            amount(analysis.LowestEquity, unit),
		LowestTime:              formatDateTime(analysis.LowestTime),
		BestBarReturn:           pct(analysis.BestBarReturn),
		WorstBarReturn:          pct(analysis.WorstBarReturn),
		BarReturnVolatility:     pct(analysis.BarReturnVolatility),
		PositiveBars:            integer(analysis.PositiveBars),
		NegativeBars:            integer(analysis.NegativeBars),
		FlatBars:                integer(analysis.FlatBars),
		MaxDrawdownDurationBars: integer(analysis.MaxDrawdownDurationBars),
		MaxDrawdownDuration:     fmt.Sprintf("%.1f h", analysis.MaxDrawdownDuration),
	}
}

func buildTradeRows(trades []backtest.Trade, unit string) []tradeRowView {
	rows := make([]tradeRowView, 0, len(trades))
	for _, trade := range trades {
		side := trade.Side.String()
		rows = append(rows, tradeRowView{
			Timestamp:  formatDateTime(trade.Timestamp),
			Security:   fmt.Sprintf("%s / %s / %s", trade.Security.Market, trade.Security.Symbol, trade.Security.Interval),
			Side:       strings.ToUpper(side),
			Reason:     fallbackText(strings.TrimSpace(trade.Note), "-"),
			Qty:        decimal(trade.Qty),
			FillPrice:  amount(trade.FillPrice, unit),
			Commission: amount(trade.Commission, unit),
			Slippage:   amount(trade.Slippage, unit),
			NetAmount:  signedAmount(trade.NetAmount(), unit),
			SideClass:  sideClass(side),
		})
	}
	return rows
}

func buildSpreadRows(spreads []backtest.SpreadPositionReport, unit string) []spreadRowView {
	rows := make([]spreadRowView, 0, len(spreads))
	for _, spread := range spreads {
		row := spreadRowView{
			ID:          spread.ID,
			Tag:         spread.Tag,
			Status:      strings.ToUpper(spread.Status),
			OpenTime:    formatDateTime(spread.OpenTime),
			CloseTime:   "-",
			DaysHeld:    fmt.Sprintf("%.2f d", spread.DaysHeld),
			NetPremium:  signedAmount(spread.NetPremium, unit),
			RealizedPnL: signedAmount(spread.RealizedPnL, unit),
			StatusClass: statusClass(spread.Status),
			Legs:        make([]spreadLegRowView, 0, len(spread.Legs)),
		}
		if spread.CloseTime != nil {
			row.CloseTime = formatDateTime(*spread.CloseTime)
		}
		for _, leg := range spread.Legs {
			expiryOpenDays := leg.Expiration.Sub(leg.EntryTime).Hours() / 24
			legView := spreadLegRowView{
				Symbol:      leg.Symbol,
				Side:        strings.ToUpper(leg.Side),
				Type:        strings.ToUpper(string(leg.Type)),
				StrikePrice: currency(leg.StrikePrice),
				Expiration:  formatDate(leg.Expiration),
				OpenSelect:  expiryOpenDelta(expiryOpenDays, leg.Delta),
				Qty:         decimal(leg.Qty),
				EntryPrice:  amount4(leg.EntryPrice, unit),
				EntryAmount: amount4(leg.Qty*leg.EntryPrice, unit),
				EntryTime:   formatDateTime(leg.EntryTime),
				ClosePrice:  nullableAmount4(leg.ClosePrice, leg.Closed, unit),
				CloseTime:   "-",
				CloseReason: fallbackText(strings.TrimSpace(leg.CloseReason), "-"),
				RealizedPnL: signedAmount(leg.RealizedPnL, unit),
				SideClass:   sideClass(leg.Side),
			}
			if leg.CloseTime != nil {
				legView.CloseTime = formatDateTime(*leg.CloseTime)
			}
			row.Legs = append(row.Legs, legView)
		}
		rows = append(rows, row)
	}
	return rows
}

func buildNotes(result *backtest.Result, candleFallback bool) []string {
	notes := make([]string, 0, 4)
	if len(result.Trades) == 0 && len(result.SpreadPositions) > 0 {
		notes = append(notes, "This strategy executed through spread tracker legs, so the raw broker trade table is empty while spread lifecycle events are still marked on the price chart.")
	}
	if result.EquityAnalysis != nil && result.EquityAnalysis.MaxDrawdownDurationBars > 0 {
		notes = append(notes, fmt.Sprintf("Longest drawdown stretch lasted %d bars (%.1f hours).", result.EquityAnalysis.MaxDrawdownDurationBars, result.EquityAnalysis.MaxDrawdownDuration))
	}
	if result.TradeOverview != nil && result.TradeOverview.RoundTrips == 0 && len(result.SpreadPositions) == 0 {
		notes = append(notes, "No closed round trips were recorded in this run.")
	}
	if candleFallback {
		notes = append(notes, "Underlying candles were reconstructed from close data because complete OHLC series were unavailable in the exported result.")
	}
	return notes
}

func buildUnderlyingCandles(result *backtest.Result) ([]chartCandlePoint, bool) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, false
	}
	closeSeries := result.Series["close"]
	if len(closeSeries) == 0 {
		return nil, false
	}
	openSeries := result.Series["open"]
	highSeries := result.Series["high"]
	lowSeries := result.Series["low"]
	n := minInt(len(result.Timestamps), len(closeSeries))
	if n == 0 {
		return nil, false
	}

	candles := make([]chartCandlePoint, 0, n)
	usedFallback := false
	prevClose := closeSeries[0]
	for i := 0; i < n; i++ {
		closeValue := closeSeries[i]
		if !chartValueValid(closeValue) {
			continue
		}

		openValue := closeValue
		if i < len(openSeries) && chartValueValid(openSeries[i]) {
			openValue = openSeries[i]
		} else {
			usedFallback = true
			if i > 0 && chartValueValid(prevClose) {
				openValue = prevClose
			}
		}

		highValue := math.Max(openValue, closeValue)
		if i < len(highSeries) && chartValueValid(highSeries[i]) {
			highValue = math.Max(highValue, highSeries[i])
		} else {
			usedFallback = true
		}

		lowValue := math.Min(openValue, closeValue)
		if i < len(lowSeries) && chartValueValid(lowSeries[i]) {
			lowValue = math.Min(lowValue, lowSeries[i])
		} else {
			usedFallback = true
		}

		candles = append(candles, chartCandlePoint{
			Time:  result.Timestamps[i].Unix(),
			Open:  openValue,
			High:  highValue,
			Low:   lowValue,
			Close: closeValue,
		})
		prevClose = closeValue
	}
	return candles, usedFallback
}

func buildLineSeries(times []time.Time, values []float64) []chartLinePoint {
	n := minInt(len(times), len(values))
	if n == 0 {
		return []chartLinePoint{}
	}
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		if !chartValueValid(values[i]) {
			continue
		}
		points = append(points, chartLinePoint{
			Time:  times[i].Unix(),
			Value: values[i],
		})
	}
	return points
}

func buildUnderlyingMarkers(result *backtest.Result) ([]chartMarker, int, int) {
	if result == nil {
		return []chartMarker{}, 0, 0
	}
	aggregated := make(map[markerKey]*chartMarker)
	tradeMarkerCount := 0
	spreadEventCount := 0

	for _, trade := range result.Trades {
		tradeMarkerCount++
		shape := "arrowUp"
		position := "belowBar"
		color := "#2dd4bf"
		label := fmt.Sprintf("BUY %s", decimal(trade.Qty))
		if trade.Side == backtest.Sell {
			shape = "arrowDown"
			position = "aboveBar"
			color = "#f59e0b"
			label = fmt.Sprintf("SELL %s", decimal(trade.Qty))
		}
		appendMarker(aggregated, chartMarker{
			Time:     trade.Timestamp.Unix(),
			Position: position,
			Color:    color,
			Shape:    shape,
			Text:     label,
		})
	}

	for _, spread := range result.SpreadPositions {
		spreadEventCount++
		appendMarker(aggregated, chartMarker{
			Time:     spread.OpenTime.Unix(),
			Position: "belowBar",
			Color:    "#60a5fa",
			Shape:    "circle",
			Text:     fmt.Sprintf("OPEN #%d", spread.ID),
		})
		if spread.CloseTime != nil {
			spreadEventCount++
			appendMarker(aggregated, chartMarker{
				Time:     spread.CloseTime.Unix(),
				Position: "aboveBar",
				Color:    "#fb7185",
				Shape:    "square",
				Text:     fmt.Sprintf("CLOSE #%d", spread.ID),
			})
		}
	}

	markers := make([]chartMarker, 0, len(aggregated))
	for _, marker := range aggregated {
		markers = append(markers, *marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].Time != markers[j].Time {
			return markers[i].Time < markers[j].Time
		}
		if markers[i].Position != markers[j].Position {
			return markers[i].Position < markers[j].Position
		}
		return markers[i].Text < markers[j].Text
	})
	return markers, tradeMarkerCount, spreadEventCount
}

func buildActiveTimes(result *backtest.Result) []int64 {
	if result == nil || len(result.Timestamps) == 0 {
		return []int64{}
	}
	n := len(result.Timestamps)
	active := make([]bool, n)

	for _, spread := range result.SpreadPositions {
		for i, ts := range result.Timestamps {
			if ts.Before(spread.OpenTime) {
				continue
			}
			if spread.CloseTime != nil && ts.After(*spread.CloseTime) {
				continue
			}
			active[i] = true
		}
	}

	trades := append([]backtest.Trade(nil), result.Trades...)
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Timestamp.Equal(trades[j].Timestamp) {
			if trades[i].Security.Symbol != trades[j].Security.Symbol {
				return trades[i].Security.Symbol < trades[j].Security.Symbol
			}
			return trades[i].ID < trades[j].ID
		}
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	positionBySecurity := make(map[string]float64)
	activeSecurities := 0
	tradeIdx := 0
	for i, ts := range result.Timestamps {
		hadTrade := false
		for tradeIdx < len(trades) && !trades[tradeIdx].Timestamp.After(ts) {
			trade := trades[tradeIdx]
			key := trade.Security.Market + "|" + trade.Security.Symbol + "|" + trade.Security.Interval
			prevQty := positionBySecurity[key]
			nextQty := prevQty
			if trade.Side == backtest.Buy {
				nextQty += trade.Qty
			} else {
				nextQty -= trade.Qty
			}
			if math.Abs(prevQty) <= 1e-9 && math.Abs(nextQty) > 1e-9 {
				activeSecurities++
			} else if math.Abs(prevQty) > 1e-9 && math.Abs(nextQty) <= 1e-9 {
				activeSecurities--
			}
			if math.Abs(nextQty) <= 1e-9 {
				delete(positionBySecurity, key)
			} else {
				positionBySecurity[key] = nextQty
			}
			hadTrade = true
			tradeIdx++
		}
		if hadTrade || activeSecurities > 0 {
			active[i] = true
		}
	}

	activeTimes := make([]int64, 0, n)
	for i, ok := range active {
		if ok {
			activeTimes = append(activeTimes, result.Timestamps[i].Unix())
		}
	}
	return activeTimes
}

func appendMarker(markers map[markerKey]*chartMarker, marker chartMarker) {
	key := markerKey{
		Time:     marker.Time,
		Position: marker.Position,
		Color:    marker.Color,
		Shape:    marker.Shape,
	}
	if existing, ok := markers[key]; ok {
		if !strings.Contains(existing.Text, marker.Text) {
			existing.Text += " · " + marker.Text
		}
		return
	}
	copyMarker := marker
	markers[key] = &copyMarker
}

func candleRange(candles []chartCandlePoint) (float64, float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	minValue := candles[0].Low
	maxValue := candles[0].High
	for _, candle := range candles[1:] {
		if candle.Low < minValue {
			minValue = candle.Low
		}
		if candle.High > maxValue {
			maxValue = candle.High
		}
	}
	return minValue, maxValue
}

func marshalJS(value any) template.JS {
	encoded, err := json.Marshal(value)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(encoded)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func chartValueValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func linePath(values []float64, width, height float64) string {
	if len(values) == 0 {
		return ""
	}
	minValue, maxValue := minMax(values)
	if maxValue-minValue == 0 {
		maxValue += 1
		minValue -= 1
	}
	var builder strings.Builder
	for i, value := range values {
		x := 0.0
		if len(values) > 1 {
			x = width * float64(i) / float64(len(values)-1)
		}
		y := height - ((value-minValue)/(maxValue-minValue))*height
		if i == 0 {
			builder.WriteString(fmt.Sprintf("M %.2f %.2f", x, y))
		} else {
			builder.WriteString(fmt.Sprintf(" L %.2f %.2f", x, y))
		}
	}
	return builder.String()
}

func drawdownSeries(equity []float64) []float64 {
	if len(equity) == 0 {
		return nil
	}
	series := make([]float64, len(equity))
	peak := equity[0]
	for i, eq := range equity {
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			series[i] = (peak - eq) / peak
		}
	}
	return series
}

func minMax(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	minValue, maxValue := values[0], values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}

func maxValue(values []float64) float64 {
	maxVal := 0.0
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}

func currency(value float64) string {
	return formatAmount(value, "", 2)
}

func amount(value float64, unit string) string {
	decimals := 2
	if strings.TrimSpace(unit) != "" {
		decimals = 4
	}
	return formatAmount(value, unit, decimals)
}

func amount4(value float64, unit string) string {
	return formatAmount(value, unit, 4)
}

func formatAmount(value float64, unit string, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" || strings.EqualFold(trimmedUnit, "USD") {
		if value < 0 {
			return "-$" + fmt.Sprintf("%.*f", decimals, -value)
		}
		return "$" + fmt.Sprintf("%.*f", decimals, value)
	}
	if value < 0 {
		return "-" + fmt.Sprintf("%.*f", decimals, -value) + " " + trimmedUnit
	}
	return fmt.Sprintf("%.*f", decimals, value) + " " + trimmedUnit
}

func currency4(value float64) string {
	return formatAmount(value, "", 4)
}

func signedCurrency(value float64) string {
	return signedAmount(value, "")
}

func signedAmount(value float64, unit string) string {
	if value > 0 {
		return "+" + amount(value, unit)
	}
	return amount(value, unit)
}

func decimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
}

func expiryOpenDelta(expiryOpenDays, delta float64) string {
	if math.IsNaN(expiryOpenDays) || math.IsInf(expiryOpenDays, 0) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f d | Δ %.2f", expiryOpenDays, delta)
}

func integer(value int) string {
	return fmt.Sprintf("%d", value)
}

func pct(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", value*100)
}

func nullableCurrency4(value float64, ok bool) string {
	return nullableAmount4(value, ok, "")
}

func nullableAmount4(value float64, ok bool, unit string) string {
	if !ok {
		return "-"
	}
	return amount4(value, unit)
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02")
}

func formatDateTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02 15:04")
}

func sideClass(side string) string {
	if strings.EqualFold(side, "buy") {
		return "text-emerald-300"
	}
	return "text-amber-300"
}

func statusClass(status string) string {
	if strings.EqualFold(status, "closed") {
		return "bg-emerald-500/15 text-emerald-200 ring-emerald-400/40"
	}
	return "bg-amber-500/15 text-amber-200 ring-amber-400/40"
}

func slugToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-", ":", "-", "#", "")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-")
	if value == "" {
		return "strategy"
	}
	return value
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{ .Title }}</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script src="https://unpkg.com/lightweight-charts@4.2.3/dist/lightweight-charts.standalone.production.js"></script>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root { color-scheme: dark; }
    body { background: #0b1117; font-family: 'Inter', system-ui, sans-serif; }
    .mono { font-family: 'IBM Plex Mono', monospace; }
    .chart-box { background: #0f1923; border: 1px solid rgba(255,255,255,0.08); border-radius: 8px; }
    table.perf td, table.perf th { padding: 6px 12px; white-space: nowrap; }
    table.perf th { color: #64748b; font-weight: 500; text-align: left; font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; }
    table.perf td { font-family: 'IBM Plex Mono', monospace; color: #e2e8f0; font-size: 13px; }
    table.perf tr { border-bottom: 1px solid rgba(255,255,255,0.06); }
    .section { background: #111922; border: 1px solid rgba(255,255,255,0.08); border-radius: 12px; padding: 20px; margin-bottom: 16px; }
    .section h2 { color: #f1f5f9; font-size: 16px; font-weight: 600; margin-bottom: 12px; }
    .text-up { color: #34d399; }
    .text-down { color: #fbbf24; }
  </style>
</head>
<body class="text-slate-300 min-h-screen p-4 lg:p-6">
  <div class="max-w-[1600px] mx-auto">
    <header class="mb-6">
      <div class="text-xs mono uppercase tracking-widest text-teal-400 mb-1">Backtest Report</div>
      <h1 class="text-2xl font-bold text-white">{{ .StrategyName }}</h1>
      <p class="text-sm text-slate-400 mt-1">{{ .Asset }} · {{ .Interval }} · {{ .Period }} · Generated {{ .GeneratedAt }}</p>
    </header>

	<div class="section">
	  <div class="flex flex-wrap items-center justify-between gap-3">
		<div>
		  <h2 class="!mb-1">Chart Time Axis</h2>
		  <p class="text-xs text-slate-400">Ignore idle periods to compress empty flat segments (applies to all charts below).</p>
		</div>
		<label class="inline-flex items-center gap-2 rounded-md border border-white/10 px-3 py-2 text-sm text-slate-200 cursor-pointer select-none">
		  <input id="toggle-ignore-idle" type="checkbox" class="accent-teal-400" />
		  <span>Ignore idle periods</span>
		</label>
	  </div>
	</div>

    <div class="section">
      <h2>Strategy Performance</h2>
      <div class="overflow-x-auto">
        <table class="perf w-full">
          <thead>
            <tr>
              <th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">Capital &amp; Returns</th>
              <th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">Risk</th>
              {{ if not .NoTradeRows }}<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">Trades</th>{{ end }}
              <th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">Equity</th>
              {{ if .SpreadSummary }}<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">Spreads</th>{{ end }}
            </tr>
          </thead>
          <tbody>
            <tr>
              <td class="text-slate-400 !font-sans">Initial Capital</td><td>{{ .InitialCapital }}</td>
              <td class="text-slate-400 !font-sans">Max Drawdown</td><td>{{ .MaxDrawdown }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Raw Fills</td><td>{{ .TradeOverview.RawFills }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Peak Equity</td><td>{{ .EquityAnalysis.PeakEquity }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Total</td><td>{{ .SpreadSummary.TotalSpreads }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Final Equity</td><td>{{ .FinalEquity }}</td>
              <td class="text-slate-400 !font-sans">Sharpe Ratio</td><td>{{ .SharpeRatio }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Round Trips</td><td>{{ .TradeOverview.RoundTrips }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Lowest Equity</td><td>{{ .EquityAnalysis.LowestEquity }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Closed</td><td>{{ .SpreadSummary.ClosedSpreads }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Net PnL</td><td>{{ .NetPnL }}</td>
              <td class="text-slate-400 !font-sans">Bar Volatility</td><td>{{ .EquityAnalysis.BarReturnVolatility }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Long / Short</td><td>{{ .TradeOverview.LongFills }} / {{ .TradeOverview.ShortFills }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Peak Time</td><td>{{ .EquityAnalysis.PeakTime }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Open</td><td>{{ .SpreadSummary.OpenSpreads }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Total Return</td><td>{{ .TotalReturn }}</td>
              <td class="text-slate-400 !font-sans">Best Bar</td><td>{{ .EquityAnalysis.BestBarReturn }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Gross Profit</td><td>{{ .TradeOverview.GrossProfit }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Lowest Time</td><td>{{ .EquityAnalysis.LowestTime }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Winning</td><td>{{ .SpreadSummary.WinningSpreads }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Annualized</td><td>{{ .AnnualizedReturn }}</td>
              <td class="text-slate-400 !font-sans">Worst Bar</td><td>{{ .EquityAnalysis.WorstBarReturn }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Gross Loss</td><td>{{ .TradeOverview.GrossLoss }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Positive Bars</td><td>{{ .EquityAnalysis.PositiveBars }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Losing</td><td>{{ .SpreadSummary.LosingSpreads }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Total Fees</td><td>{{ .TotalFees }}</td>
              <td class="text-slate-400 !font-sans">DD Duration</td><td>{{ .EquityAnalysis.MaxDrawdownDuration }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Net PnL (trades)</td><td>{{ .TradeOverview.NetPnL }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Negative Bars</td><td>{{ .EquityAnalysis.NegativeBars }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Win Rate</td><td>{{ .SpreadSummary.WinRate }}</td>{{ end }}
            </tr>
            <tr>
              <td class="text-slate-400 !font-sans">Bars</td><td>{{ .BarsCount }}</td>
              <td class="text-slate-400 !font-sans">DD Duration Bars</td><td>{{ .EquityAnalysis.MaxDrawdownDurationBars }}</td>
              {{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">Avg Round Trip</td><td>{{ .TradeOverview.AvgPnLPerRoundTrip }}</td>{{ end }}
              <td class="text-slate-400 !font-sans">Flat Bars</td><td>{{ .EquityAnalysis.FlatBars }}</td>
              {{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">Spread PnL</td><td>{{ .SpreadSummary.TotalPnL }}</td>{{ end }}
            </tr>
            {{ if not .NoTradeRows }}
            <tr>
              <td></td><td></td>
              <td></td><td></td>
              <td class="text-slate-400 !font-sans">Avg Commission</td><td>{{ .TradeOverview.AvgCommissionPerFill }}</td>
              <td></td><td></td>
              {{ if .SpreadSummary }}<td></td><td></td>{{ end }}
            </tr>
            {{ end }}
          </tbody>
        </table>
      </div>
    </div>

    {{ if .HasUnderlyingChart }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
        <h2 class="!mb-0">Underlying Price</h2>
        <div class="flex gap-2 text-xs mono">
          <span class="text-up">BUY</span>
          <span class="text-down">SELL</span>
          <span class="text-blue-400">OPEN</span>
          <span class="text-rose-400">CLOSE</span>
        </div>
      </div>
      {{ if .UnderlyingChartNote }}<p class="text-xs text-amber-300 mb-3">{{ .UnderlyingChartNote }}</p>{{ end }}
      <div class="chart-box p-1">
        <div id="underlying-chart" style="width:100%;height:420px;"></div>
      </div>
    </div>
    {{ end }}

    <div class="section">
      <h2>Equity Curve</h2>
      <p class="text-xs text-slate-400 mb-3">Range {{ .EquityMin }} to {{ .EquityMax }} · Fees {{ .TotalFees }}</p>
      <div class="chart-box p-1">
        <div id="equity-chart" style="width:100%;height:300px;"></div>
      </div>
    </div>

    {{ if .HasPnLUSD }}
    <div class="section">
      <h2>PnL Curve (USD)</h2>
      <p class="text-xs text-slate-400 mb-3">Range {{ .PnLUSDMin }} to {{ .PnLUSDMax }} · (equity − initial) × BTC price</p>
      <div class="chart-box p-1">
        <div id="pnl-usd-chart" style="width:100%;height:300px;"></div>
      </div>
    </div>
    {{ end }}

    <div class="section">
      <h2>Drawdown</h2>
      <p class="text-xs text-slate-400 mb-3">Max drawdown {{ .DrawdownMax }}</p>
      <div class="chart-box p-1">
        <div id="drawdown-chart" style="width:100%;height:220px;"></div>
      </div>
    </div>

    {{ if not .NoSpreadRows }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
        <h2 class="!mb-0">Spread Activity</h2>
        <span class="mono text-xs text-slate-400">{{ .SpreadsCount }} positions</span>
      </div>
      {{ range .Spreads }}
      <div class="mb-4 border border-white/5 rounded-lg overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 bg-white/[0.02] border-b border-white/5">
          <div class="flex items-center gap-3">
            <span class="font-medium text-slate-200">#{{ .ID }} {{ .Tag }}</span>
            <span class="mono text-xs px-2 py-0.5 rounded {{ .StatusClass }}">{{ .Status }}</span>
          </div>
          <div class="flex gap-5 text-xs text-slate-400">
            <span>Open {{ .OpenTime }}</span>
            <span>Held {{ .DaysHeld }}</span>
            <span>Premium <span class="mono text-slate-300">{{ .NetPremium }}</span></span>
            <span>PnL <span class="mono text-slate-300">{{ .RealizedPnL }}</span></span>
          </div>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="text-left text-slate-500 text-xs uppercase border-b border-white/5">
                <th class="px-4 py-2 font-medium">Symbol</th>
                <th class="px-4 py-2 font-medium">Side</th>
                <th class="px-4 py-2 font-medium">Type</th>
                <th class="px-4 py-2 font-medium">Strike</th>
                <th class="px-4 py-2 font-medium">Expiry</th>
				<th class="px-4 py-2 font-medium">Expiry-Open / Delta</th>
                <th class="px-4 py-2 font-medium">Qty</th>
                <th class="px-4 py-2 font-medium">Entry Price</th>
				<th class="px-4 py-2 font-medium">Entry Amount</th>
                <th class="px-4 py-2 font-medium">Entry Time</th>
                <th class="px-4 py-2 font-medium">Close Price</th>
                <th class="px-4 py-2 font-medium">Close Time</th>
				<th class="px-4 py-2 font-medium">Close Reason</th>
                <th class="px-4 py-2 font-medium">PnL</th>
              </tr>
            </thead>
            <tbody>
              {{ range .Legs }}
              <tr class="border-b border-white/[0.03]">
                <td class="px-4 py-1.5 mono text-slate-300">{{ .Symbol }}</td>
                <td class="px-4 py-1.5 font-medium {{ .SideClass }}">{{ .Side }}</td>
                <td class="px-4 py-1.5 text-slate-400">{{ .Type }}</td>
                <td class="px-4 py-1.5 mono text-slate-300">{{ .StrikePrice }}</td>
                <td class="px-4 py-1.5 mono text-slate-400">{{ .Expiration }}</td>
				<td class="px-4 py-1.5 mono text-slate-300">{{ .OpenSelect }}</td>
                <td class="px-4 py-1.5 mono text-slate-300">{{ .Qty }}</td>
                <td class="px-4 py-1.5 mono text-slate-300">{{ .EntryPrice }}</td>
				<td class="px-4 py-1.5 mono text-slate-300">{{ .EntryAmount }}</td>
                <td class="px-4 py-1.5 mono text-slate-400">{{ .EntryTime }}</td>
                <td class="px-4 py-1.5 mono text-slate-400">{{ .ClosePrice }}</td>
                <td class="px-4 py-1.5 mono text-slate-400">{{ .CloseTime }}</td>
				<td class="px-4 py-1.5 text-slate-300">{{ .CloseReason }}</td>
                <td class="px-4 py-1.5 mono text-slate-300">{{ .RealizedPnL }}</td>
              </tr>
              {{ end }}
            </tbody>
          </table>
        </div>
      </div>
      {{ end }}
    </div>
    {{ end }}

    {{ if not .NoTradeRows }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
        <h2 class="!mb-0">Trade Blotter</h2>
        <span class="mono text-xs text-slate-400">{{ .TradesCount }} fills</span>
      </div>
      <div class="overflow-x-auto border border-white/8 rounded-lg">
        <div class="max-h-[32rem] overflow-auto">
          <table class="w-full text-sm">
            <thead class="sticky top-0 bg-[#111922]">
              <tr class="text-left text-slate-400 text-xs uppercase border-b border-white/8">
                <th class="px-4 py-2">Time</th>
                <th class="px-4 py-2">Security</th>
                <th class="px-4 py-2">Side</th>
								<th class="px-4 py-2">Reason</th>
                <th class="px-4 py-2">Qty</th>
                <th class="px-4 py-2">Fill</th>
                <th class="px-4 py-2">Fee</th>
                <th class="px-4 py-2">Slippage</th>
                <th class="px-4 py-2">Net</th>
              </tr>
            </thead>
            <tbody>
              {{ range .Trades }}
              <tr class="border-b border-white/5">
                <td class="px-4 py-2 mono text-slate-300">{{ .Timestamp }}</td>
                <td class="px-4 py-2 text-slate-200">{{ .Security }}</td>
                <td class="px-4 py-2 font-semibold {{ .SideClass }}">{{ .Side }}</td>
								<td class="px-4 py-2 text-slate-300">{{ .Reason }}</td>
                <td class="px-4 py-2 mono text-slate-200">{{ .Qty }}</td>
                <td class="px-4 py-2 mono text-slate-200">{{ .FillPrice }}</td>
                <td class="px-4 py-2 mono text-slate-300">{{ .Commission }}</td>
                <td class="px-4 py-2 mono text-slate-300">{{ .Slippage }}</td>
                <td class="px-4 py-2 mono text-white">{{ .NetAmount }}</td>
              </tr>
              {{ end }}
            </tbody>
          </table>
        </div>
      </div>
    </div>
    {{ end }}

    {{ if .Notes }}
    <div class="section">
      <h2>Notes</h2>
      {{ range .Notes }}
      <p class="text-sm text-slate-400 mb-1">{{ . }}</p>
      {{ end }}
    </div>
    {{ end }}
  </div>

  <script>
    const underlyingCandles = {{ .UnderlyingCandleData }};
    const underlyingMarkers = {{ .UnderlyingMarkerData }};
    const equitySeries = {{ .EquitySeriesData }};
    const drawdownSeries = {{ .DrawdownSeriesData }};
    const pnlUSDSeries = {{ .PnLUSDSeriesData }};
	const activeTimes = {{ .ActiveTimeData }};

    const chartTheme = {
      layout: {
        background: { color: '#0f1923' },
        textColor: '#94a3b8',
        fontFamily: 'IBM Plex Mono, monospace'
      },
      grid: {
        vertLines: { color: 'rgba(255,255,255,0.04)' },
        horzLines: { color: 'rgba(255,255,255,0.04)' }
      },
      timeScale: {
        borderColor: 'rgba(255,255,255,0.06)',
        timeVisible: true,
        secondsVisible: false,
        minBarSpacing: 0.1
      },
      rightPriceScale: {
        borderColor: 'rgba(255,255,255,0.06)'
      },
      crosshair: {
        vertLine: { color: 'rgba(94,234,212,0.3)', labelBackgroundColor: '#115e59' },
        horzLine: { color: 'rgba(251,191,36,0.3)', labelBackgroundColor: '#92400e' }
      }
    };

    function createChart(id, height) {
      var el = document.getElementById(id);
      if (!el || typeof LightweightCharts === 'undefined') return null;
      var chart = LightweightCharts.createChart(el, Object.assign({
        width: el.clientWidth, height: height,
        handleScroll: true, handleScale: true
      }, chartTheme));
      if (typeof ResizeObserver !== 'undefined') {
        new ResizeObserver(function() { chart.applyOptions({ width: el.clientWidth }); }).observe(el);
      }
      return chart;
    }

		function buildActiveTimeSet() {
			var set = new Set();
			if (!Array.isArray(activeTimes)) return set;
			activeTimes.forEach(function(t) {
				if (typeof t === 'number') set.add(t);
			});
			return set;
		}

		function filterLineSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

		function filterCandleSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

		function filterMarkerSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

    function syncCharts(charts) {
      var syncing = false;
      charts.forEach(function(src) {
        src.timeScale().subscribeVisibleLogicalRangeChange(function(r) {
          if (!r || syncing) return;
          syncing = true;
          charts.forEach(function(t) { if (t !== src) t.timeScale().setVisibleLogicalRange(r); });
          syncing = false;
        });
      });
    }

    var charts = [];
		var activeSet = buildActiveTimeSet();
		var hasActiveFilter = activeSet.size > 1;

		var underlyingChart = null;
		var underlyingSeries = null;
		var equityChart = null;
		var equityPlot = null;
		var pnlChart = null;
		var pnlPlot = null;
		var drawdownChart = null;
		var drawdownPlot = null;

    if (underlyingCandles.length > 0) {
      var pc = createChart('underlying-chart', 420);
      if (pc) {
				var cs = pc.addCandlestickSeries({
          upColor: '#22c55e', downColor: '#f97316',
          wickUpColor: '#22c55e', wickDownColor: '#f97316',
          borderUpColor: '#22c55e', borderDownColor: '#f97316'
        });
				cs.setData(underlyingCandles);
				cs.setMarkers(underlyingMarkers);
        pc.timeScale().fitContent();
				underlyingChart = pc;
				underlyingSeries = cs;
        charts.push(pc);
      }
    }

    var ec = createChart('equity-chart', 300);
    if (ec) {
      var el = ec.addAreaSeries({
        lineColor: '#2dd4bf', topColor: 'rgba(45,212,191,0.28)',
        bottomColor: 'rgba(45,212,191,0.02)', lineWidth: 2
      });
      el.setData(equitySeries);
      ec.timeScale().fitContent();
			equityChart = ec;
			equityPlot = el;
      charts.push(ec);
    }

    if (pnlUSDSeries.length > 0) {
      var puc = createChart('pnl-usd-chart', 300);
      if (puc) {
        var pul = puc.addAreaSeries({
          lineColor: '#a78bfa', topColor: 'rgba(167,139,250,0.25)',
          bottomColor: 'rgba(167,139,250,0.02)', lineWidth: 2
        });
        pul.setData(pnlUSDSeries);
        puc.timeScale().fitContent();
				pnlChart = puc;
				pnlPlot = pul;
        charts.push(puc);
      }
    }

    var dc = createChart('drawdown-chart', 220);
    if (dc) {
      var dl = dc.addAreaSeries({
        lineColor: '#fbbf24', topColor: 'rgba(251,191,36,0.25)',
        bottomColor: 'rgba(251,191,36,0.02)', lineWidth: 2
      });
      dl.setData(drawdownSeries);
      dc.timeScale().fitContent();
			drawdownChart = dc;
			drawdownPlot = dl;
      charts.push(dc);
    }

		function applyIdleFilter(enabled) {
			var useFilter = enabled && hasActiveFilter;

			if (underlyingSeries) {
				underlyingSeries.setData(filterCandleSeriesByTimes(underlyingCandles, activeSet, useFilter));
				underlyingSeries.setMarkers(filterMarkerSeriesByTimes(underlyingMarkers, activeSet, useFilter));
			}
			if (equityPlot) {
				equityPlot.setData(filterLineSeriesByTimes(equitySeries, activeSet, useFilter));
			}
			if (pnlPlot) {
				pnlPlot.setData(filterLineSeriesByTimes(pnlUSDSeries, activeSet, useFilter));
			}
			if (drawdownPlot) {
				drawdownPlot.setData(filterLineSeriesByTimes(drawdownSeries, activeSet, useFilter));
			}

			charts.forEach(function(chart) {
				chart.timeScale().fitContent();
			});
		}

    if (charts.length > 1) syncCharts(charts);

		var toggle = document.getElementById('toggle-ignore-idle');
		if (toggle) {
			toggle.disabled = !hasActiveFilter;
			if (!hasActiveFilter) {
				toggle.checked = false;
				toggle.title = 'No active positions/trades were detected on the timeline.';
			}
			toggle.addEventListener('change', function(e) {
				applyIdleFilter(e.target.checked);
			});
		}
  </script>
</body>
</html>`

const combinedHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>{{ .Title }}</title>
	<script>
		tailwind = window.tailwind || {};
		tailwind.config = {
			theme: {
				extend: {
					colors: {
						canvas: '#071019',
						shell: '#0d1823',
						panel: '#122130',
						mist: '#dbe8e2',
						aqua: '#3dd6c6',
						ember: '#f0a43a',
						rose: '#fb7185',
						steel: '#7dd3fc'
					},
					fontFamily: {
						sans: ['Sora', 'system-ui', 'sans-serif'],
						mono: ['IBM Plex Mono', 'monospace']
					},
					boxShadow: {
						report: '0 30px 90px rgba(0,0,0,.34)'
					}
				}
			}
		}
	</script>
	<script src="https://cdn.tailwindcss.com"></script>
	<script src="https://unpkg.com/lightweight-charts@4.2.3/dist/lightweight-charts.standalone.production.js"></script>
	<link rel="preconnect" href="https://fonts.googleapis.com">
	<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=Sora:wght@500;600;700;800&display=swap" rel="stylesheet">
	<style>
		:root {
			color-scheme: dark;
		}
		body {
			background:
				radial-gradient(circle at top left, rgba(61,214,198,.18), transparent 26%),
				radial-gradient(circle at top right, rgba(240,164,58,.14), transparent 22%),
				linear-gradient(180deg, #050d14 0%, #09131b 34%, #0d1823 100%);
		}
		.glass-panel {
			background: linear-gradient(180deg, rgba(255,255,255,.05) 0%, rgba(255,255,255,.02) 100%);
			backdrop-filter: blur(18px);
		}
		.chart-shell {
			background:
				linear-gradient(180deg, rgba(8,17,26,.94), rgba(8,17,26,.78)),
				radial-gradient(circle at top, rgba(61,214,198,.08), transparent 40%);
		}
		.chart-host {
			width: 100%;
			min-height: 18rem;
		}
		.chart-host.tall {
			min-height: 34rem;
		}
	</style>
</head>
<body class="min-h-screen font-sans text-mist">
	<main class="w-full px-3 py-4 sm:px-5 lg:px-8 lg:py-6">
		<section class="overflow-hidden rounded-[2rem] border border-white/10 bg-canvas/85 shadow-report">
			<div class="border-b border-white/10 px-5 py-7 sm:px-7 lg:px-10">
				<div class="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
					<div class="max-w-4xl">
						<p class="font-mono text-xs uppercase tracking-[0.34em] text-aqua/80">Combined Backtest Report</p>
						<h1 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-4xl lg:text-5xl">{{ .Asset }} · {{ .Interval }}</h1>
						<p class="mt-4 max-w-3xl text-sm leading-7 text-slate-300 sm:text-base">{{ .Period }}. All selected strategies are rendered in a single HTML file, with charts, trade records, and spread activity grouped by strategy.</p>
					</div>
					<div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-2 xl:min-w-[24rem]">
						<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Generated</div>
							<div class="mt-2 text-sm text-white sm:text-base">{{ .GeneratedAt }}</div>
						</div>
						<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Strategies</div>
							<div class="mt-2 text-base text-white">{{ len .Strategies }}</div>
						</div>
					</div>
				</div>
				<div class="mt-6 flex flex-wrap gap-3">
					{{ range .Strategies }}
					<a href="#{{ .AnchorID }}" class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-200 transition hover:border-aqua/40 hover:text-white">{{ .Report.StrategyName }}</a>
					{{ end }}
				</div>
			</div>

			<div class="space-y-8 px-5 py-6 sm:px-7 lg:px-10 lg:py-8">
				{{ range .Strategies }}
				<section id="{{ .AnchorID }}" class="rounded-[1.75rem] border border-white/10 bg-panel/45 p-5 lg:p-6">
					<div class="flex flex-col gap-4 border-b border-white/10 pb-5 lg:flex-row lg:items-end lg:justify-between">
						<div>
							<p class="font-mono text-xs uppercase tracking-[0.28em] text-aqua/70">Strategy</p>
							<h2 class="mt-2 text-3xl font-bold text-white">{{ .Report.StrategyName }}</h2>
							<p class="mt-2 text-sm text-slate-300">{{ .Report.Asset }} · {{ .Report.Interval }} · {{ .Report.Period }}</p>
						</div>
						<div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-3 xl:min-w-[32rem]">
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Final Equity</div>
								<div class="mt-2 text-base text-white">{{ .Report.FinalEquity }}</div>
							</div>
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Total Return</div>
								<div class="mt-2 text-base text-white">{{ .Report.TotalReturn }}</div>
							</div>
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Max Drawdown</div>
								<div class="mt-2 text-base text-white">{{ .Report.MaxDrawdown }}</div>
							</div>
						</div>
					</div>

					<div class="mt-5 grid gap-4 sm:grid-cols-2 2xl:grid-cols-6">
						<article class="glass-panel rounded-3xl border border-white/10 p-5 2xl:col-span-2">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Net PnL</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.NetPnL }}</div>
							<div class="mt-2 text-sm text-slate-300">Initial capital {{ .Report.InitialCapital }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Annualized</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.AnnualizedReturn }}</div>
							<div class="mt-2 text-sm text-slate-300">Sharpe {{ .Report.SharpeRatio }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Bars</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.BarsCount }}</div>
							<div class="mt-2 text-sm text-slate-300">Fees {{ .Report.TotalFees }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Trade Markers</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.TradeMarkerCount }}</div>
							<div class="mt-2 text-sm text-slate-300">Spread events {{ .Report.SpreadEventCount }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Activity</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.TradesCount }}</div>
							<div class="mt-2 text-sm text-slate-300">Spread positions {{ .Report.SpreadsCount }}</div>
						</article>
					</div>

					<div class="mt-6 space-y-6">
						<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
							<div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
								<div>
									<div class="flex flex-wrap items-center gap-3">
										<h3 class="text-2xl font-bold text-white">Underlying Price</h3>
										<span class="rounded-full border border-steel/30 bg-steel/10 px-3 py-1 font-mono text-[11px] uppercase tracking-[0.2em] text-steel">Candlestick + markers</span>
									</div>
									<p class="mt-2 text-sm text-slate-300">Range {{ .Report.UnderlyingPriceMin }} to {{ .Report.UnderlyingPriceMax }}. Buy and sell fills are marked directly on the price bars. Spread open and close events are also annotated when available.</p>
								</div>
							</div>
							{{ if .Report.UnderlyingChartNote }}
							<div class="mt-4 rounded-2xl border border-amber-400/15 bg-amber-400/8 px-4 py-3 text-sm text-amber-100">{{ .Report.UnderlyingChartNote }}</div>
							{{ end }}
							<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
								{{ if .Report.HasUnderlyingChart }}
								<div id="{{ .AnchorID }}-underlying-chart" class="chart-host tall"></div>
								{{ else }}
								<div class="flex min-h-[20rem] items-center justify-center rounded-[1.25rem] border border-dashed border-white/10 bg-white/[0.03] px-6 text-center text-sm text-slate-300">Underlying OHLC data was not present in the backtest result, so a candlestick chart could not be rendered.</div>
								{{ end }}
							</div>
						</section>

						<div class="space-y-6">
							<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
								<h3 class="text-2xl font-bold text-white">Equity Curve</h3>
								<p class="mt-2 text-sm text-slate-300">Portfolio equity path from {{ .Report.EquityMin }} to {{ .Report.EquityMax }}.</p>
								<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
									<div id="{{ .AnchorID }}-equity-chart" class="chart-host"></div>
								</div>
							</section>

							<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
								<h3 class="text-2xl font-bold text-white">Drawdown Trace</h3>
								<p class="mt-2 text-sm text-slate-300">Peak-to-trough stress reached {{ .Report.DrawdownMax }} at its deepest point.</p>
								<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
									<div id="{{ .AnchorID }}-drawdown-chart" class="chart-host"></div>
								</div>
							</section>
						</div>

						{{ if .Report.Notes }}
						<div class="grid gap-3 xl:grid-cols-{{ if gt (len .Report.Notes) 1 }}2{{ else }}1{{ end }}">
							{{ range .Report.Notes }}
							<div class="rounded-2xl border border-white/10 bg-white/[0.04] px-4 py-3 text-sm text-slate-300">{{ . }}</div>
							{{ end }}
						</div>
						{{ end }}

						<div class="grid gap-6 xl:grid-cols-3">
							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">Trade Overview</h3>
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">Raw fills</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.RawFills }}</dd></div>
									<div><dt class="text-slate-400">Round trips</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.RoundTrips }}</dd></div>
									<div><dt class="text-slate-400">Long fills</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.LongFills }}</dd></div>
									<div><dt class="text-slate-400">Short fills</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.ShortFills }}</dd></div>
									<div><dt class="text-slate-400">Total notional</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.TotalNotional }}</dd></div>
									<div><dt class="text-slate-400">Net PnL</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.NetPnL }}</dd></div>
									<div><dt class="text-slate-400">Gross profit</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.GrossProfit }}</dd></div>
									<div><dt class="text-slate-400">Gross loss</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.GrossLoss }}</dd></div>
									<div><dt class="text-slate-400">Avg round trip</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.AvgPnLPerRoundTrip }}</dd></div>
									<div><dt class="text-slate-400">Avg commission</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.AvgCommissionPerFill }}</dd></div>
								</dl>
							</section>

							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">Equity Analysis</h3>
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">Peak equity</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PeakEquity }}</dd></div>
									<div><dt class="text-slate-400">Lowest equity</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.LowestEquity }}</dd></div>
									<div><dt class="text-slate-400">Peak time</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PeakTime }}</dd></div>
									<div><dt class="text-slate-400">Lowest time</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.LowestTime }}</dd></div>
									<div><dt class="text-slate-400">Best bar</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.BestBarReturn }}</dd></div>
									<div><dt class="text-slate-400">Worst bar</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.WorstBarReturn }}</dd></div>
									<div><dt class="text-slate-400">Volatility</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.BarReturnVolatility }}</dd></div>
									<div><dt class="text-slate-400">Positive bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PositiveBars }}</dd></div>
									<div><dt class="text-slate-400">Negative bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.NegativeBars }}</dd></div>
									<div><dt class="text-slate-400">Flat bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.FlatBars }}</dd></div>
									<div><dt class="text-slate-400">DD duration bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.MaxDrawdownDurationBars }}</dd></div>
									<div><dt class="text-slate-400">DD duration</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.MaxDrawdownDuration }}</dd></div>
								</dl>
							</section>

							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">Spread Summary</h3>
								{{ if .Report.SpreadSummary }}
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">Total spreads</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.TotalSpreads }}</dd></div>
									<div><dt class="text-slate-400">Closed</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.ClosedSpreads }}</dd></div>
									<div><dt class="text-slate-400">Open</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.OpenSpreads }}</dd></div>
									<div><dt class="text-slate-400">Winning</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.WinningSpreads }}</dd></div>
									<div><dt class="text-slate-400">Losing</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.LosingSpreads }}</dd></div>
									<div><dt class="text-slate-400">Win rate</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.WinRate }}</dd></div>
									<div class="col-span-2"><dt class="text-slate-400">Total spread PnL</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.TotalPnL }}</dd></div>
								</dl>
								{{ else }}
								<p class="mt-4 text-sm text-slate-300">No spread summary was recorded for this run.</p>
								{{ end }}
							</section>
						</div>

						<section class="rounded-[1.75rem] border border-white/10 bg-panel/40 p-5 lg:p-6">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
								<h3 class="text-2xl font-bold text-white">Trade Blotter</h3>
								<span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .Report.TradesCount }} fills</span>
							</div>
							{{ if .Report.NoTradeRows }}
							<div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">No raw broker fills were recorded in this run.</div>
							{{ else }}
							<div class="mt-5 overflow-hidden rounded-2xl border border-white/10">
								<div class="max-h-[34rem] overflow-auto">
									<table class="min-w-full divide-y divide-white/10 text-sm">
										<thead class="sticky top-0 bg-canvas/95 backdrop-blur">
											<tr class="text-left text-slate-400">
												<th class="px-4 py-3 font-medium">Time</th>
												<th class="px-4 py-3 font-medium">Security</th>
												<th class="px-4 py-3 font-medium">Side</th>
												<th class="px-4 py-3 font-medium">Reason</th>
												<th class="px-4 py-3 font-medium">Qty</th>
												<th class="px-4 py-3 font-medium">Fill</th>
												<th class="px-4 py-3 font-medium">Fee</th>
												<th class="px-4 py-3 font-medium">Slippage</th>
												<th class="px-4 py-3 font-medium">Net</th>
											</tr>
										</thead>
										<tbody class="divide-y divide-white/5 bg-white/[0.02]">
											{{ range .Report.Trades }}
											<tr>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Timestamp }}</td>
												<td class="px-4 py-3 text-slate-200">{{ .Security }}</td>
												<td class="px-4 py-3 font-semibold {{ .SideClass }}">{{ .Side }}</td>
												<td class="px-4 py-3 text-slate-300">{{ .Reason }}</td>
												<td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
												<td class="px-4 py-3 font-mono text-slate-200">{{ .FillPrice }}</td>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Commission }}</td>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Slippage }}</td>
												<td class="px-4 py-3 font-mono text-white">{{ .NetAmount }}</td>
											</tr>
											{{ end }}
										</tbody>
									</table>
								</div>
							</div>
							{{ end }}
						</section>

						<section class="rounded-[1.75rem] border border-white/10 bg-panel/40 p-5 lg:p-6">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
								<h3 class="text-2xl font-bold text-white">Spread Activity</h3>
								<span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .Report.SpreadsCount }} positions</span>
							</div>
							{{ if .Report.NoSpreadRows }}
							<div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">No spread positions were recorded in this run.</div>
							{{ else }}
							<div class="mt-5 space-y-4">
								{{ range .Report.Spreads }}
								<article class="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.03]">
									<div class="flex flex-col gap-3 border-b border-white/10 px-4 py-4 lg:flex-row lg:items-center lg:justify-between">
										<div>
											<div class="flex flex-wrap items-center gap-3">
												<h4 class="text-lg font-bold text-white">#{{ .ID }} {{ .Tag }}</h4>
												<span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .StatusClass }}">{{ .Status }}</span>
											</div>
											<p class="mt-2 text-sm text-slate-300">Opened {{ .OpenTime }} · Closed {{ .CloseTime }} · Held {{ .DaysHeld }}</p>
										</div>
										<div class="grid grid-cols-2 gap-4 text-sm lg:text-right">
											<div><div class="text-slate-400">Net premium</div><div class="font-mono text-white">{{ .NetPremium }}</div></div>
											<div><div class="text-slate-400">Realized PnL</div><div class="font-mono text-white">{{ .RealizedPnL }}</div></div>
										</div>
									</div>
									<div class="overflow-auto">
										<table class="min-w-full divide-y divide-white/10 text-sm">
											<thead class="bg-canvas/60 text-left text-slate-400">
												<tr>
													<th class="px-4 py-3 font-medium">Symbol</th>
													<th class="px-4 py-3 font-medium">Side</th>
													<th class="px-4 py-3 font-medium">Type</th>
													<th class="px-4 py-3 font-medium">Strike</th>
													<th class="px-4 py-3 font-medium">Expiry</th>
													<th class="px-4 py-3 font-medium">Expiry-Open / Delta</th>
													<th class="px-4 py-3 font-medium">Qty</th>
													<th class="px-4 py-3 font-medium">Entry</th>
													<th class="px-4 py-3 font-medium">Close</th>
													<th class="px-4 py-3 font-medium">Close Reason</th>
													<th class="px-4 py-3 font-medium">PnL</th>
												</tr>
											</thead>
											<tbody class="divide-y divide-white/5">
												{{ range .Legs }}
												<tr>
													<td class="px-4 py-3 font-mono text-slate-200">{{ .Symbol }}</td>
													<td class="px-4 py-3 font-semibold {{ .SideClass }}">{{ .Side }}</td>
													<td class="px-4 py-3 text-slate-200">{{ .Type }}</td>
													<td class="px-4 py-3 font-mono text-slate-200">{{ .StrikePrice }}</td>
													<td class="px-4 py-3 font-mono text-slate-300">{{ .Expiration }}</td>
													<td class="px-4 py-3 font-mono text-slate-200">{{ .OpenSelect }}</td>
													<td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
													<td class="px-4 py-3 font-mono text-slate-200">{{ .EntryPrice }}</td>
													<td class="px-4 py-3 font-mono text-slate-300">{{ .ClosePrice }}</td>
													<td class="px-4 py-3 text-slate-300">{{ .CloseReason }}</td>
													<td class="px-4 py-3 font-mono text-white">{{ .RealizedPnL }}</td>
												</tr>
												{{ end }}
											</tbody>
										</table>
									</div>
								</article>
								{{ end }}
							</div>
							{{ end }}
						</section>
					</div>
				</section>
				{{ end }}
			</div>
		</section>
	</main>

	<script>
		const strategyReports = [
			{{ range .Strategies }}
			{
				anchorId: {{ .AnchorID }},
				hasUnderlyingChart: {{ if .Report.HasUnderlyingChart }}true{{ else }}false{{ end }},
				underlyingCandles: {{ .Report.UnderlyingCandleData }},
				underlyingMarkers: {{ .Report.UnderlyingMarkerData }},
				equitySeries: {{ .Report.EquitySeriesData }},
				drawdownSeries: {{ .Report.DrawdownSeriesData }}
			},
			{{ end }}
		];

		const chartTheme = {
			layout: {
				background: { color: '#08111a' },
				textColor: '#b6c6d2',
				fontFamily: 'IBM Plex Mono, monospace'
			},
			grid: {
				vertLines: { color: 'rgba(255,255,255,0.05)' },
				horzLines: { color: 'rgba(255,255,255,0.05)' }
			},
			timeScale: {
				borderColor: 'rgba(255,255,255,0.08)',
				timeVisible: true,
				secondsVisible: false
			},
			rightPriceScale: {
				borderColor: 'rgba(255,255,255,0.08)'
			},
			crosshair: {
				vertLine: {
					color: 'rgba(61,214,198,0.35)',
					labelBackgroundColor: '#0f766e'
				},
				horzLine: {
					color: 'rgba(240,164,58,0.35)',
					labelBackgroundColor: '#a16207'
				}
			}
		};

		function createResponsiveChart(containerId, height, extraOptions) {
			const container = document.getElementById(containerId);
			if (!container || typeof LightweightCharts === 'undefined') {
				return null;
			}
			const chart = LightweightCharts.createChart(
				container,
				Object.assign(
					{
						width: container.clientWidth,
						height: height,
						handleScroll: true,
						handleScale: true,
						localization: {
							locale: 'en-US'
						}
					},
					chartTheme,
					extraOptions || {}
				)
			);

			const resizeChart = function() {
				chart.applyOptions({ width: container.clientWidth });
			};

			if (typeof ResizeObserver !== 'undefined') {
				const observer = new ResizeObserver(function() {
					resizeChart();
				});
				observer.observe(container);
			} else {
				window.addEventListener('resize', resizeChart);
			}

			return chart;
		}

		function syncVisibleRanges(charts) {
			let syncing = false;
			charts.forEach(function(sourceChart) {
				sourceChart.timeScale().subscribeVisibleLogicalRangeChange(function(range) {
					if (!range || syncing) {
						return;
					}
					syncing = true;
					charts.forEach(function(targetChart) {
						if (targetChart !== sourceChart) {
							targetChart.timeScale().setVisibleLogicalRange(range);
						}
					});
					syncing = false;
				});
			});
		}

		strategyReports.forEach(function(report) {
			const charts = [];

			if (report.hasUnderlyingChart && report.underlyingCandles.length > 0) {
				const priceChart = createResponsiveChart(report.anchorId + '-underlying-chart', 560, {
					watermark: {
						visible: true,
						text: 'UNDERLYING',
						fontSize: 42,
						color: 'rgba(255,255,255,0.04)'
					}
				});
				if (priceChart) {
					const candleSeries = priceChart.addCandlestickSeries({
						upColor: '#22c55e',
						downColor: '#f97316',
						wickUpColor: '#22c55e',
						wickDownColor: '#f97316',
						borderUpColor: '#22c55e',
						borderDownColor: '#f97316',
						lastValueVisible: true,
						priceLineVisible: true
					});
					candleSeries.setData(report.underlyingCandles);
					candleSeries.setMarkers(report.underlyingMarkers);
					priceChart.timeScale().fitContent();
					charts.push(priceChart);
				}
			}

			const equityChart = createResponsiveChart(report.anchorId + '-equity-chart', 320, {
				watermark: {
					visible: true,
					text: 'EQUITY',
					fontSize: 30,
					color: 'rgba(255,255,255,0.035)'
				}
			});
			if (equityChart) {
				const equityLine = equityChart.addAreaSeries({
					lineColor: '#3dd6c6',
					topColor: 'rgba(61,214,198,0.34)',
					bottomColor: 'rgba(61,214,198,0.04)',
					lineWidth: 3,
					priceLineVisible: true,
					lastValueVisible: true
				});
				equityLine.setData(report.equitySeries);
				equityChart.timeScale().fitContent();
				charts.push(equityChart);
			}

			const drawdownChart = createResponsiveChart(report.anchorId + '-drawdown-chart', 320, {
				watermark: {
					visible: true,
					text: 'DRAWDOWN',
					fontSize: 30,
					color: 'rgba(255,255,255,0.035)'
				}
			});
			if (drawdownChart) {
				const drawdownLine = drawdownChart.addAreaSeries({
					lineColor: '#f0a43a',
					topColor: 'rgba(240,164,58,0.32)',
					bottomColor: 'rgba(240,164,58,0.04)',
					lineWidth: 3,
					priceLineVisible: true,
					lastValueVisible: true
				});
				drawdownLine.setData(report.drawdownSeries);
				drawdownChart.timeScale().fitContent();
				charts.push(drawdownChart);
			}

			if (charts.length > 1) {
				syncVisibleRanges(charts);
			}
		});
	</script>
</body>
</html>`
