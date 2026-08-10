package report

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func chartValueValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func currency(value float64) string {
	return formatAmount(value, "", 2)
}

func amount(value float64, unit string) string {
	decimals := 2
	if strings.TrimSpace(unit) != "" {
		decimals = 4
	}
	return formatAmount(value, unit, decimals)
}

func amount4(value float64, unit string) string {
	return formatAmount(value, unit, 4)
}

func formatAmount(value float64, unit string, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" || strings.EqualFold(trimmedUnit, "USD") {
		if value < 0 {
			return "-$" + fmt.Sprintf("%.*f", decimals, -value)
		}
		return "$" + fmt.Sprintf("%.*f", decimals, value)
	}
	if value < 0 {
		return "-" + fmt.Sprintf("%.*f", decimals, -value) + " " + trimmedUnit
	}
	return fmt.Sprintf("%.*f", decimals, value) + " " + trimmedUnit
}

func signedAmount(value float64, unit string) string {
	if value > 0 {
		return "+" + amount(value, unit)
	}
	return amount(value, unit)
}

func decimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
}

func ratio(value float64) string {
	if math.IsNaN(value) {
		return "-"
	}
	if math.IsInf(value, 1) {
		return "∞"
	}
	if math.IsInf(value, -1) {
		return "-∞"
	}
	return fmt.Sprintf("%.2f", value)
}

func expiryOpenDelta(expiryOpenDays, delta float64) string {
	if math.IsNaN(expiryOpenDays) || math.IsInf(expiryOpenDays, 0) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f 天 | Δ %.2f", expiryOpenDays, delta)
}

func integer(value int) string {
	return fmt.Sprintf("%d", value)
}

func pct(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return backtest.FormatPercent(value)
}

func nullableAmount4(value float64, ok bool, unit string) string {
	if !ok {
		return "-"
	}
	return amount4(value, unit)
}

func formatReportMetricValue(value float64, decimals int) string {
	if !chartValueValid(value) {
		return "-"
	}
	if decimals < 0 {
		decimals = 0
	}
	return fmt.Sprintf("%.*f", decimals, value)
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02")
}

func formatDateTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
}

func sideClass(side string) string {
	if strings.EqualFold(side, "buy") {
		return "text-emerald-300"
	}
	return "text-amber-300"
}

func translateSide(side string) string {
	switch {
	case strings.EqualFold(side, "buy"):
		return "买入"
	case strings.EqualFold(side, "sell"):
		return "卖出"
	default:
		return strings.ToUpper(side)
	}
}

func translateOptionType(optionType string) string {
	switch {
	case strings.EqualFold(optionType, "call"):
		return "看涨"
	case strings.EqualFold(optionType, "put"):
		return "看跌"
	default:
		return strings.ToUpper(optionType)
	}
}

func translateSpreadStatus(status string) string {
	switch {
	case strings.EqualFold(status, "open"):
		return "已开仓"
	case strings.EqualFold(status, "closed"):
		return "已平仓"
	case strings.EqualFold(status, "expired"):
		return "已到期"
	case strings.EqualFold(status, "exercised"):
		return "已行权"
	case strings.EqualFold(status, "assigned"):
		return "已指派"
	default:
		return strings.ToUpper(status)
	}
}

func statusClass(status string) string {
	if strings.EqualFold(status, "closed") {
		return "bg-emerald-500/15 text-emerald-200 ring-emerald-400/40"
	}
	return "bg-amber-500/15 text-amber-200 ring-amber-400/40"
}
