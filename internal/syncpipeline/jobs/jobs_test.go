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
