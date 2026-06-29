package macro

import (
	"math"
	"testing"
	"time"
)

func TestBuildDeribitDVOLCatalogRows(t *testing.T) {
	rows := BuildDeribitDVOLCatalogRows(DefaultDeribitDVOLBTCDataset, "BTC")
	if len(rows) != 4 {
		t.Fatalf("len(rows)=%d want 4", len(rows))
	}
	if rows[0].Dataset != DefaultDeribitDVOLBTCDataset {
		t.Fatalf("Dataset=%s want %s", rows[0].Dataset, DefaultDeribitDVOLBTCDataset)
	}
	if rows[0].PreferredFrequency != defaultHourlyFrequency {
		t.Fatalf("PreferredFrequency=%s want %s", rows[0].PreferredFrequency, defaultHourlyFrequency)
	}
	if rows[0].Source != DefaultDeribitDVOLSource {
		t.Fatalf("Source=%s want %s", rows[0].Source, DefaultDeribitDVOLSource)
	}
	if rows[0].ReferenceMarket != DefaultCryptoReferenceMarket || rows[0].ReferenceSymbol != "BTC" {
		t.Fatalf("reference=%s/%s want crypto/BTC", rows[0].ReferenceMarket, rows[0].ReferenceSymbol)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.FactorCode] = true
	}
	for _, factor := range []string{"open", "high", "low", "close"} {
		if !seen[factor] {
			t.Fatalf("missing factor %s in %#v", factor, seen)
		}
	}
}

func TestBuildDeribitDVOLObservationRows(t *testing.T) {
	ts := time.Date(2026, 6, 29, 8, 0, 0, 0, time.UTC)
	rows := BuildDeribitDVOLObservationRows(DefaultDeribitDVOLBTCDataset, []DeribitDVOLBar{{Symbol: "BTC", Timestamp: ts, Open: 50, High: 55, Low: 49, Close: 53}}, "BTC")
	if len(rows) != 4 {
		t.Fatalf("len(rows)=%d want 4", len(rows))
	}
	for _, row := range rows {
		if row.Dataset != DefaultDeribitDVOLBTCDataset {
			t.Fatalf("Dataset=%s want %s", row.Dataset, DefaultDeribitDVOLBTCDataset)
		}
		if !row.EventTS.Equal(ts) || !row.KnownAt.Equal(ts) || !row.PeriodStart.Equal(ts) || !row.PeriodEnd.Equal(ts.Add(time.Hour)) {
			t.Fatalf("unexpected timestamps: %+v", row)
		}
		if row.Source != DefaultDeribitDVOLSource || row.ReferenceMarket != DefaultCryptoReferenceMarket || row.ReferenceSymbol != "BTC" {
			t.Fatalf("unexpected source/reference: %+v", row)
		}
		if !math.IsNaN(row.AnchorValue) {
			t.Fatalf("AnchorValue=%v want NaN", row.AnchorValue)
		}
	}
	if rows[0].FactorCode != "close" || rows[1].FactorCode != "high" || rows[2].FactorCode != "low" || rows[3].FactorCode != "open" {
		t.Fatalf("unexpected sorted factors: %s %s %s %s", rows[0].FactorCode, rows[1].FactorCode, rows[2].FactorCode, rows[3].FactorCode)
	}
}

func TestDeribitDVOLDatasetMappings(t *testing.T) {
	if dataset, ok := DeribitDVOLDatasetForSymbol("btc"); !ok || dataset != DefaultDeribitDVOLBTCDataset {
		t.Fatalf("BTC dataset=%s ok=%v", dataset, ok)
	}
	if symbol, ok := DeribitDVOLSymbolForDataset(DefaultDeribitDVOLETHDataset); !ok || symbol != "ETH" {
		t.Fatalf("ETH symbol=%s ok=%v", symbol, ok)
	}
	if IsDeribitDVOLDataset("cboe-vix") {
		t.Fatalf("cboe-vix should not be a DVOL dataset")
	}
}
