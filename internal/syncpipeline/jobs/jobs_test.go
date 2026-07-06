package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestFMPForexSourceKeysUsesSymbolsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forex.txt")
	content := "# watchlist\nEURUSD\naudusd\nEURUSD\n\nUSDJPY\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write symbols file: %v", err)
	}

	syncer, err := NewFMPForex(FMPForexConfig{
		APIKey:           "test-key",
		SymbolsFile:      path,
		ResolveAtStartup: true,
		Interval:         fmp.Interval1Min,
	})
	if err != nil {
		t.Fatalf("NewFMPForex returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"AUDUSD", "EURUSD", "USDJPY"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
}

func TestFMPForexSourceKeysPrefersExplicitSymbols(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forex.txt")
	if err := os.WriteFile(path, []byte("EURUSD\nAUDUSD\n"), 0o644); err != nil {
		t.Fatalf("write symbols file: %v", err)
	}

	syncer, err := NewFMPForex(FMPForexConfig{
		APIKey:           "test-key",
		Symbols:          []string{"USDJPY", "EURUSD"},
		SymbolsFile:      path,
		ResolveAtStartup: true,
		Interval:         fmp.Interval1Min,
	})
	if err != nil {
		t.Fatalf("NewFMPForex returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"USDJPY", "EURUSD"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
}

func TestFMPUSStockSplitsSourceKeysPrefersExplicitSymbols(t *testing.T) {
	syncer, err := NewFMPUSStockSplits(FMPUSStockSplitsConfig{
		APIKey:           "test-key",
		Symbols:          []string{"aapl", "MSFT", "aapl"},
		ResolveAtStartup: true,
	})
	if err != nil {
		t.Fatalf("NewFMPUSStockSplits returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"AAPL", "MSFT"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
}

func TestFMPUSStockSplitsCursorUsesPersistedSplitDate(t *testing.T) {
	syncer, err := NewFMPUSStockSplits(FMPUSStockSplitsConfig{
		APIKey:           "test-key",
		Symbols:          []string{"AAPL"},
		ResolveAtStartup: true,
	})
	if err != nil {
		t.Fatalf("NewFMPUSStockSplits returned error: %v", err)
	}

	conn := &queryRowConn{value: "2024-08-31"}
	cursor, ok, err := syncer.ResolveCursor(context.Background(), conn, "aapl")
	if err != nil {
		t.Fatalf("ResolveCursor returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected cursor")
	}
	want := time.Date(2024, 8, 31, 0, 0, 0, 0, time.UTC)
	if !cursor.Equal(want) {
		t.Fatalf("cursor = %s, want %s", cursor, want)
	}
	if !strings.Contains(conn.query, "FROM us_stock_splits") || !strings.Contains(conn.query, "maxOrNull(toDate(split_date))") {
		t.Fatalf("ResolveCursor query used wrong persisted cursor source: %s", conn.query)
	}
	if strings.Contains(conn.query, "import_ledger") || strings.Contains(conn.query, "completed_at") || strings.Contains(conn.query, "updated_at") {
		t.Fatalf("ResolveCursor query should not use sync execution time columns: %s", conn.query)
	}
}

func TestFMPStockEarningsCalendarBackfillDefaultsAndSourceKeys(t *testing.T) {
	syncer, err := NewFMPStockEarningsCalendarBackfill(FMPStockEarningsCalendarBackfillConfig{
		APIKey:   "test-key",
		MySQLDSN: "user:pass@tcp(localhost:3306)/toktik",
	})
	if err != nil {
		t.Fatalf("NewFMPStockEarningsCalendarBackfill returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"_default"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
	if got := syncer.ColdStartFloor("_"); !got.Equal(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ColdStartFloor = %s, want 1990-01-01", got)
	}
	if got := syncer.(*fmpStockEarningsCalendarBackfill).cfg.ChunkDays; got != 1 {
		t.Fatalf("ChunkDays = %d, want 1", got)
	}
}

func TestEarningsCalendarBackfillNotesReportsCapHits(t *testing.T) {
	notes := earningsCalendarBackfillNotes(3, 12000, 2, false)
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"chunks=3", "fetched_events=12000", "possible_fmp_cap_chunks=2", "rows reports fetched events"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notes %q missing %q", joined, want)
		}
	}
}

func TestCalendarDateChunksSplitsInclusiveRanges(t *testing.T) {
	from := time.Date(2024, 1, 1, 10, 0, 0, 0, time.FixedZone("test", 8*3600))
	to := time.Date(2024, 1, 5, 23, 0, 0, 0, time.FixedZone("test", 8*3600))
	chunks := calendarDateChunks(from, to, 2)

	want := []calendarDateChunk{
		{from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), to: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{from: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), to: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)},
		{from: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), to: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
	}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("calendarDateChunks = %#v, want %#v", chunks, want)
	}
}

type queryRowConn struct {
	driver.Conn
	query string
	value string
}

func (c *queryRowConn) QueryRow(_ context.Context, query string, _ ...any) driver.Row {
	c.query = query
	return queryRow{value: c.value}
}

type queryRow struct {
	driver.Row
	value string
}

func (r queryRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("expected one scan destination")
	}
	ptr, ok := dest[0].(*string)
	if !ok {
		return errors.New("expected string scan destination")
	}
	*ptr = r.value
	return nil
}

func (r queryRow) Err() error { return nil }
