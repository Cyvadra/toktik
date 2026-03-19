package thetadata

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// Store wraps a ClickHouse connection for equity options data.
type Store struct {
	conn driver.Conn
}

// NewStore opens a ClickHouse connection.
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

// InitSchema runs DDL from a file and creates kline aggregate tables.
func (s *Store) InitSchema(ctx context.Context, schemaFile string) error {
	if schemaFile != "" {
		if err := cryptooptions.InitSchema(ctx, s.conn, schemaFile); err != nil {
			return err
		}
	}
	return cryptooptions.InitKlineSchemaForPrefix(ctx, s.conn, "equity_options", "equity_spot")
}

// Close closes the ClickHouse connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

// InsertEODBars inserts assembled EOD bars into equity_options_bar_1m and
// symbol metadata into equity_options_symbol_meta.
// greeks is keyed by contractKey (symbol|expiration|strike|right).
// oi is keyed by the same contractKey.
func (s *Store) InsertEODBars(ctx context.Context, root string, date time.Time,
	eodRows []EODRow, greeksMap map[string]*GreeksEODRow, oiMap map[string]int) error {

	if len(eodRows) == 0 {
		return nil
	}

	// Prepare symbol metadata batch.
	symBatch, err := s.conn.PrepareBatch(ctx,
		`INSERT INTO equity_options_symbol_meta (
			symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
		)`)
	if err != nil {
		return fmt.Errorf("prepare symbol_meta batch: %w", err)
	}

	// Prepare bar batch.
	barBatch, err := s.conn.PrepareBatch(ctx,
		`INSERT INTO equity_options_bar_1m (
			timestamp, symbol_id, base_asset,
			mark_open, mark_high, mark_low, mark_close,
			last_open, last_high, last_low, last_close,
			bid_open, bid_high, bid_low, bid_close,
			ask_open, ask_high, ask_low, ask_close,
			mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
			delta, gamma, vega, theta, rho,
			open_interest, tick_count
		)`)
	if err != nil {
		return fmt.Errorf("prepare bar batch: %w", err)
	}

	// Track underlying prices for spot bar.
	var underlyingPrice float64
	seenUnderlying := false

	for _, row := range eodRows {
		right := normalizeOptionRight(row.Right)
		if right != "call" && right != "put" {
			continue
		}

		contractKey := contractKeyStr(row.Symbol, row.Expiration, row.Strike, right)
		symbolStr := formatEquitySymbol(row.Symbol, row.Expiration, row.Strike, right)
		symID := cryptooptions.SymbolID(symbolStr)

		exp, _ := time.Parse("2006-01-02", row.Expiration)

		if err := symBatch.Append(
			symID, symbolStr, root, right,
			float32(row.Strike), exp, root,
		); err != nil {
			return fmt.Errorf("append symbol %s: %w", symbolStr, err)
		}

		// Merge Greeks if available.
		var iv, delta, gamma, vega, theta, rho float32
		if g, ok := greeksMap[contractKey]; ok {
			iv = float32(g.ImpliedVol)
			delta = float32(g.Delta)
			gamma = float32(g.Gamma)
			vega = float32(g.Vega)
			theta = float32(g.Theta)
			rho = float32(g.Rho)
			if g.UnderlyingPrice > 0 && !seenUnderlying {
				underlyingPrice = g.UnderlyingPrice
				seenUnderlying = true
			}
		}

		var oi float32
		if v, ok := oiMap[contractKey]; ok {
			oi = float32(v)
		}

		mid := float32((row.Bid + row.Ask) / 2)
		if mid == 0 && row.Close > 0 {
			mid = float32(row.Close)
		}

		if err := barBatch.Append(
			date,               // timestamp
			symID,              // symbol_id
			root,               // base_asset
			mid, mid, mid, mid, // mark OHLC (EOD: single point)
			float32(row.Open), float32(row.High), float32(row.Low), float32(row.Close), // last OHLC
			float32(row.Bid), float32(row.Bid), float32(row.Bid), float32(row.Bid), // bid OHLC
			float32(row.Ask), float32(row.Ask), float32(row.Ask), float32(row.Ask), // ask OHLC
			iv, iv, // mark_iv_open, mark_iv_close
			float32(0), float32(0), // bid_iv, ask_iv (not available in EOD)
			delta, gamma, vega, theta, rho, // greeks
			oi,                // open_interest
			uint16(row.Count), // tick_count
		); err != nil {
			return fmt.Errorf("append bar: %w", err)
		}
	}

	if err := symBatch.Send(); err != nil {
		return fmt.Errorf("send symbol batch: %w", err)
	}
	if err := barBatch.Send(); err != nil {
		return fmt.Errorf("send bar batch: %w", err)
	}

	// Insert spot bar if we have an underlying price.
	if seenUnderlying && underlyingPrice > 0 {
		spotBatch, err := s.conn.PrepareBatch(ctx,
			`INSERT INTO equity_spot_bar_1m (
				timestamp, symbol, price_source, open, high, low, close, tick_count
			)`)
		if err != nil {
			return fmt.Errorf("prepare spot batch: %w", err)
		}
		p := float32(underlyingPrice)
		if err := spotBatch.Append(date, root, "greeks_eod", p, p, p, p, uint32(1)); err != nil {
			return fmt.Errorf("append spot: %w", err)
		}
		if err := spotBatch.Send(); err != nil {
			return fmt.Errorf("send spot batch: %w", err)
		}
	}

	return nil
}

// HasDateData checks whether rows exist for a root/date in equity_options_bar_1m.
func (s *Store) HasDateData(ctx context.Context, root string, date time.Time) (bool, error) {
	var count uint64
	err := s.conn.QueryRow(ctx,
		`SELECT count() FROM equity_options_bar_1m
		 WHERE base_asset = ? AND toDate(timestamp) = ?`,
		root, date.Format("2006-01-02"),
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteDateData removes rows for a root/date from both bar tables.
func (s *Store) DeleteDateData(ctx context.Context, root string, date time.Time) error {
	d := date.Format("2006-01-02")
	err := s.conn.Exec(ctx,
		`ALTER TABLE equity_options_bar_1m DELETE WHERE base_asset = ? AND toDate(timestamp) = ? SETTINGS mutations_sync = 1`,
		root, d)
	if err != nil {
		return err
	}
	return s.conn.Exec(ctx,
		`ALTER TABLE equity_spot_bar_1m DELETE WHERE symbol = ? AND toDate(timestamp) = ? SETTINGS mutations_sync = 1`,
		root, d)
}

// contractKeyStr builds a map key for matching EOD/Greeks/OI rows by contract identity.
func contractKeyStr(symbol, expiration string, strike float64, right string) string {
	return fmt.Sprintf("%s|%s|%.3f|%s", symbol, expiration, strike, normalizeOptionRight(right))
}

// formatEquitySymbol creates a canonical symbol string for equity options.
// Format: ROOT YYMMDD C/P STRIKE  (e.g., "AAPL 241108 C 220.000")
func formatEquitySymbol(root, expiration string, strike float64, right string) string {
	t, err := time.Parse("2006-01-02", expiration)
	if err != nil {
		return fmt.Sprintf("%s_%s_%.3f_%s", root, expiration, strike, right)
	}
	r := "C"
	if right == "put" {
		r = "P"
	}
	return fmt.Sprintf("%s %s %s %.3f", root, t.Format("060102"), r, strike)
}
