package cryptooptions

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Store provides structured access to crypto-options data in ClickHouse.
// It wraps a driver.Conn and delegates to the existing standalone functions,
// giving callers a single dependency instead of passing a raw connection around.
type Store struct {
	conn driver.Conn
}

// NewStore wraps an existing ClickHouse connection.
func NewStore(conn driver.Conn) *Store {
	return &Store{conn: conn}
}

// Conn returns the underlying ClickHouse connection for callers that need
// direct access (e.g. schema init, ad-hoc queries).
func (s *Store) Conn() driver.Conn {
	return s.conn
}

// InsertSymbols batch-inserts symbol metadata.
func (s *Store) InsertSymbols(ctx context.Context, symbols []SymbolMeta) error {
	return InsertSymbols(ctx, s.conn, symbols)
}

// CountExistingBars returns how many sampled option bars already exist.
func (s *Store) CountExistingBars(ctx context.Context, bars []Bar1m) (int, error) {
	return CountExistingBars(ctx, s.conn, bars)
}

// InsertBars batch-inserts 1-minute option bars from a channel.
func (s *Store) InsertBars(ctx context.Context, bars <-chan Bar1m, batchSize int) (int64, error) {
	return InsertBars(ctx, s.conn, bars, batchSize)
}

// CountExistingSpotBars returns how many sampled spot bars already exist.
func (s *Store) CountExistingSpotBars(ctx context.Context, bars []SpotBar1m) (int, error) {
	return CountExistingSpotBars(ctx, s.conn, bars)
}

// InsertSpotBars batch-inserts 1-minute spot bars from a channel.
func (s *Store) InsertSpotBars(ctx context.Context, bars <-chan SpotBar1m, batchSize int) (int64, error) {
	return InsertSpotBars(ctx, s.conn, bars, batchSize)
}

// FindMissingBarDays returns dates that have no option bar data.
func (s *Store) FindMissingBarDays(ctx context.Context, fromDate, toDate time.Time, baseAsset string) ([]time.Time, error) {
	return FindMissingBarDays(ctx, s.conn, fromDate, toDate, baseAsset)
}

// BackfillKlineWindows backfills aggregated kline tables from 1m base data.
func (s *Store) BackfillKlineWindows(ctx context.Context, opts KlineBackfillOptions) error {
	return BackfillKlineWindows(ctx, s.conn, opts)
}
