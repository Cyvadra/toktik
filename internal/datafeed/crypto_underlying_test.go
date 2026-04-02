package datafeed

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestProjectUnderlyingSpotQueryIncludesVolumeBase(t *testing.T) {
	query := projectUnderlyingSpotQuery("SELECT * FROM crypto_spot_bar_30m")

	if !strings.Contains(query, "volume_base") {
		t.Fatalf("projectUnderlyingSpotQuery() = %q, want volume_base in projection", query)
	}
	if !strings.Contains(query, "tick_count") {
		t.Fatalf("projectUnderlyingSpotQuery() = %q, want tick_count in projection", query)
	}
}

func TestBuildUnderlyingDataSetUsesVolumeBaseSeries(t *testing.T) {
	timestamps := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
	}
	ds := buildUnderlyingDataSet(
		timestamps,
		[]float64{100, 101},
		[]float64{102, 103},
		[]float64{99, 100},
		[]float64{101, 102},
		[]float64{2, 2},
		[]float64{12.5, 18.75},
		false,
	)

	volume := ds.Column("volume")
	tickCount := ds.Column("tick_count")
	fallback := ds.Column("compat_fallback")

	if len(volume) != 2 || len(tickCount) != 2 {
		t.Fatalf("unexpected series lengths: volume=%d tick_count=%d", len(volume), len(tickCount))
	}
	if volume[0] != 12.5 || volume[1] != 18.75 {
		t.Fatalf("volume = %v, want [12.5 18.75]", volume)
	}
	if tickCount[0] != 2 || tickCount[1] != 2 {
		t.Fatalf("tick_count = %v, want [2 2]", tickCount)
	}
	if len(fallback) != 2 || fallback[0] != 0 || fallback[1] != 0 {
		t.Fatalf("compat_fallback = %v, want [0 0]", fallback)
	}
}

func TestBuildUnderlyingDataSetUsesNaNVolumeForCompatibilityFallback(t *testing.T) {
	timestamps := []time.Time{time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)}
	ds := buildUnderlyingDataSet(
		timestamps,
		[]float64{100},
		[]float64{101},
		[]float64{99},
		[]float64{100.5},
		[]float64{math.NaN()},
		[]float64{math.NaN()},
		true,
	)

	volume := ds.Column("volume")
	fallback := ds.Column("compat_fallback")
	if len(volume) != 1 || !math.IsNaN(volume[0]) {
		t.Fatalf("volume = %v, want [NaN]", volume)
	}
	if len(fallback) != 1 || fallback[0] != 1 {
		t.Fatalf("compat_fallback = %v, want [1]", fallback)
	}
}
