package cryptooptions

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
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

// CountExistingBars returns how many sampled bars already exist in crypto_options_bar_1m.
func CountExistingBars(ctx context.Context, conn driver.Conn, bars []Bar1m) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}

	var query strings.Builder
	query.WriteString(`SELECT count() FROM crypto_options_bar_1m WHERE (toUnixTimestamp(timestamp), symbol_id, base_asset, mark_open, mark_close, last_open, last_close, open_interest, tick_count) IN (`)

	for i, bar := range bars {
		if i > 0 {
			query.WriteString(",")
		}

		query.WriteString("(")
		fmt.Fprintf(&query, "%d,%d,'%s',%s,%s,%s,%s,%s,%d",
			bar.Timestamp.UTC().Unix(),
			bar.SymbolID,
			escapeSingleQuote(bar.BaseAsset),
			float32Literal(bar.MarkOpen),
			float32Literal(bar.MarkClose),
			float32Literal(bar.LastOpen),
			float32Literal(bar.LastClose),
			float32Literal(bar.OpenInterest),
			bar.TickCount,
		)
		query.WriteString(")")
	}

	query.WriteString(")")

	rows, err := conn.Query(ctx, query.String())
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

func float32Literal(value float32) string {
	return "toFloat32(" + strconv.FormatFloat(float64(value), 'g', -1, 32) + ")"
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

const barInsertSQL = `INSERT INTO crypto_options_bar_1m (
timestamp, symbol_id, base_asset,
mark_open, mark_high, mark_low, mark_close,
last_open, last_high, last_low, last_close,
bid_open, bid_close, ask_open, ask_close,
mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
delta, gamma, vega, theta, rho,
underlying_price_open, underlying_price_close,
open_interest, tick_count
)`

// InsertBars batch-inserts 1-minute bars into crypto_options_bar_1m.
func InsertBars(ctx context.Context, conn driver.Conn, bars <-chan Bar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, barInsertSQL)
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
			bar.BidOpen, bar.BidClose, bar.AskOpen, bar.AskClose,
			bar.MarkIVOpen, bar.MarkIVClose, bar.BidIVOpen, bar.AskIVOpen,
			bar.Delta, bar.Gamma, bar.Vega, bar.Theta, bar.Rho,
			bar.UnderlyingPriceOpen, bar.UnderlyingPriceClose,
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

			batch, err = conn.PrepareBatch(ctx, barInsertSQL)
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
