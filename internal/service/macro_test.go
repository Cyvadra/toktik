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

func TestBuildExpandedMacroDailySeriesWithoutReferenceBarsKeepsEarlyCBOEVIXHistory(t *testing.T) {
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC)
	observations := map[string][]macroObservation{
		"close": {
			{FactorCode: "close", EventTS: time.Date(2019, 12, 31, 21, 0, 0, 0, time.UTC), KnownAt: time.Date(2019, 12, 31, 21, 0, 0, 0, time.UTC), Value: 13.78, Source: "cboe", ReferenceMarket: defaultMacroReferenceMarket, ReferenceSymbol: "SPY"},
			{FactorCode: "close", EventTS: time.Date(2020, 1, 2, 21, 0, 0, 0, time.UTC), KnownAt: time.Date(2020, 1, 2, 21, 0, 0, 0, time.UTC), Value: 12.47, Source: "cboe", ReferenceMarket: defaultMacroReferenceMarket, ReferenceSymbol: "SPY"},
		},
	}

	points := buildExpandedMacroDailySeriesWithoutReferenceBars([]string{"close"}, observations, nil, defaultMacroReferenceMarket, "SPY", from, to)
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}
	if got := points[0].Timestamp; !got.Equal(from) {
		t.Fatalf("first timestamp=%s want %s", got, from)
	}
	if got := points[0].Value; math.Abs(got-13.78) > 1e-9 {
		t.Fatalf("first value=%v want 13.78", got)
	}
	if got := points[2].Timestamp; !got.Equal(time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("third timestamp=%s want 2020-01-03", got)
	}
	if got := points[2].Value; math.Abs(got-12.47) > 1e-9 {
		t.Fatalf("third value=%v want 12.47", got)
	}
	for _, point := range points {
		if !point.Filled {
			t.Fatalf("expected filled point, got %+v", point)
		}
		if point.Realtime {
			t.Fatalf("expected non-realtime point, got %+v", point)
		}
	}
}

func TestBuildDeribitDVOLAggregateSeriesUsesOHLCRules(t *testing.T) {
	from := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	observations := map[string][]macroObservation{
		"open": {
			{FactorCode: "open", EventTS: from, KnownAt: from, Value: 50, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
			{FactorCode: "open", EventTS: from.Add(time.Hour), KnownAt: from.Add(time.Hour), Value: 52, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
		},
		"high": {
			{FactorCode: "high", EventTS: from, KnownAt: from, Value: 55, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
			{FactorCode: "high", EventTS: from.Add(time.Hour), KnownAt: from.Add(time.Hour), Value: 58, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
		},
		"low": {
			{FactorCode: "low", EventTS: from, KnownAt: from, Value: 49, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
			{FactorCode: "low", EventTS: from.Add(time.Hour), KnownAt: from.Add(time.Hour), Value: 48, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
		},
		"close": {
			{FactorCode: "close", EventTS: from, KnownAt: from, Value: 53, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
			{FactorCode: "close", EventTS: from.Add(time.Hour), KnownAt: from.Add(time.Hour), Value: 57, Source: "deribit", ReferenceMarket: "crypto", ReferenceSymbol: "BTC"},
		},
	}

	points := buildDeribitDVOLAggregateSeries([]string{"open", "high", "low", "close"}, observations, "2h", from, to)
	if len(points) != 4 {
		t.Fatalf("len(points)=%d want 4", len(points))
	}
	values := map[string]float64{}
	for _, point := range points {
		values[point.Factor] = point.Value
		if !point.Timestamp.Equal(from) || !point.EventTS.Equal(from) {
			t.Fatalf("unexpected timestamp for %+v", point)
		}
		if point.ReferenceMarket != "crypto" || point.ReferenceSymbol != "BTC" || !point.Filled {
			t.Fatalf("unexpected metadata for %+v", point)
		}
	}
	if values["open"] != 50 || values["high"] != 58 || values["low"] != 48 || values["close"] != 57 {
		t.Fatalf("unexpected OHLC values: %#v", values)
	}
}
