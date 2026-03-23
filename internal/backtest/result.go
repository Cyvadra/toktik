package backtest

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

type resultJSONExport struct {
	StrategyName string    `json:"strategy_name"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	BarsCount    int       `json:"bars_count"`

	InitialCapital   *float64 `json:"initial_capital"`
	FinalEquity      *float64 `json:"final_equity"`
	AccountUnit      string   `json:"account_unit,omitempty"`
	TotalReturn      *float64 `json:"total_return"`
	AnnualizedReturn *float64 `json:"annualized_return"`
	SharpeRatio      *float64 `json:"sharpe_ratio"`
	MaxDrawdown      *float64 `json:"max_drawdown"`
	MaxDrawdownStart int      `json:"max_drawdown_start"`
	MaxDrawdownEnd   int      `json:"max_drawdown_end"`

	TotalTrades   int      `json:"total_trades"`
	WinningTrades int      `json:"winning_trades"`
	LosingTrades  int      `json:"losing_trades"`
	WinRate       *float64 `json:"win_rate"`
	ProfitFactor  *float64 `json:"profit_factor"`
	AvgWin        *float64 `json:"avg_win"`
	AvgLoss       *float64 `json:"avg_loss"`
	TotalFees     *float64 `json:"total_fees"`

	Trades          []tradeJSONExport                `json:"trades"`
	EquityCurve     []*float64                       `json:"equity_curve"`
	Timestamps      []time.Time                      `json:"timestamps"`
	Series          map[string][]*float64            `json:"series,omitempty"`
	ReportColumns   []ReportColumn                   `json:"report_columns,omitempty"`
	TradeOverview   *tradeOverviewJSONExport         `json:"trade_overview,omitempty"`
	EquityAnalysis  *equityAnalysisJSONExport        `json:"equity_analysis,omitempty"`
	SpreadPositions []spreadPositionReportJSONExport `json:"spread_positions,omitempty"`

	SpreadSummary *spreadSummaryJSONExport `json:"spread_summary,omitempty"`
}

type tradeJSONExport struct {
	ID         int         `json:"id"`
	OrderID    int         `json:"order_id"`
	Security   SecurityRef `json:"security"`
	Side       Side        `json:"side"`
	Note       string      `json:"note"`
	Qty        *float64    `json:"qty"`
	FillPrice  *float64    `json:"fill_price"`
	Commission *float64    `json:"commission"`
	Slippage   *float64    `json:"slippage"`
	BarIndex   int         `json:"bar_index"`
	Timestamp  time.Time   `json:"timestamp"`
}

type tradeOverviewJSONExport struct {
	RawFills             int      `json:"raw_fills"`
	RoundTrips           int      `json:"round_trips"`
	LongFills            int      `json:"long_fills"`
	ShortFills           int      `json:"short_fills"`
	TotalNotional        *float64 `json:"total_notional"`
	GrossProfit          *float64 `json:"gross_profit"`
	GrossLoss            *float64 `json:"gross_loss"`
	NetPnL               *float64 `json:"net_pnl"`
	AvgPnLPerRoundTrip   *float64 `json:"avg_pnl_per_round_trip"`
	AvgCommissionPerFill *float64 `json:"avg_commission_per_fill"`
}

type equityAnalysisJSONExport struct {
	PeakEquity              *float64  `json:"peak_equity"`
	PeakTime                time.Time `json:"peak_time"`
	LowestEquity            *float64  `json:"lowest_equity"`
	LowestTime              time.Time `json:"lowest_time"`
	BestBarReturn           *float64  `json:"best_bar_return"`
	WorstBarReturn          *float64  `json:"worst_bar_return"`
	BarReturnVolatility     *float64  `json:"bar_return_volatility"`
	PositiveBars            int       `json:"positive_bars"`
	NegativeBars            int       `json:"negative_bars"`
	FlatBars                int       `json:"flat_bars"`
	MaxDrawdownDurationBars int       `json:"max_drawdown_duration_bars"`
	MaxDrawdownDuration     *float64  `json:"max_drawdown_duration_hours"`
}

type spreadPositionReportJSONExport struct {
	ID          int                         `json:"id"`
	Tag         string                      `json:"tag"`
	Status      string                      `json:"status"`
	OpenTime    time.Time                   `json:"open_time"`
	CloseTime   *time.Time                  `json:"close_time,omitempty"`
	DaysHeld    *float64                    `json:"days_held"`
	NetPremium  *float64                    `json:"net_premium"`
	RealizedPnL *float64                    `json:"realized_pnl"`
	Legs        []spreadLegReportJSONExport `json:"legs"`
}

type spreadLegReportJSONExport struct {
	Symbol      string     `json:"symbol"`
	Side        string     `json:"side"`
	Type        OptionType `json:"type"`
	StrikePrice *float64   `json:"strike_price"`
	Expiration  time.Time  `json:"expiration"`
	Delta       *float64   `json:"delta"`
	Qty         *float64   `json:"qty"`
	EntryPrice  *float64   `json:"entry_price"`
	EntryTime   time.Time  `json:"entry_time"`
	Closed      bool       `json:"closed"`
	ClosePrice  *float64   `json:"close_price,omitempty"`
	CloseTime   *time.Time `json:"close_time,omitempty"`
	CloseReason string     `json:"close_reason,omitempty"`
	RealizedPnL *float64   `json:"realized_pnl"`
}

type spreadSummaryJSONExport struct {
	TotalSpreads   int      `json:"total_spreads"`
	ClosedSpreads  int      `json:"closed_spreads"`
	OpenSpreads    int      `json:"open_spreads"`
	TotalPnL       *float64 `json:"total_pnl"`
	WinningSpreads int      `json:"winning_spreads"`
	LosingSpreads  int      `json:"losing_spreads"`
	WinRate        *float64 `json:"win_rate"`
}

// Result holds the output of a completed backtest.
type Result struct {
	StrategyName string    `json:"strategy_name"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	BarsCount    int       `json:"bars_count"`

	// Performance
	InitialCapital   float64 `json:"initial_capital"`
	FinalEquity      float64 `json:"final_equity"`
	AccountUnit      string  `json:"account_unit,omitempty"`
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
	Trades          []Trade                `json:"trades"`
	EquityCurve     []float64              `json:"equity_curve"`
	Timestamps      []time.Time            `json:"timestamps"`
	Series          map[string][]float64   `json:"series,omitempty"` // indicator series
	ReportColumns   []ReportColumn         `json:"report_columns,omitempty"`
	TradeOverview   *TradeOverview         `json:"trade_overview,omitempty"`
	EquityAnalysis  *EquityAnalysis        `json:"equity_analysis,omitempty"`
	SpreadPositions []SpreadPositionReport `json:"spread_positions,omitempty"`

	// Options spread summary
	SpreadSummary *SpreadSummary `json:"spread_summary,omitempty"`
}

