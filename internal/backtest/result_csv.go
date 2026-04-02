package backtest

import (
	"bufio"
	"encoding/csv"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	tradeCSVQtyDecimals    = 4
	tradeCSVPriceDecimals  = 6
	tradeCSVFeeDecimals    = 6
	tradeCSVPnLDecimals    = 6
	tradeCSVDeltaDecimals  = 4
	tradeCSVStrikeDecimals = 2
	tradeCSVBufferSize     = 64 * 1024
)

var tradeCSVHeader = []string{
	"ts",
	"kind",
	"id",
	"grp",
	"leg",
	"symbol",
	"side",
	"type",
	"qty",
	"price",
	"fee",
	"pnl",
	"delta",
	"expiry",
	"strike",
	"status",
	"note",
}

func (r *Result) ExportTradesCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := bufio.NewWriterSize(f, tradeCSVBufferSize)
	w := csv.NewWriter(buf)
	if err := w.Write(tradeCSVHeader); err != nil {
		return err
	}

	for _, trade := range r.Trades {
		if err := w.Write([]string{
			formatTradeCSVTime(trade.Timestamp),
			"trade",
			itoa(trade.ID),
			"",
			"",
			compactTradeSymbol(trade.Security),
			trade.Side.String(),
			"",
			compactTradeFloat(trade.Qty, tradeCSVQtyDecimals),
			compactTradeFloat(trade.FillPrice, tradeCSVPriceDecimals),
			compactTradeFloat(trade.Commission, tradeCSVFeeDecimals),
			"",
			"",
			"",
			"",
			"",
			compactTradeText(trade.Note),
		}); err != nil {
			return err
		}
	}

	for _, position := range r.SpreadPositions {
		groupID := compactTradeInt(position.GroupID)
		status := compactTradeText(position.Status)
		openNote := compactTradeText(position.Tag)
		closeNote := compactTradeText(firstNonEmpty(position.CloseNote, position.Tag))
		for idx, leg := range position.Legs {
			legIndex := itoa(idx + 1)
			if err := w.Write([]string{
				formatTradeCSVTime(leg.EntryTime),
				"option_open",
				itoa(position.ID),
				groupID,
				legIndex,
				compactTradeText(leg.Symbol),
				compactTradeText(leg.Side),
				string(leg.Type),
				compactTradeFloat(leg.Qty, tradeCSVQtyDecimals),
				compactTradeFloat(leg.EntryPrice, tradeCSVPriceDecimals),
				"",
				"",
				compactTradeFloat(leg.Delta, tradeCSVDeltaDecimals),
				formatTradeCSVDate(leg.Expiration),
				compactTradeFloat(leg.StrikePrice, tradeCSVStrikeDecimals),
				status,
				openNote,
			}); err != nil {
				return err
			}

			if !leg.Closed {
				continue
			}

			if err := w.Write([]string{
				formatTradeCSVTime(derefTradeTime(leg.CloseTime)),
				"option_close",
				itoa(position.ID),
				groupID,
				legIndex,
				compactTradeText(leg.Symbol),
				oppositeTradeSide(leg.Side),
				string(leg.Type),
				compactTradeFloat(leg.Qty, tradeCSVQtyDecimals),
				compactTradeFloat(leg.ClosePrice, tradeCSVPriceDecimals),
				"",
				compactTradeFloat(leg.RealizedPnL, tradeCSVPnLDecimals),
				compactTradeFloat(derefTradeFloat(leg.CloseDelta), tradeCSVDeltaDecimals),
				formatTradeCSVDate(leg.Expiration),
				compactTradeFloat(leg.StrikePrice, tradeCSVStrikeDecimals),
				status,
				compactTradeText(firstNonEmpty(leg.CloseReason, closeNote)),
			}); err != nil {
				return err
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return buf.Flush()
}

func compactTradeSymbol(ref SecurityRef) string {
	if symbol := strings.TrimSpace(ref.Symbol); symbol != "" {
		return symbol
	}
	return compactTradeText(ref.Market)
}

func compactTradeText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func compactTradeInt(value int) string {
	if value == 0 {
		return ""
	}
	return itoa(value)
}

func compactTradeFloat(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	factor := math.Pow10(decimals)
	rounded := math.Round(value*factor) / factor
	if rounded == 0 {
		rounded = 0
	}
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

func formatTradeCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatTradeCSVDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02")
}

func oppositeTradeSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy":
		return "sell"
	case "sell":
		return "buy"
	default:
		return ""
	}
}

func derefTradeTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func derefTradeFloat(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
