package retracementratioprotectivespread

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestParseSignalSource(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "12h", raw: "12h", want: "12h"},
		{name: "1d", raw: "1d", want: "1d"},
		{name: "trim and lower", raw: " 12H ", want: "12h"},
		{name: "missing", raw: "", wantErr: true},
		{name: "invalid", raw: "6h", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSignalSource(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSignalSource(%q) expected error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSignalSource(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseSignalSource(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadSignalTimesFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.csv")
	content := "交易 #,类型,日期和时间,信号\n1,多头进场,2023-01-06 08:00,Long_Init\n2,多头进场,2023-01-06 08:00,Long_Init\n3,多头进场,2023-05-03 08:00,Long_Init\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	times, err := loadSignalTimesFromCSV(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromCSV() error = %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len(times) = %d, want 2", len(times))
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	for _, raw := range []string{"2023-01-06 08:00", "2023-05-03 08:00"} {
		ts, err := time.ParseInLocation(signalTimeLayout, raw, utc8)
		if err != nil {
			t.Fatalf("parse time %q: %v", raw, err)
		}
		if _, ok := times[ts.UTC().Unix()]; !ok {
			t.Fatalf("missing expected timestamp for %s", raw)
		}
	}
}

func TestDynamicLongDeltaClamps(t *testing.T) {
	tests := []struct {
		name         string
		hvPercentile float64
		ivPercentile float64
		want         float64
	}{
		{name: "lower clamp", hvPercentile: 0, ivPercentile: 0, want: minLongDelta},
		{name: "formula", hvPercentile: 75, ivPercentile: 60, want: 0.6},
		{name: "upper clamp", hvPercentile: 100, ivPercentile: 100, want: maxLongDelta},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dynamicLongDelta(tc.hvPercentile, tc.ivPercentile)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("dynamicLongDelta(%v, %v) = %v, want %v", tc.hvPercentile, tc.ivPercentile, got, tc.want)
			}
		})
	}
}

func TestSelectAmbushUsesSplitLongLegs(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "P-50000", Type: backtest.Put, StrikePrice: 50000, Expiration: expiry, Delta: -0.50, BidPrice: 5.0, AskPrice: 5.2, MarkPrice: 5.1},
		{Symbol: "P-48000", Type: backtest.Put, StrikePrice: 48000, Expiration: expiry, Delta: -0.39, BidPrice: 2.4, AskPrice: 2.6, MarkPrice: 2.5},
		{Symbol: "P-47000", Type: backtest.Put, StrikePrice: 47000, Expiration: expiry, Delta: -0.31, BidPrice: 2.2, AskPrice: 2.4, MarkPrice: 2.3},
		{Symbol: "P-46000", Type: backtest.Put, StrikePrice: 46000, Expiration: expiry, Delta: -0.24, BidPrice: 1.8, AskPrice: 2.0, MarkPrice: 1.9},
		{Symbol: "P-45000", Type: backtest.Put, StrikePrice: 45000, Expiration: expiry, Delta: -0.18, BidPrice: 1.4, AskPrice: 1.6, MarkPrice: 1.5},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectAmbush(backtest.NewOptionsChain(contracts, now), now, sideShort)
	if !ok {
		t.Fatalf("selectAmbush() did not find a valid ratio spread")
	}
	if selection.short.Symbol != "P-50000" {
		t.Fatalf("short = %s, want P-50000", selection.short.Symbol)
	}
	if selection.longLowerHalf.Symbol != "P-47000" {
		t.Fatalf("longLowerHalf = %s, want P-47000", selection.longLowerHalf.Symbol)
	}
	if selection.longUpperHalf.Symbol != "P-48000" {
		t.Fatalf("longUpperHalf = %s, want P-48000", selection.longUpperHalf.Symbol)
	}
	if math.Abs((selection.longLowerPrice+selection.longUpperPrice)-selection.shortPrice) > 1e-9 {
		t.Fatalf("combined long premium = %v, short premium = %v, want zero-cost split", selection.longLowerPrice+selection.longUpperPrice, selection.shortPrice)
	}
}