// TradeOverview aggregates raw fill and round-trip level trade metrics.
type TradeOverview struct {
	RawFills             int     `json:"raw_fills"`
	RoundTrips           int     `json:"round_trips"`
	LongFills            int     `json:"long_fills"`
	ShortFills           int     `json:"short_fills"`
	TotalNotional        float64 `json:"total_notional"`
	GrossProfit          float64 `json:"gross_profit"`
	GrossLoss            float64 `json:"gross_loss"`
	NetPnL               float64 `json:"net_pnl"`
	AvgPnLPerRoundTrip   float64 `json:"avg_pnl_per_round_trip"`
	AvgCommissionPerFill float64 `json:"avg_commission_per_fill"`
}

// EquityAnalysis captures higher-level diagnostics on the equity curve.
type EquityAnalysis struct {
	PeakEquity              float64   `json:"peak_equity"`
	PeakTime                time.Time `json:"peak_time"`
	LowestEquity            float64   `json:"lowest_equity"`
	LowestTime              time.Time `json:"lowest_time"`
	BestBarReturn           float64   `json:"best_bar_return"`
	WorstBarReturn          float64   `json:"worst_bar_return"`
	BarReturnVolatility     float64   `json:"bar_return_volatility"`
	PositiveBars            int       `json:"positive_bars"`
	NegativeBars            int       `json:"negative_bars"`
	FlatBars                int       `json:"flat_bars"`
	MaxDrawdownDurationBars int       `json:"max_drawdown_duration_bars"`
	MaxDrawdownDuration     float64   `json:"max_drawdown_duration_hours"`
}

