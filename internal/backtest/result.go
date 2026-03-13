package backtest

import (
	"encoding/json"
	"math"
	"os"
	"time"
)

// Result holds the output of a completed backtest.
type Result struct {
	StrategyName string    `json:"strategy_name"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	BarsCount    int       `json:"bars_count"`

	// Performance
	InitialCapital   float64 `json:"initial_capital"`
	FinalEquity      float64 `json:"final_equity"`
	TotalReturn      float64 `json:"total_return"` // as fraction, e.g. 0.15 = 15%
	AnnualizedReturn float64 `json:"annualized_return"`
	SharpeRatio      float64 `json:"sharpe_ratio"`
	MaxDrawdown      float64 `json:"max_drawdown"`       // as fraction of peak
	MaxDrawdownStart int     `json:"max_drawdown_start"` // bar index
	MaxDrawdownEnd   int     `json:"max_drawdown_end"`   // bar index

	// Trade statistics
	TotalTrades   int     `json:"total_trades"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
	WinRate       float64 `json:"win_rate"`
	ProfitFactor  float64 `json:"profit_factor"`
	AvgWin        float64 `json:"avg_win"`
	AvgLoss       float64 `json:"avg_loss"`
	TotalFees     float64 `json:"total_fees"`

	// Series data (for visualization / further analysis)
	Trades      []Trade              `json:"trades"`
	EquityCurve []float64            `json:"equity_curve"`
	Timestamps  []time.Time          `json:"timestamps"`
	Series      map[string][]float64 `json:"series,omitempty"` // indicator series

	// Options spread summary
	SpreadSummary *SpreadSummary `json:"spread_summary,omitempty"`
}

// SpreadSummary aggregates metrics across all spread positions in a backtest.
type SpreadSummary struct {
	TotalSpreads   int     `json:"total_spreads"`
	ClosedSpreads  int     `json:"closed_spreads"`
	OpenSpreads    int     `json:"open_spreads"`
	TotalPnL       float64 `json:"total_pnl"`
	WinningSpreads int     `json:"winning_spreads"`
	LosingSpreads  int     `json:"losing_spreads"`
	WinRate        float64 `json:"win_rate"`
}

// ExportJSON writes the result to a JSON file.
func (r *Result) ExportJSON(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Summary returns a compact text summary of the result.
func (r *Result) Summary() string {
	return "Strategy:          " + r.StrategyName + "\n" +
		"Period:            " + r.StartTime.Format("2006-01-02") + " to " + r.EndTime.Format("2006-01-02") + "\n" +
		"Bars:              " + itoa(r.BarsCount) + "\n" +
		"Initial Capital:   " + ftoa(r.InitialCapital) + "\n" +
		"Final Equity:      " + ftoa(r.FinalEquity) + "\n" +
		"Total Return:      " + pct(r.TotalReturn) + "\n" +
		"Annualized Return: " + pct(r.AnnualizedReturn) + "\n" +
		"Sharpe Ratio:      " + ftoa(r.SharpeRatio) + "\n" +
		"Max Drawdown:      " + pct(r.MaxDrawdown) + "\n" +
		"Total Trades:      " + itoa(r.TotalTrades) + "\n" +
		"Win Rate:          " + pct(r.WinRate) + "\n" +
		"Profit Factor:     " + ftoa(r.ProfitFactor) + "\n" +
		"Avg Win:           " + ftoa(r.AvgWin) + "\n" +
		"Avg Loss:          " + ftoa(r.AvgLoss) + "\n" +
		"Total Fees:        " + ftoa(r.TotalFees)
}

func computeResult(
	strategyName string,
	trades []Trade,
	equityCurve []float64,
	timestamps []time.Time,
	initialCapital float64,
	series map[string][]float64,
) *Result {
	n := len(equityCurve)
	r := &Result{
		StrategyName:   strategyName,
		BarsCount:      n,
		InitialCapital: initialCapital,
		Trades:         trades,
		EquityCurve:    equityCurve,
		Timestamps:     timestamps,
		Series:         series,
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
	peak := initialCapital
	ddStart := 0
	maxDD := 0.0
	maxDDStart, maxDDEnd := 0, 0
	for i, eq := range equityCurve {
		if eq > peak {
			peak = eq
			ddStart = i
		}
		if peak > 0 {
			dd := (peak - eq) / peak
			if dd > maxDD {
				maxDD = dd
				maxDDStart = ddStart
				maxDDEnd = i
			}
		}
	}
	r.MaxDrawdown = maxDD
	r.MaxDrawdownStart = maxDDStart
	r.MaxDrawdownEnd = maxDDEnd

	// Sharpe ratio (daily returns assumed)
	if n > 1 {
		returns := make([]float64, n-1)
		for i := 1; i < n; i++ {
			if equityCurve[i-1] != 0 {
				returns[i-1] = (equityCurve[i] - equityCurve[i-1]) / equityCurve[i-1]
			}
		}
		mean, stddev := meanStd(returns)
		if stddev > 0 {
			// Annualize assuming ~252 trading days
			r.SharpeRatio = (mean / stddev) * math.Sqrt(252)
		}
	}

	// Trade statistics
	r.TotalTrades = len(trades)
	if r.TotalTrades > 0 {
		// Group trades into round trips to compute win/loss
		// Simple approach: pair consecutive buy/sell on same security
		pnlByTrade := computeTradePnL(trades)
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

	return r
}

// computeTradePnL pairs entries and exits to compute per-round-trip PnL.
func computeTradePnL(trades []Trade) []float64 {
	type openEntry struct {
		side  Side
		qty   float64
		price float64
	}

	// Track pending entries per security
	pending := make(map[SecurityRef]*openEntry)
	var pnls []float64

	for _, t := range trades {
		entry, hasPending := pending[t.Security]
		if !hasPending {
			// New entry
			pending[t.Security] = &openEntry{side: t.Side, qty: t.Qty, price: t.FillPrice}
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
			pnl -= t.Commission
			pnls = append(pnls, pnl)

			remaining := entry.qty - closeQty
			if remaining > 0 {
				entry.qty = remaining
			} else {
				excess := t.Qty - closeQty
				if excess > 0 {
					pending[t.Security] = &openEntry{side: t.Side, qty: excess, price: t.FillPrice}
				} else {
					delete(pending, t.Security)
				}
			}
		} else {
			// Adding to position
			totalQty := entry.qty + t.Qty
			entry.price = (entry.price*entry.qty + t.FillPrice*t.Qty) / totalQty
			entry.qty = totalQty
		}
	}

	return pnls
}

func meanStd(data []float64) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))

	sumSq := 0.0
	for _, v := range data {
		d := v - mean
		sumSq += d * d
	}
	variance := sumSq / float64(len(data))
	return mean, math.Sqrt(variance)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 12)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func ftoa(f float64) string {
	if math.IsInf(f, 0) {
		return "∞"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	// Simple formatting: 2 decimal places
	neg := ""
	if f < 0 {
		neg = "-"
		f = -f
	}
	whole := int(f)
	frac := int((f-float64(whole))*100 + 0.5)
	if frac >= 100 {
		whole++
		frac -= 100
	}
	return neg + itoa(whole) + "." + string([]byte{byte('0' + frac/10), byte('0' + frac%10)})
}

func pct(f float64) string {
	return ftoa(f*100) + "%"
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
