package forexmarket

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestIntradayChunkDays(t *testing.T) {
	tests := []struct {
		interval fmp.IntradayInterval
		want     int
	}{
		{interval: fmp.Interval1Min, want: 3},
		{interval: fmp.Interval5Min, want: 15},
		{interval: fmp.Interval15Min, want: 45},
		{interval: fmp.Interval30Min, want: 90},
		{interval: fmp.Interval1Hour, want: 180},
		{interval: fmp.Interval4Hour, want: 365},
	}
	for _, tt := range tests {
		if got := intradayChunkDays(tt.interval); got != tt.want {
			t.Fatalf("intradayChunkDays(%q) = %d, want %d", tt.interval, got, tt.want)
		}
	}
}

func TestFetchFMPIntradayChunkedFuncUsesIntervalAwareChunksAndNormalizesBars(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	var windows [][2]string

	bars, err := fetchFMPIntradayChunkedFunc(context.Background(), func(_ context.Context, symbol string, interval fmp.IntradayInterval, from, to string) ([]fmp.IntradayBar, error) {
		windows = append(windows, [2]string{from, to})
		if from == "2026-05-01" {
			return []fmp.IntradayBar{
				{Date: "2026-05-03 01:00:00", Open: 1.3, High: 1.4, Low: 1.2, Close: 1.35, Volume: 100},
				{Date: "2026-05-02 00:00:00", Open: 1.2, High: 1.3, Low: 1.1, Close: 1.25, Volume: 90},
				{Date: "2026-05-01 00:00:00", Open: 1.1, High: 1.2, Low: 1.0, Close: 1.15, Volume: 80},
			}, nil
		}
		return []fmp.IntradayBar{
			{Date: "2026-05-06 00:00:00", Open: 1.6, High: 1.7, Low: 1.5, Close: 1.65, Volume: 120},
			{Date: "2026-05-03 01:00:00", Open: 1.3, High: 1.4, Low: 1.2, Close: 1.35, Volume: 100},
			{Date: "2026-05-04 00:00:00", Open: 1.4, High: 1.5, Low: 1.3, Close: 1.45, Volume: 110},
		}, nil
	}, "EURUSD", fmp.Interval1Min, from, to)
	if err != nil {
		t.Fatalf("fetchFMPIntradayChunkedFunc returned error: %v", err)
	}

	wantWindows := [][2]string{{"2026-05-01", "2026-05-03"}, {"2026-05-04", "2026-05-06"}, {"2026-05-07", "2026-05-07"}}
	if !reflect.DeepEqual(windows, wantWindows) {
		t.Fatalf("chunk windows = %#v, want %#v", windows, wantWindows)
	}

	gotDates := make([]string, 0, len(bars))
	for _, bar := range bars {
		gotDates = append(gotDates, bar.Date)
	}
	wantDates := []string{
		"2026-05-01 00:00:00",
		"2026-05-02 00:00:00",
		"2026-05-03 01:00:00",
		"2026-05-04 00:00:00",
		"2026-05-06 00:00:00",
	}
	if !reflect.DeepEqual(gotDates, wantDates) {
		t.Fatalf("normalized dates = %#v, want %#v", gotDates, wantDates)
	}
}

func TestQueryAggregationSQLDoesNotRequireCompleteBucket(t *testing.T) {
	sql, err := QueryAggregationSQL("1d")
	if err != nil {
		t.Fatalf("QueryAggregationSQL returned error: %v", err)
	}
	forbidden := []string{"HAVING", "count()", "count(*)", "isNotNull(open) AND isNotNull(close)"}
	upper := strings.ToUpper(sql)
	for _, fragment := range forbidden {
		if strings.Contains(upper, strings.ToUpper(fragment)) {
			t.Fatalf("expected partial-data aggregation SQL, found forbidden fragment %q in %q", fragment, sql)
		}
	}
	if !strings.Contains(sql, "GROUP BY timestamp, symbol") {
		t.Fatalf("expected grouped aggregation SQL, got %q", sql)
	}
}

func TestSyncFMPKlinesReplaceDoesNotDeleteOnFetchError(t *testing.T) {
	originalFetch := fetchFMPIntradayBars
	originalDelete := deleteBarsForSymbolScope
	originalInsert := insertForexBars
	t.Cleanup(func() {
		fetchFMPIntradayBars = originalFetch
		deleteBarsForSymbolScope = originalDelete
		insertForexBars = originalInsert
	})

	fetchErr := errors.New("boom")
	deleteCalled := false
	insertCalled := false

	fetchFMPIntradayBars = func(_ context.Context, _ *fmp.Client, _ string, _ fmp.IntradayInterval, _, _ time.Time) ([]fmp.IntradayBar, error) {
		return nil, fetchErr
	}
	deleteBarsForSymbolScope = func(context.Context, driver.Conn, string, time.Time, time.Time) error {
		deleteCalled = true
		return nil
	}
	insertForexBars = func(context.Context, driver.Conn, <-chan Bar1m, int) (int64, error) {
		insertCalled = true
		return 0, nil
	}

	result, err := SyncFMPKlines(context.Background(), nil, FMPKlineSyncConfig{
		APIKey:   "test-key",
		Symbols:  []string{"EURUSD"},
		From:     time.Date(2022, 5, 29, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2022, 5, 31, 0, 0, 0, 0, time.UTC),
		Interval: fmp.Interval1Min,
		Replace:  true,
	})
	if err != nil {
		t.Fatalf("SyncFMPKlines returned error: %v", err)
	}
	if result.FailedSymbols != 1 {
		t.Fatalf("FailedSymbols = %d, want 1", result.FailedSymbols)
	}
	if deleteCalled {
		t.Fatal("expected replace delete to be skipped when fetch fails")
	}
	if insertCalled {
		t.Fatal("expected insert to be skipped when fetch fails")
	}
	if result.ProcessedSymbols != 0 {
		t.Fatalf("ProcessedSymbols = %d, want 0", result.ProcessedSymbols)
	}
	if result.FetchedBars != 0 {
		t.Fatalf("FetchedBars = %d, want 0", result.FetchedBars)
	}
	if result.InsertedRows != 0 {
		t.Fatalf("InsertedRows = %d, want 0", result.InsertedRows)
	}
}

