package strategies

import "testing"

func TestRollingPercentileRank(t *testing.T) {
	history := []float64{10, 20, 30, 40, 50}

	if got := rollingPercentileRank(history, 5, 10); got != 20 {
		t.Fatalf("expected 20, got %.2f", got)
	}

	if got := rollingPercentileRank(history, 5, 35); got != 60 {
		t.Fatalf("expected 60, got %.2f", got)
	}

	if got := rollingPercentileRank(history, 3, 35); got != (100.0 / 3.0) {
		t.Fatalf("expected %.6f, got %.6f", 100.0/3.0, got)
	}
}

func TestIVMultiplierLong(t *testing.T) {
	s := &optionTrendReplacementStrategy{mode: optionTrendLongCall}

	cases := []struct {
		pct  float64
		want float64
	}{
		{pct: 10, want: 1.2},
		{pct: 40, want: 1.0},
		{pct: 75, want: 0.7},
		{pct: 95, want: 0.7},
	}

	for _, tc := range cases {
		if got := s.ivMultiplier(tc.pct); got != tc.want {
			t.Fatalf("pct %.2f: expected %.2f, got %.2f", tc.pct, tc.want, got)
		}
	}
}

func TestIVMultiplierShortPut(t *testing.T) {
	s := &optionTrendReplacementStrategy{mode: optionTrendShortPut}

	cases := []struct {
		pct  float64
		want float64
	}{
		{pct: 10, want: 1.2},
		{pct: 40, want: 1.0},
		{pct: 75, want: 0.7},
		{pct: 90, want: 0.5},
	}

	for _, tc := range cases {
		if got := s.ivMultiplier(tc.pct); got != tc.want {
			t.Fatalf("pct %.2f: expected %.2f, got %.2f", tc.pct, tc.want, got)
		}
	}
}
