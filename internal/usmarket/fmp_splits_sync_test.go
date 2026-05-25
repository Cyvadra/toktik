package usmarket

import (
	"fmt"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestConvertFMPStockSplitsNormalizesAndHashesRows(t *testing.T) {
	updatedAt := time.Date(2026, 5, 24, 1, 2, 3, 0, time.UTC)
	splits, err := convertFMPStockSplits("aapl", []fmp.StockSplit{{Date: "2020-08-31", Numerator: 4, Denominator: 1, SplitType: "Stock Split"}}, updatedAt)
	if err != nil {
		t.Fatalf("convertFMPStockSplits returned error: %v", err)
	}
	if len(splits) != 1 {
		t.Fatalf("expected one split, got %#v", splits)
	}
	got := splits[0]
	if got.Symbol != "AAPL" || !got.SplitDate.Equal(time.Date(2020, 8, 31, 0, 0, 0, 0, time.UTC)) || got.Numerator != 4 || got.Denominator != 1 || got.Source != "fmp" || got.SourceHash == "" || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected split conversion: %#v", got)
	}
}

func TestConvertFMPStockSplitsRejectsInvalidRatio(t *testing.T) {
	_, err := convertFMPStockSplits("AAPL", []fmp.StockSplit{{Symbol: "AAPL", Date: "2020-08-31", Numerator: 0, Denominator: 1}}, time.Now())
	if err == nil {
		t.Fatal("expected invalid ratio error")
	}
}

func TestConvertFMPStockSplitsSkipsImplausibleFutureDates(t *testing.T) {
	updatedAt := time.Date(2026, 5, 24, 1, 2, 3, 0, time.UTC)
	splits, err := convertFMPStockSplits("AAPL", []fmp.StockSplit{
		{Symbol: "AAPL", Date: "2020-08-31", Numerator: 4, Denominator: 1},
		{Symbol: "AAPL", Date: "2148-11-22", Numerator: 3, Denominator: 1},
	}, updatedAt)
	if err != nil {
		t.Fatalf("convertFMPStockSplits returned error: %v", err)
	}
	if len(splits) != 1 {
		t.Fatalf("expected one retained split, got %#v", splits)
	}
	if !splits[0].SplitDate.Equal(time.Date(2020, 8, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected retained split date: %#v", splits[0])
	}
}

func TestNormalizeStockSplitSymbolsDeduplicates(t *testing.T) {
	got := normalizeStockSplitSymbols([]string{"aapl", " AAPL ", "msft", ""})
	if len(got) != 2 || got[0] != "AAPL" || got[1] != "MSFT" {
		t.Fatalf("normalizeStockSplitSymbols = %#v", got)
	}
}

func TestShouldSkipFMPStockSplitSymbol(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "premium unsupported symbol",
			err: &fmp.HTTPStatusError{
				StatusCode: 402,
				Body:       "Premium Query Parameter: 'Special Endpoint : This value set for 'symbol' is not available under your current subscription'",
			},
			want: true,
		},
		{
			name: "wrapped premium unsupported symbol",
			err: fmt.Errorf("wrapped: %w", &fmp.HTTPStatusError{
				StatusCode: 402,
				Body:       "This value set for 'symbol' is not available under your current subscription.",
			}),
			want: true,
		},
		{
			name: "non premium http error",
			err: &fmp.HTTPStatusError{
				StatusCode: 500,
				Body:       "server error",
			},
			want: false,
		},
		{
			name: "generic error",
			err:  fmt.Errorf("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		if got := shouldSkipFMPStockSplitSymbol(tt.err); got != tt.want {
			t.Fatalf("%s: shouldSkipFMPStockSplitSymbol() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
