package report

import (
	"fmt"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
)

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
		MaxDrawdownDuration:     fmt.Sprintf("%.1f 小时", analysis.MaxDrawdownDuration),
	}
}

func buildPerformanceMetricCard(name, unit string, snapshot *backtest.PerformanceSnapshot) performanceMetricCardView {
	view := performanceMetricCardView{
		Name:      name,
		ValueUnit: fallbackText(strings.TrimSpace(unit), "-"),
	}
	if snapshot == nil {
		return view
	}
	view.AnnualizedReturn = pct(snapshot.AnnualizedReturn)
	view.AnnualizedVolatility = pct(snapshot.AnnualizedVolatility)
	view.MaxDrawdown = pct(snapshot.MaxDrawdown)
	view.SharpeRatio = decimal(snapshot.SharpeRatio)
	view.CalmarRatio = ratio(snapshot.CalmarRatio)
	return view
}

func buildTradeRows(trades []backtest.Trade, unit string) []tradeRowView {
	rows := make([]tradeRowView, 0, len(trades))
	for _, trade := range trades {
		side := trade.Side.String()
		rows = append(rows, tradeRowView{
			Timestamp:  formatDateTime(trade.Timestamp),
			Security:   fmt.Sprintf("%s / %s / %s", trade.Security.Market, trade.Security.Symbol, trade.Security.Interval),
			Side:       translateSide(side),
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

func buildNotes(result *backtest.Result, candleFallback bool) []string {
	notes := make([]string, 0, 6)
	if len(result.Trades) == 0 && len(result.SpreadPositions) > 0 {
		notes = append(notes, "该策略通过价差跟踪腿执行，因此原始经纪商成交表为空，但价差生命周期事件仍会标注在价格图上。")
	}
	if result.EquityAnalysis != nil && result.EquityAnalysis.MaxDrawdownDurationBars > 0 {
		notes = append(notes, fmt.Sprintf("最长回撤持续了 %d 根 bar（%.1f 小时）。", result.EquityAnalysis.MaxDrawdownDurationBars, result.EquityAnalysis.MaxDrawdownDuration))
	}
	if result.TradeOverview != nil && result.TradeOverview.RoundTrips == 0 && len(result.SpreadPositions) == 0 {
		notes = append(notes, "本次运行未记录到已完成的往返交易。")
	}
	if reportUsesCompatFallback(result) {
		notes = append(notes, "该报告使用了兼容性回退市场数据源。价格 bar 仍可使用，但部分辅助字段可能缺失或为重建值。")
	}
	if !reportHasUsableVolume(result) {
		notes = append(notes, "本次运行没有可用的原生成交量序列。基于成交量的指标和悬停窗口中的成交量字段将显示为 n/a；若使用报告图表覆盖，请确认覆盖源同时暴露 volume 或 tick_count。")
	}
	if candleFallback {
		notes = append(notes, "由于导出结果中缺少完整 OHLC 序列，参考价格图已根据收盘价数据重建。")
	}
	return notes
}

func reportUsesCompatFallback(result *backtest.Result) bool {
	if result == nil || result.Series == nil {
		return false
	}
	return seriesHasTruthyFlag(result.Series["compat_fallback"])
}

func reportHasUsableVolume(result *backtest.Result) bool {
	if result == nil || result.Series == nil {
		return false
	}
	if seriesHasFiniteValue(result.Series["volume"]) {
		return true
	}
	if seriesHasFiniteValue(result.Series["tick_count"]) {
		return true
	}
	return seriesHasFiniteValue(result.Series["vol_norm"])
}

func seriesHasTruthyFlag(values []float64) bool {
	for _, value := range values {
		if chartValueValid(value) && value != 0 {
			return true
		}
	}
	return false
}

func seriesHasFiniteValue(values []float64) bool {
	for _, value := range values {
		if chartValueValid(value) {
			return true
		}
	}
	return false
}