// SpreadPositionReport is a report-friendly snapshot of a multi-leg options spread.
type SpreadPositionReport struct {
	ID          int               `json:"id"`
	Tag         string            `json:"tag"`
	Status      string            `json:"status"`
	OpenTime    time.Time         `json:"open_time"`
	CloseTime   *time.Time        `json:"close_time,omitempty"`
	DaysHeld    float64           `json:"days_held"`
	NetPremium  float64           `json:"net_premium"`
	RealizedPnL float64           `json:"realized_pnl"`
	Legs        []SpreadLegReport `json:"legs"`
}

// SpreadLegReport is a report-friendly snapshot of an individual spread leg.
type SpreadLegReport struct {
	Symbol      string     `json:"symbol"`
	Side        string     `json:"side"`
	Type        OptionType `json:"type"`
	StrikePrice float64    `json:"strike_price"`
	Expiration  time.Time  `json:"expiration"`
	Delta       float64    `json:"delta"`
	Qty         float64    `json:"qty"`
	EntryPrice  float64    `json:"entry_price"`
	EntryTime   time.Time  `json:"entry_time"`
	Closed      bool       `json:"closed"`
	ClosePrice  float64    `json:"close_price,omitempty"`
	CloseTime   *time.Time `json:"close_time,omitempty"`
	CloseReason string     `json:"close_reason,omitempty"`
	RealizedPnL float64    `json:"realized_pnl"`
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
	return enc.Encode(r.jsonExport())
}

