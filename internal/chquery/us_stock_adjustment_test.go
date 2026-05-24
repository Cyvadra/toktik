package chquery

import (
	"strings"
	"testing"
)

func TestUSStockSplitPriceFactor(t *testing.T) {
	tests := []struct {
		name        string
		numerator   float64
		denominator float64
		want        float64
		wantOK      bool
	}{
		{name: "two for one", numerator: 2, denominator: 1, want: 0.5, wantOK: true},
		{name: "one for ten reverse", numerator: 1, denominator: 10, want: 10, wantOK: true},
		{name: "invalid numerator", numerator: 0, denominator: 1, wantOK: false},
		{name: "invalid denominator", numerator: 1, denominator: 0, wantOK: false},
	}
	for _, tt := range tests {
		got, ok := USStockSplitPriceFactor(tt.numerator, tt.denominator)
		if ok != tt.wantOK || got != tt.want {
			t.Fatalf("%s: factor=%v ok=%v, want factor=%v ok=%v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestUSStockSplitJoinSQLUsesDistinctAggregateAliases(t *testing.T) {
	query := USStockSplitJoinSQL("b", "sp")

	if !strings.Contains(query, "argMax(numerator, updated_at) AS latest_numerator") {
		t.Fatalf("expected latest_numerator alias in split query, got %q", query)
	}
	if !strings.Contains(query, "argMax(denominator, updated_at) AS latest_denominator") {
		t.Fatalf("expected latest_denominator alias in split query, got %q", query)
	}
	if strings.Contains(query, "argMax(numerator, updated_at) AS numerator") {
		t.Fatalf("unexpected aggregate alias reuse for numerator in split query: %q", query)
	}
	if strings.Contains(query, "argMax(denominator, updated_at) AS denominator") {
		t.Fatalf("unexpected aggregate alias reuse for denominator in split query: %q", query)
	}
	if !strings.Contains(USStockSplitFactorSQL("sp"), "sp.latest_numerator") {
		t.Fatalf("expected factor SQL to reference latest_numerator alias")
	}
	if !strings.Contains(USStockSplitFactorSQL("sp"), "sp.latest_denominator") {
		t.Fatalf("expected factor SQL to reference latest_denominator alias")
	}
}
