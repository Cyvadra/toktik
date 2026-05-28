package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

type fakeUSOptionsConn struct {
	queryFn        func(query string, args ...any) (driver.Rows, error)
	prepareBatchFn func(query string) (driver.Batch, error)
	execQueries    []string
}

func (f *fakeUSOptionsConn) Contributors() []string                            { return nil }
func (f *fakeUSOptionsConn) ServerVersion() (*driver.ServerVersion, error)     { return nil, nil }
func (f *fakeUSOptionsConn) Select(context.Context, any, string, ...any) error { return nil }
func (f *fakeUSOptionsConn) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(query, args...)
	}
	return &fakeUSOptionsRows{}, nil
}
func (f *fakeUSOptionsConn) QueryRow(context.Context, string, ...any) driver.Row {
	return fakeForexRow{}
}
func (f *fakeUSOptionsConn) PrepareBatch(_ context.Context, query string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	if f.prepareBatchFn != nil {
		return f.prepareBatchFn(query)
	}
	return &fakeUSOptionsBatch{}, nil
}
func (f *fakeUSOptionsConn) Exec(_ context.Context, query string, _ ...any) error {
	f.execQueries = append(f.execQueries, query)
	return nil
}
func (f *fakeUSOptionsConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }
func (f *fakeUSOptionsConn) Ping(context.Context) error                              { return nil }
func (f *fakeUSOptionsConn) Stats() driver.Stats                                     { return driver.Stats{} }
func (f *fakeUSOptionsConn) Close() error                                            { return nil }

type fakeUSOptionsRows struct {
	data   [][]any
	idx    int
	closed bool
	err    error
}

func (r *fakeUSOptionsRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeUSOptionsRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return fmt.Errorf("scan called without current row")
	}
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan arg mismatch: got %d want %d", len(dest), len(row))
	}
	for i := range dest {
		switch ptr := dest[i].(type) {
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("column %d: want time.Time, got %T", i, row[i])
			}
			*ptr = value
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("column %d: want string, got %T", i, row[i])
			}
			*ptr = value
		default:
			return fmt.Errorf("unsupported scan destination %T", dest[i])
		}
	}
	return nil
}

func (r *fakeUSOptionsRows) ScanStruct(any) error             { return fmt.Errorf("unsupported") }
func (r *fakeUSOptionsRows) ColumnTypes() []driver.ColumnType { return nil }
func (r *fakeUSOptionsRows) Totals(...any) error              { return nil }
func (r *fakeUSOptionsRows) Columns() []string                { return nil }
func (r *fakeUSOptionsRows) Close() error                     { r.closed = true; return nil }
func (r *fakeUSOptionsRows) Err() error                       { return r.err }

type fakeUSOptionsBatch struct {
	appends [][]any
	sent    bool
}

func (b *fakeUSOptionsBatch) Abort() error { return nil }
func (b *fakeUSOptionsBatch) Append(v ...any) error {
	copyValues := make([]any, len(v))
	copy(copyValues, v)
	b.appends = append(b.appends, copyValues)
	return nil
}
func (b *fakeUSOptionsBatch) AppendStruct(any) error        { return nil }
func (b *fakeUSOptionsBatch) Column(int) driver.BatchColumn { return nil }
func (b *fakeUSOptionsBatch) Flush() error                  { return nil }
func (b *fakeUSOptionsBatch) Send() error                   { b.sent = true; return nil }
func (b *fakeUSOptionsBatch) IsSent() bool                  { return b.sent }
func (b *fakeUSOptionsBatch) Rows() int                     { return len(b.appends) }
func (b *fakeUSOptionsBatch) Columns() []column.Interface   { return nil }
func (b *fakeUSOptionsBatch) Close() error                  { return nil }

type fakeOptionWallPolygonClient struct {
	requests  []polygonpkg.OptionChainRequest
	responses map[string][]polygonpkg.OptionChainContract
}

func (f *fakeOptionWallPolygonClient) OptionChain(_ context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error) {
	f.requests = append(f.requests, req)
	key := req.ExpirationDate + "|" + req.Order
	return append([]polygonpkg.OptionChainContract(nil), f.responses[key]...), nil
}

