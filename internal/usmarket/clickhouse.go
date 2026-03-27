package usmarket

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

// ConnectClickHouse establishes a connection to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string) (driver.Conn, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
	}
	if opts.DialTimeout == 0 || opts.DialTimeout < defaultDialTimeout {
		opts.DialTimeout = defaultDialTimeout
	}
	if opts.ReadTimeout != 0 && opts.ReadTimeout < defaultReadTimeout {
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
	return nil
}

const optionBarInsertSQL = `INSERT INTO us_options_bar_1m (
	timestamp, symbol, underlying, option_type, expiration, strike,
	open, high, low, close, volume, transactions
)`

const stockBarInsertSQL = `INSERT INTO us_stocks_bar_1m (
	timestamp, symbol, open, high, low, close, volume, transactions
)`

// InsertOptionBars batch-inserts option bars from a channel into us_options_bar_1m.
func InsertOptionBars(ctx context.Context, conn driver.Conn, bars <-chan OptionBar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, optionBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare option batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.Underlying,
			bar.OptionType,
			bar.Expiration,
			bar.Strike,
			bar.Open, bar.High, bar.Low, bar.Close,
			bar.Volume,
			bar.Transactions,
		); err != nil {
			return totalRows, fmt.Errorf("append option row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send option batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d option rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, optionBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next option batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final option batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d option rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// InsertStockBars batch-inserts stock bars from a channel into us_stocks_bar_1m.
func InsertStockBars(ctx context.Context, conn driver.Conn, bars <-chan StockBar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, stockBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare stock batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.Open, bar.High, bar.Low, bar.Close,
			bar.Volume,
			bar.Transactions,
		); err != nil {
			return totalRows, fmt.Errorf("append stock row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send stock batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d stock rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, stockBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next stock batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final stock batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d stock rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// CountExistingOptionBars checks if option data already exists for a given date range.
func CountExistingOptionBars(ctx context.Context, conn driver.Conn, from, to time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_options_bar_1m WHERE timestamp >= ? AND timestamp < ?`,
		from, to,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing option bars: %w", err)
	}
	return count, nil
}

// CountExistingStockBars checks if stock data already exists for a given date range.
func CountExistingStockBars(ctx context.Context, conn driver.Conn, from, to time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_stocks_bar_1m WHERE timestamp >= ? AND timestamp < ?`,
		from, to,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing stock bars: %w", err)
	}
	return count, nil
}
