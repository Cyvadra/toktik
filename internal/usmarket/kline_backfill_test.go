package usmarket

import "testing"

func TestNormalizeBackfillIntervalsDefaultsToDaily(t *testing.T) {
	t.Parallel()

	got, err := normalizeBackfillIntervals(nil)
	if err != nil {
		t.Fatalf("normalizeBackfillIntervals returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "1d" {
		t.Fatalf("expected [1d], got %v", got)
	}
}

func TestNormalizeBackfillIntervalsRejectsNonDaily(t *testing.T) {
	t.Parallel()

	if _, err := normalizeBackfillIntervals([]string{"1h"}); err == nil {
		t.Fatalf("expected error for non-daily interval")
	}
}
