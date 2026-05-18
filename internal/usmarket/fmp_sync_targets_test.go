package usmarket

import "testing"

func TestBuildStoredStockSyncTargetUsesIndexAlias(t *testing.T) {
	target := buildStoredStockSyncTarget("SPX")
	if target.StoreSymbol != "SPX" {
		t.Fatalf("unexpected store symbol: %+v", target)
	}
	if target.FetchSymbol != "^GSPC" {
		t.Fatalf("expected SPX to fetch via ^GSPC, got %+v", target)
	}
	if target.Source != "stored-stock-index-alias" {
		t.Fatalf("unexpected source: %+v", target)
	}
}

func TestBuildStoredStockSyncTargetLeavesRegularStocksUnchanged(t *testing.T) {
	target := buildStoredStockSyncTarget("AAPL")
	if target.FetchSymbol != "AAPL" || target.Source != "stored-stock" {
		t.Fatalf("unexpected regular stock target: %+v", target)
	}
}
