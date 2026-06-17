package backtest

import (
	"testing"
	"time"
)

type testSnapshotSourceProvider struct {
	byTime map[int64][]OptionContract
}

func (p *testSnapshotSourceProvider) AvailableContracts(t time.Time) []OptionContract {
	return p.byTime[t.UTC().Unix()]
}

func TestSnapshotOptionsChainProviderCopiesContractsAndMatchesUSAliases(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	sourceContracts := []OptionContract{{Symbol: "AAPL-P-100", Underlying: "AAPL", UnderlyingMarket: "us", MarkPrice: 1.25}}
	source := &testSnapshotSourceProvider{byTime: map[int64][]OptionContract{ts.Unix(): sourceContracts}}

	snapshot := NewOptionsChainSnapshot(source, "us", "AAPL", []time.Time{ts})
	sourceContracts[0].MarkPrice = 9.99

	provider := NewSnapshotOptionsChainProvider(snapshot)
	contracts := provider.AvailableContractsFor(ts, "us-stocks", "AAPL")
	if len(contracts) != 1 {
		t.Fatalf("expected one contract, got %d", len(contracts))
	}
	if contracts[0].MarkPrice != 1.25 {
		t.Fatalf("snapshot contract was not independent from source: mark=%v", contracts[0].MarkPrice)
	}
	if got := provider.AvailableContractsFor(ts, "crypto", "AAPL"); len(got) != 0 {
		t.Fatalf("expected crypto market alias mismatch to return no contracts, got %d", len(got))
	}
}

func TestOptionsChainExpiryNextMonth(t *testing.T) {
	tests := []struct {
		name        string
		now         time.Time
		contracts   []OptionContract
		wantSymbols []string
	}{
		{
			name: "selects only next calendar month",
			now:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			contracts: []OptionContract{
				{Symbol: "JAN", Expiration: time.Date(2024, 1, 26, 8, 0, 0, 0, time.UTC)},
				{Symbol: "FEB-A", Expiration: time.Date(2024, 2, 2, 8, 0, 0, 0, time.UTC)},
				{Symbol: "FEB-B", Expiration: time.Date(2024, 2, 23, 8, 0, 0, 0, time.UTC)},
				{Symbol: "MAR", Expiration: time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)},
			},
			wantSymbols: []string{"FEB-A", "FEB-B"},
		},
		{
			name: "handles december to january rollover",
			now:  time.Date(2024, 12, 20, 12, 0, 0, 0, time.UTC),
			contracts: []OptionContract{
				{Symbol: "DEC", Expiration: time.Date(2024, 12, 27, 8, 0, 0, 0, time.UTC)},
				{Symbol: "JAN", Expiration: time.Date(2025, 1, 3, 8, 0, 0, 0, time.UTC)},
				{Symbol: "FEB", Expiration: time.Date(2025, 2, 7, 8, 0, 0, 0, time.UTC)},
			},
			wantSymbols: []string{"JAN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewOptionsChain(tt.contracts, tt.now)
			filtered := chain.ExpiryNextMonth()

			if filtered.Len() != len(tt.wantSymbols) {
				t.Fatalf("ExpiryNextMonth() len = %d, want %d", filtered.Len(), len(tt.wantSymbols))
			}

			for i, contract := range filtered.Contracts() {
				if contract.Symbol != tt.wantSymbols[i] {
					t.Fatalf("ExpiryNextMonth()[%d] = %s, want %s", i, contract.Symbol, tt.wantSymbols[i])
				}
			}
		})
	}
}
