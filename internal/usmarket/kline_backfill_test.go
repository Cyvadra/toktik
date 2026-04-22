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

func TestChunkUSBackfillWindowsSplitsLargeRanges(t *testing.T) {
	t.Parallel()

	windows := chunkUSBackfillWindows([]usBackfillWindow{{
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
	}}, 7*24*time.Hour)

	if len(windows) != 3 {
		t.Fatalf("expected 3 chunked windows, got %d", len(windows))
	}
	if !windows[0].From.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !windows[0].To.Equal(time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first chunk: %+v", windows[0])
	}
	if !windows[1].From.Equal(time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)) || !windows[1].To.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected second chunk: %+v", windows[1])
	}
	if !windows[2].From.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) || !windows[2].To.Equal(time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected third chunk: %+v", windows[2])
	}
}

func TestMissingTradingDayWindowsMergesConsecutiveGaps(t *testing.T) {
	t.Parallel()

	sourceDays := []time.Time{
		time.Date(2023, 1, 20, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 23, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 24, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 25, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 26, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 27, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 30, 15, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 31, 15, 0, 0, 0, time.UTC),
	}
	aggDays := []time.Time{
		time.Date(2023, 1, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2023, 1, 30, 0, 0, 0, 0, time.UTC),
	}

	windows := missingTradingDayWindows(sourceDays, aggDays)
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	if !windows[0].From.Equal(time.Date(2023, 1, 23, 0, 0, 0, 0, time.UTC)) || !windows[0].To.Equal(time.Date(2023, 1, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first window: %+v", windows[0])
	}
	if !windows[1].From.Equal(time.Date(2023, 1, 25, 0, 0, 0, 0, time.UTC)) || !windows[1].To.Equal(time.Date(2023, 1, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected second window: %+v", windows[1])
	}
	if !windows[2].From.Equal(time.Date(2023, 1, 31, 0, 0, 0, 0, time.UTC)) || !windows[2].To.Equal(time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected third window: %+v", windows[2])
	}
}

func TestMissingTradingDayWindowsReturnsNilWhenAggregateCoversSourceDays(t *testing.T) {
	t.Parallel()

	days := []time.Time{
		time.Date(2026, 4, 1, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 2, 15, 0, 0, 0, time.UTC),
	}

	windows := missingTradingDayWindows(days, days)
	if len(windows) != 0 {
		t.Fatalf("expected no missing windows, got %+v", windows)
	}
}

func TestNormalizeAndSortTradingDaysDeduplicatesAndNormalizes(t *testing.T) {
	t.Parallel()

	days := []time.Time{
		time.Date(2026, 4, 2, 15, 30, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC),
		time.Date(2026, 4, 2, 10, 15, 0, 0, time.UTC),
	}

	normalized := normalizeAndSortTradingDays(days)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 days, got %d", len(normalized))
	}
	if !normalized[0].Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first day: %s", normalized[0].Format(time.RFC3339))
	}
	if !normalized[1].Equal(time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected second day: %s", normalized[1].Format(time.RFC3339))
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

func TestScopedSourceBoundsTreatsEmptyResultAsNoRows(t *testing.T) {
	t.Parallel()

	from, to, hasRows, err := scopedSourceBounds(
		0,
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("scopedSourceBounds returned error: %v", err)
	}
	if hasRows {
		t.Fatalf("expected empty result to report no rows, got from=%s to=%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	if !from.IsZero() || !to.IsZero() {
		t.Fatalf("expected zero bounds for empty result, got from=%s to=%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
}

func TestScopedSourceBoundsNormalizesInclusiveDateRange(t *testing.T) {
	t.Parallel()

	from, to, hasRows, err := scopedSourceBounds(
		2,
		time.Date(2022, 4, 19, 12, 0, 0, 0, time.UTC),
		time.Date(2022, 12, 31, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("scopedSourceBounds returned error: %v", err)
	}
	if !hasRows {
		t.Fatal("expected rows to be reported")
	}
	if got := from.Format("2006-01-02"); got != "2022-04-19" {
		t.Fatalf("unexpected from bound: %s", got)
	}
	if got := to.Format("2006-01-02"); got != "2023-01-01" {
		t.Fatalf("unexpected to bound: %s", got)
	}
}
