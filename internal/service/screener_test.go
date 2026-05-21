package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

func TestExpirationColUsesUSArrayJoinAlias(t *testing.T) {
	if got := expirationCol(false); got != "expiration_val" {
		t.Fatalf("expirationCol(false) = %q, want expiration_val", got)
	}
	if got := expirationCol(true); got != "m.expiration" {
		t.Fatalf("expirationCol(true) = %q, want m.expiration", got)
	}
}

func TestTurnoverIntersectionCandidateLimit(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: 0},
		{limit: 1, want: 2},
		{limit: 5, want: 7},
		{limit: 100, want: 135},
	}

	for _, tt := range tests {
		if got := turnoverIntersectionCandidateLimit(tt.limit); got != tt.want {
			t.Fatalf("turnoverIntersectionCandidateLimit(%d) = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

func TestCanonicalUSTurnoverIntersectionCacheLimit(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: 0},
		{limit: 1, want: 60},
		{limit: 30, want: 60},
		{limit: 60, want: 60},
		{limit: 61, want: 61},
	}

	for _, tt := range tests {
		if got := canonicalUSTurnoverIntersectionCacheLimit(tt.limit); got != tt.want {
			t.Fatalf("canonicalUSTurnoverIntersectionCacheLimit(%d) = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

func TestStoreUSTurnoverIntersectionInCacheSkipsEmptyResponses(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewScreenerService(nil, store)
	key := usTurnoverIntersectionCacheKey(100, 20, false)
	resp := &dto.ScreenUSTurnoverIntersectionResponse{}

	if err := svc.storeUSTurnoverIntersectionInCache(context.Background(), key, resp); err != nil {
		t.Fatalf("storeUSTurnoverIntersectionInCache() error = %v", err)
	}
	if _, ok, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("cache.Get() error = %v", err)
	} else if ok {
		t.Fatalf("cache.Get() ok = true, want false for empty response")
	}
}

func TestUSTurnoverIntersectionCacheRoundTrip(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewScreenerService(nil, store)
	key := usTurnoverIntersectionCacheKey(100, 20, false)
	want := &dto.ScreenUSTurnoverIntersectionResponse{
		LookbackDays:   20,
		Limit:          100,
		CandidateLimit: 135,
		Data: []dto.ScreenedUSTurnoverIntersectionRow{{
			Underlying:          "AAPL",
			StockTurnoverUSD:    1,
			OptionTurnoverUSD:   2,
			CombinedTurnoverUSD: 3,
		}},
	}

	if err := svc.storeUSTurnoverIntersectionInCache(context.Background(), key, want); err != nil {
		t.Fatalf("storeUSTurnoverIntersectionInCache() error = %v", err)
	}
	got, ok, err := svc.loadUSTurnoverIntersectionFromCache(context.Background(), key, want.Limit)
	if err != nil {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() error = %v", err)
	}
	if !ok {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() ok = false, want true")
	}
	if len(got.Data) != 1 || got.Data[0].Underlying != "AAPL" || got.CandidateLimit != 135 {
		t.Fatalf("unexpected cached response: %+v", got)
	}
}

func TestUSTurnoverIntersectionCacheRoundTripCanServeSmallerLimit(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewScreenerService(nil, store)
	key := usTurnoverIntersectionCacheKey(60, 20, true)
	full := &dto.ScreenUSTurnoverIntersectionResponse{
		LookbackDays:   20,
		Limit:          60,
		CandidateLimit: turnoverIntersectionCandidateLimit(60),
		Data: []dto.ScreenedUSTurnoverIntersectionRow{
			{Underlying: "AAPL", CombinedTurnoverUSD: 300},
			{Underlying: "MSFT", CombinedTurnoverUSD: 200},
			{Underlying: "NVDA", CombinedTurnoverUSD: 100},
		},
	}

	if err := svc.storeUSTurnoverIntersectionInCache(context.Background(), key, full); err != nil {
		t.Fatalf("storeUSTurnoverIntersectionInCache() error = %v", err)
	}
	got, ok, err := svc.loadUSTurnoverIntersectionFromCache(context.Background(), key, 2)
	if err != nil {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() error = %v", err)
	}
	if !ok {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() ok = false, want true")
	}
	if got.Limit != 2 || got.CandidateLimit != turnoverIntersectionCandidateLimit(2) {
		t.Fatalf("unexpected cached metadata: %+v", got)
	}
	if len(got.Data) != 2 || got.Data[0].Underlying != "AAPL" || got.Data[1].Underlying != "MSFT" {
		t.Fatalf("unexpected cached slice: %+v", got.Data)
	}
}

func TestUSTurnoverIntersectionCacheKeyIncludesUniverseFilter(t *testing.T) {
	withETF := usTurnoverIntersectionCacheKey(100, 20, false)
	nonETFOnly := usTurnoverIntersectionCacheKey(100, 20, true)

	if withETF == nonETFOnly {
		t.Fatalf("cache key should differ by universe filter: %q", withETF)
	}
}

func TestUSStocksFundamentalsUniverseFilterClauseRequiresPEAndPB(t *testing.T) {
	clause := usStocksFundamentalsUniverseFilterClause("symbol")
	if !strings.Contains(clause, "factor_code IN ('pe', 'pb')") {
		t.Fatalf("expected pe/pb filter in clause, got %q", clause)
	}
	if !strings.Contains(clause, "HAVING countDistinct(factor_code) = 2") {
		t.Fatalf("expected both factors requirement in clause, got %q", clause)
	}
	if !strings.Contains(clause, "AND symbol IN") {
		t.Fatalf("expected symbol column to be interpolated, got %q", clause)
	}
}

func TestFilterNonETFUSTurnoverResultsDropsETFProfiles(t *testing.T) {
	provider := &stubUSStockCompanyProfileProvider{}
	provider.requests = nil
	svc := NewScreenerService(nil).WithCompanyProfileProvider(provider)
	rows := []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "AAPL"}, {Underlying: "SLV"}, {Underlying: "MSFT"}}
	providerBySymbol := map[string]*dto.USStockCompanyProfile{
		"AAPL": {Symbol: "AAPL"},
		"SLV":  {Symbol: "SLV", IsETF: true},
		"MSFT": {Symbol: "MSFT"},
	}
	providerFn := &stubUSStockCompanyProfileProviderFunc{fn: func(_ context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
		return providerBySymbol[symbol], nil
	}}
	svc.companyInfo = providerFn

	got := svc.filterNonETFUSTurnoverResults(context.Background(), rows)
	if len(got) != 2 || got[0].Underlying != "AAPL" || got[1].Underlying != "MSFT" {
		t.Fatalf("unexpected filtered rows: %+v", got)
	}
}

func TestScreenUSTurnoverIntersectionUsesCandidateFilteredOptionQuery(t *testing.T) {
	conn := &fakeScreenerConn{rows: []driver.Rows{
		&fakeScreenerRows{data: [][]any{
			{"AAPL", "AAPL", 1000.0, 50000.0, uint32(20)},
			{"MSFT", "MSFT", 900.0, 45000.0, uint32(20)},
			{"BRK.B", "BRKB", 800.0, 40000.0, uint32(20)},
		}},
		&fakeScreenerRows{data: [][]any{
			{"AAPL", 2000.0, 200.0, uint32(20)},
			{"BRKB", 1500.0, 100.0, uint32(20)},
		}},
	}}
	svc := NewScreenerService(chrepo.NewRepo(conn))

	resp, err := svc.ScreenUSTurnoverIntersection(context.Background(), dto.ScreenUSTurnoverIntersectionRequest{Limit: 2, LookbackDays: 20})
	if err != nil {
		t.Fatalf("ScreenUSTurnoverIntersection() error = %v", err)
	}
	if len(conn.queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(conn.queries))
	}
	if !strings.Contains(conn.queries[1], "underlying IN ({underlyings:Array(String)})") {
		t.Fatalf("expected option query to filter candidate underlyings, got %q", conn.queries[1])
	}
	if !strings.Contains(conn.queries[0], "timestamp >=") || !strings.Contains(conn.queries[1], "timestamp >=") {
		t.Fatalf("expected range-based timestamp filtering, got stock=%q option=%q", conn.queries[0], conn.queries[1])
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(resp.Data))
	}
	if resp.Data[0].Underlying != "AAPL" || resp.Data[0].CombinedTurnoverUSD != 3000.0 {
		t.Fatalf("unexpected first row: %+v", resp.Data[0])
	}
	if resp.Data[1].Underlying != "BRK.B" || resp.Data[1].CombinedTurnoverUSD != 2300.0 {
		t.Fatalf("unexpected second row: %+v", resp.Data[1])
	}
	for _, row := range resp.Data {
		if row.Underlying == "MSFT" {
			t.Fatalf("expected rows without option aggregates to be excluded: %+v", resp.Data)
		}
	}
}

