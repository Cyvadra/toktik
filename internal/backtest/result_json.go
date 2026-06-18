package backtest

import (
	"math"
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
	CapitalMode      string   `json:"capital_mode,omitempty"`
	CapitalProfile   string   `json:"capital_profile,omitempty"`
	CapitalNote      string   `json:"capital_note,omitempty"`
	TotalReturn      *float64 `json:"total_return"`
	AnnualizedReturn *float64 `json:"annualized_return"`
	SharpeRatio      *float64 `json:"sharpe_ratio"`
	CalmarRatio      *float64 `json:"calmar_ratio"`
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
	Warnings        []BacktestWarning                `json:"warnings,omitempty"`
	TradeOverview   *tradeOverviewJSONExport         `json:"trade_overview,omitempty"`
	EquityAnalysis  *equityAnalysisJSONExport        `json:"equity_analysis,omitempty"`
	SpreadPositions []spreadPositionReportJSONExport `json:"spread_positions,omitempty"`
	SpreadGroups    []SpreadGroupReport              `json:"spread_groups,omitempty"`

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
	ID               int                         `json:"id"`
	Tag              string                      `json:"tag"`
	CloseNote        string                      `json:"close_note,omitempty"`
	Status           string                      `json:"status"`
	OpenTime         time.Time                   `json:"open_time"`
	CloseTriggerTime *time.Time                  `json:"close_trigger_time,omitempty"`
	CloseTime        *time.Time                  `json:"close_time,omitempty"`
	DaysHeld         *float64                    `json:"days_held"`
	NetPremium       *float64                    `json:"net_premium"`
	RealizedPnL      *float64                    `json:"realized_pnl"`
	GroupID          int                         `json:"group_id,omitempty"`
	Legs             []spreadLegReportJSONExport `json:"legs"`
}

type spreadLegReportJSONExport struct {
	Symbol           string     `json:"symbol"`
	Side             string     `json:"side"`
	Type             OptionType `json:"type"`
	StrikePrice      *float64   `json:"strike_price"`
	Expiration       time.Time  `json:"expiration"`
	EntryDelta       *float64   `json:"entry_delta,omitempty"`
	Delta            *float64   `json:"delta"`
	Qty              *float64   `json:"qty"`
	EntryPrice       *float64   `json:"entry_price"`
	EntryTime        time.Time  `json:"entry_time"`
	Closed           bool       `json:"closed"`
	ClosePrice       *float64   `json:"close_price,omitempty"`
	CloseTriggerTime *time.Time `json:"close_trigger_time,omitempty"`
	CloseTime        *time.Time `json:"close_time,omitempty"`
	CloseDelta       *float64   `json:"close_delta,omitempty"`
	CloseReason      string     `json:"close_reason,omitempty"`
	RealizedPnL      *float64   `json:"realized_pnl"`
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

func (r *Result) jsonExport() resultJSONExport {
	out := resultJSONExport{
		StrategyName:     r.StrategyName,
		StartTime:        r.StartTime,
		EndTime:          r.EndTime,
		BarsCount:        r.BarsCount,
		InitialCapital:   jsonFloat(r.InitialCapital),
		FinalEquity:      jsonFloat(r.FinalEquity),
		AccountUnit:      r.AccountUnit,
		CapitalMode:      r.CapitalMode,
		CapitalProfile:   r.CapitalProfile,
		CapitalNote:      r.CapitalNote,
		TotalReturn:      jsonFloat(r.TotalReturn),
		AnnualizedReturn: jsonFloat(r.AnnualizedReturn),
		SharpeRatio:      jsonFloat(r.SharpeRatio),
		CalmarRatio:      jsonFloat(r.CalmarRatio),
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

	if len(r.Warnings) > 0 {
		out.Warnings = append([]BacktestWarning(nil), r.Warnings...)
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
					Symbol:           leg.Symbol,
					Side:             leg.Side,
					Type:             leg.Type,
					StrikePrice:      jsonFloat(leg.StrikePrice),
					Expiration:       leg.Expiration,
					EntryDelta:       leg.EntryDelta,
					Delta:            jsonFloat(leg.Delta),
					Qty:              jsonFloat(leg.Qty),
					EntryPrice:       jsonFloat(leg.EntryPrice),
					EntryTime:        leg.EntryTime,
					Closed:           leg.Closed,
					ClosePrice:       jsonFloat(leg.ClosePrice),
					CloseTriggerTime: leg.CloseTriggerTime,
					CloseTime:        leg.CloseTime,
					CloseDelta:       leg.CloseDelta,
					CloseReason:      leg.CloseReason,
					RealizedPnL:      jsonFloat(leg.RealizedPnL),
				}
			}

			out.SpreadPositions[i] = spreadPositionReportJSONExport{
				ID:               position.ID,
				Tag:              position.Tag,
				CloseNote:        position.CloseNote,
				Status:           position.Status,
				OpenTime:         position.OpenTime,
				CloseTriggerTime: position.CloseTriggerTime,
				CloseTime:        position.CloseTime,
				DaysHeld:         jsonFloat(position.DaysHeld),
				NetPremium:       jsonFloat(position.NetPremium),
				RealizedPnL:      jsonFloat(position.RealizedPnL),
				GroupID:          position.GroupID,
				Legs:             legs,
			}
		}
	}

	if len(r.SpreadGroups) > 0 {
		out.SpreadGroups = append([]SpreadGroupReport(nil), r.SpreadGroups...)
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
