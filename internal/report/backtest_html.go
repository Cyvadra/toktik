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
	EquityAnalysis       equityAnalysisView
	TradeOverview        tradeOverviewView
	SpreadSummary        *spreadSummaryView
	Trades               []tradeRowView
	Spreads              []spreadRowView
	NoTradeRows          bool
	NoSpreadRows         bool
	Notes                []string
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
	Qty         string
	EntryPrice  string
	ClosePrice  string
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
		InitialCapital:       currency(result.InitialCapital),
		FinalEquity:          currency(result.FinalEquity),
		NetPnL:               signedCurrency(result.FinalEquity - result.InitialCapital),
		TotalReturn:          pct(result.TotalReturn),
		AnnualizedReturn:     pct(result.AnnualizedReturn),
		SharpeRatio:          decimal(result.SharpeRatio),
		MaxDrawdown:          pct(result.MaxDrawdown),
		TotalFees:            currency(result.TotalFees),
		BarsCount:            result.BarsCount,
		TradesCount:          len(result.Trades),
		SpreadsCount:         len(result.SpreadPositions),
		NoTradeRows:          len(result.Trades) == 0,
		NoSpreadRows:         len(result.SpreadPositions) == 0,
		UnderlyingCandleData: template.JS("[]"),
		UnderlyingMarkerData: template.JS("[]"),
		EquitySeriesData:     marshalJS(buildLineSeries(result.Timestamps, result.EquityCurve)),
		DrawdownSeriesData:   marshalJS(buildLineSeries(result.Timestamps, drawdown)),
	}

	minEq, maxEq := minMax(result.EquityCurve)
	view.EquityMin = currency(minEq)
	view.EquityMax = currency(maxEq)
	view.DrawdownMax = pct(maxValue(drawdown))
	view.TradeOverview = buildTradeOverviewView(result.TradeOverview)
	view.EquityAnalysis = buildEquityAnalysisView(result.EquityAnalysis)
	view.Trades = buildTradeRows(result.Trades)
	view.Spreads = buildSpreadRows(result.SpreadPositions)

	if result.SpreadSummary != nil {
		s := result.SpreadSummary
		view.SpreadSummary = &spreadSummaryView{
			TotalSpreads:   integer(s.TotalSpreads),
			ClosedSpreads:  integer(s.ClosedSpreads),
			OpenSpreads:    integer(s.OpenSpreads),
			WinningSpreads: integer(s.WinningSpreads),
			LosingSpreads:  integer(s.LosingSpreads),
			WinRate:        pct(s.WinRate),
			TotalPnL:       signedCurrency(s.TotalPnL),
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
	view.TradeMarkerCount = tradeMarkerCount
	view.SpreadEventCount = spreadEventCount
	view.Notes = buildNotes(result, candleFallback)

	return view
}

func buildTradeOverviewView(overview *backtest.TradeOverview) tradeOverviewView {
	if overview == nil {
		return tradeOverviewView{}
	}
	return tradeOverviewView{
		RawFills:             integer(overview.RawFills),
		RoundTrips:           integer(overview.RoundTrips),
		LongFills:            integer(overview.LongFills),
		ShortFills:           integer(overview.ShortFills),
		TotalNotional:        currency(overview.TotalNotional),
		GrossProfit:          currency(overview.GrossProfit),
		GrossLoss:            currency(overview.GrossLoss),
		NetPnL:               signedCurrency(overview.NetPnL),
		AvgPnLPerRoundTrip:   signedCurrency(overview.AvgPnLPerRoundTrip),
		AvgCommissionPerFill: currency(overview.AvgCommissionPerFill),
	}
}

func buildEquityAnalysisView(analysis *backtest.EquityAnalysis) equityAnalysisView {
	if analysis == nil {
		return equityAnalysisView{}
	}
	return equityAnalysisView{
		PeakEquity:              currency(analysis.PeakEquity),
		PeakTime:                formatDateTime(analysis.PeakTime),
		LowestEquity:            currency(analysis.LowestEquity),
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

func buildTradeRows(trades []backtest.Trade) []tradeRowView {
	rows := make([]tradeRowView, 0, len(trades))
	for _, trade := range trades {
		side := trade.Side.String()
		rows = append(rows, tradeRowView{
			Timestamp:  formatDateTime(trade.Timestamp),
			Security:   fmt.Sprintf("%s / %s / %s", trade.Security.Market, trade.Security.Symbol, trade.Security.Interval),
			Side:       strings.ToUpper(side),
			Qty:        decimal(trade.Qty),
			FillPrice:  currency(trade.FillPrice),
			Commission: currency(trade.Commission),
			Slippage:   currency(trade.Slippage),
			NetAmount:  signedCurrency(trade.NetAmount()),
			SideClass:  sideClass(side),
		})
	}
	return rows
}

func buildSpreadRows(spreads []backtest.SpreadPositionReport) []spreadRowView {
	rows := make([]spreadRowView, 0, len(spreads))
	for _, spread := range spreads {
		row := spreadRowView{
			ID:          spread.ID,
			Tag:         spread.Tag,
			Status:      strings.ToUpper(spread.Status),
			OpenTime:    formatDateTime(spread.OpenTime),
			CloseTime:   "-",
			DaysHeld:    fmt.Sprintf("%.2f d", spread.DaysHeld),
			NetPremium:  signedCurrency(spread.NetPremium),
			RealizedPnL: signedCurrency(spread.RealizedPnL),
			StatusClass: statusClass(spread.Status),
			Legs:        make([]spreadLegRowView, 0, len(spread.Legs)),
		}
		if spread.CloseTime != nil {
			row.CloseTime = formatDateTime(*spread.CloseTime)
		}
		for _, leg := range spread.Legs {
			row.Legs = append(row.Legs, spreadLegRowView{
				Symbol:      leg.Symbol,
				Side:        strings.ToUpper(leg.Side),
				Type:        strings.ToUpper(string(leg.Type)),
				StrikePrice: currency(leg.StrikePrice),
				Expiration:  formatDate(leg.Expiration),
				Qty:         decimal(leg.Qty),
				EntryPrice:  currency4(leg.EntryPrice),
				ClosePrice:  nullableCurrency4(leg.ClosePrice, leg.Closed),
				RealizedPnL: signedCurrency(leg.RealizedPnL),
				SideClass:   sideClass(leg.Side),
			})
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
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	if value < 0 {
		return "-$" + fmt.Sprintf("%.2f", -value)
	}
	return "$" + fmt.Sprintf("%.2f", value)
}

func currency4(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	if value < 0 {
		return "-$" + fmt.Sprintf("%.4f", -value)
	}
	return "$" + fmt.Sprintf("%.4f", value)
}

func signedCurrency(value float64) string {
	if value > 0 {
		return "+" + currency(value)
	}
	return currency(value)
}

func decimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
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
	if !ok {
		return "-"
	}
	return currency4(value)
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

const htmlTemplate = `<!DOCTYPE html>
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
            <p class="font-mono text-xs uppercase tracking-[0.34em] text-aqua/80">Backtest Report</p>
            <h1 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-4xl lg:text-5xl">{{ .StrategyName }}</h1>
            <p class="mt-4 max-w-3xl text-sm leading-7 text-slate-300 sm:text-base">{{ .Asset }} · {{ .Interval }} · {{ .Period }}. The dashboard is optimized for full-width review with an interactive underlying candlestick chart, execution markers, equity trajectory, drawdown behavior, and spread lifecycle details.</p>
          </div>
          <div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-3 xl:min-w-[28rem]">
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Generated</div>
              <div class="mt-2 text-sm text-white sm:text-base">{{ .GeneratedAt }}</div>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Bars</div>
              <div class="mt-2 text-base text-white">{{ .BarsCount }}</div>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Initial Capital</div>
              <div class="mt-2 text-base text-white">{{ .InitialCapital }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-4 px-5 py-5 sm:grid-cols-2 2xl:grid-cols-6 sm:px-7 lg:px-10">
        <article class="glass-panel rounded-3xl border border-white/10 p-5 2xl:col-span-2">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Final Equity</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .FinalEquity }}</div>
          <div class="mt-2 text-sm text-slate-300">Net PnL {{ .NetPnL }}</div>
        </article>
        <article class="glass-panel rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Total Return</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .TotalReturn }}</div>
          <div class="mt-2 text-sm text-slate-300">Annualized {{ .AnnualizedReturn }}</div>
        </article>
        <article class="glass-panel rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Risk</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .MaxDrawdown }}</div>
          <div class="mt-2 text-sm text-slate-300">Sharpe {{ .SharpeRatio }}</div>
        </article>
        <article class="glass-panel rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Trade Markers</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .TradeMarkerCount }}</div>
          <div class="mt-2 text-sm text-slate-300">Spread events {{ .SpreadEventCount }}</div>
        </article>
        <article class="glass-panel rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Activity</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .TradesCount }}</div>
          <div class="mt-2 text-sm text-slate-300">Spread positions {{ .SpreadsCount }}</div>
        </article>
      </div>

      <div class="space-y-6 px-5 pb-6 sm:px-7 lg:px-10 lg:pb-8">
        <section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
          <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <div class="flex flex-wrap items-center gap-3">
                <h2 class="text-2xl font-bold text-white">Underlying Price</h2>
                <span class="rounded-full border border-steel/30 bg-steel/10 px-3 py-1 font-mono text-[11px] uppercase tracking-[0.2em] text-steel">Candlestick + markers</span>
              </div>
              <p class="mt-2 text-sm text-slate-300">Range {{ .UnderlyingPriceMin }} to {{ .UnderlyingPriceMax }}. Buy and sell fills are marked directly on the price bars. Spread open and close events are also annotated when available.</p>
            </div>
            <div class="flex flex-wrap gap-2 text-xs text-slate-300">
              <span class="rounded-full border border-emerald-400/20 bg-emerald-400/10 px-3 py-1 font-mono">BUY</span>
              <span class="rounded-full border border-amber-400/20 bg-amber-400/10 px-3 py-1 font-mono">SELL</span>
              <span class="rounded-full border border-steel/20 bg-steel/10 px-3 py-1 font-mono">SPREAD OPEN</span>
              <span class="rounded-full border border-rose/20 bg-rose/10 px-3 py-1 font-mono">SPREAD CLOSE</span>
            </div>
          </div>
          {{ if .UnderlyingChartNote }}
          <div class="mt-4 rounded-2xl border border-amber-400/15 bg-amber-400/8 px-4 py-3 text-sm text-amber-100">{{ .UnderlyingChartNote }}</div>
          {{ end }}
          <div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
            {{ if .HasUnderlyingChart }}
            <div id="underlying-chart" class="chart-host tall"></div>
            {{ else }}
            <div class="flex min-h-[20rem] items-center justify-center rounded-[1.25rem] border border-dashed border-white/10 bg-white/[0.03] px-6 text-center text-sm text-slate-300">Underlying OHLC data was not present in the backtest result, so a candlestick chart could not be rendered.</div>
            {{ end }}
          </div>
        </section>

        <section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 class="text-2xl font-bold text-white">Equity Curve</h2>
              <p class="mt-2 text-sm text-slate-300">Portfolio equity path from {{ .EquityMin }} to {{ .EquityMax }}. Fees paid {{ .TotalFees }}.</p>
            </div>
          </div>
          <div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
            <div id="equity-chart" class="chart-host"></div>
          </div>
        </section>

        <section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 class="text-2xl font-bold text-white">Drawdown Trace</h2>
              <p class="mt-2 text-sm text-slate-300">Peak-to-trough stress reached {{ .DrawdownMax }} at its deepest point.</p>
            </div>
          </div>
          <div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
            <div id="drawdown-chart" class="chart-host"></div>
          </div>
          {{ if .Notes }}
          <div class="mt-5 grid gap-3 xl:grid-cols-{{ if gt (len .Notes) 1 }}2{{ else }}1{{ end }}">
            {{ range .Notes }}
            <div class="rounded-2xl border border-white/10 bg-white/[0.04] px-4 py-3 text-sm text-slate-300">{{ . }}</div>
            {{ end }}
          </div>
          {{ end }}
        </section>
      </div>

      <div class="grid gap-6 px-5 pb-6 sm:px-7 xl:grid-cols-3 lg:px-10">
        <section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
          <h2 class="text-lg font-bold text-white">Trade Overview</h2>
          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
            <div><dt class="text-slate-400">Raw fills</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.RawFills }}</dd></div>
            <div><dt class="text-slate-400">Round trips</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.RoundTrips }}</dd></div>
            <div><dt class="text-slate-400">Long fills</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.LongFills }}</dd></div>
            <div><dt class="text-slate-400">Short fills</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.ShortFills }}</dd></div>
            <div><dt class="text-slate-400">Total notional</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.TotalNotional }}</dd></div>
            <div><dt class="text-slate-400">Net PnL</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.NetPnL }}</dd></div>
            <div><dt class="text-slate-400">Gross profit</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.GrossProfit }}</dd></div>
            <div><dt class="text-slate-400">Gross loss</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.GrossLoss }}</dd></div>
            <div><dt class="text-slate-400">Avg round trip</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.AvgPnLPerRoundTrip }}</dd></div>
            <div><dt class="text-slate-400">Avg commission</dt><dd class="mt-1 font-mono text-white">{{ .TradeOverview.AvgCommissionPerFill }}</dd></div>
          </dl>
        </section>

        <section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
          <h2 class="text-lg font-bold text-white">Equity Analysis</h2>
          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
            <div><dt class="text-slate-400">Peak equity</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.PeakEquity }}</dd></div>
            <div><dt class="text-slate-400">Lowest equity</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.LowestEquity }}</dd></div>
            <div><dt class="text-slate-400">Peak time</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.PeakTime }}</dd></div>
            <div><dt class="text-slate-400">Lowest time</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.LowestTime }}</dd></div>
            <div><dt class="text-slate-400">Best bar</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.BestBarReturn }}</dd></div>
            <div><dt class="text-slate-400">Worst bar</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.WorstBarReturn }}</dd></div>
            <div><dt class="text-slate-400">Volatility</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.BarReturnVolatility }}</dd></div>
            <div><dt class="text-slate-400">Positive bars</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.PositiveBars }}</dd></div>
            <div><dt class="text-slate-400">Negative bars</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.NegativeBars }}</dd></div>
            <div><dt class="text-slate-400">Flat bars</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.FlatBars }}</dd></div>
            <div><dt class="text-slate-400">DD duration bars</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.MaxDrawdownDurationBars }}</dd></div>
            <div><dt class="text-slate-400">DD duration</dt><dd class="mt-1 font-mono text-white">{{ .EquityAnalysis.MaxDrawdownDuration }}</dd></div>
          </dl>
        </section>

        <section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
          <h2 class="text-lg font-bold text-white">Spread Summary</h2>
          {{ if .SpreadSummary }}
          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
            <div><dt class="text-slate-400">Total spreads</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.TotalSpreads }}</dd></div>
            <div><dt class="text-slate-400">Closed</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.ClosedSpreads }}</dd></div>
            <div><dt class="text-slate-400">Open</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.OpenSpreads }}</dd></div>
            <div><dt class="text-slate-400">Winning</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.WinningSpreads }}</dd></div>
            <div><dt class="text-slate-400">Losing</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.LosingSpreads }}</dd></div>
            <div><dt class="text-slate-400">Win rate</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.WinRate }}</dd></div>
            <div class="col-span-2"><dt class="text-slate-400">Total spread PnL</dt><dd class="mt-1 font-mono text-white">{{ .SpreadSummary.TotalPnL }}</dd></div>
          </dl>
          {{ else }}
          <p class="mt-4 text-sm text-slate-300">No spread summary was recorded for this run.</p>
          {{ end }}
        </section>
      </div>

      <div class="space-y-6 px-5 pb-8 sm:px-7 lg:px-10 lg:pb-10">
        <section class="rounded-[1.75rem] border border-white/10 bg-panel/40 p-5 lg:p-6">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 class="text-2xl font-bold text-white">Trade Blotter</h2>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .TradesCount }} fills</span>
          </div>
          {{ if .NoTradeRows }}
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
                    <th class="px-4 py-3 font-medium">Qty</th>
                    <th class="px-4 py-3 font-medium">Fill</th>
                    <th class="px-4 py-3 font-medium">Fee</th>
                    <th class="px-4 py-3 font-medium">Slippage</th>
                    <th class="px-4 py-3 font-medium">Net</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-white/5 bg-white/[0.02]">
                  {{ range .Trades }}
                  <tr>
                    <td class="px-4 py-3 font-mono text-slate-300">{{ .Timestamp }}</td>
                    <td class="px-4 py-3 text-slate-200">{{ .Security }}</td>
                    <td class="px-4 py-3 font-semibold {{ .SideClass }}">{{ .Side }}</td>
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
            <h2 class="text-2xl font-bold text-white">Spread Activity</h2>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .SpreadsCount }} positions</span>
          </div>
          {{ if .NoSpreadRows }}
          <div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">No spread positions were recorded in this run.</div>
          {{ else }}
          <div class="mt-5 space-y-4">
            {{ range .Spreads }}
            <article class="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.03]">
              <div class="flex flex-col gap-3 border-b border-white/10 px-4 py-4 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <div class="flex flex-wrap items-center gap-3">
                    <h3 class="text-lg font-bold text-white">#{{ .ID }} {{ .Tag }}</h3>
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
                      <th class="px-4 py-3 font-medium">Qty</th>
                      <th class="px-4 py-3 font-medium">Entry</th>
                      <th class="px-4 py-3 font-medium">Close</th>
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
                      <td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
                      <td class="px-4 py-3 font-mono text-slate-200">{{ .EntryPrice }}</td>
                      <td class="px-4 py-3 font-mono text-slate-300">{{ .ClosePrice }}</td>
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
  </main>

  <script>
    const underlyingCandles = {{ .UnderlyingCandleData }};
    const underlyingMarkers = {{ .UnderlyingMarkerData }};
    const equitySeries = {{ .EquitySeriesData }};
    const drawdownSeries = {{ .DrawdownSeriesData }};

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

    const charts = [];

    if (underlyingCandles.length > 0) {
      const priceChart = createResponsiveChart('underlying-chart', 560, {
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
        candleSeries.setData(underlyingCandles);
        candleSeries.setMarkers(underlyingMarkers);
        priceChart.timeScale().fitContent();
        charts.push(priceChart);
      }
    }

    const equityChart = createResponsiveChart('equity-chart', 320, {
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
      equityLine.setData(equitySeries);
      equityChart.timeScale().fitContent();
      charts.push(equityChart);
    }

    const drawdownChart = createResponsiveChart('drawdown-chart', 260, {
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
      drawdownLine.setData(drawdownSeries);
      drawdownChart.timeScale().fitContent();
      charts.push(drawdownChart);
    }

    if (charts.length > 1) {
      syncVisibleRanges(charts);
    }
  </script>
</body>
</html>`
