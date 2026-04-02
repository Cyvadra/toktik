package validation

import (
	"math"
	"testing"
)

func TestIsValidPrice(t *testing.T) {
	tests := []struct {
		name   string
		value  float64
		expect bool
	}{
		{"positive", 100.0, true},
		{"small positive", 0.001, true},
		{"zero", 0.0, false},
		{"negative", -100.0, false},
		{"NaN", math.NaN(), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPrice(tt.value); got != tt.expect {
				t.Errorf("IsValidPrice(%v) = %v, want %v", tt.value, got, tt.expect)
			}
		})
	}
}

func TestAllValid(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		expect bool
	}{
		{"all valid", []float64{1.0, 2.0, 3.0}, true},
		{"with zero", []float64{0.0, 1.0, 2.0}, true},
		{"with negative", []float64{-1.0, 1.0, 2.0}, true},
		{"with NaN", []float64{1.0, math.NaN(), 2.0}, false},
		{"with Inf", []float64{1.0, math.Inf(1), 2.0}, false},
		{"empty", []float64{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllValid(tt.values...); got != tt.expect {
				t.Errorf("AllValid(%v) = %v, want %v", tt.values, got, tt.expect)
			}
		})
	}
}

func TestAllPositive(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		expect bool
	}{
		{"all positive", []float64{1.0, 2.0, 3.0}, true},
		{"with zero", []float64{0.0, 1.0, 2.0}, false},
		{"with negative", []float64{-1.0, 1.0, 2.0}, false},
		{"with NaN", []float64{1.0, math.NaN(), 2.0}, false},
		{"empty", []float64{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllPositive(tt.values...); got != tt.expect {
				t.Errorf("AllPositive(%v) = %v, want %v", tt.values, got, tt.expect)
			}
		})
	}
}

func TestFloatOrDefault(t *testing.T) {
	tests := []struct {
		value    float64
		fallback float64
		expect   float64
	}{
		{5.0, 10.0, 5.0},
		{0.0, 10.0, 10.0},
		{-5.0, 10.0, -5.0},
	}

	for _, tt := range tests {
		if got := FloatOrDefault(tt.value, tt.fallback); got != tt.expect {
			t.Errorf("FloatOrDefault(%v, %v) = %v, want %v", tt.value, tt.fallback, got, tt.expect)
		}
	}
}

func TestClampFraction(t *testing.T) {
	tests := []struct {
		value  float64
		expect float64
	}{
		{0.5, 0.5},
		{0.0, 0.0},
		{1.0, 1.0},
		{1.5, 1.0},
		{-0.5, 0.0},
	}

	for _, tt := range tests {
		if got := ClampFraction(tt.value); got != tt.expect {
			t.Errorf("ClampFraction(%v) = %v, want %v", tt.value, got, tt.expect)
		}
	}
}
