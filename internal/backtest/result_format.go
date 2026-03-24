package backtest

import (
	"math"
	"strings"
)

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
