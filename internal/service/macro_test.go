package service

import (
	"math"
	"testing"
	"time"
)

func TestCollectMacroAnchorEventTimestampsKeepsCurrentAndFutureWindowObservations(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	first := time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC)
	second := time.Date(2026, 5, 29, 19, 59, 0, 0, time.UTC)
	third := time.Date(2026, 6, 30, 19, 59, 0, 0, time.UTC)

	got := collectMacroAnchorEventTimestamps(map[string][]macroObservation{
		"pe10": {
			{EventTS: first, KnownAt: time.Date(2026, 4, 7, 13, 30, 0, 0, time.UTC)},
			{EventTS: second, KnownAt: time.Date(2026, 5, 8, 13, 30, 0, 0, time.UTC)},
			{EventTS: third, KnownAt: time.Date(2026, 6, 8, 13, 30, 0, 0, time.UTC)},
		},
	}, from, to)

	if len(got) != 2 {
		t.Fatalf("expected 2 timestamps, got %d (%v)", len(got), got)
	}
	if !got[0].Equal(first) || !got[1].Equal(second) {
		t.Fatalf("unexpected timestamps: %v", got)
	}
}

func TestResolveMacroAnchorValuePrefersRequestedSymbolAnchor(t *testing.T) {
	eventTS := time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC)
	current := macroObservation{EventTS: eventTS, ReferenceSymbol: "SPX", AnchorValue: 7139.24}
	got := resolveMacroAnchorValue(current, map[time.Time]float64{eventTS: 739.09}, "SPY")
	if math.Abs(got-739.09) > 1e-9 {
		t.Fatalf("expected requested symbol anchor, got %v", got)
	}
}

func TestResolveMacroAnchorValueFallsBackToStoredAnchorForMatchingSymbol(t *testing.T) {
	eventTS := time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC)
	current := macroObservation{EventTS: eventTS, ReferenceSymbol: "SPX", AnchorValue: 7139.24}
	got := resolveMacroAnchorValue(current, nil, "SPX")
	if math.Abs(got-7139.24) > 1e-9 {
		t.Fatalf("expected stored anchor fallback, got %v", got)
	}
}

func TestResolveMacroAnchorValueDoesNotFallbackAcrossSymbols(t *testing.T) {
	eventTS := time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC)
	current := macroObservation{EventTS: eventTS, ReferenceSymbol: "SPX", AnchorValue: 7139.24}
	got := resolveMacroAnchorValue(current, nil, "SPY")
	if !math.IsNaN(got) {
		t.Fatalf("expected NaN when requested symbol anchor is missing, got %v", got)
	}
}

func TestVirtualMacroFactorsForFMPShillerDatasets(t *testing.T) {
	for _, dataset := range []string{macroDatasetFMPSP500Shiller, macroDatasetFMPNDXShiller} {
		factors := virtualMacroFactorsForDataset(dataset, nil)
		if _, ok := factors["pe10_live"]; !ok {
			t.Fatalf("expected pe10_live virtual factor for %s", dataset)
		}
		if _, ok := factors["cape_earnings_yield_live"]; !ok {
			t.Fatalf("expected cape_earnings_yield_live virtual factor for %s", dataset)
		}
	}
}
