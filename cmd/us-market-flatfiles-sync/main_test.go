package main

import (
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

func TestResolveRequestedDateRangeParsesOverrideWindow(t *testing.T) {
	start, end, err := resolveRequestedDateRange("2022-05-01", "2022-12-31")
	if err != nil {
		t.Fatalf("resolveRequestedDateRange returned error: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2022-05-01" {
		t.Fatalf("unexpected start date: %s", got)
	}
	if got := end.Format("2006-01-02"); got != "2022-12-31" {
		t.Fatalf("unexpected end date: %s", got)
	}
}

func TestResolveRequestedDateRangeRejectsInvertedWindow(t *testing.T) {
	_, _, err := resolveRequestedDateRange("2022-12-31", "2022-05-01")
	if err == nil {
		t.Fatal("expected inverted range error")
	}
	if !strings.Contains(err.Error(), "before start-date") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverageBootstrapScopeUsesExclusiveUpperBound(t *testing.T) {
	start := time.Date(2022, 4, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2022, 12, 31, 0, 0, 0, 0, time.UTC)
	scope := coverageBootstrapScope(start, end)
	if !scope.From.Equal(start) {
		t.Fatalf("unexpected scope start: %s", scope.From.Format(time.RFC3339))
	}
	wantTo := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if !scope.To.Equal(wantTo) {
		t.Fatalf("unexpected scope end: %s", scope.To.Format(time.RFC3339))
	}
}
