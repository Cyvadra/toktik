package backtest

import (
	"encoding/json"
	"os"
	"strings"
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

	// Options spread group tracking
	SpreadGroups []SpreadGroupReport `json:"spread_groups,omitempty"`

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
	ID               int               `json:"id"`
	Tag              string            `json:"tag"`
	CloseNote        string            `json:"close_note,omitempty"`
	Status           string            `json:"status"`
	OpenTime         time.Time         `json:"open_time"`
	CloseTriggerTime *time.Time        `json:"close_trigger_time,omitempty"`
	CloseTime        *time.Time        `json:"close_time,omitempty"`
	DaysHeld         float64           `json:"days_held"`
	NetPremium       float64           `json:"net_premium"`
	RealizedPnL      float64           `json:"realized_pnl"`
	GroupID          int               `json:"group_id,omitempty"`
	Legs             []SpreadLegReport `json:"legs"`
}

// SpreadGroupReport is a report-friendly snapshot of a spread group (roll chain).
type SpreadGroupReport struct {
	ID          int        `json:"id"`
	Tag         string     `json:"tag"`
	SpreadIDs   []int      `json:"spread_ids"`
	InitAmount  float64    `json:"init_amount"`
	DecayFactor float64    `json:"decay_factor"`
	RollCount   int        `json:"roll_count"`
	TotalPnL    float64    `json:"total_pnl"`
	Status      string     `json:"status"`
	OpenTime    time.Time  `json:"open_time"`
	CloseTime   *time.Time `json:"close_time,omitempty"`
}

// SpreadLegReport is a report-friendly snapshot of an individual spread leg.
type SpreadLegReport struct {
	Symbol           string     `json:"symbol"`
	Side             string     `json:"side"`
	Type             OptionType `json:"type"`
	StrikePrice      float64    `json:"strike_price"`
	Expiration       time.Time  `json:"expiration"`
	Delta            float64    `json:"delta"`
	Qty              float64    `json:"qty"`
	EntryPrice       float64    `json:"entry_price"`
	EntryTime        time.Time  `json:"entry_time"`
	Closed           bool       `json:"closed"`
	ClosePrice       float64    `json:"close_price,omitempty"`
	CloseTriggerTime *time.Time `json:"close_trigger_time,omitempty"`
	CloseTime        *time.Time `json:"close_time,omitempty"`
	CloseDelta       *float64   `json:"close_delta,omitempty"`
	CloseReason      string     `json:"close_reason,omitempty"`
	RealizedPnL      float64    `json:"realized_pnl"`
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
