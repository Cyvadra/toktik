package service

import (
	"reflect"
	"testing"
)

func TestUSStockLogoSymbolCandidatesUseAliasGroups(t *testing.T) {
	tests := []struct {
		symbol string
		want   []string
	}{
		{symbol: "GOOG", want: []string{"GOOG", "GOOGL"}},
		{symbol: "GOOGL", want: []string{"GOOGL", "GOOG"}},
		{symbol: "SPX", want: []string{"SPX", "SPY"}},
		{symbol: "brk-b", want: []string{"BRK-B", "BRK.B", "BRK.A", "BRK-A"}},
		{symbol: "AAPL", want: []string{"AAPL"}},
	}
	for _, tc := range tests {
		if got := usStockLogoSymbolCandidates(tc.symbol); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("usStockLogoSymbolCandidates(%q) = %#v, want %#v", tc.symbol, got, tc.want)
		}
	}
}