func (r *Result) jsonExport() resultJSONExport {
	out := resultJSONExport{
		StrategyName:     r.StrategyName,
		StartTime:        r.StartTime,
		EndTime:          r.EndTime,
		BarsCount:        r.BarsCount,
		InitialCapital:   jsonFloat(r.InitialCapital),
		FinalEquity:      jsonFloat(r.FinalEquity),
		AccountUnit:      r.AccountUnit,
		TotalReturn:      jsonFloat(r.TotalReturn),
		AnnualizedReturn: jsonFloat(r.AnnualizedReturn),
		SharpeRatio:      jsonFloat(r.SharpeRatio),
		MaxDrawdown:      jsonFloat(r.MaxDrawdown),
		MaxDrawdownStart: r.MaxDrawdownStart,
		MaxDrawdownEnd:   r.MaxDrawdownEnd,
		TotalTrades:      r.TotalTrades,
		WinningTrades:    r.WinningTrades,
		LosingTrades:     r.LosingTrades,
		WinRate:          jsonFloat(r.WinRate),
		ProfitFactor:     jsonFloat(r.ProfitFactor),
		AvgWin:           jsonFloat(r.AvgWin),
		AvgLoss:          jsonFloat(r.AvgLoss),
		TotalFees:        jsonFloat(r.TotalFees),
		EquityCurve:      jsonFloatSlice(r.EquityCurve),
		Timestamps:       append([]time.Time(nil), r.Timestamps...),
	}

	if len(r.Trades) > 0 {
		out.Trades = make([]tradeJSONExport, len(r.Trades))
		for i, trade := range r.Trades {
			out.Trades[i] = tradeJSONExport{
				ID:         trade.ID,
				OrderID:    trade.OrderID,
				Security:   trade.Security,
				Side:       trade.Side,
				Note:       trade.Note,
				Qty:        jsonFloat(trade.Qty),
				FillPrice:  jsonFloat(trade.FillPrice),
				Commission: jsonFloat(trade.Commission),
				Slippage:   jsonFloat(trade.Slippage),
				BarIndex:   trade.BarIndex,
				Timestamp:  trade.Timestamp,
			}
		}
	}

	if len(r.Series) > 0 {
		out.Series = make(map[string][]*float64, len(r.Series))
		for name, values := range r.Series {
			out.Series[name] = jsonFloatSlice(values)
		}
	}

	if len(r.ReportColumns) > 0 {
		out.ReportColumns = append([]ReportColumn(nil), r.ReportColumns...)
	}

	if r.TradeOverview != nil {
		out.TradeOverview = &tradeOverviewJSONExport{
			RawFills:             r.TradeOverview.RawFills,
			RoundTrips:           r.TradeOverview.RoundTrips,
			LongFills:            r.TradeOverview.LongFills,
			ShortFills:           r.TradeOverview.ShortFills,
			TotalNotional:        jsonFloat(r.TradeOverview.TotalNotional),
			GrossProfit:          jsonFloat(r.TradeOverview.GrossProfit),
			GrossLoss:            jsonFloat(r.TradeOverview.GrossLoss),
			NetPnL:               jsonFloat(r.TradeOverview.NetPnL),
			AvgPnLPerRoundTrip:   jsonFloat(r.TradeOverview.AvgPnLPerRoundTrip),
			AvgCommissionPerFill: jsonFloat(r.TradeOverview.AvgCommissionPerFill),
		}
	}

	if r.EquityAnalysis != nil {
		out.EquityAnalysis = &equityAnalysisJSONExport{
			PeakEquity:              jsonFloat(r.EquityAnalysis.PeakEquity),
			PeakTime:                r.EquityAnalysis.PeakTime,
			LowestEquity:            jsonFloat(r.EquityAnalysis.LowestEquity),
			LowestTime:              r.EquityAnalysis.LowestTime,
			BestBarReturn:           jsonFloat(r.EquityAnalysis.BestBarReturn),
			WorstBarReturn:          jsonFloat(r.EquityAnalysis.WorstBarReturn),
			BarReturnVolatility:     jsonFloat(r.EquityAnalysis.BarReturnVolatility),
			PositiveBars:            r.EquityAnalysis.PositiveBars,
			NegativeBars:            r.EquityAnalysis.NegativeBars,
			FlatBars:                r.EquityAnalysis.FlatBars,
			MaxDrawdownDurationBars: r.EquityAnalysis.MaxDrawdownDurationBars,
			MaxDrawdownDuration:     jsonFloat(r.EquityAnalysis.MaxDrawdownDuration),
		}
	}

	if len(r.SpreadPositions) > 0 {
		out.SpreadPositions = make([]spreadPositionReportJSONExport, len(r.SpreadPositions))
		for i, position := range r.SpreadPositions {
			legs := make([]spreadLegReportJSONExport, len(position.Legs))
			for j, leg := range position.Legs {
				legs[j] = spreadLegReportJSONExport{
					Symbol:      leg.Symbol,
					Side:        leg.Side,
					Type:        leg.Type,
					StrikePrice: jsonFloat(leg.StrikePrice),
					Expiration:  leg.Expiration,
					Delta:       jsonFloat(leg.Delta),
					Qty:         jsonFloat(leg.Qty),
					EntryPrice:  jsonFloat(leg.EntryPrice),
					EntryTime:   leg.EntryTime,
					Closed:      leg.Closed,
					ClosePrice:  jsonFloat(leg.ClosePrice),
					CloseTime:   leg.CloseTime,
					CloseReason: leg.CloseReason,
					RealizedPnL: jsonFloat(leg.RealizedPnL),
				}
			}

			out.SpreadPositions[i] = spreadPositionReportJSONExport{
				ID:          position.ID,
				Tag:         position.Tag,
				Status:      position.Status,
				OpenTime:    position.OpenTime,
				CloseTime:   position.CloseTime,
				DaysHeld:    jsonFloat(position.DaysHeld),
				NetPremium:  jsonFloat(position.NetPremium),
				RealizedPnL: jsonFloat(position.RealizedPnL),
				Legs:        legs,
			}
		}
	}

	if r.SpreadSummary != nil {
		out.SpreadSummary = &spreadSummaryJSONExport{
			TotalSpreads:   r.SpreadSummary.TotalSpreads,
			ClosedSpreads:  r.SpreadSummary.ClosedSpreads,
			OpenSpreads:    r.SpreadSummary.OpenSpreads,
			TotalPnL:       jsonFloat(r.SpreadSummary.TotalPnL),
			WinningSpreads: r.SpreadSummary.WinningSpreads,
			LosingSpreads:  r.SpreadSummary.LosingSpreads,
			WinRate:        jsonFloat(r.SpreadSummary.WinRate),
		}
	}

	return out
}

