package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

type fakeForexConn struct {
	rows      driver.Rows
	rowSets   []driver.Rows
	queryText string
	queryErr  error
	closed    bool
}

func (f *fakeForexConn) Contributors() []string { return nil }

func (f *fakeForexConn) ServerVersion() (*driver.ServerVersion, error) { return nil, nil }

func (f *fakeForexConn) Select(context.Context, any, string, ...any) error { return nil }

func (f *fakeForexConn) Query(_ context.Context, query string, _ ...any) (driver.Rows, error) {
	f.queryText = query
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.rowSets) > 0 {
		rows := f.rowSets[0]
		f.rowSets = f.rowSets[1:]
		return rows, nil
	}
	if f.rows == nil {
		return &fakeForexRows{}, nil
	}
	return f.rows, nil
}

func (f *fakeForexConn) QueryRow(context.Context, string, ...any) driver.Row { return fakeForexRow{} }

func (f *fakeForexConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return nil, nil
}

func (f *fakeForexConn) Exec(context.Context, string, ...any) error { return nil }

func (f *fakeForexConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }

func (f *fakeForexConn) Ping(context.Context) error { return nil }

func (f *fakeForexConn) Stats() driver.Stats { return driver.Stats{} }

func (f *fakeForexConn) Close() error {
	f.closed = true
	return nil
}

type fakeForexRow struct{}

func (fakeForexRow) Err() error           { return nil }
func (fakeForexRow) Scan(...any) error    { return nil }
func (fakeForexRow) ScanStruct(any) error { return nil }

type fakeForexRows struct {
	data [][]any
	idx  int
	err  error

	closed bool
}

func (r *fakeForexRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeForexRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return fmt.Errorf("scan called without current row")
	}
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan arg mismatch: got %d want %d", len(dest), len(row))
	}
	for index := range dest {
		switch ptr := dest[index].(type) {
		case *time.Time:
			value, ok := row[index].(time.Time)
			if !ok {
				return fmt.Errorf("column %d: want time.Time, got %T", index, row[index])
			}
			*ptr = value
		case *string:
			value, ok := row[index].(string)
			if !ok {
				return fmt.Errorf("column %d: want string, got %T", index, row[index])
			}
			*ptr = value
		case *[]string:
			value, ok := row[index].([]string)
			if !ok {
				return fmt.Errorf("column %d: want []string, got %T", index, row[index])
			}
			*ptr = append((*ptr)[:0], value...)
		case *[]time.Time:
			value, ok := row[index].([]time.Time)
			if !ok {
				return fmt.Errorf("column %d: want []time.Time, got %T", index, row[index])
			}
			*ptr = append((*ptr)[:0], value...)
		case *[]float32:
			value, ok := row[index].([]float32)
			if !ok {
				return fmt.Errorf("column %d: want []float32, got %T", index, row[index])
			}
			*ptr = append((*ptr)[:0], value...)
		case *[]float64:
			value, ok := row[index].([]float64)
			if !ok {
				return fmt.Errorf("column %d: want []float64, got %T", index, row[index])
			}
			*ptr = append((*ptr)[:0], value...)
		case *[]uint64:
			value, ok := row[index].([]uint64)
			if !ok {
				return fmt.Errorf("column %d: want []uint64, got %T", index, row[index])
			}
			*ptr = append((*ptr)[:0], value...)
		case *sql.NullString:
			if row[index] == nil {
				*ptr = sql.NullString{}
				continue
			}
			value, ok := row[index].(string)
			if !ok {
				return fmt.Errorf("column %d: want string, got %T", index, row[index])
			}
			*ptr = sql.NullString{String: value, Valid: true}
		case *float32:
			value, ok := row[index].(float32)
			if !ok {
				return fmt.Errorf("column %d: want float32, got %T", index, row[index])
			}
			*ptr = value
		case *float64:
			value, ok := row[index].(float64)
			if !ok {
				return fmt.Errorf("column %d: want float64, got %T", index, row[index])
			}
			*ptr = value
		case **float64:
			if row[index] == nil {
				*ptr = nil
				continue
			}
			value, ok := row[index].(*float64)
			if !ok {
				return fmt.Errorf("column %d: want *float64, got %T", index, row[index])
			}
			*ptr = value
		case *uint8:
			value, ok := row[index].(uint8)
			if !ok {
				return fmt.Errorf("column %d: want uint8, got %T", index, row[index])
			}
			*ptr = value
		case *uint16:
			value, ok := row[index].(uint16)
			if !ok {
				return fmt.Errorf("column %d: want uint16, got %T", index, row[index])
			}
			*ptr = value
		case *uint32:
			value, ok := row[index].(uint32)
			if !ok {
				return fmt.Errorf("column %d: want uint32, got %T", index, row[index])
			}
			*ptr = value
		case *uint64:
			value, ok := row[index].(uint64)
			if !ok {
				return fmt.Errorf("column %d: want uint64, got %T", index, row[index])
			}
			*ptr = value
		default:
			return fmt.Errorf("unsupported scan dest %T", dest[index])
		}
	}
	return nil
}

