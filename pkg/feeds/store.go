package feeds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Store provides ClickHouse persistence for feed bars.
// Each feed x window combination gets its own table (e.g., feed_dvol_1m).
type Store struct {
	conn driver.Conn
}

// NewStore opens a ClickHouse connection for feed storage.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &Store{conn: conn}, nil
}

// Close closes the ClickHouse connection.
func (s *Store) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// EnsureTable creates the table for a given feed and window if it doesn't exist.
func (s *Store) EnsureTable(ctx context.Context, feedName string, w Window) error {
	table := TableName(feedName, w)
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    symbol   String,
    timestamp DateTime64(3, 'UTC'),
    open     Float32,
    high     Float32,
    low      Float32,
    close    Float32,
    ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (symbol, timestamp)`, table)

	return s.conn.Exec(ctx, ddl)
}

// EnsureAllTables creates tables for all predefined windows of a feed.
func (s *Store) EnsureAllTables(ctx context.Context, feedName string) error {
	for _, w := range PredefinedWindows {
		if err := s.EnsureTable(ctx, feedName, w); err != nil {
			return fmt.Errorf("create table %s: %w", TableName(feedName, w), err)
		}
	}
	return nil
}

// InsertBars inserts bars into the table for the given feed and window.
func (s *Store) InsertBars(ctx context.Context, feedName string, w Window, bars []Bar, batchSize int) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 5000
	}

	table := TableName(feedName, w)
	sql := fmt.Sprintf("INSERT INTO %s (symbol, timestamp, open, high, low, close)", table)

	batch, err := s.conn.PrepareBatch(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("prepare batch for %s: %w", table, err)
	}

	total := 0
	pending := 0
	for _, b := range bars {
		if err := batch.Append(
			b.Symbol,
			b.Timestamp,
			float32(b.Open),
			float32(b.High),
			float32(b.Low),
			float32(b.Close),
		); err != nil {
			return total, fmt.Errorf("append row to %s: %w", table, err)
		}
		total++
		pending++

		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return total, fmt.Errorf("send batch to %s: %w", table, err)
			}
			batch, err = s.conn.PrepareBatch(ctx, sql)
			if err != nil {
				return total, fmt.Errorf("prepare next batch for %s: %w", table, err)
			}
			pending = 0
		}
	}

	if pending > 0 {
		if err := batch.Send(); err != nil {
			return total, fmt.Errorf("send final batch to %s: %w", table, err)
		}
	}

	return total, nil
}

// QueryBars reads bars from the table for a given feed, window, symbol, and time range.
func (s *Store) QueryBars(ctx context.Context, feedName string, w Window, symbol string, start, end time.Time) ([]Bar, error) {
	table := TableName(feedName, w)
	query := fmt.Sprintf(
		"SELECT symbol, timestamp, open, high, low, close FROM %s FINAL WHERE symbol = @symbol AND timestamp >= @start AND timestamp < @end ORDER BY timestamp",
		table,
	)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", strings.ToUpper(symbol)),
		clickhouse.Named("start", start.UTC()),
		clickhouse.Named("end", end.UTC()),
	)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var bars []Bar
	for rows.Next() {
		var b Bar
		var o, h, l, c float32
		if err := rows.Scan(&b.Symbol, &b.Timestamp, &o, &h, &l, &c); err != nil {
			return nil, fmt.Errorf("scan row from %s: %w", table, err)
		}
		b.Open, b.High, b.Low, b.Close = float64(o), float64(h), float64(l), float64(c)
		b.Timestamp = b.Timestamp.UTC()
		bars = append(bars, b)
	}
	return bars, rows.Err()
}

// SyncFeed fetches data from the external source at its finest available window,
// stores it, and aggregates to all coarser predefined windows.
func (s *Store) SyncFeed(ctx context.Context, f Feed, symbol string, start, end time.Time) (int, error) {
	srcWindows := f.SourceWindows()
	if len(srcWindows) == 0 {
		return 0, fmt.Errorf("feed %q has no source windows", f.Name())
	}
	finest := SmallestSourceWindow(srcWindows)

	// Fetch from external source at finest granularity
	bars, err := f.Fetch(ctx, FetchRequest{
		Symbol: symbol,
		Window: finest,
		Start:  start,
		End:    end,
	})
	if err != nil {
		return 0, fmt.Errorf("fetch %s/%s: %w", f.Name(), symbol, err)
	}
	if len(bars) == 0 {
		return 0, nil
	}

	// Insert finest-window bars
	total := 0
	n, err := s.InsertBars(ctx, f.Name(), finest, bars, 5000)
	if err != nil {
		return n, fmt.Errorf("insert %s bars: %w", TableName(f.Name(), finest), err)
	}
	total += n

	// Aggregate and insert into coarser windows
	targets := WindowsAbove(finest)
	for _, target := range targets {
		if target.Duration == finest.Duration {
			continue // already inserted
		}
		agg := AggregateBars(bars, target)
		n, err := s.InsertBars(ctx, f.Name(), target, agg, 5000)
		if err != nil {
			return total + n, fmt.Errorf("insert %s bars: %w", TableName(f.Name(), target), err)
		}
		total += n
	}

	return total, nil
}
