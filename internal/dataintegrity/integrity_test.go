package dataintegrity

import (
	"testing"
	"time"
)

func TestNormalizeTargetsDefaultsAndAll(t *testing.T) {
	for _, input := range [][]string{nil, {}, {"all"}} {
		got := normalizeTargets(input)
		if len(got) != len(defaultTargets) {
			t.Fatalf("normalizeTargets(%v) len=%d, want %d", input, len(got), len(defaultTargets))
		}
		for i := range defaultTargets {
			if got[i] != defaultTargets[i] {
				t.Fatalf("normalizeTargets(%v)[%d]=%q, want %q", input, i, got[i], defaultTargets[i])
			}
		}
	}
}

func TestNormalizeTargetsSplitsDeduplicatesAndLowercases(t *testing.T) {
	got := normalizeTargets([]string{"US-OPTIONS-AGGREGATES, features", "features", " fundamentals "})
	want := []string{TargetUSOptionsAggregates, TargetFeatures, TargetFundamentals}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeSymbolsSplitsDeduplicatesSortsAndUppercases(t *testing.T) {
	got := normalizeSymbols([]string{"pltr, NFLX", "lite", "PLTR"})
	want := []string{"LITE", "NFLX", "PLTR"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestFeatureFindingSeverityAndRatio(t *testing.T) {
	ok := featureFinding("check", "table", 0, 10, "label")
	if ok.Severity != SeverityInfo || ok.MissingRatio != 0 {
		t.Fatalf("ok finding = %+v", ok)
	}

	bad := featureFinding("check", "table", 2, 10, "label")
	if bad.Severity != SeverityCritical {
		t.Fatalf("severity=%s, want %s", bad.Severity, SeverityCritical)
	}
	if bad.MissingRatio != 0.2 {
		t.Fatalf("ratio=%v, want 0.2", bad.MissingRatio)
	}
}

func TestSplitMonthlyWindowsInclusive(t *testing.T) {
	from := time.Date(2025, 8, 1, 13, 0, 0, 0, time.UTC)
	to := time.Date(2025, 10, 3, 23, 0, 0, 0, time.UTC)
	got := splitMonthlyWindowsInclusive(from, to)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	checks := []struct {
		index int
		from  string
		to    string
	}{
		{index: 0, from: "2025-08-01", to: "2025-08-31"},
		{index: 1, from: "2025-09-01", to: "2025-09-30"},
		{index: 2, from: "2025-10-01", to: "2025-10-03"},
	}
	for _, check := range checks {
		if got[check.index].From.Format("2006-01-02") != check.from || got[check.index].To.Format("2006-01-02") != check.to {
			t.Fatalf("window[%d]=%s..%s, want %s..%s", check.index, got[check.index].From.Format("2006-01-02"), got[check.index].To.Format("2006-01-02"), check.from, check.to)
		}
	}
}

func TestAppendSamplesRespectsLimit(t *testing.T) {
	dst := []string{"a", "b"}
	appendSamples(&dst, []string{"c", "d"}, 3)
	if len(dst) != 3 {
		t.Fatalf("len=%d, want 3", len(dst))
	}
	if dst[2] != "c" {
		t.Fatalf("dst[2]=%q, want c", dst[2])
	}
}
