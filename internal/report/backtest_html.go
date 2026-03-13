package report

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
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
	Title               string
	StrategyName        string
	Asset               string
	Interval            string
	Period              string
	GeneratedAt         string
	InitialCapital      string
	FinalEquity         string
	NetPnL              string
	TotalReturn         string
	AnnualizedReturn    string
	SharpeRatio         string
	MaxDrawdown         string
	TotalFees           string
	BarsCount           int
	TradesCount         int
	SpreadsCount        int
	EquityMin           string
	EquityMax           string
	DrawdownMax         string
	EquityPath          string
	DrawdownPath        string
	HasUnderlyingPrice  bool
	UnderlyingPricePath string
	UnderlyingPriceMin  string
	UnderlyingPriceMax  string
	EquityAnalysis      equityAnalysisView
	TradeOverview       tradeOverviewView
	SpreadSummary       *spreadSummaryView
	Trades              []tradeRowView
	Spreads             []spreadRowView
	NoTradeRows         bool
	NoSpreadRows        bool
	Notes               []string
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
	view := htmlReportView{
		Title:            fmt.Sprintf("%s Backtest Report", result.StrategyName),
		StrategyName:     result.StrategyName,
		Asset:            meta.Asset,
		Interval:         meta.Interval,
		Period:           fmt.Sprintf("%s to %s", formatDate(result.StartTime), formatDate(result.EndTime)),
		GeneratedAt:      meta.GeneratedAt.Format("2006-01-02 15:04:05"),
		InitialCapital:   currency(result.InitialCapital),
		FinalEquity:      currency(result.FinalEquity),
		NetPnL:           signedCurrency(result.FinalEquity - result.InitialCapital),
		TotalReturn:      pct(result.TotalReturn),
		AnnualizedReturn: pct(result.AnnualizedReturn),
		SharpeRatio:      decimal(result.SharpeRatio),
		MaxDrawdown:      pct(result.MaxDrawdown),
		TotalFees:        currency(result.TotalFees),
		BarsCount:        result.BarsCount,
		TradesCount:      len(result.Trades),
		SpreadsCount:     len(result.SpreadPositions),
		EquityPath:       linePath(result.EquityCurve, 960, 320),
		DrawdownPath:     linePath(drawdownSeries(result.EquityCurve), 960, 180),
		NoTradeRows:      len(result.Trades) == 0,
		NoSpreadRows:     len(result.SpreadPositions) == 0,
	}

	minEq, maxEq := minMax(result.EquityCurve)
	view.EquityMin = currency(minEq)
	view.EquityMax = currency(maxEq)
	view.DrawdownMax = pct(maxValue(drawdownSeries(result.EquityCurve)))
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
	if underlying, ok := result.Series["close"]; ok && len(underlying) > 0 {
		view.HasUnderlyingPrice = true
		minU, maxU := minMax(underlying)
		view.UnderlyingPriceMin = currency(minU)
		view.UnderlyingPriceMax = currency(maxU)
		view.UnderlyingPricePath = linePath(underlying, 960, 320)
	}
	view.Notes = buildNotes(result)
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

