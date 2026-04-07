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

func TestSplitUSBackfillWindowsUsesDaySizedChunks(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 4, 1, 13, 30, 0, 0, time.UTC)
	to := time.Date(2026, 4, 3, 20, 0, 0, 0, time.UTC)
	windows := splitUSBackfillWindows(from, to)
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	if !windows[0].From.Equal(from) || !windows[0].To.Equal(time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first window: %+v", windows[0])
	}
	if !windows[1].From.Equal(time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)) || !windows[1].To.Equal(time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected middle window: %+v", windows[1])
	}
	if !windows[2].From.Equal(time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)) || !windows[2].To.Equal(to) {
		t.Fatalf("unexpected last window: %+v", windows[2])
	}
}
