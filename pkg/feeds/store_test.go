package feeds

import (
	"context"
	"testing"
	"time"
)

func TestFilterExistingBarsSkipsExistingRows(t *testing.T) {
	store := &Store{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []Bar{
		{Symbol: "btc", Timestamp: base, Open: 1, High: 2, Low: 0.5, Close: 1.5},
		{Symbol: "BTC", Timestamp: base.Add(time.Hour), Open: 2, High: 3, Low: 1.5, Close: 2.5},
		{Symbol: "ETH", Timestamp: base, Open: 3, High: 4, Low: 2.5, Close: 3.5},
	}
	existing := map[string]struct{}{
		existingBarKey("BTC", base.UnixMilli()): {},
	}

	filtered := store.filterExistingBarsWithSet(context.Background(), bars, existing)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 bars after filtering, got %d", len(filtered))
	}
	if filtered[0].Symbol != "BTC" || !filtered[0].Timestamp.Equal(base.Add(time.Hour)) {
		t.Fatalf("unexpected first remaining bar: %+v", filtered[0])
	}
	if filtered[1].Symbol != "ETH" || !filtered[1].Timestamp.Equal(base) {
		t.Fatalf("unexpected second remaining bar: %+v", filtered[1])
	}
}
