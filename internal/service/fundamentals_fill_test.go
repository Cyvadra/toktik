package service

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestBuildFilledFundamentalSeriesForwardFillsAcrossGrid(t *testing.T) {
	grid := []time.Time{
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
	}
	points := []dto.FundamentalSeriesPoint{
		{EventTS: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Value: 20, Source: "fmp"},
		{EventTS: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC), Value: 24, Source: "fmp"},
	}

	got := buildFilledFundamentalSeries(grid, points, fundamentalFillForwardFill, 0, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 filled points, got %d", len(got))
	}
	if !got[0].EventTS.Equal(grid[0]) || got[0].Value != 20 || !got[0].Filled {
		t.Fatalf("unexpected first filled point: %#v", got[0])
	}
	if !got[1].EventTS.Equal(grid[1]) || got[1].Value != 20 || !got[1].Filled {
		t.Fatalf("unexpected second filled point: %#v", got[1])
	}
	if !got[2].EventTS.Equal(grid[2]) || got[2].Value != 24 || got[2].Filled {
		t.Fatalf("unexpected third filled point: %#v", got[2])
	}
}

func TestBuildFilledFundamentalSeriesRespectsLimitedForwardFill(t *testing.T) {
	grid := []time.Time{
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	points := []dto.FundamentalSeriesPoint{{
		EventTS: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Value:   20,
	}}

	got := buildFilledFundamentalSeries(grid, points, fundamentalFillForwardLimited, 1, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 points under limited fill, got %d", len(got))
	}
	if !got[0].EventTS.Equal(grid[0]) || got[0].Filled {
		t.Fatalf("unexpected first limited point: %#v", got[0])
	}
	if !got[1].EventTS.Equal(grid[1]) || !got[1].Filled {
		t.Fatalf("unexpected second limited point: %#v", got[1])
	}
}

func TestBuildFilledFundamentalSeriesRecomputesPriceDerivedValues(t *testing.T) {
	grid := []time.Time{
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	}
	points := []dto.FundamentalSeriesPoint{{
		EventTS: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Value:   20,
	}}
	denominators := map[string]float64{
		fundamentalObservationKey("pe", points[0].EventTS): 10,
	}
	prices := map[time.Time]float64{
		grid[0]: 200,
		grid[1]: 250,
	}

	got := buildFilledFundamentalSeries(grid, points, fundamentalFillForwardFill, 0, func(gridTS time.Time, point dto.FundamentalSeriesPoint) float64 {
		return revaluePriceDerivedFundamental("pe", gridTS, point, denominators, prices)
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 price-derived points, got %d", len(got))
	}
	if got[0].Value != 20 {
		t.Fatalf("expected first point to keep event-day PE, got %v", got[0].Value)
	}
	if got[1].Value != 25 {
		t.Fatalf("expected second point to be recomputed from price grid, got %v", got[1].Value)
	}
}

func TestRevaluePriceDerivedFundamentalUsesGridPrice(t *testing.T) {
	gridTS := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	eventTS := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	point := dto.FundamentalSeriesPoint{EventTS: eventTS, Value: 20}
	denominators := map[string]float64{fundamentalObservationKey("pe", eventTS): 10}
	prices := map[time.Time]float64{gridTS: 250}

	if got := revaluePriceDerivedFundamental("pe", gridTS, point, denominators, prices); got != 25 {
		t.Fatalf("expected price-derived value 25, got %v", got)
	}
	if got := revaluePriceDerivedFundamental("market_cap", gridTS, point, denominators, prices); got != 20 {
		t.Fatalf("expected non price-derived value to stay unchanged, got %v", got)
	}
}

func TestSplitFundamentalFactorSelectionKeepsBasePE(t *testing.T) {
	selection := splitFundamentalFactorSelection([]string{"pe", "pe10_live", "pb"})
	if len(selection.base) != 2 || selection.base[0] != "pe" || selection.base[1] != "pb" {
		t.Fatalf("unexpected base selection: %#v", selection.base)
	}
	if !selection.includePE {
		t.Fatal("expected PE virtual override to remain enabled")
	}
	if !selection.includePE10Live {
		t.Fatal("expected pe10_live virtual selection to remain enabled")
	}
}
