package report

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// OverviewItem represents one strategy result entry in a multi-strategy overview report.
type OverviewItem struct {
	Result   *backtest.Result
	HTMLPath string
}

type overviewReportView struct {
	Title          string
	Asset          string
	Interval       string
	Period         string
	GeneratedAt    string
	StrategyCount  int
	BestReturn     string
	BestSharpe     string
	LowestDrawdown string
	TotalSpreads   string
	ComparisonPath string
	Strategies     []overviewStrategyView
}

type overviewStrategyView struct {
	Name           string
	HTMLLink       string
	FinalEquity    string
	NetPnL         string
	TotalReturn    string
	Annualized     string
	Sharpe         string
	Drawdown       string
	Trades         string
	Spreads        string
	SpreadWinRate  string
	SpreadPnL      string
	BadgeClass     string
	ReturnClass    string
	DrawdownClass  string
	NormalizedPath string
}

// WriteBacktestOverviewHTML renders an overview page for multiple strategy results.
func WriteBacktestOverviewHTML(path string, items []OverviewItem, meta HTMLMeta) error {
	if len(items) == 0 {
		return nil
	}
	view := buildOverviewView(path, items, meta)
	tmpl, err := template.New("backtest-overview").Parse(overviewHTMLTemplate)
	if err != nil {
		return fmt.Errorf("parse overview template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return fmt.Errorf("render overview template: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create overview directory: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write overview html: %w", err)
	}
	return nil
}

func buildOverviewView(outputPath string, items []OverviewItem, meta HTMLMeta) overviewReportView {
	if meta.GeneratedAt.IsZero() {
		meta.GeneratedAt = nowLocal()
	}

	view := overviewReportView{
		Title:         fmt.Sprintf("%s %s Multi-Strategy Overview", meta.Asset, meta.Interval),
		Asset:         meta.Asset,
		Interval:      meta.Interval,
		GeneratedAt:   meta.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		StrategyCount: len(items),
		Strategies:    make([]overviewStrategyView, 0, len(items)),
	}

	periodStart := items[0].Result.StartTime
	periodEnd := items[0].Result.EndTime
	bestReturn := items[0].Result
	bestSharpe := items[0].Result
	lowestDrawdown := items[0].Result
	totalSpreads := 0

	sorted := append([]OverviewItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Result.TotalReturn > sorted[j].Result.TotalReturn
	})

	for _, item := range sorted {
		result := item.Result
		if result.StartTime.Before(periodStart) {
			periodStart = result.StartTime
		}
		if result.EndTime.After(periodEnd) {
			periodEnd = result.EndTime
		}
		if result.TotalReturn > bestReturn.TotalReturn {
			bestReturn = result
		}
		if result.SharpeRatio > bestSharpe.SharpeRatio {
			bestSharpe = result
		}
		if result.MaxDrawdown < lowestDrawdown.MaxDrawdown {
			lowestDrawdown = result
		}
		if result.SpreadSummary != nil {
			totalSpreads += result.SpreadSummary.TotalSpreads
		}

		spreadWinRate := "-"
		spreadPnL := "-"
		spreads := "0"
		if result.SpreadSummary != nil {
			spreadWinRate = pct(result.SpreadSummary.WinRate)
			spreadPnL = signedAmount(result.SpreadSummary.TotalPnL, result.AccountUnit)
			spreads = integer(result.SpreadSummary.TotalSpreads)
		}

		link := item.HTMLPath
		if rel, err := filepath.Rel(filepath.Dir(outputPath), item.HTMLPath); err == nil {
			link = filepath.ToSlash(rel)
		}

		view.Strategies = append(view.Strategies, overviewStrategyView{
			Name:           result.StrategyName,
			HTMLLink:       link,
			FinalEquity:    amount(result.FinalEquity, result.AccountUnit),
			NetPnL:         signedAmount(result.FinalEquity-result.InitialCapital, result.AccountUnit),
			TotalReturn:    pct(result.TotalReturn),
			Annualized:     pct(result.AnnualizedReturn),
			Sharpe:         decimal(result.SharpeRatio),
			Drawdown:       pct(result.MaxDrawdown),
			Trades:         integer(len(result.Trades)),
			Spreads:        spreads,
			SpreadWinRate:  spreadWinRate,
			SpreadPnL:      spreadPnL,
			BadgeClass:     overviewBadgeClass(result.TotalReturn),
			ReturnClass:    returnClass(result.TotalReturn),
			DrawdownClass:  drawdownClass(result.MaxDrawdown),
			NormalizedPath: linePath(normalizedCurve(result.EquityCurve, result.InitialCapital), 720, 160),
		})
	}

	view.Period = fmt.Sprintf("%s to %s", formatDate(periodStart), formatDate(periodEnd))
	view.BestReturn = fmt.Sprintf("%s · %s", bestReturn.StrategyName, pct(bestReturn.TotalReturn))
	view.BestSharpe = fmt.Sprintf("%s · %s", bestSharpe.StrategyName, decimal(bestSharpe.SharpeRatio))
	view.LowestDrawdown = fmt.Sprintf("%s · %s", lowestDrawdown.StrategyName, pct(lowestDrawdown.MaxDrawdown))
	view.TotalSpreads = integer(totalSpreads)
	return view
}