func TestQueryOptionWallComputesPersistsAndCaches(t *testing.T) {
	snapshotDay := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	expirationA := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	expirationB := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	batch := &fakeUSOptionsBatch{}
	conn := &fakeUSOptionsConn{
		queryFn: func(query string, _ ...any) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "FROM us_options_bar_1d"):
				return &fakeUSOptionsRows{data: [][]any{{expirationA}, {expirationB}}}, nil
			case strings.Contains(query, "FROM us_options_option_wall_daily"):
				return &fakeUSOptionsRows{}, nil
			default:
				return nil, fmt.Errorf("unexpected query: %s", query)
			}
		},
		prepareBatchFn: func(query string) (driver.Batch, error) {
			if !strings.Contains(query, "INSERT INTO us_options_option_wall_daily") {
				return nil, fmt.Errorf("unexpected batch query: %s", query)
			}
			return batch, nil
		},
	}
	polygonClient := &fakeOptionWallPolygonClient{responses: map[string][]polygonpkg.OptionChainContract{
		"2026-06-19|asc": {
			makeWallContract("O:AAPL260619C00200000", "call", "2026-06-19", 200, 120, 1.0, 1.2),
		},
		"2026-06-19|desc": {
			makeWallContract("O:AAPL260619P00200000", "put", "2026-06-19", 200, 95, 0.8, 1.0),
		},
		"2026-06-26|asc": {
			makeWallContract("O:AAPL260626C00210000", "call", "2026-06-26", 210, 80, 0.7, 0.9),
		},
		"2026-06-26|desc": {
			makeWallContract("O:AAPL260626P00210000", "put", "2026-06-26", 210, 70, 0.6, 0.85),
		},
	}}
	store := cache.NewMemoryStore()
	svc := NewUSOptionsService(chrepo.NewRepo(conn)).WithPolygonClient(polygonClient).WithCache(store)
	svc.now = func() time.Time { return snapshotDay }

	resp, err := svc.QueryOptionWall(context.Background(), dto.USOptionWallRequest{Symbol: "AAPL", MinDTE: 20, MaxDTE: 40})
	if err != nil {
		t.Fatalf("QueryOptionWall() error = %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 expirations, got %d", len(resp.Data))
	}
	if len(polygonClient.requests) != 4 {
		t.Fatalf("expected 4 polygon requests, got %d", len(polygonClient.requests))
	}
	for _, req := range polygonClient.requests {
		if !req.DisablePagination {
			t.Fatalf("expected DisablePagination=true, got %+v", req)
		}
		if req.Sort != "ticker" || req.Limit != optionWallSnapshotLimit {
			t.Fatalf("unexpected snapshot request: %+v", req)
		}
	}
	if batch.Rows() != 2 {
		t.Fatalf("expected 2 persisted rows, got %d", batch.Rows())
	}
	for _, expiration := range []time.Time{expirationA, expirationB} {
		if _, ok, err := store.Get(context.Background(), optionWallCacheKey("AAPL", expiration, snapshotDay)); err != nil {
			t.Fatalf("cache.Get() error = %v", err)
		} else if !ok {
			t.Fatalf("expected cache entry for %s", expiration.Format("2006-01-02"))
		}
	}
	strikeRow := resp.Data[0].Strikes[0]
	if strikeRow.TotalOpenInterest != 215 || strikeRow.CallOpenInterest != 120 || strikeRow.PutOpenInterest != 95 {
		t.Fatalf("unexpected strike aggregation: %+v", strikeRow)
	}
}

func TestQueryOptionWallUsesCacheBeforePolygon(t *testing.T) {
	snapshotDay := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	expiration := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	conn := &fakeUSOptionsConn{
		queryFn: func(query string, _ ...any) (driver.Rows, error) {
			if strings.Contains(query, "FROM us_options_bar_1d") {
				return &fakeUSOptionsRows{data: [][]any{{expiration}}}, nil
			}
			return &fakeUSOptionsRows{}, nil
		},
	}
	store := cache.NewMemoryStore()
	wall := dto.USOptionWall{Symbol: "AAPL", Expiration: expiration, SnapshotDay: snapshotDay, DaysToExpiry: 22, Strikes: []dto.USOptionWallStrikeRow{{Strike: 200, TotalOpenInterest: 50}}}
	payload, err := json.Marshal(&wall)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := store.Set(context.Background(), optionWallCacheKey("AAPL", expiration, snapshotDay), payload, optionWallCacheTTL); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}
	polygonClient := &fakeOptionWallPolygonClient{}
	svc := NewUSOptionsService(chrepo.NewRepo(conn)).WithPolygonClient(polygonClient).WithCache(store)
	svc.now = func() time.Time { return snapshotDay }

	resp, err := svc.QueryOptionWall(context.Background(), dto.USOptionWallRequest{Symbol: "AAPL", MinDTE: 20, MaxDTE: 25})
	if err != nil {
		t.Fatalf("QueryOptionWall() error = %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Strikes[0].TotalOpenInterest != 50 {
		t.Fatalf("unexpected cached response: %+v", resp)
	}
	if len(polygonClient.requests) != 0 {
		t.Fatalf("expected no polygon calls, got %d", len(polygonClient.requests))
	}
}

func makeWallContract(ticker, contractType, expiration string, strike, openInterest, bid, ask float64) polygonpkg.OptionChainContract {
	bidValue := bid
	askValue := ask
	return polygonpkg.OptionChainContract{
		Contract: polygonpkg.OptionContract{
			Ticker:         ticker,
			ContractType:   contractType,
			ExpirationDate: expiration,
			StrikePrice:    strike,
		},
		OpenInterest: openInterest,
		LastQuote: polygonpkg.Quote{
			BidPrice: &bidValue,
			AskPrice: &askValue,
		},
	}
}
