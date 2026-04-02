// Package helpers provides shared utility functions for strategy implementations.
package helpers

import (
	"math"
)

// PositionSizeFromEquity calculates the quantity to buy given a budget
// derived from equity percentage and the current price.
//
// Parameters:
//   - cash: available cash balance
//   - equity: total equity value
//   - price: current price per unit
//   - equityPct: fraction of equity to allocate (0-1)
//
// Returns the quantity to buy, or 0 if inputs are invalid.
func PositionSizeFromEquity(cash, equity, price, equityPct float64) float64 {
	if cash <= 0 || equity <= 0 || price <= 0 || equityPct <= 0 {
		return 0
	}
	budget := math.Min(cash, equity*equityPct)
	if budget <= 0 {
		return 0
	}
	return budget / price
}

// PositionSizeFromCash calculates the quantity to buy given cash percentage.
//
// Parameters:
//   - cash: available cash balance
//   - price: current price per unit
//   - cashPct: fraction of cash to allocate (0-1)
//
// Returns the quantity to buy, or 0 if inputs are invalid.
func PositionSizeFromCash(cash, price, cashPct float64) float64 {
	if cash <= 0 || price <= 0 || cashPct <= 0 {
		return 0
	}
	return (cash * cashPct) / price
}

// PositionSizeFromNotional calculates quantity from a fixed notional amount.
//
// Parameters:
//   - notional: the notional value to allocate
//   - price: current price per unit
//
// Returns the quantity, or 0 if inputs are invalid.
func PositionSizeFromNotional(notional, price float64) float64 {
	if notional <= 0 || price <= 0 {
		return 0
	}
	return notional / price
}

// ClampPositionPct ensures a position percentage is valid (0-1 range).
// If raw is <= 0, returns the default value.
// If raw > 1, returns 1.
func ClampPositionPct(raw, defaultPct float64) float64 {
	if raw <= 0 {
		return defaultPct
	}
	if raw > 1 {
		return 1
	}
	return raw
}

// CopySeries creates a copy of a float64 slice.
// Returns nil if the input is nil.
func CopySeries(values []float64) []float64 {
	if values == nil {
		return nil
	}
	out := make([]float64, len(values))
	copy(out, values)
	return out
}

// SafeDivide performs division with NaN protection.
// Returns NaN if divisor is zero, NaN, or infinite.
func SafeDivide(numerator, divisor float64) float64 {
	if divisor == 0 || math.IsNaN(divisor) || math.IsInf(divisor, 0) {
		return math.NaN()
	}
	return numerator / divisor
}

// ValidSeriesValue returns the value if it's valid (non-NaN, non-Inf),
// otherwise returns the fallback.
func ValidSeriesValue(value, fallback float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return value
}
