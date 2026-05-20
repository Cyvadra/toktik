package service

import "testing"

func TestResolveVirtualFundamentalMacroTarget(t *testing.T) {
	tests := []struct {
		name          string
		market        string
		symbol        string
		factor        string
		wantOK        bool
		wantDataset   string
		wantRefSymbol string
	}{
		{name: "spy direct", market: "us-stocks", symbol: "SPY", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPSP500Shiller, wantRefSymbol: "SPY"},
		{name: "spx alias", market: "us-stocks", symbol: "SPX", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPSP500Shiller, wantRefSymbol: "SPY"},
		{name: "qqq direct", market: "us-stocks", symbol: "QQQ", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPNDXShiller, wantRefSymbol: "QQQ"},
		{name: "ndx alias", market: "us-stocks", symbol: "NDX", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPNDXShiller, wantRefSymbol: "QQQ"},
		{name: "other symbol", market: "us-stocks", symbol: "IWM", factor: "pe10_live", wantOK: false},
		{name: "other factor", market: "us-stocks", symbol: "SPY", factor: "pe", wantOK: false},
		{name: "other market", market: "crypto-spot", symbol: "SPY", factor: "pe10_live", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveVirtualFundamentalMacroTarget(tc.market, tc.symbol, tc.factor)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Dataset != tc.wantDataset {
				t.Fatalf("dataset=%q want %q", got.Dataset, tc.wantDataset)
			}
			if got.ReferenceSymbol != tc.wantRefSymbol {
				t.Fatalf("reference symbol=%q want %q", got.ReferenceSymbol, tc.wantRefSymbol)
			}
		})
	}
}

func TestAppendVirtualFundamentalCatalogEntries(t *testing.T) {
	provider := newVirtualFundamentalsProvider(nil)
	entries := provider.appendCatalogEntries(nil, "us-stocks")
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].FactorCode != virtualFundamentalFactorPE10Live {
		t.Fatalf("factor=%q want %q", entries[0].FactorCode, virtualFundamentalFactorPE10Live)
	}

	entries = provider.appendCatalogEntries(entries, "us-stocks")
	if len(entries) != 1 {
		t.Fatalf("expected deduplicated virtual factor, got %d entries", len(entries))
	}

	otherMarket := provider.appendCatalogEntries(nil, "crypto-spot")
	if len(otherMarket) != 0 {
		t.Fatalf("expected no virtual entries for crypto-spot, got %d", len(otherMarket))
	}
}

func TestSplitFundamentalFactorSelection(t *testing.T) {
	selection := splitFundamentalFactorSelection([]string{"pb", virtualFundamentalFactorPE10Live, "pe"})
	if !selection.includePE10Live {
		t.Fatalf("expected virtual pe10_live to be selected")
	}
	if len(selection.base) != 2 {
		t.Fatalf("len(base)=%d want 2", len(selection.base))
	}
	if selection.base[0] != "pb" || selection.base[1] != "pe" {
		t.Fatalf("base=%v want [pb pe]", selection.base)
	}

	selection = splitFundamentalFactorSelection([]string{virtualFundamentalFactorPE10Live})
	if !selection.includePE10Live {
		t.Fatalf("expected virtual-only selection to include pe10_live")
	}
	if selection.base != nil {
		t.Fatalf("expected nil base factors for virtual-only selection, got %v", selection.base)
	}
}
