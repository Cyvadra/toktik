package helpers

import (
	"math"
	"testing"
)

func TestPositionSizeFromEquity(t *testing.T) {
	tests := []struct {
		name      string
		cash      float64
		equity    float64
		price     float64
		equityPct float64
		want      float64
	}{
		{"normal case", 10000, 10000, 100, 0.95, 95},
		{"cash limited", 500, 10000, 100, 0.95, 5},
		{"zero cash", 0, 10000, 100, 0.95, 0},
		{"zero price", 10000, 10000, 0, 0.95, 0},
		{"zero equity", 10000, 0, 100, 0.95, 0},
		{"zero pct", 10000, 10000, 100, 0, 0},
		{"negative cash", -100, 10000, 100, 0.95, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PositionSizeFromEquity(tt.cash, tt.equity, tt.price, tt.equityPct)
			if got != tt.want {
				t.Errorf("PositionSizeFromEquity(%v, %v, %v, %v) = %v, want %v",
					tt.cash, tt.equity, tt.price, tt.equityPct, got, tt.want)
			}
		})
	}
}

func TestPositionSizeFromCash(t *testing.T) {
	tests := []struct {
		cash    float64
		price   float64
		cashPct float64
		want    float64
	}{
		{10000, 100, 0.5, 50},
		{10000, 100, 1.0, 100},
		{0, 100, 0.5, 0},
	}

	for _, tt := range tests {
		got := PositionSizeFromCash(tt.cash, tt.price, tt.cashPct)
		if got != tt.want {
			t.Errorf("PositionSizeFromCash(%v, %v, %v) = %v, want %v",
				tt.cash, tt.price, tt.cashPct, got, tt.want)
		}
	}
}

func TestClampPositionPct(t *testing.T) {
	tests := []struct {
		raw     float64
		def     float64
		want    float64
	}{
		{0.5, 0.95, 0.5},
		{0, 0.95, 0.95},
		{-0.1, 0.95, 0.95},
		{1.5, 0.95, 1.0},
		{1.0, 0.95, 1.0},
	}

	for _, tt := range tests {
		got := ClampPositionPct(tt.raw, tt.def)
		if got != tt.want {
			t.Errorf("ClampPositionPct(%v, %v) = %v, want %v",
				tt.raw, tt.def, got, tt.want)
		}
	}
}

func TestCopySeries(t *testing.T) {
	t.Run("normal slice", func(t *testing.T) {
		orig := []float64{1, 2, 3}
		copied := CopySeries(orig)
		if len(copied) != len(orig) {
			t.Fatalf("len mismatch: got %d, want %d", len(copied), len(orig))
		}
		for i := range orig {
			if copied[i] != orig[i] {
				t.Errorf("index %d: got %v, want %v", i, copied[i], orig[i])
			}
		}
		// Verify it's a copy, not the same slice
		copied[0] = 999
		if orig[0] == 999 {
			t.Error("modifying copy affected original")
		}
	})

	t.Run("nil input", func(t *testing.T) {
		if CopySeries(nil) != nil {
			t.Error("expected nil output for nil input")
		}
	})
}

func TestSafeDivide(t *testing.T) {
	tests := []struct {
		numerator float64
		divisor   float64
		wantNaN   bool
		want      float64
	}{
		{10, 2, false, 5},
		{10, 0, true, 0},
		{10, math.NaN(), true, 0},
		{10, math.Inf(1), true, 0},
	}

	for _, tt := range tests {
		got := SafeDivide(tt.numerator, tt.divisor)
		if tt.wantNaN {
			if !math.IsNaN(got) {
				t.Errorf("SafeDivide(%v, %v) = %v, want NaN",
					tt.numerator, tt.divisor, got)
			}
		} else {
			if got != tt.want {
				t.Errorf("SafeDivide(%v, %v) = %v, want %v",
					tt.numerator, tt.divisor, got, tt.want)
			}
		}
	}
}
