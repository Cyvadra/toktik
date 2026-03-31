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
	open, high, low, close,
	underlying_close, implied_volatility, delta, gamma, vega, theta, rho,
	volume, transactions,
	market_date, session_kind, is_regular_session, session_open, session_seq
)`

const stockBarInsertSQL = `INSERT INTO us_stocks_bar_1m (
	timestamp, symbol, open, high, low, close, volume, transactions,
	market_date, session_kind, is_regular_session, session_open, session_seq
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
			bar.UnderlyingClose,
			bar.ImpliedVolatility,
			bar.Delta,
			bar.Gamma,
			bar.Vega,
			bar.Theta,
			bar.Rho,
			bar.Volume,
			bar.Transactions,
			bar.MarketDate,
			bar.SessionKind,
			bar.IsRegularSession,
			bar.SessionOpen,
			bar.SessionSeq,
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
			bar.MarketDate,
			bar.SessionKind,
			bar.IsRegularSession,
			bar.SessionOpen,
			bar.SessionSeq,
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

// CountExistingOptionBars checks if option data already exists for a given market date.
func CountExistingOptionBars(ctx context.Context, conn driver.Conn, marketDate time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_options_bar_1m WHERE market_date = ?`,
		marketDate,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing option bars: %w", err)
	}
	return count, nil
}

// CountExistingStockBars checks if stock data already exists for a given market date.
func CountExistingStockBars(ctx context.Context, conn driver.Conn, marketDate time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_stocks_bar_1m WHERE market_date = ?`,
		marketDate,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing stock bars: %w", err)
	}
	return count, nil
}

// EnsureOptionGreeksColumns makes the option base table compatible with greek enrichment
// when the table already existed before these columns were introduced.
func EnsureOptionGreeksColumns(ctx context.Context, conn driver.Conn) error {
	stmts := []string{
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS underlying_close Float32 AFTER close`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS implied_volatility Float32 AFTER underlying_close`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS delta Float32 AFTER implied_volatility`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS gamma Float32 AFTER delta`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS vega Float32 AFTER gamma`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS theta Float32 AFTER vega`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS rho Float32 AFTER theta`,
	}

	for _, stmt := range stmts {
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure option greeks columns: %w", err)
		}
	}

	return nil
}

// LoadStockCloseMap loads all stock closes for a given market date.
func LoadStockCloseMap(ctx context.Context, conn driver.Conn, symbols []string, marketDate time.Time) (map[stockCloseKey]float64, map[string]struct{}, error) {
	stockCloses := make(map[stockCloseKey]float64)
	seenSymbols := make(map[string]struct{}, len(symbols))

	if len(symbols) == 0 {
		return stockCloses, seenSymbols, nil
	}

	rows, err := conn.Query(ctx,
		`SELECT symbol, timestamp, close
		FROM us_stocks_bar_1m
		WHERE symbol IN ({symbols:Array(String)})
		  AND market_date = {market_date:Date}`,
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("market_date", marketDate),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query stock closes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			symbol    string
			timestamp time.Time
			close     float32
		)
		if err := rows.Scan(&symbol, &timestamp, &close); err != nil {
			return nil, nil, fmt.Errorf("scan stock close row: %w", err)
		}
		seenSymbols[symbol] = struct{}{}
		stockCloses[newStockCloseKey(symbol, timestamp)] = float64(close)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate stock close rows: %w", err)
	}

	return stockCloses, seenSymbols, nil
}
