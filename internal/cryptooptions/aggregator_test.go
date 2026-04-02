package cryptooptions

import (
	"math"
	"testing"
	"time"
)

func TestSpotAggregationUsesThreeNearestUnderlyingIndices(t *testing.T) {
	t.Parallel()

	minute := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	agg := NewAggregator()

	ticks := []TickRow{
		{
			Symbol:          "BTC-2JAN23-10000-C",
			Timestamp:       minute.Add(100 * time.Millisecond),
			UnderlyingIndex: "BTC-2JAN23",
			UnderlyingPrice: 10,
		},
		{
			Symbol:          "BTC-6JAN23-10000-C",
			Timestamp:       minute.Add(200 * time.Millisecond),
			UnderlyingIndex: "BTC-6JAN23",
			UnderlyingPrice: 20,
		},
		{
			Symbol:          "BTC-20JAN23-10000-C",
			Timestamp:       minute.Add(59*time.Second + 900*time.Millisecond),
			UnderlyingIndex: "SYN.BTC-20JAN23",
			UnderlyingPrice: 30,
		},
		{
			Symbol:          "BTC-31MAR23-10000-C",
			Timestamp:       minute.Add(300 * time.Millisecond),
			UnderlyingIndex: "BTC-31MAR23",
			UnderlyingPrice: 1000,
		},
	}

	for _, tick := range ticks {
		agg.Add(tick)
	}

	var got []SpotBar1m
	if _, err := agg.FlushSortedSpotBatches(10, func(batch []SpotBar1m) error {
		got = append(got, batch...)
		return nil
	}); err != nil {
		t.Fatalf("FlushSortedSpotBatches: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("bar count = %d, want 1", len(got))
	}
	bar := got[0]

	if bar.TickCount != 3 {
		t.Fatalf("tick_count = %d, want 3", bar.TickCount)
	}
	if bar.PriceSource != "mixed" {
		t.Fatalf("price_source = %q, want mixed", bar.PriceSource)
	}
	assertFloat32Near(t, "open", bar.Open, 15, 1e-6)
	assertFloat32Near(t, "close", bar.Close, 30, 1e-6)
	assertFloat32Near(t, "high", bar.High, 29, 1e-6)
	assertFloat32Near(t, "low", bar.Low, 11, 1e-6)
}

func TestNearestUnderlyingIndicesHandlesCrossYear(t *testing.T) {
	t.Parallel()

	minute := time.Date(2023, time.December, 31, 23, 59, 0, 0, time.UTC)
	observations := []spotObservation{
		{timestamp: minute, underlyingIndex: "BTC-29DEC23", price: 1},
		{timestamp: minute, underlyingIndex: "BTC-5JAN24", price: 2},
		{timestamp: minute, underlyingIndex: "SYN.BTC-12JAN24", price: 3},
		{timestamp: minute, underlyingIndex: "BTC-29MAR24", price: 4},
	}

	got := nearestUnderlyingIndices(observations, minute, 3)
	want := []string{"BTC-29DEC23", "BTC-5JAN24", "SYN.BTC-12JAN24"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func assertFloat32Near(t *testing.T, field string, got, want, tolerance float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(tolerance) {
		t.Fatalf("%s = %.6f, want %.6f", field, got, want)
	}
}