type stubUSStockCompanyProfileProviderFunc struct {
	fn func(context.Context, string) (*dto.USStockCompanyProfile, error)
}

func (s *stubUSStockCompanyProfileProviderFunc) CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
	return s.fn(ctx, symbol)
}

func (s *stubUSStockCompanyProfileProviderFunc) IsETFLike(ctx context.Context, symbol string) (bool, error) {
	profile, err := s.CompanyProfile(ctx, symbol)
	if err != nil {
		return false, err
	}
	return isETFLikeUSStockProfile(profile), nil
}

func (s *stubUSStockCompanyProfileProviderFunc) IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error) {
	result := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		isETFLike, err := s.IsETFLike(ctx, symbol)
		if err != nil {
			return nil, err
		}
		result[symbol] = isETFLike
	}
	return result, nil
}

type fakeScreenerConn struct {
	rows    []driver.Rows
	queries []string
	idx     int
}

func (f *fakeScreenerConn) Contributors() []string { return nil }

func (f *fakeScreenerConn) ServerVersion() (*driver.ServerVersion, error) { return nil, nil }

func (f *fakeScreenerConn) Select(context.Context, any, string, ...any) error { return nil }

func (f *fakeScreenerConn) Query(_ context.Context, query string, _ ...any) (driver.Rows, error) {
	f.queries = append(f.queries, query)
	if f.idx >= len(f.rows) {
		return nil, fmt.Errorf("unexpected query %d: %s", f.idx+1, query)
	}
	rows := f.rows[f.idx]
	f.idx++
	return rows, nil
}

