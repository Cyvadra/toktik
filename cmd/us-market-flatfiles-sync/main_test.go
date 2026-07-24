package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/usmarket"
)

func TestColdStartAssetClassesIncludesAnyAssetWithoutData(t *testing.T) {
	states := []usmarket.FlatFileAssetState{
		{AssetClass: "stocks", HasData: true, LastImported: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)},
		{AssetClass: "options", HasData: false},
	}

	got := coldStartAssetClasses(states)
	want := []string{"options"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coldStartAssetClasses() = %v, want %v", got, want)
	}
}

func TestColdStartAssetClassesReturnsBothWhenDatabaseIsEmpty(t *testing.T) {
	states := []usmarket.FlatFileAssetState{
		{AssetClass: "stocks", HasData: false},
		{AssetClass: "options", HasData: false},
	}

	got := coldStartAssetClasses(states)
	want := []string{"stocks", "options"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coldStartAssetClasses() = %v, want %v", got, want)
	}
}

func TestResolveRequestedSyncScopeParsesExplicitDates(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "dates-*.txt")
	if err != nil {
		t.Fatalf("create temp dates file: %v", err)
	}
	if _, err := file.WriteString("2025-01-09\n# comment\n2025-02-21\n"); err != nil {
		t.Fatalf("write temp dates file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp dates file: %v", err)
	}

	start, end, dates, err := resolveRequestedSyncScope("", "", "2025-02-21,2025-01-09,2025-01-09", file.Name())
	if err != nil {
		t.Fatalf("resolveRequestedSyncScope explicit dates returned error: %v", err)
	}
	if !start.IsZero() || !end.IsZero() {
		t.Fatalf("expected zero range, got start=%s end=%s", start, end)
	}
	want := []string{"2025-01-09", "2025-02-21"}
	got := make([]string, 0, len(dates))
	for _, date := range dates {
		got = append(got, date.Format("2006-01-02"))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected explicit dates: got=%v want=%v", got, want)
	}
}

func TestResolveRequestedSyncScopeRejectsMixedRangeAndExplicitDates(t *testing.T) {
	_, _, _, err := resolveRequestedSyncScope("2025-01-01", "", "2025-01-09", "")
	if err == nil {
		t.Fatal("expected mixed scope error")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageBootstrapScopeUsesExclusiveUpperBound(t *testing.T) {
	start := time.Date(2022, 4, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2022, 12, 31, 0, 0, 0, 0, time.UTC)
	scope := coverageBootstrapScope(start, end, nil)
	if !scope.From.Equal(start) {
		t.Fatalf("unexpected scope start: %s", scope.From.Format(time.RFC3339))
	}
	wantTo := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if !scope.To.Equal(wantTo) {
		t.Fatalf("unexpected scope end: %s", scope.To.Format(time.RFC3339))
	}
}

func TestCoverageBootstrapScopeSkipsRangeBootstrapForExplicitDates(t *testing.T) {
	scope := coverageBootstrapScope(time.Time{}, time.Time{}, []time.Time{
		time.Date(2025, 2, 21, 15, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 9, 15, 0, 0, 0, time.UTC),
	})
	if !scope.From.IsZero() || !scope.To.IsZero() {
		t.Fatalf("expected explicit-date bootstrap scope to stay zero, got from=%s to=%s", scope.From, scope.To)
	}
}