func jsonFloat(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	out := value
	return &out
}

func jsonFloatSlice(values []float64) []*float64 {
	if len(values) == 0 {
		return nil
	}
	out := make([]*float64, len(values))
	for i, value := range values {
		out[i] = jsonFloat(value)
	}
	return out
}

// Summary returns a compact text summary of the result.
func (r *Result) Summary() string {
	unit := strings.TrimSpace(r.AccountUnit)
	return "Strategy:          " + r.StrategyName + "\n" +
		"Period:            " + r.StartTime.Format("2006-01-02") + " to " + r.EndTime.Format("2006-01-02") + "\n" +
		"Bars:              " + itoa(r.BarsCount) + "\n" +
		"Initial Capital:   " + formatSummaryAmount(r.InitialCapital, unit) + "\n" +
		"Final Equity:      " + formatSummaryAmount(r.FinalEquity, unit) + "\n" +
		"Total Return:      " + pct(r.TotalReturn) + "\n" +
		"Annualized Return: " + pct(r.AnnualizedReturn) + "\n" +
		"Sharpe Ratio:      " + ftoa(r.SharpeRatio) + "\n" +
		"Max Drawdown:      " + pct(r.MaxDrawdown) + "\n" +
		"Total Trades:      " + itoa(r.TotalTrades) + "\n" +
		"Win Rate:          " + pct(r.WinRate) + "\n" +
		"Profit Factor:     " + ftoa(r.ProfitFactor) + "\n" +
		"Avg Win:           " + formatSummaryAmount(r.AvgWin, unit) + "\n" +
		"Avg Loss:          " + formatSummaryAmount(r.AvgLoss, unit) + "\n" +
		"Total Fees:        " + formatSummaryAmount(r.TotalFees, unit)
}

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

	// Sharpe ratio – annualised using the actual bar interval inferred from timestamps
	if n > 1 {
		returns := make([]float64, n-1)
		for i := 1; i < n; i++ {
			if equityCurve[i-1] != 0 {
				returns[i-1] = (equityCurve[i] - equityCurve[i-1]) / equityCurve[i-1]
			}
		}
		mean, stddev := meanStd(returns)
		if stddev > 0 {
			barsPerYear := inferBarsPerYear(timestamps)
			r.SharpeRatio = (mean / stddev) * math.Sqrt(barsPerYear)
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

	r.TradeOverview = computeTradeOverview(trades)
	r.EquityAnalysis = computeEquityAnalysis(equityCurve, timestamps)

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
		})
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func computeTradeOverview(trades []Trade) *TradeOverview {
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

	pnls := computeTradePnL(trades)
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

func computeEquityAnalysis(equityCurve []float64, timestamps []time.Time) *EquityAnalysis {
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

// computeTradePnL pairs entries and exits to compute per-round-trip PnL.
func computeTradePnL(trades []Trade) []float64 {
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

func formatSummaryAmount(value float64, unit string) string {
	if strings.TrimSpace(unit) == "" {
		return ftoa(value)
	}
	if math.IsInf(value, 0) {
		return "∞"
	}
	if math.IsNaN(value) {
		return "NaN"
	}
	return formatFloat(value, 4) + " " + strings.TrimSpace(unit)
}

func formatFloat(value float64, decimals int) string {
	if math.IsInf(value, 0) {
		return "∞"
	}
	if math.IsNaN(value) {
		return "NaN"
	}
	neg := ""
	if value < 0 {
		neg = "-"
		value = -value
	}
	pow10 := 1.0
	for i := 0; i < decimals; i++ {
		pow10 *= 10
	}
	rounded := value*pow10 + 0.5
	whole := int(rounded / pow10)
	frac := int(rounded) - whole*int(pow10)
	fracDigits := make([]byte, decimals)
	for i := decimals - 1; i >= 0; i-- {
		fracDigits[i] = byte('0' + frac%10)
		frac /= 10
	}
	if decimals == 0 {
		return neg + itoa(whole)
	}
	return neg + itoa(whole) + "." + string(fracDigits)
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