func (r *fakeForexRows) ScanStruct(any) error             { return nil }
func (r *fakeForexRows) ColumnTypes() []driver.ColumnType { return nil }
func (r *fakeForexRows) Totals(...any) error              { return nil }
func (r *fakeForexRows) Columns() []string                { return nil }
func (r *fakeForexRows) Err() error                       { return r.err }
func (r *fakeForexRows) Close() error                     { r.closed = true; return nil }

func TestForexServiceQueryBarsUsesIntervalTableAndPagination(t *testing.T) {
	first := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	third := second.Add(time.Hour)
	rows := &fakeForexRows{data: [][]any{
		{first, "EURUSD", float32(1.10), float32(1.11), float32(1.09), float32(1.105), 1200.0, uint64(15)},
		{second, "EURUSD", float32(1.105), float32(1.12), float32(1.10), float32(1.115), 1300.0, uint64(18)},
		{third, "EURUSD", float32(1.115), float32(1.13), float32(1.11), float32(1.125), 1400.0, uint64(21)},
	}}
	conn := &fakeForexConn{rows: rows}
	svc := NewForexService(chrepo.NewRepo(conn))

	resp, err := svc.QueryBars(context.Background(), dto.ForexBarRequest{
		Symbol:   "EURUSD",
		Interval: "1h",
		From:     "2026-05-01",
		To:       "2026-05-03",
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("QueryBars returned error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 bars after pagination, got %d", len(resp.Data))
	}
	if resp.NextCursor != encodeCursor(second) {
		t.Fatalf("expected next cursor for second row, got %q", resp.NextCursor)
	}
	if !strings.Contains(conn.queryText, "FROM forex_bar_1h") {
		t.Fatalf("expected 1h precomputed table, query was %q", conn.queryText)
	}
	if !rows.closed {
		t.Fatalf("expected rows to be closed")
	}
}

func TestForexServiceQueryBarsAppliesCursorToTimeRange(t *testing.T) {
	conn := &fakeForexConn{rows: &fakeForexRows{}}
	svc := NewForexService(chrepo.NewRepo(conn))
	cursorTS := time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC)

	_, err := svc.QueryBars(context.Background(), dto.ForexBarRequest{
		Symbol:   "USDJPY",
		Interval: "1m",
		From:     "2026-05-01",
		To:       "2026-05-07",
		Limit:    10,
		Cursor:   encodeCursor(cursorTS),
	})
	if err != nil {
		t.Fatalf("QueryBars returned error: %v", err)
	}
	if !strings.Contains(conn.queryText, "FROM forex_bar_1m") {
		t.Fatalf("expected 1m base table, query was %q", conn.queryText)
	}
	if !strings.Contains(conn.queryText, clickhouseDateTimeLiteral(cursorTS)) {
		t.Fatalf("expected cursor timestamp in query, query was %q", conn.queryText)
	}
}

func TestForexServiceQuerySymbolsSupportsSearchAndCursorPagination(t *testing.T) {
	rows := &fakeForexRows{data: [][]any{{"EURAUD"}, {"EURGBP"}, {"EURJPY"}}}
	conn := &fakeForexConn{rows: rows}
	svc := NewForexService(chrepo.NewRepo(conn))

	resp, err := svc.QuerySymbols(context.Background(), dto.ForexSymbolRequest{
		Search: "EUR",
		Cursor: encodeCursorString("EURAUD"),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("QuerySymbols returned error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 symbols after pagination, got %d", len(resp.Data))
	}
	if resp.NextCursor != encodeCursorString("EURGBP") {
		t.Fatalf("expected next cursor for EURGBP, got %q", resp.NextCursor)
	}
	if !strings.Contains(conn.queryText, "symbol ILIKE '%EUR%'") {
		t.Fatalf("expected search filter in query, query was %q", conn.queryText)
	}
	if !strings.Contains(conn.queryText, "symbol > 'EURAUD'") {
		t.Fatalf("expected cursor filter in query, query was %q", conn.queryText)
	}
	if !rows.closed {
		t.Fatalf("expected rows to be closed")
	}
}

func TestForexServiceQueryBarsRejectsUnsupportedInterval(t *testing.T) {
	svc := NewForexService(chrepo.NewRepo(&fakeForexConn{}))
	_, err := svc.QueryBars(context.Background(), dto.ForexBarRequest{
		Symbol:   "EURUSD",
		Interval: "3h",
		From:     "2026-05-01",
		To:       "2026-05-02",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported forex interval") {
		t.Fatalf("expected unsupported interval validation error, got %v", err)
	}
}
