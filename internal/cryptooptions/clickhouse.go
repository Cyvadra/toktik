package cryptooptions

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const dateLayout = "2006-01-02"

// InitSchema reads the DDL file and executes each statement against ClickHouse.
func InitSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
	data, err := os.ReadFile(ddlPath)
	if err != nil {
		return fmt.Errorf("read DDL file %s: %w", ddlPath, err)
	}

	statements := splitSQLStatements(string(data))
	for _, stmt := range statements {
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec DDL: %w\nStatement: %s", err, stmt)
		}
	}
	return nil
}

func splitSQLStatements(ddl string) []string {
	lines := strings.Split(ddl, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		filtered = append(filtered, line)
	}

	parts := strings.Split(strings.Join(filtered, "\n"), ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		statements = append(statements, stmt)
	}

	return statements
}

// ConnectClickHouse establishes a connection to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string) (driver.Conn, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse connection: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return conn, nil
}

// InsertSymbols batch-inserts symbol metadata into crypto_options_symbol_meta.
func InsertSymbols(ctx context.Context, conn driver.Conn, symbols []SymbolMeta) error {
	if len(symbols) == 0 {
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, `INSERT INTO crypto_options_symbol_meta (
symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
)`)
	if err != nil {
		return fmt.Errorf("prepare symbol_meta batch: %w", err)
	}

	for _, s := range symbols {
		if err := batch.Append(
			s.SymbolID,
			s.Symbol,
			s.BaseAsset,
			s.OptionType,
			s.StrikePrice,
			s.Expiration,
			s.UnderlyingIndex,
		); err != nil {
			return fmt.Errorf("append symbol %s: %w", s.Symbol, err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send symbol_meta batch: %w", err)
	}
	return nil
}

// CountExistingBars returns how many sampled option bars already exist in crypto_options_bar_1m.
func CountExistingBars(ctx context.Context, conn driver.Conn, bars []Bar1m) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}

	const tuplePlaceholder = "(?,?,?,toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),toFloat32(?),?)"

	var query strings.Builder
	query.WriteString(`SELECT count() FROM crypto_options_bar_1m WHERE (toUnixTimestamp(timestamp), symbol_id, base_asset, mark_open, mark_close, last_open, last_close, bid_open, bid_close, ask_open, ask_close, open_interest, tick_count) IN (`)

	args := make([]interface{}, 0, len(bars)*13)
	for i, bar := range bars {
		if i > 0 {
			query.WriteString(",")
		}
		query.WriteString(tuplePlaceholder)
		args = append(args,
			bar.Timestamp.UTC().Unix(),
			bar.SymbolID,
			bar.BaseAsset,
			float64(bar.MarkOpen),
			float64(bar.MarkClose),
			float64(bar.LastOpen),
			float64(bar.LastClose),
			float64(bar.BidOpen),
			float64(bar.BidClose),
			float64(bar.AskOpen),
			float64(bar.AskClose),
			float64(bar.OpenInterest),
			bar.TickCount,
		)
	}

	query.WriteString(")")

	rows, err := conn.Query(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("query existing sampled bars: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, nil
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan sampled bar count: %w", err)
	}

	return int(count), nil
}

const optionBarInsertSQL = `INSERT INTO crypto_options_bar_1m (
timestamp, symbol_id, base_asset,
mark_open, mark_high, mark_low, mark_close,
last_open, last_high, last_low, last_close,
bid_open, bid_high, bid_low, bid_close,
ask_open, ask_high, ask_low, ask_close,
mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
delta, gamma, vega, theta, rho,
open_interest, tick_count
)`

const spotBarInsertSQL = `INSERT INTO crypto_spot_bar_1m (
timestamp, symbol, price_source,
open, high, low, close,
tick_count
)`

// InsertBars batch-inserts 1-minute bars into crypto_options_bar_1m.
func InsertBars(ctx context.Context, conn driver.Conn, bars <-chan Bar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, optionBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare bar_1m batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.SymbolID,
			bar.BaseAsset,
			bar.MarkOpen, bar.MarkHigh, bar.MarkLow, bar.MarkClose,
			bar.LastOpen, bar.LastHigh, bar.LastLow, bar.LastClose,
			bar.BidOpen, bar.BidHigh, bar.BidLow, bar.BidClose,
			bar.AskOpen, bar.AskHigh, bar.AskLow, bar.AskClose,
			bar.MarkIVOpen, bar.MarkIVClose, bar.BidIVOpen, bar.AskIVOpen,
			bar.Delta, bar.Gamma, bar.Vega, bar.Theta, bar.Rho,
			bar.OpenInterest, bar.TickCount,
		); err != nil {
			return totalRows, fmt.Errorf("append bar row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send bar_1m batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, optionBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next bar_1m batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final bar_1m batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// CountExistingSpotBars returns how many sampled spot bars already exist in crypto_spot_bar_1m.
func CountExistingSpotBars(ctx context.Context, conn driver.Conn, bars []SpotBar1m) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}

	const tuplePlaceholder = "(?,?,toFloat32(?),toFloat32(?),?)"

	var query strings.Builder
	query.WriteString(`SELECT count() FROM crypto_spot_bar_1m WHERE (toUnixTimestamp(timestamp), symbol, open, close, tick_count) IN (`)

	args := make([]interface{}, 0, len(bars)*5)
	for i, bar := range bars {
		if i > 0 {
			query.WriteString(",")
		}
		query.WriteString(tuplePlaceholder)
		args = append(args,
			bar.Timestamp.UTC().Unix(),
			bar.Symbol,
			float64(bar.Open),
			float64(bar.Close),
			bar.TickCount,
		)
	}

	query.WriteString(")")

	rows, err := conn.Query(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("query existing sampled spot bars: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, nil
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan sampled spot bar count: %w", err)
	}

	return int(count), nil
}

// InsertSpotBars batch-inserts 1-minute spot bars into crypto_spot_bar_1m.
func InsertSpotBars(ctx context.Context, conn driver.Conn, bars <-chan SpotBar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, spotBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare spot_bar_1m batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.PriceSource,
			bar.Open,
			bar.High,
			bar.Low,
			bar.Close,
			bar.TickCount,
		); err != nil {
			return totalRows, fmt.Errorf("append spot bar row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send spot_bar_1m batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d spot rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, spotBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next spot_bar_1m batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final spot_bar_1m batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d spot rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}
