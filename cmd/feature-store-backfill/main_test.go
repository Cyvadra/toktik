package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/service"
)

func TestResolveBackfillRangeIncremental(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	from, to, err := resolveBackfillRange("", "", 3, now)
	if err != nil {
		t.Fatalf("resolveBackfillRange returned error: %v", err)
	}
	expectedFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC)
	if !from.Equal(expectedFrom) || !to.Equal(expectedTo) {
		t.Fatalf("unexpected range: from=%s to=%s", from, to)
	}
}

func TestResolveBackfillRangeExplicitOverridesIncremental(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	from, to, err := resolveBackfillRange("2026-03-01", "2026-03-03", 7, now)
	if err != nil {
		t.Fatalf("resolveBackfillRange returned error: %v", err)
	}
	if !from.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected from: %s", from)
	}
	if !to.Equal(time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected to: %s", to)
	}
}

func TestResolveBackfillRangeRejectsNegativeIncrementalDays(t *testing.T) {
	_, _, err := resolveBackfillRange("", "", -1, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for negative incremental days")
	}
}

func TestFormatBackfillSummary(t *testing.T) {
	startedAt := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(1500 * time.Millisecond)
	summary := formatBackfillSummary(service.FeatureBackfillStats{
		MarketsProcessed:      2,
		UnderlyingsConsidered: 5,
		UnderlyingsWritten:    3,
		UnderlyingsSkipped:    1,
		UnderlyingsEmpty:      1,
		ScopesReplaced:        2,
		RowsWritten:           640,
		LookbackDays:          252,
		Failures:              []service.FeatureBackfillFailure{{Market: "us-options", Underlying: "AAPL", Stage: "insert-rows", Error: "boom"}},
	}, startedAt, finishedAt, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC), true)

	checks := []string{
		"markets=2",
		"underlyings_considered=5",
		"rows_written=640",
		"failures=1",
		"replace=true",
		"from=2026-04-01",
		"to=2026-04-03",
		"elapsed=1.5s",
	}
	for _, check := range checks {
		if !strings.Contains(summary, check) {
			t.Fatalf("summary missing %q: %s", check, summary)
		}
	}
}
