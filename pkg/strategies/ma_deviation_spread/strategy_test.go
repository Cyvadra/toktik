package madeviationspread

import (
	"testing"
	"time"
)

func TestComputeDivergenceSignalsTopAndBottom(t *testing.T) {
	topHigh := []float64{1, 5, 2, 6, 1}
	topLow := []float64{0, 1, 0, 1, 0}
	topDif := []float64{0, 10, 0, 7, 0}
	topHist := []float64{0, 8, 0, 3, 0}
	top, bot := computeDivergenceSignals(topHigh, topLow, topDif, topHist, 1)
	if top[4] != 1 {
		t.Fatalf("expected bearish divergence confirmation at index 4, got %v", top[4])
	}
	if bot[4] != 0 {
		t.Fatalf("unexpected bullish divergence at index 4")
	}

	botHigh2 := []float64{5, 6, 5, 6, 5}
	botLow2 := []float64{5, 2, 4, 1, 3}
	botDif2 := []float64{0, -10, 0, -6, 0}
	botHist2 := []float64{0, -8, 0, -2, 0}
	top2, bot2 := computeDivergenceSignals(botHigh2, botLow2, botDif2, botHist2, 1)
	if bot2[4] != 1 {
		t.Fatalf("expected bullish divergence confirmation at index 4, got %v", bot2[4])
	}
	if top2[4] != 0 {
		t.Fatalf("unexpected bearish divergence at index 4")
	}
}

func TestBuildEntrySignalColumns(t *testing.T) {
	timestamps := []time.Time{
		time.Date(2022, 12, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 4, 0, 0, 0, time.UTC),
	}
	open := []float64{10, 10, 10, 10, 11, 10}
	close := []float64{9, 10, 10, 10, 10, 11}
	divWideTop := []float64{1, 1, 0, 0, 0, 0}
	divQuickBot := []float64{0, 0, 0, 0, 0, 1}
	rsi12h := []float64{60, 60, 55, 55, 55, 45}
	volCondition := []float64{1, 1, 1, 1, 1, 1}

	barsSinceWideTop, condLong, condShort := buildEntrySignalColumns(
		timestamps,
		open,
		close,
		divWideTop,
		divQuickBot,
		rsi12h,
		volCondition,
		5,
	)

	if barsSinceWideTop[0] != 0 {
		t.Fatalf("expected barsSince to reset on the signal bar, got %v", barsSinceWideTop[0])
	}
	if condLong[0] != 0 || condShort[0] != 0 {
		t.Fatalf("expected no tradable signals before 2023: long=%v short=%v", condLong[0], condShort[0])
	}
	if condShort[4] != 1 {
		t.Fatalf("expected short signal at index 4, got %v", condShort[4])
	}
	if condLong[5] != 1 {
		t.Fatalf("expected long signal at index 5, got %v", condLong[5])
	}
	if condLong[1] != 0 || condShort[1] != 0 {
		t.Fatalf("unexpected signals on index 1: long=%v short=%v", condLong[1], condShort[1])
	}
}
