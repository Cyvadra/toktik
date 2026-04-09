package main

import (
	"reflect"
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
