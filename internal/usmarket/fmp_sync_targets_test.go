package usmarket

import (
	"context"
	"testing"
)

func TestBuildStoredStockSyncTargetUsesIndexAlias(t *testing.T) {
	target := buildStoredStockSyncTarget("SPX", USStockSyncTargetProviderFMP)
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
	target := buildStoredStockSyncTarget("AAPL", USStockSyncTargetProviderFMP)
	if target.FetchSymbol != "AAPL" || target.Source != "stored-stock" {
		t.Fatalf("unexpected regular stock target: %+v", target)
	}
}

func TestStoreSymbolsFromSyncTargetsDeduplicatesAndNormalizes(t *testing.T) {
	targets := []FMPStockSyncTarget{
		{StoreSymbol: " spx ", FetchSymbol: "^GSPC"},
		{StoreSymbol: "AAPL", FetchSymbol: "AAPL"},
		{StoreSymbol: "SPX", FetchSymbol: "^GSPC"},
		{StoreSymbol: "", FetchSymbol: "IGNORED"},
	}
	got := storeSymbolsFromSyncTargets(targets)
	want := []string{"SPX", "AAPL"}
	if len(got) != len(want) {
		t.Fatalf("expected %d symbols, got %d: %#v", len(want), len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected symbols: want %#v got %#v", want, got)
		}
	}
}

func TestFetchSymbolsFromSyncTargetsDeduplicatesAndNormalizes(t *testing.T) {
	targets := []FMPStockSyncTarget{
		{FetchSymbol: " qqq "},
		{FetchSymbol: "SPY"},
		{FetchSymbol: "QQQ"},
		{FetchSymbol: ""},
	}
	got := FetchSymbolsFromSyncTargets(targets)
	want := []string{"QQQ", "SPY"}
	if len(got) != len(want) {
		t.Fatalf("expected %d symbols, got %d: %#v", len(want), len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected symbols: want %#v got %#v", want, got)
		}
	}
}

func TestResolveUSStockSyncTargetsWithOptionsOverrideWinsAlias(t *testing.T) {
	targets, err := ResolveUSStockSyncTargetsWithOptions(context.Background(), nil, []string{"NDX", "SPY"}, 0, USStockSyncTargetResolverOptions{
		Provider:       USStockSyncTargetProviderFMP,
		FetchOverrides: map[string]string{"NDX": "QQQ"},
	})
	if err != nil {
		t.Fatalf("resolve targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %#v", len(targets), targets)
	}
	if targets[0].StoreSymbol != "NDX" || targets[0].FetchSymbol != "QQQ" {
		t.Fatalf("expected NDX to fetch via QQQ override, got %+v", targets[0])
	}
	if targets[0].Source != "explicit-index-alias-fetch-override" {
		t.Fatalf("unexpected override source: %+v", targets[0])
	}
	if targets[1].StoreSymbol != "SPY" || targets[1].FetchSymbol != "SPY" {
		t.Fatalf("unexpected second target: %+v", targets[1])
	}
}

func TestNormalizeUSStockSyncTargetsDeduplicatesByStoreAndUppercasesFetch(t *testing.T) {
	targets := []FMPStockSyncTarget{
		{StoreSymbol: " ndx ", FetchSymbol: "qqq", Source: "a"},
		{StoreSymbol: "NDX", FetchSymbol: "^IXIC", Source: "b"},
		{StoreSymbol: "spy", FetchSymbol: "", Source: "c"},
	}
	got := NormalizeUSStockSyncTargets(targets)
	if len(got) != 2 {
		t.Fatalf("expected 2 targets, got %d: %#v", len(got), got)
	}
	if got[0].StoreSymbol != "NDX" || got[0].FetchSymbol != "QQQ" || got[0].Source != "a" {
		t.Fatalf("unexpected first target: %+v", got[0])
	}
	if got[1].StoreSymbol != "SPY" || got[1].FetchSymbol != "SPY" || got[1].Source != "c" {
		t.Fatalf("unexpected second target: %+v", got[1])
	}
}

func TestResolveUSStockSyncTargetsWithOptionsNormalizesOverrideKeys(t *testing.T) {
	targets, err := ResolveUSStockSyncTargetsWithOptions(context.Background(), nil, []string{"NDX"}, 0, USStockSyncTargetResolverOptions{
		Provider:       USStockSyncTargetProviderFMP,
		FetchOverrides: map[string]string{" ndx ": " qqq "},
	})
	if err != nil {
		t.Fatalf("resolve targets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d: %#v", len(targets), targets)
	}
	if targets[0].FetchSymbol != "QQQ" {
		t.Fatalf("expected normalized override fetch symbol QQQ, got %+v", targets[0])
	}
}
