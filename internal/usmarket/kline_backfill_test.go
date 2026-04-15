package usmarket

import (
	"testing"
	"time"
)

func TestNormalizeBackfillIntervalsDefaultsToSupportedWindows(t *testing.T) {
	t.Parallel()

	got, err := normalizeBackfillIntervals(nil)
	if err != nil {
		t.Fatalf("normalizeBackfillIntervals returned error: %v", err)
	}
	want := []string{"5m", "15m", "30m", "1h", "2h", "4h", "1d"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestNormalizeBackfillIntervalsAcceptsIntradayWindows(t *testing.T) {
	t.Parallel()

	got, err := normalizeBackfillIntervals([]string{"1h", "1d", "1h"})
	if err != nil {
		t.Fatalf("normalizeBackfillIntervals returned error: %v", err)
	}
	want := []string{"1h", "1d"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestNormalizeBackfillIntervalsRejectsUnsupportedWindow(t *testing.T) {
	t.Parallel()

	if _, err := normalizeBackfillIntervals([]string{"3h"}); err == nil {
		t.Fatalf("expected error for unsupported interval")
	}
}

func TestSplitUSBackfillWindowsUsesMonthSizedChunks(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 4, 1, 13, 30, 0, 0, time.UTC)
	to := time.Date(2026, 6, 3, 20, 0, 0, 0, time.UTC)
	windows := splitUSBackfillWindows(from, to)
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	if !windows[0].From.Equal(from) || !windows[0].To.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first window: %+v", windows[0])
	}
	if !windows[1].From.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) || !windows[1].To.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected middle window: %+v", windows[1])
	}
	if !windows[2].From.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) || !windows[2].To.Equal(to) {
		t.Fatalf("unexpected last window: %+v", windows[2])
	}
}

func TestNeedsAggregateCoverageBackfillDetectsLeadingHistoryGap(t *testing.T) {
	t.Parallel()

	sourceFrom := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	sourceTo := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	aggMin := time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC)
	aggMax := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)

	needsBackfill, reason, err := needsAggregateCoverageBackfill(sourceFrom, sourceTo, 4, aggMin, aggMax)
	if err != nil {
		t.Fatalf("needsAggregateCoverageBackfill returned error: %v", err)
	}
	if !needsBackfill {
		t.Fatalf("expected backfill to be required")
	}
	if reason == "" {
		t.Fatalf("expected a reason describing the gap")
	}
}

func TestNeedsAggregateCoverageBackfillSkipsCoveredRange(t *testing.T) {
	t.Parallel()

	sourceFrom := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	sourceTo := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	aggMin := time.Date(2023, 1, 3, 13, 30, 0, 0, time.UTC)
	aggMax := time.Date(2026, 4, 14, 19, 30, 0, 0, time.UTC)

	needsBackfill, reason, err := needsAggregateCoverageBackfill(sourceFrom, sourceTo, 1, aggMin, aggMax)
	if err != nil {
		t.Fatalf("needsAggregateCoverageBackfill returned error: %v", err)
	}
	if needsBackfill {
		t.Fatalf("expected no backfill, got reason: %s", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}
