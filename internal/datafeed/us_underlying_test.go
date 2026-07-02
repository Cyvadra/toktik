package datafeed

import (
	"strings"
	"testing"
)

func TestUSStockUnderlyingPriceSQLCanReturnRawUnadjustedPrices(t *testing.T) {
	got := usStockUnderlyingPriceSQL("b", "close", "sp", false)
	if got != "toFloat64(b.close)" {
		t.Fatalf("raw price SQL = %q, want toFloat64(b.close)", got)
	}
	if strings.Contains(got, "split") || strings.Contains(got, "sp.") {
		t.Fatalf("raw price SQL should not reference split adjustment: %s", got)
	}
}

func TestUSUnderlyingDataFeedDefaultAdjustedMode(t *testing.T) {
	if !NewUSUnderlyingDataFeed(nil).defaultAdjusted() {
		t.Fatal("default US underlying feed should preserve adjusted prices")
	}
	if NewUSUnderlyingDataFeedWithAdjusted(nil, false).defaultAdjusted() {
		t.Fatal("US underlying feed configured with adjusted=false should request raw prices")
	}
}
