package datafeed

import "testing"

func TestFeatureVolatilityFactorFeedFields(t *testing.T) {
	feed := NewFeatureVolatilityFactorFeed(nil)
	got := feed.Fields()
	want := []string{"iv", "current_iv", "hv10", "hv20", "hv30", "iv_percentile", "iv_rank", "price_observations", "iv_observations"}
	if len(got) != len(want) {
		t.Fatalf("Fields len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Fields[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVolatilityFeatureMarketMapsPrimaryMarkets(t *testing.T) {
	tests := []struct {
		name     string
		market   string
		fallback string
		want     string
	}{
		{name: "us stocks", market: "us-stocks", fallback: "us-options", want: "us-options"},
		{name: "us underlying feed", market: "us-underlying", fallback: "us-options", want: "us-options"},
		{name: "us shorthand", market: "us", fallback: "us-options", want: "us-options"},
		{name: "crypto spot", market: "crypto-spot", fallback: "us-options", want: "crypto-options"},
		{name: "crypto underlying feed", market: "crypto-underlying", fallback: "us-options", want: "crypto-options"},
		{name: "explicit options", market: "us-options", fallback: "crypto-options", want: "us-options"},
		{name: "empty", market: "", fallback: "us-options", want: "us-options"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := volatilityFeatureMarket(tt.market, tt.fallback); got != tt.want {
				t.Fatalf("volatilityFeatureMarket(%q, %q) = %q, want %q", tt.market, tt.fallback, got, tt.want)
			}
		})
	}
}
