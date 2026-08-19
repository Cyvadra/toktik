package backtest

import (
	"math"
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
	if got := provider.AvailableContractsFor(ts, "us-options", "AAPL"); len(got) != 1 {
		t.Fatalf("expected us-options alias to return one contract, got %d", len(got))
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

func TestOptionsChainExpirationsExpiryAndMinIV(t *testing.T) {
	first := time.Date(2026, 9, 18, 20, 0, 0, 0, time.UTC)
	second := time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)
	chain := NewOptionsChain([]OptionContract{
		{Symbol: "SECOND", Expiration: second, StrikePrice: 210, IV: 0.18},
		{Symbol: "FIRST-HIGH", Expiration: first, StrikePrice: 205, IV: 0.24},
		{Symbol: "FIRST-TIE-B", Expiration: first, StrikePrice: 200, IV: 0.15},
		{Symbol: "FIRST-TIE-A", Expiration: first, StrikePrice: 200, IV: 0.15},
		{Symbol: "FIRST-ZERO", Expiration: first, StrikePrice: 190, IV: 0},
		{Symbol: "FIRST-NAN", Expiration: first, StrikePrice: 195, IV: math.NaN()},
	}, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))

	expirations := chain.Expirations()
	if len(expirations) != 2 || !expirations[0].Equal(first) || !expirations[1].Equal(second) {
		t.Fatalf("Expirations() = %v, want [%v %v]", expirations, first, second)
	}

	firstChain := chain.Expiry(first)
	if firstChain.Len() != 5 {
		t.Fatalf("Expiry(first).Len() = %d, want 5", firstChain.Len())
	}
	minimum := firstChain.MinIV()
	if minimum == nil || minimum.Symbol != "FIRST-TIE-A" {
		t.Fatalf("MinIV() = %#v, want FIRST-TIE-A", minimum)
	}

	invalid := NewOptionsChain([]OptionContract{{IV: 0}, {IV: math.Inf(1)}}, time.Time{})
	if minimum := invalid.MinIV(); minimum != nil {
		t.Fatalf("invalid MinIV() = %#v, want nil", minimum)
	}

	lowest := firstChain.LowestIV(2)
	wantLowest := []string{"FIRST-TIE-A", "FIRST-TIE-B"}
	if len(lowest) != len(wantLowest) {
		t.Fatalf("LowestIV(2) len = %d, want %d", len(lowest), len(wantLowest))
	}
	for i, contract := range lowest {
		if contract.Symbol != wantLowest[i] {
			t.Fatalf("LowestIV(2)[%d] = %s, want %s", i, contract.Symbol, wantLowest[i])
		}
	}
	if got := len(firstChain.LowestIV(10)); got != 3 {
		t.Fatalf("LowestIV(10) len = %d, want 3 (excludes zero/NaN IV)", got)
	}
	if got := invalid.LowestIV(5); len(got) != 0 {
		t.Fatalf("invalid LowestIV(5) = %#v, want empty", got)
	}
}