func TestSyncFMPKlinesReplaceDeletesOnEmptyFetch(t *testing.T) {
	originalFetch := fetchFMPIntradayBars
	originalDelete := deleteBarsForSymbolScope
	originalInsert := insertForexBars
	t.Cleanup(func() {
		fetchFMPIntradayBars = originalFetch
		deleteBarsForSymbolScope = originalDelete
		insertForexBars = originalInsert
	})

	deleteCalled := false
	insertCalled := false

	fetchFMPIntradayBars = func(_ context.Context, _ *fmp.Client, _ string, _ fmp.IntradayInterval, _, _ time.Time) ([]fmp.IntradayBar, error) {
		return []fmp.IntradayBar{}, nil
	}
	deleteBarsForSymbolScope = func(context.Context, driver.Conn, string, time.Time, time.Time) error {
		deleteCalled = true
		return nil
	}
	insertForexBars = func(context.Context, driver.Conn, <-chan Bar1m, int) (int64, error) {
		insertCalled = true
		return 0, nil
	}

	result, err := SyncFMPKlines(context.Background(), nil, FMPKlineSyncConfig{
		APIKey:   "test-key",
		Symbols:  []string{"EURUSD"},
		From:     time.Date(2022, 5, 29, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2022, 5, 31, 0, 0, 0, 0, time.UTC),
		Interval: fmp.Interval1Min,
		Replace:  true,
	})
	if err != nil {
		t.Fatalf("SyncFMPKlines returned error: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected replace delete to run when fetch succeeds with no bars")
	}
	if insertCalled {
		t.Fatal("expected insert to be skipped when no bars are returned")
	}
	if result.ProcessedSymbols != 1 {
		t.Fatalf("ProcessedSymbols = %d, want 1", result.ProcessedSymbols)
	}
	if result.FailedSymbols != 0 {
		t.Fatalf("FailedSymbols = %d, want 0", result.FailedSymbols)
	}
	if result.InsertedRows != 0 {
		t.Fatalf("InsertedRows = %d, want 0", result.InsertedRows)
	}
	if result.FetchedBars != 0 {
		t.Fatalf("FetchedBars = %d, want 0", result.FetchedBars)
	}
}

func TestSyncFMPKlinesDropsVendorVolume(t *testing.T) {
	originalFetch := fetchFMPIntradayBars
	originalDelete := deleteBarsForSymbolScope
	originalInsert := insertForexBars
	t.Cleanup(func() {
		fetchFMPIntradayBars = originalFetch
		deleteBarsForSymbolScope = originalDelete
		insertForexBars = originalInsert
	})

	fetchFMPIntradayBars = func(_ context.Context, _ *fmp.Client, _ string, _ fmp.IntradayInterval, _, _ time.Time) ([]fmp.IntradayBar, error) {
		return []fmp.IntradayBar{{
			Date:   "2026-05-01 00:00:00",
			Open:   1.1,
			High:   1.2,
			Low:    1.0,
			Close:  1.15,
			Volume: 123456,
		}}, nil
	}
	deleteBarsForSymbolScope = func(context.Context, driver.Conn, string, time.Time, time.Time) error {
		return nil
	}
	insertForexBars = func(_ context.Context, _ driver.Conn, bars <-chan Bar1m, _ int) (int64, error) {
		bar, ok := <-bars
		if !ok {
			return 0, errors.New("expected one bar")
		}
		if bar.Volume != 0 {
			return 0, fmt.Errorf("expected volume to be zeroed, got %v", bar.Volume)
		}
		if _, ok := <-bars; ok {
			return 0, errors.New("expected only one bar")
		}
		return 1, nil
	}

	result, err := SyncFMPKlines(context.Background(), nil, FMPKlineSyncConfig{
		APIKey:   "test-key",
		Symbols:  []string{"USDJPY"},
		From:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Interval: fmp.Interval1Min,
	})
	if err != nil {
		t.Fatalf("SyncFMPKlines returned error: %v", err)
	}
	if result.InsertedRows != 1 {
		t.Fatalf("InsertedRows = %d, want 1", result.InsertedRows)
	}
}
