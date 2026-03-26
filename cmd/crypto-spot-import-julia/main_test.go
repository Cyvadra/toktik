package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAllocateHourlyVolumeUsesNormalizedWeights(t *testing.T) {
	priceBars := []jsonMinuteBar{
		{VolumeSeed: 1},
		{VolumeSeed: 2},
		{VolumeSeed: 1},
	}

	allocations := allocateHourlyVolume(priceBars, []int{0, 1, 2}, 80)
	want := []uint32{20, 40, 20}
	if !reflect.DeepEqual(allocations, want) {
		t.Fatalf("allocations = %v, want %v", allocations, want)
	}
}

func TestAllocateHourlyVolumeFallsBackToUniformWeights(t *testing.T) {
	priceBars := []jsonMinuteBar{
		{VolumeSeed: 0},
		{VolumeSeed: -1},
		{VolumeSeed: 0},
		{VolumeSeed: 0},
	}

	allocations := allocateHourlyVolume(priceBars, []int{0, 1, 2, 3}, 10)
	want := []uint32{3, 3, 2, 2}
	if !reflect.DeepEqual(allocations, want) {
		t.Fatalf("allocations = %v, want %v", allocations, want)
	}
}

func TestMergeJSONPriceBarsWithCSVVolumes(t *testing.T) {
	hour1 := time.Date(2025, 6, 6, 16, 0, 0, 0, time.UTC)
	hour2 := hour1.Add(time.Hour)
	priceBars := []jsonMinuteBar{
		{Timestamp: hour1, Open: 1, High: 2, Low: 0.5, Close: 1.5, VolumeSeed: 1},
		{Timestamp: hour1.Add(time.Minute), Open: 1.5, High: 2.5, Low: 1, Close: 2, VolumeSeed: 2},
		{Timestamp: hour1.Add(2 * time.Minute), Open: 2, High: 3, Low: 1.5, Close: 2.5, VolumeSeed: 1},
		{Timestamp: hour2, Open: 3, High: 4, Low: 2.5, Close: 3.5, VolumeSeed: 0},
		{Timestamp: hour2.Add(time.Minute), Open: 3.5, High: 4.5, Low: 3, Close: 4, VolumeSeed: 0},
		{Timestamp: hour2.Add(2 * time.Minute), Open: 4, High: 5, Low: 3.5, Close: 4.5, VolumeSeed: 0},
	}

	hourlyVolumes := map[int64]float64{
		hour1.Unix(): 80,
		hour2.Unix(): 5,
	}

	bars, usedHours, ignoredHours, skippedHours, err := mergeJSONPriceBarsWithCSVVolumes(priceBars, hourlyVolumes, "BTC", "dual-source")
	if err != nil {
		t.Fatalf("mergeJSONPriceBarsWithCSVVolumes returned error: %v", err)
	}
	if usedHours != 2 {
		t.Fatalf("usedHours = %d, want 2", usedHours)
	}
	if ignoredHours != 0 {
		t.Fatalf("ignoredHours = %d, want 0", ignoredHours)
	}
	if skippedHours != 0 {
		t.Fatalf("skippedHours = %d, want 0", skippedHours)
	}

	gotVolumes := []uint32{
		bars[0].TickCount,
		bars[1].TickCount,
		bars[2].TickCount,
		bars[3].TickCount,
		bars[4].TickCount,
		bars[5].TickCount,
	}
	wantVolumes := []uint32{20, 40, 20, 2, 2, 1}
	if !reflect.DeepEqual(gotVolumes, wantVolumes) {
		t.Fatalf("tick counts = %v, want %v", gotVolumes, wantVolumes)
	}

	if bars[1].Open != 1.5 || bars[1].Close != 2 {
		t.Fatalf("OHLC was not preserved from JSON: open=%v close=%v", bars[1].Open, bars[1].Close)
	}
}

func TestMergeJSONPriceBarsWithCSVVolumesSkipsNonOverlappingHours(t *testing.T) {
	hour1 := time.Date(2025, 6, 6, 16, 0, 0, 0, time.UTC)
	hour2 := hour1.Add(time.Hour)
	priceBars := []jsonMinuteBar{
		{Timestamp: hour1, Open: 1, High: 2, Low: 0.5, Close: 1.5, VolumeSeed: 1},
		{Timestamp: hour1.Add(time.Minute), Open: 1.5, High: 2.5, Low: 1, Close: 2, VolumeSeed: 1},
		{Timestamp: hour2, Open: 3, High: 4, Low: 2.5, Close: 3.5, VolumeSeed: 1},
		{Timestamp: hour2.Add(time.Minute), Open: 3.5, High: 4.5, Low: 3, Close: 4, VolumeSeed: 1},
	}

	bars, usedHours, ignoredHours, skippedHours, err := mergeJSONPriceBarsWithCSVVolumes(priceBars, map[int64]float64{hour1.Unix(): 10}, "BTC", "dual-source")
	if err != nil {
		t.Fatalf("mergeJSONPriceBarsWithCSVVolumes returned error: %v", err)
	}
	if usedHours != 1 || ignoredHours != 0 || skippedHours != 1 {
		t.Fatalf("unexpected overlap stats: used=%d ignored=%d skipped=%d", usedHours, ignoredHours, skippedHours)
	}
	if len(bars) != 2 {
		t.Fatalf("len(bars) = %d, want 2", len(bars))
	}
}

func TestMergeJSONPriceBarsWithCSVVolumesErrorsWithoutAnyOverlap(t *testing.T) {
	hour := time.Date(2025, 6, 6, 16, 0, 0, 0, time.UTC)
	priceBars := []jsonMinuteBar{
		{Timestamp: hour, VolumeSeed: 1},
		{Timestamp: hour.Add(time.Minute), VolumeSeed: 1},
	}

	_, _, _, _, err := mergeJSONPriceBarsWithCSVVolumes(priceBars, map[int64]float64{}, "BTC", "dual-source")
	if err == nil {
		t.Fatal("expected no-overlap error, got nil")
	}
	if !strings.Contains(err.Error(), "no overlapping hours") {
		t.Fatalf("unexpected error: %v", err)
	}
}
