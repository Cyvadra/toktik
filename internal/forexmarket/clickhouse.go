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