func (f *fakeScreenerConn) QueryRow(context.Context, string, ...any) driver.Row {
	return fakeScreenerRow{}
}

func (f *fakeScreenerConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return nil, nil
}

func (f *fakeScreenerConn) Exec(context.Context, string, ...any) error { return nil }

func (f *fakeScreenerConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }

func (f *fakeScreenerConn) Ping(context.Context) error { return nil }

func (f *fakeScreenerConn) Stats() driver.Stats { return driver.Stats{} }

func (f *fakeScreenerConn) Close() error { return nil }

type fakeScreenerRow struct{}

func (fakeScreenerRow) Err() error           { return nil }
func (fakeScreenerRow) Scan(...any) error    { return nil }
func (fakeScreenerRow) ScanStruct(any) error { return nil }

type fakeScreenerRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *fakeScreenerRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeScreenerRows) Scan(dest ...any) error {
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
		case *float64:
			value, ok := row[index].(float64)
			if !ok {
				return fmt.Errorf("column %d: want float64, got %T", index, row[index])
			}
			*ptr = value
		case *uint32:
			value, ok := row[index].(uint32)
			if !ok {
				return fmt.Errorf("column %d: want uint32, got %T", index, row[index])
			}
			*ptr = value
		default:
			return fmt.Errorf("unsupported scan dest %T", dest[index])
		}
	}
	return nil
}

func (r *fakeScreenerRows) ScanStruct(any) error             { return nil }
func (r *fakeScreenerRows) ColumnTypes() []driver.ColumnType { return nil }
func (r *fakeScreenerRows) Totals(...any) error              { return nil }
func (r *fakeScreenerRows) Columns() []string                { return nil }
func (r *fakeScreenerRows) Err() error                       { return r.err }
func (r *fakeScreenerRows) Close() error                     { return nil }
