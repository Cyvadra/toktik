// Package validation provides shared validation utilities for numeric values.
package validation

import "math"

// IsValidPrice returns true if the value is a valid market price:
// positive, non-NaN, and non-infinite.
func IsValidPrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// IsValidValue returns true if the value is usable (non-NaN, non-infinite).
func IsValidValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// IsPositiveValue returns true if the value is positive, non-NaN, and non-infinite.
func IsPositiveValue(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// AllValid returns true if all provided values are usable (non-NaN, non-infinite).
func AllValid(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// AllPositive returns true if all values are positive, non-NaN, and non-infinite.
func AllPositive(values ...float64) bool {
	for _, v := range values {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// FloatOrDefault returns value if non-zero, otherwise fallback.
// This is a convenience helper for applying default values.
func FloatOrDefault(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

// IntOrDefault returns value if non-zero, otherwise fallback.
func IntOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// ClampFloat clamps a value to the range [min, max].
func ClampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampPositive ensures a float value is at least 0.
func ClampPositive(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

// ClampFraction ensures a value is within [0, 1] range.
func ClampFraction(value float64) float64 {
	return ClampFloat(value, 0, 1)
}
