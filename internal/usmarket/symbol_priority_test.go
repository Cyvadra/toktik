package usmarket

import (
	"sort"
	"testing"
	"time"
)

func TestSortUSSymbolsByPriority(t *testing.T) {
	symbols := []string{"XOM", "VIX", "NVDA", "AAPL", "ZZZZ", "SPY", "MSFT"}
	infos := map[string]usSymbolPriorityInfo{
		"SPY":  {Symbol: "SPY", PresetRank: 0, HasDollarVolume: true, AverageDollarVolume: 10},
		"AAPL": {Symbol: "AAPL", PresetRank: 8, HasDollarVolume: true, AverageDollarVolume: 900},
		"MSFT": {Symbol: "MSFT", PresetRank: 9, HasDollarVolume: true, AverageDollarVolume: 800},
		"NVDA": {Symbol: "NVDA", PresetRank: 10, HasDollarVolume: true, AverageDollarVolume: 1000},
		"VIX":  {Symbol: "VIX", PresetRank: 25, HasDollarVolume: false},
		"XOM":  {Symbol: "XOM", PresetRank: -1, HasDollarVolume: true, AverageDollarVolume: 700},
		"ZZZZ": {Symbol: "ZZZZ", PresetRank: -1, HasDollarVolume: false},
	}
	got := sortUSSymbolsByPriority(symbols, infos)
	want := []string{"SPY", "AAPL", "MSFT", "NVDA", "VIX", "XOM", "ZZZZ"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected order at %d: got=%v want=%v", i, got, want)
		}
	}
}

func TestNormalizeUSPrioritySymbols(t *testing.T) {
	got := normalizeUSPrioritySymbols([]string{" spy ", "SPY", "aapl", "", " AAPL "})
	want := []string{"SPY", "AAPL"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected normalized symbols: got=%v want=%v", got, want)
		}
	}
}

func TestNormalizeUSPriorityOrder(t *testing.T) {
	if got, err := NormalizeUSPriorityOrder(""); err != nil || got != PriorityOrderUSDefault {
		t.Fatalf("unexpected default priority order: got=%q err=%v", got, err)
	}
	if got, err := NormalizeUSPriorityOrder("NONE"); err != nil || got != PriorityOrderNone {
		t.Fatalf("unexpected none priority order: got=%q err=%v", got, err)
	}
	if _, err := NormalizeUSPriorityOrder("weird"); err == nil {
		t.Fatal("expected invalid priority order error")
	}
}

func TestMissingOptionGreeksTaskPrioritySort(t *testing.T) {
	tasks := []MissingOptionGreeksTask{
		{Underlying: "XOM", MarketDate: mustDate("2026-05-16")},
		{Underlying: "SPY", MarketDate: mustDate("2026-05-17")},
		{Underlying: "SPY", MarketDate: mustDate("2026-05-15")},
	}
	infos := map[string]usSymbolPriorityInfo{
		"SPY": {Symbol: "SPY", PresetRank: 0},
		"XOM": {Symbol: "XOM", PresetRank: -1, HasDollarVolume: true, AverageDollarVolume: 10},
	}
	ordered := append([]MissingOptionGreeksTask(nil), tasks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := infos[ordered[i].Underlying]
		right := infos[ordered[j].Underlying]
		if compareUSSymbolPriority(left, right) {
			return true
		}
		if compareUSSymbolPriority(right, left) {
			return false
		}
		return ordered[i].MarketDate.Before(ordered[j].MarketDate)
	})
	if ordered[0].Underlying != "SPY" || !ordered[0].MarketDate.Equal(mustDate("2026-05-15")) {
		t.Fatalf("unexpected ordered tasks: %+v", ordered)
	}
	if ordered[1].Underlying != "SPY" || !ordered[1].MarketDate.Equal(mustDate("2026-05-17")) {
		t.Fatalf("unexpected ordered tasks: %+v", ordered)
	}
	if ordered[2].Underlying != "XOM" {
		t.Fatalf("unexpected ordered tasks: %+v", ordered)
	}
}

func mustDate(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}
