package report

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strings"
)

func marshalJS(value any) template.JS {
	encoded, err := json.Marshal(value)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(encoded)
}

func roundChartFloat(value float64) float64 {
	if !chartValueValid(value) {
		return value
	}
	const precision = 10000
	return math.Round(value*precision) / precision
}

func buildCompressedChartPayload(view htmlReportView) template.JS {
	payload := map[string]string{
		"underlyingCandles":      compressJSONLiteral(string(view.UnderlyingCandleData)),
		"underlyingVolumeSeries": compressJSONLiteral(string(view.UnderlyingVolumeData)),
		"underlyingMarkers":      compressJSONLiteral(string(view.UnderlyingMarkerData)),
		"hoverColumns":           compressJSONLiteral(string(view.HoverColumnsData)),
		"equitySeries":           compressJSONLiteral(string(view.EquitySeriesData)),
		"settledEquitySeries":    compressJSONLiteral(string(view.SettledEquitySeriesData)),
		"settledFloatingProfit":  compressJSONLiteral(string(view.SettledFloatingProfitData)),
		"settledFloatingLoss":    compressJSONLiteral(string(view.SettledFloatingLossData)),
		"settledExposure":        compressJSONLiteral(string(view.SettledExposureData)),
		"quoteNetValueSeries":    compressJSONLiteral(string(view.QuoteNetValueSeriesData)),
		"dailyQuoteNetValue":     compressJSONLiteral(string(view.DailyQuoteNetValueSeriesData)),
		"dailyBuyHold":           compressJSONLiteral(string(view.DailyBuyHoldSeriesData)),
		"dailyAssetPnL":          compressJSONLiteral(string(view.DailyAssetPnLSeriesData)),
		"buyHoldSeries":          compressJSONLiteral(string(view.BuyHoldSeriesData)),
		"drawdownSeries":         compressJSONLiteral(string(view.DrawdownSeriesData)),
		"activeTimes":            compressJSONLiteral(string(view.ActiveTimeData)),
	}
	return marshalJS(payload)
}

func compressTextLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write([]byte(trimmed)); err != nil {
		return ""
	}
	if err := writer.Close(); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func compressJSONLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "[]"
	}
	return compressTextLiteral(trimmed)
}

func renderTradeRowsHTML(rows []tradeRowView) string {
	if len(rows) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&builder, "<tr class=\"border-b border-white/5\"><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 text-slate-200\">%s</td><td class=\"px-4 py-2 font-semibold %s\">%s</td><td class=\"px-4 py-2 text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-slate-200\">%s</td><td class=\"px-4 py-2 mono text-slate-200\">%s</td><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-white\">%s</td></tr>",
			template.HTMLEscapeString(row.Timestamp),
			template.HTMLEscapeString(row.Security),
			row.SideClass,
			template.HTMLEscapeString(row.Side),
			template.HTMLEscapeString(row.Reason),
			template.HTMLEscapeString(row.Qty),
			template.HTMLEscapeString(row.FillPrice),
			template.HTMLEscapeString(row.Commission),
			template.HTMLEscapeString(row.Slippage),
			template.HTMLEscapeString(row.NetAmount),
		)
	}
	return builder.String()
}