func TestSelectAmbushLongSideUsesSplitCallLegs(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.50, BidPrice: 5.0, AskPrice: 5.2, MarkPrice: 5.1},
		{Symbol: "C-52000", Type: backtest.Call, StrikePrice: 52000, Expiration: expiry, Delta: 0.39, BidPrice: 2.4, AskPrice: 2.6, MarkPrice: 2.5},
		{Symbol: "C-53000", Type: backtest.Call, StrikePrice: 53000, Expiration: expiry, Delta: 0.31, BidPrice: 2.2, AskPrice: 2.4, MarkPrice: 2.3},
		{Symbol: "C-54000", Type: backtest.Call, StrikePrice: 54000, Expiration: expiry, Delta: 0.24, BidPrice: 1.8, AskPrice: 2.0, MarkPrice: 1.9},
		{Symbol: "C-55000", Type: backtest.Call, StrikePrice: 55000, Expiration: expiry, Delta: 0.18, BidPrice: 1.4, AskPrice: 1.6, MarkPrice: 1.5},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectAmbush(backtest.NewOptionsChain(contracts, now), now, sideLong)
	if !ok {
		t.Fatalf("selectAmbush() did not find a valid long-side ratio spread")
	}
	if selection.short.Symbol != "C-50000" {
		t.Fatalf("short = %s, want C-50000", selection.short.Symbol)
	}
	if selection.longLowerHalf.Symbol != "C-53000" {
		t.Fatalf("longLowerHalf = %s, want C-53000", selection.longLowerHalf.Symbol)
	}
	if selection.longUpperHalf.Symbol != "C-52000" {
		t.Fatalf("longUpperHalf = %s, want C-52000", selection.longUpperHalf.Symbol)
	}
	if math.Abs((selection.longLowerPrice+selection.longUpperPrice)-selection.shortPrice) > 1e-9 {
		t.Fatalf("combined long premium = %v, short premium = %v, want zero-cost split", selection.longLowerPrice+selection.longUpperPrice, selection.shortPrice)
	}
}

func TestAmbushLegQuantitiesCanDifferBetweenShortAndLongWings(t *testing.T) {
	selection := &ambushSelection{
		shortPrice:     5.0,
		longLowerPrice: 2.1,
		longUpperPrice: 2.3,
	}

	shortQtyTotal := ambushPremiumBaseBTC / selection.shortPrice
	longQtyTotal := ambushPremiumBaseBTC / (selection.longLowerPrice + selection.longUpperPrice)
	if math.Abs(shortQtyTotal-longQtyTotal) < 1e-9 {
		t.Fatalf("shortQtyTotal = %v, longQtyTotal = %v, want different quantities when wing premium sum != short premium", shortQtyTotal, longQtyTotal)
	}
	if math.Abs(shortQtyTotal-1.0) > 1e-9 {
		t.Fatalf("shortQtyTotal = %v, want 1.0", shortQtyTotal)
	}
	if math.Abs(longQtyTotal-(5.0/4.4)) > 1e-9 {
		t.Fatalf("longQtyTotal = %v, want %v", longQtyTotal, 5.0/4.4)
	}
}

func TestSelectTrendLongSideUsesCallDebitSpread(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.60, BidPrice: 5.8, AskPrice: 6.2, MarkPrice: 6.0},
		{Symbol: "C-57000", Type: backtest.Call, StrikePrice: 57000, Expiration: expiry, Delta: 0.22, BidPrice: 4.0, AskPrice: 4.2, MarkPrice: 4.1},
		{Symbol: "C-58000", Type: backtest.Call, StrikePrice: 58000, Expiration: expiry, Delta: 0.20, BidPrice: 3.8, AskPrice: 4.2, MarkPrice: 4.0},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectTrend(backtest.NewOptionsChain(contracts, now), now, 75, 60, 2.0, sideLong)
	if !ok {
		t.Fatalf("selectTrend() did not find a valid long-side debit spread")
	}
	if selection.long.Symbol != "C-50000" {
		t.Fatalf("long = %s, want C-50000", selection.long.Symbol)
	}
	if selection.short.Symbol != "C-58000" {
		t.Fatalf("short = %s, want C-58000", selection.short.Symbol)
	}
	wantSpreadCost := selection.longPrice - selection.shortPrice
	if math.Abs(selection.spreadCost-wantSpreadCost) > 1e-9 {
		t.Fatalf("spreadCost = %v, want %v", selection.spreadCost, wantSpreadCost)
	}
	wantQty := 2.0 / selection.spreadCost
	if math.Abs(selection.qty-wantQty) > 1e-9 {
		t.Fatalf("qty = %v, want %v", selection.qty, wantQty)
	}
}

func TestSelectTrendRejectsLongLegOutsideDocumentedDeltaRange(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.05, BidPrice: 5.8, AskPrice: 6.2, MarkPrice: 6.0},
		{Symbol: "C-58000", Type: backtest.Call, StrikePrice: 58000, Expiration: expiry, Delta: 0.20, BidPrice: 3.8, AskPrice: 4.2, MarkPrice: 4.0},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	if _, ok := s.selectTrend(backtest.NewOptionsChain(contracts, now), now, 75, 60, 2.0, sideLong); ok {
		t.Fatalf("selectTrend() unexpectedly accepted a long leg with |delta| below 0.1")
	}
}