func buildNotes(result *backtest.Result) []string {
	notes := make([]string, 0, 3)
	if len(result.Trades) == 0 && len(result.SpreadPositions) > 0 {
		notes = append(notes, "This strategy executed through spread tracker legs, so the raw broker trade table is empty while spread activity is listed below.")
	}
	if result.EquityAnalysis != nil && result.EquityAnalysis.MaxDrawdownDurationBars > 0 {
		notes = append(notes, fmt.Sprintf("Longest drawdown stretch lasted %d bars (%.1f hours).", result.EquityAnalysis.MaxDrawdownDurationBars, result.EquityAnalysis.MaxDrawdownDuration))
	}
	if result.TradeOverview != nil && result.TradeOverview.RoundTrips == 0 && len(result.SpreadPositions) == 0 {
		notes = append(notes, "No closed round trips were recorded in this run.")
	}
	return notes
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

func nullableCurrency(value float64, ok bool) string {
	if !ok {
		return "-"
	}
	return currency(value)
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
            canvas: '#09121a',
            ink: '#e6f0ea',
            accent: '#e59f32',
            tide: '#4ad0c2',
            panel: '#10212d'
          },
          fontFamily: {
            sans: ['Manrope', 'system-ui', 'sans-serif'],
            mono: ['IBM Plex Mono', 'monospace']
          },
          boxShadow: {
            report: '0 24px 80px rgba(0,0,0,.28)'
          }
        }
      }
    }
  </script>
  <script src="https://cdn.tailwindcss.com"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=Manrope:wght@500;700;800&display=swap" rel="stylesheet">
  <style>
    body {
      background:
        radial-gradient(circle at top left, rgba(74,208,194,.18), transparent 32%),
        radial-gradient(circle at top right, rgba(229,159,50,.14), transparent 26%),
        linear-gradient(180deg, #071018 0%, #0c1820 38%, #13232d 100%);
    }
    .metric-card {
      background: linear-gradient(180deg, rgba(255,255,255,.05) 0%, rgba(255,255,255,.02) 100%);
      backdrop-filter: blur(16px);
    }
  </style>
</head>
<body class="min-h-screen text-ink font-sans">
  <main class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
    <section class="overflow-hidden rounded-[2rem] border border-white/10 bg-canvas/80 shadow-report">
      <div class="border-b border-white/10 px-6 py-8 sm:px-10">
        <div class="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
          <div class="max-w-3xl">
            <p class="font-mono text-xs uppercase tracking-[0.3em] text-tide/80">Backtest Report</p>
            <h1 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-5xl">{{ .StrategyName }}</h1>
            <p class="mt-4 max-w-2xl text-sm leading-6 text-slate-300 sm:text-base">{{ .Asset }} · {{ .Interval }} · {{ .Period }}. The page includes equity diagnostics, transaction activity and options spread lifecycle details.</p>
          </div>
          <div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-2">
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Generated</div>
              <div class="mt-2 text-base text-white">{{ .GeneratedAt }}</div>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Bars</div>
              <div class="mt-2 text-base text-white">{{ .BarsCount }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-4 px-6 py-6 sm:grid-cols-2 xl:grid-cols-4 sm:px-10">
        <article class="metric-card rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Final Equity</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .FinalEquity }}</div>
          <div class="mt-2 text-sm text-slate-300">Net PnL {{ .NetPnL }}</div>
        </article>
        <article class="metric-card rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Total Return</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .TotalReturn }}</div>
          <div class="mt-2 text-sm text-slate-300">Annualized {{ .AnnualizedReturn }}</div>
        </article>
        <article class="metric-card rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Risk</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .MaxDrawdown }}</div>
          <div class="mt-2 text-sm text-slate-300">Sharpe {{ .SharpeRatio }}</div>
        </article>
        <article class="metric-card rounded-3xl border border-white/10 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Activity</div>
          <div class="mt-3 text-3xl font-bold text-white">{{ .TradesCount }}</div>
          <div class="mt-2 text-sm text-slate-300">Spread positions {{ .SpreadsCount }}</div>
        </article>
      </div>

      <div class="grid gap-6 px-6 pb-6 sm:px-10 xl:grid-cols-[1.5fr,1fr]">
        <section class="rounded-3xl border border-white/10 bg-panel/60 p-5">
          <div class="flex items-center justify-between gap-4">
            <div>
              <h2 class="text-xl font-bold text-white">Equity Curve</h2>
              <p class="mt-1 text-sm text-slate-300">Range {{ .EquityMin }} to {{ .EquityMax }}</p>
            </div>
            <div class="rounded-full border border-tide/30 bg-tide/10 px-3 py-1 font-mono text-xs text-tide">Fees {{ .TotalFees }}</div>
          </div>
          <div class="mt-5 overflow-hidden rounded-2xl border border-white/10 bg-[#08131c] p-4">
            {{ if .HasUnderlyingPrice }}
            <div class="mb-3 flex items-center gap-4 text-xs text-slate-400">
              <span class="flex items-center gap-1.5"><span class="inline-block h-0.5 w-5 rounded bg-[#4ad0c2]"></span> Equity</span>
              <span class="flex items-center gap-1.5"><span class="inline-block h-0.5 w-5 rounded bg-[#e59f32] opacity-70"></span> Underlying Price ({{ .UnderlyingPriceMin }} – {{ .UnderlyingPriceMax }})</span>
            </div>
            {{ end }}
            <svg viewBox="0 0 960 320" class="h-72 w-full">
              {{ if .HasUnderlyingPrice }}
              <path d="{{ .UnderlyingPricePath }}" fill="none" stroke="#e59f32" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" opacity="0.7" />
              {{ end }}
              <path d="{{ .EquityPath }}" fill="none" stroke="#4ad0c2" stroke-width="4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </div>
        </section>

        <section class="rounded-3xl border border-white/10 bg-panel/60 p-5">
          <h2 class="text-xl font-bold text-white">Drawdown Trace</h2>
          <p class="mt-1 text-sm text-slate-300">Maximum observed drawdown {{ .DrawdownMax }}</p>
          <div class="mt-5 overflow-hidden rounded-2xl border border-white/10 bg-[#08131c] p-4">
            <svg viewBox="0 0 960 180" class="h-44 w-full">
              <path d="{{ .DrawdownPath }}" fill="none" stroke="#e59f32" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </div>
          {{ if .Notes }}
          <div class="mt-5 space-y-3 text-sm text-slate-300">
            {{ range .Notes }}
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">{{ . }}</div>
            {{ end }}
          </div>
          {{ end }}
        </section>
      </div>

      <div class="grid gap-6 px-6 pb-6 sm:px-10 xl:grid-cols-3">
        <section class="rounded-3xl border border-white/10 bg-panel/50 p-5">
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

        <section class="rounded-3xl border border-white/10 bg-panel/50 p-5">
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

        <section class="rounded-3xl border border-white/10 bg-panel/50 p-5">
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

      <div class="grid gap-6 px-6 pb-10 sm:px-10 xl:grid-cols-[1.1fr,1.4fr]">
        <section class="rounded-3xl border border-white/10 bg-panel/40 p-5">
          <div class="flex items-center justify-between">
            <h2 class="text-xl font-bold text-white">Trade Blotter</h2>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .TradesCount }} fills</span>
          </div>
          {{ if .NoTradeRows }}
          <div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">No raw broker fills were recorded in this run.</div>
          {{ else }}
          <div class="mt-5 overflow-hidden rounded-2xl border border-white/10">
            <div class="max-h-[32rem] overflow-auto">
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

        <section class="rounded-3xl border border-white/10 bg-panel/40 p-5">
          <div class="flex items-center justify-between">
            <h2 class="text-xl font-bold text-white">Spread Activity</h2>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .SpreadsCount }} positions</span>
          </div>
          {{ if .NoSpreadRows }}
          <div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">No spread positions were recorded in this run.</div>
          {{ else }}
          <div class="mt-5 space-y-4">
            {{ range .Spreads }}
            <article class="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.03]">
              <div class="flex flex-col gap-3 border-b border-white/10 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div class="flex items-center gap-3">
                    <h3 class="text-lg font-bold text-white">#{{ .ID }} {{ .Tag }}</h3>
                    <span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .StatusClass }}">{{ .Status }}</span>
                  </div>
                  <p class="mt-2 text-sm text-slate-300">Opened {{ .OpenTime }} · Closed {{ .CloseTime }} · Held {{ .DaysHeld }}</p>
                </div>
                <div class="grid grid-cols-2 gap-3 text-sm sm:text-right">
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
</body>
</html>`
