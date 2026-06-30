package usexport

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunRejectsBareCryptoAssetSymbolsByDefault(t *testing.T) {
	_, err := Run(context.Background(), nil, Config{
		Symbols:                []string{"BTC"},
		StartDate:              time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
		Interval:               "1d",
		IncludeStocks:          true,
		IncludeOptionContracts: true,
		IncludeOptionBars:      true,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want ambiguous crypto symbol error")
	}
	if got := err.Error(); !strings.Contains(got, "bare crypto asset symbol") || !strings.Contains(got, "US-listed ticker") {
		t.Fatalf("Run() error = %q, want ambiguity guidance", got)
	}
}

func TestFormatCSVValueDoesNotDropFloatMagnitude(t *testing.T) {
	if got := FormatCSVValue(float64(60123.45)); got != "60123.45" {
		t.Fatalf("FormatCSVValue(60123.45) = %q, want 60123.45", got)
	}
}
