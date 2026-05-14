package forexmarket

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	defaultDialTimeout = 30 * time.Second
	defaultReadTimeout = 30 * time.Minute
)

const barInsertSQL = `INSERT INTO forex_bar_1m (
	timestamp, symbol, open, high, low, close, volume, transactions,
	market_date, session_kind, is_regular_session, session_open, session_seq
)`

// ConnectClickHouse establishes a connection to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string) (driver.Conn, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
	}
	if opts.DialTimeout == 0 || opts.DialTimeout < defaultDialTimeout {
		opts.DialTimeout = defaultDialTimeout
	}
	if opts.ReadTimeout == 0 || opts.ReadTimeout < defaultReadTimeout {
		opts.ReadTimeout = defaultReadTimeout
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return conn, nil
}

// InitSchema reads and executes a DDL SQL file.
func InitSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
	data, err := os.ReadFile(ddlPath)
	if err != nil {
		return fmt.Errorf("read DDL file %s: %w", ddlPath, err)
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if commentIndex := strings.Index(line, "--"); commentIndex >= 0 {
			line = line[:commentIndex]
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		filtered = append(filtered, line)
	}

	parts := strings.Split(strings.Join(filtered, "\n"), ";")
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec DDL: %w\nStatement: %s", err, stmt)
		}
	}
	if err := conn.Exec(ctx, "ALTER TABLE forex_bar_1m MODIFY COLUMN volume Float64"); err != nil {
		return fmt.Errorf("ensure forex volume column: %w", err)
	}
	if err := InitKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("init forex kline schema: %w", err)
	}
	return nil
}

// InsertBars batch-inserts bars into forex_bar_1m.
func InsertBars(ctx context.Context, conn driver.Conn, bars <-chan Bar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, barInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare forex batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.Open,
			bar.High,
			bar.Low,
			bar.Close,
			bar.Volume,
			bar.Transactions,
			bar.MarketDate,
			bar.SessionKind,
			bar.IsRegular,
			bar.SessionOpen,
			bar.SessionSeq,
		); err != nil {
			return totalRows, fmt.Errorf("append forex row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send forex batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d forex rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, barInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next forex batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final forex batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d forex rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// DeleteBarsSymbolScope deletes forex base rows and precomputed aggregates for
// one symbol in [from, to). It is used by FMP replace-syncs to keep reruns
// idempotent for both 1m data and AggregatingMergeTree rollups.
func DeleteBarsSymbolScope(ctx context.Context, conn driver.Conn, symbol string, from, to time.Time) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	where, args := forexScope("timestamp", symbol, from, to)
	if err := conn.Exec(ctx, "ALTER TABLE forex_bar_1m DELETE "+where+" SETTINGS mutations_sync = 1", args...); err != nil {
		return fmt.Errorf("delete forex base rows for %s: %w", symbol, err)
	}
	for _, iv := range KlineIntervals {
		aggTable := "forex_bar_" + iv.Suffix + "_agg"
		where, args := forexScope("ts", symbol, from, to)
		if err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DELETE %s SETTINGS mutations_sync = 1", aggTable, where), args...); err != nil {
			return fmt.Errorf("delete forex aggregate %s for %s: %w", iv.Suffix, symbol, err)
		}
	}
	return nil
}

func forexScope(timeColumn, symbol string, from, to time.Time) (string, []any) {
	parts := []string{"symbol = {symbol:String}"}
	args := []any{clickhouse.Named("symbol", symbol)}
	if !from.IsZero() {
		parts = append(parts, timeColumn+" >= toDateTime({from:String}, 'UTC')")
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		parts = append(parts, timeColumn+" < toDateTime({to:String}, 'UTC')")
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}