func normalizedCurve(equity []float64, initial float64) []float64 {
	if len(equity) == 0 {
		return nil
	}
	base := initial
	if base == 0 {
		base = equity[0]
	}
	if base == 0 {
		base = 1
	}
	out := make([]float64, len(equity))
	for i, value := range equity {
		out[i] = value / base
	}
	return out
}

func overviewBadgeClass(totalReturn float64) string {
	if totalReturn > 0 {
		return "bg-emerald-500/15 text-emerald-200 ring-emerald-400/40"
	}
	if totalReturn < 0 {
		return "bg-rose-500/15 text-rose-200 ring-rose-400/40"
	}
	return "bg-slate-500/15 text-slate-200 ring-slate-400/40"
}

func returnClass(totalReturn float64) string {
	if totalReturn > 0 {
		return "text-emerald-300"
	}
	if totalReturn < 0 {
		return "text-rose-300"
	}
	return "text-slate-200"
}

func drawdownClass(drawdown float64) string {
	if drawdown < 0.02 {
		return "text-emerald-300"
	}
	if drawdown < 0.05 {
		return "text-amber-300"
	}
	return "text-rose-300"
}

func nowLocal() time.Time {
	return time.Now()
}

const overviewHTMLTemplate = `<!DOCTYPE html>
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
            canvas: '#0a1117',
            panel: '#101a22',
            tide: '#56d8cb',
            ember: '#f2a63a'
          },
          fontFamily: {
            sans: ['Manrope', 'system-ui', 'sans-serif'],
            mono: ['IBM Plex Mono', 'monospace']
          },
          boxShadow: {
            report: '0 28px 90px rgba(0,0,0,.34)'
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
        radial-gradient(circle at top left, rgba(86,216,203,.18), transparent 28%),
        radial-gradient(circle at top right, rgba(242,166,58,.14), transparent 24%),
        linear-gradient(180deg, #070d12 0%, #0b1319 40%, #111c24 100%);
    }
  </style>
</head>
<body class="min-h-screen bg-canvas text-slate-100 font-sans">
  <main class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
    <section class="overflow-hidden rounded-[2rem] border border-white/10 bg-canvas/80 shadow-report">
      <div class="border-b border-white/10 px-6 py-8 sm:px-10">
        <div class="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
          <div class="max-w-3xl">
            <p class="font-mono text-xs uppercase tracking-[0.3em] text-tide/80">Strategy Overview</p>
            <h1 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-5xl">{{ .Asset }} {{ .Interval }} Multi-Run Dashboard</h1>
            <p class="mt-4 text-sm leading-6 text-slate-300 sm:text-base">{{ .Period }}. This overview compares strategy outcomes side by side and links to each detailed HTML report.</p>
          </div>
          <div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-2">
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Generated</div>
              <div class="mt-2 text-base text-white">{{ .GeneratedAt }}</div>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Strategies</div>
              <div class="mt-2 text-base text-white">{{ .StrategyCount }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="grid gap-4 px-6 py-6 md:grid-cols-2 xl:grid-cols-4 sm:px-10">
        <article class="rounded-3xl border border-white/10 bg-panel/70 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Best Return</div>
          <div class="mt-3 text-lg font-bold text-white">{{ .BestReturn }}</div>
        </article>
        <article class="rounded-3xl border border-white/10 bg-panel/70 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Best Sharpe</div>
          <div class="mt-3 text-lg font-bold text-white">{{ .BestSharpe }}</div>
        </article>
        <article class="rounded-3xl border border-white/10 bg-panel/70 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Lowest Drawdown</div>
          <div class="mt-3 text-lg font-bold text-white">{{ .LowestDrawdown }}</div>
        </article>
        <article class="rounded-3xl border border-white/10 bg-panel/70 p-5">
          <div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Total Spreads</div>
          <div class="mt-3 text-lg font-bold text-white">{{ .TotalSpreads }}</div>
        </article>
      </div>

      <div class="px-6 pb-6 sm:px-10">
        <section class="rounded-3xl border border-white/10 bg-panel/60 p-5">
          <h2 class="text-xl font-bold text-white">Normalized Equity Comparison</h2>
          <p class="mt-1 text-sm text-slate-300">Each sparkline starts at 1.00 so you can compare trajectory independent of starting capital.</p>
          <div class="mt-5 grid gap-4 lg:grid-cols-2">
            {{ range .Strategies }}
            <article class="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
              <div class="flex items-center justify-between gap-3">
                <div>
                  <div class="text-lg font-bold text-white">{{ .Name }}</div>
                  <div class="mt-1 text-sm {{ .ReturnClass }}">Return {{ .TotalReturn }}</div>
                </div>
                <span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .BadgeClass }}">{{ .Drawdown }}</span>
              </div>
              <div class="mt-4 overflow-hidden rounded-2xl border border-white/10 bg-[#081118] p-3">
                <svg viewBox="0 0 720 160" class="h-28 w-full">
                  <path d="{{ .NormalizedPath }}" fill="none" stroke="#56d8cb" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </div>
            </article>
            {{ end }}
          </div>
        </section>
      </div>

      <div class="px-6 pb-10 sm:px-10">
        <section class="overflow-hidden rounded-3xl border border-white/10 bg-panel/40">
          <div class="border-b border-white/10 px-5 py-4">
            <h2 class="text-xl font-bold text-white">Strategy Matrix</h2>
          </div>
          <div class="overflow-auto">
            <table class="min-w-full divide-y divide-white/10 text-sm">
              <thead class="bg-canvas/90 text-left text-slate-400">
                <tr>
                  <th class="px-4 py-3 font-medium">Strategy</th>
                  <th class="px-4 py-3 font-medium">Final Equity</th>
                  <th class="px-4 py-3 font-medium">Net PnL</th>
                  <th class="px-4 py-3 font-medium">Return</th>
                  <th class="px-4 py-3 font-medium">Annualized</th>
                  <th class="px-4 py-3 font-medium">Sharpe</th>
                  <th class="px-4 py-3 font-medium">Drawdown</th>
                  <th class="px-4 py-3 font-medium">Fills</th>
                  <th class="px-4 py-3 font-medium">Spreads</th>
                  <th class="px-4 py-3 font-medium">Spread Win Rate</th>
                  <th class="px-4 py-3 font-medium">Spread PnL</th>
                  <th class="px-4 py-3 font-medium">Report</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-white/5 bg-white/[0.02]">
                {{ range .Strategies }}
                <tr>
                  <td class="px-4 py-3 font-semibold text-white">{{ .Name }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .FinalEquity }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .NetPnL }}</td>
                  <td class="px-4 py-3 font-mono {{ .ReturnClass }}">{{ .TotalReturn }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .Annualized }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .Sharpe }}</td>
                  <td class="px-4 py-3 font-mono {{ .DrawdownClass }}">{{ .Drawdown }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .Trades }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .Spreads }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .SpreadWinRate }}</td>
                  <td class="px-4 py-3 font-mono text-slate-200">{{ .SpreadPnL }}</td>
                  <td class="px-4 py-3"><a class="inline-flex rounded-full border border-tide/30 bg-tide/10 px-3 py-1 font-mono text-xs text-tide hover:bg-tide/20" href="{{ .HTMLLink }}">Open</a></td>
                </tr>
                {{ end }}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </section>
  </main>
</body>
</html>`
